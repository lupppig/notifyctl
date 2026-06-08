//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	notifyv1 "github.com/lupppig/notifyctl/pkg/grpc/notify/v1"
)

// --- Core lifecycle ---

func TestE2EDeadLetterLifecycle(t *testing.T) {
	h := newHarness(t)
	svcID, key := h.registerService("e2e-lifecycle")

	nid := h.send(key, svcID, `{"event":"order.created","id":"42"}`)

	// The simulated worker delivers; wait for the normal happy path first so
	// we exercise the real state machine, then inject failure.
	h.waitFor("initial delivery", func() bool { return h.jobStatus(nid) == "DELIVERED" })

	h.exhaustRetries(nid)
	dlID := h.waitForDeadLetter(nid)

	// ListDeadLetters over real gRPC returns it with intact fields.
	resp, err := h.client.ListDeadLetters(authCtx(key), &notifyv1.ListDeadLettersRequest{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(resp.DeadLetters) != 1 {
		t.Fatalf("expected 1 dead letter, got %d", len(resp.DeadLetters))
	}
	dl := resp.DeadLetters[0]
	if dl.Id != dlID || dl.NotificationId != nid || dl.ServiceId != svcID {
		t.Errorf("dead letter fields mismatch: %+v", dl)
	}
	if dl.AttemptCount != 10 {
		t.Errorf("expected attempt_count 10, got %d", dl.AttemptCount)
	}
	if _, err := time.Parse(time.RFC3339, dl.FailedAt); err != nil {
		t.Errorf("failed_at not RFC3339: %q", dl.FailedAt)
	}
}

// TestE2EDeadLetterExactlyOnceAcrossPolls is the regression test for the
// infinite re-processing bug: an exhausted job must be dead-lettered once,
// not once per poll.
func TestE2EDeadLetterExactlyOnceAcrossPolls(t *testing.T) {
	h := newHarness(t)
	svcID, key := h.registerService("e2e-once")

	nid := h.send(key, svcID, `{"event":"x"}`)
	h.waitFor("delivery", func() bool { return h.jobStatus(nid) == "DELIVERED" })
	h.exhaustRetries(nid)
	h.waitForDeadLetter(nid)

	// Let the scheduler poll many more times.
	time.Sleep(10 * pollInterval)

	if n := h.deadLetterCount(nid); n != 1 {
		t.Errorf("expected exactly 1 dead letter after many polls, got %d", n)
	}
	// FAILED stat incremented exactly once.
	var failed int64
	err := h.db.Pool.QueryRow(context.Background(),
		`SELECT coalesce(sum(count),0) FROM notification_stats WHERE service_id=$1 AND status='FAILED'`,
		svcID).Scan(&failed)
	if err != nil {
		t.Fatalf("stats query: %v", err)
	}
	if failed != 1 {
		t.Errorf("expected FAILED stat == 1 (no inflation), got %d", failed)
	}
}

// --- Replay ---

func TestE2EReplayReentersDispatcherAndRedelivers(t *testing.T) {
	h := newHarness(t)
	svcID, key := h.registerService("e2e-replay")

	nid := h.send(key, svcID, `{"event":"replay.me"}`)
	h.waitFor("delivery", func() bool { return h.jobStatus(nid) == "DELIVERED" })
	h.exhaustRetries(nid)
	dlID := h.waitForDeadLetter(nid)

	resp, err := h.client.ReplayDeadLetter(authCtx(key), &notifyv1.ReplayDeadLetterRequest{Id: dlID})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if resp.NotificationId != nid {
		t.Errorf("expected original notification id %s, got %s", nid, resp.NotificationId)
	}

	// The job must travel the real pipeline again: worker picks it up off
	// NATS and re-delivers.
	h.waitFor("re-delivery after replay", func() bool { return h.jobStatus(nid) == "DELIVERED" })

	if n := h.deadLetterCount(nid); n != 0 {
		t.Errorf("dead letter must be removed after replay, got %d", n)
	}
	// Retry budget reset.
	var retryCount int
	if err := h.db.Pool.QueryRow(context.Background(),
		`SELECT retry_count FROM notification_jobs WHERE request_id=$1`, nid).Scan(&retryCount); err != nil {
		t.Fatalf("retry_count: %v", err)
	}
	if retryCount != 0 {
		t.Errorf("expected retry_count reset to 0, got %d", retryCount)
	}
}

// TestE2ERefailAfterReplayDeadLettersAgain covers the full cycle:
// dead-letter -> replay -> fail again -> dead-letter again (idempotency keyed
// by notification_id must not block the SECOND dead-lettering, because the
// first record was deleted on replay).
func TestE2ERefailAfterReplayDeadLettersAgain(t *testing.T) {
	h := newHarness(t)
	svcID, key := h.registerService("e2e-refail")

	nid := h.send(key, svcID, `{"event":"refail"}`)
	h.waitFor("delivery", func() bool { return h.jobStatus(nid) == "DELIVERED" })

	h.exhaustRetries(nid)
	dl1 := h.waitForDeadLetter(nid)

	if _, err := h.client.ReplayDeadLetter(authCtx(key), &notifyv1.ReplayDeadLetterRequest{Id: dl1}); err != nil {
		t.Fatalf("first replay: %v", err)
	}
	h.waitFor("re-delivery", func() bool { return h.jobStatus(nid) == "DELIVERED" })

	// Fail it again.
	h.exhaustRetries(nid)
	dl2 := h.waitForDeadLetter(nid)
	if dl2 == dl1 {
		t.Errorf("second dead-lettering must create a fresh record, got same id %s", dl1)
	}

	// And replay again.
	if _, err := h.client.ReplayDeadLetter(authCtx(key), &notifyv1.ReplayDeadLetterRequest{Id: dl2}); err != nil {
		t.Fatalf("second replay: %v", err)
	}
	h.waitFor("re-delivery 2", func() bool { return h.jobStatus(nid) == "DELIVERED" })
}

// --- Race conditions ---

// TestE2EConcurrentReplaySameDeadLetter hammers one dead letter with parallel
// replay RPCs. Exactly-once delivery of the *record* is not required (the
// job reset is an upsert) but invariants are: >=1 success, every failure is
// NotFound, the dead letter ends deleted, the job ends back in the pipeline,
// and no constraint violations or panics occur.
func TestE2EConcurrentReplaySameDeadLetter(t *testing.T) {
	h := newHarness(t)
	svcID, key := h.registerService("e2e-replay-race")

	nid := h.send(key, svcID, `{"event":"race"}`)
	h.waitFor("delivery", func() bool { return h.jobStatus(nid) == "DELIVERED" })
	h.exhaustRetries(nid)
	dlID := h.waitForDeadLetter(nid)

	const racers = 12
	var wg sync.WaitGroup
	var successes, notFounds, other int64
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := h.client.ReplayDeadLetter(authCtx(key), &notifyv1.ReplayDeadLetterRequest{Id: dlID})
			switch {
			case err == nil:
				atomic.AddInt64(&successes, 1)
			case status.Code(err) == codes.NotFound:
				atomic.AddInt64(&notFounds, 1)
			default:
				atomic.AddInt64(&other, 1)
				t.Errorf("unexpected replay error: %v", err)
			}
		}()
	}
	wg.Wait()

	if successes < 1 {
		t.Fatalf("expected at least one successful replay, got successes=%d notFounds=%d", successes, notFounds)
	}
	if other != 0 {
		t.Fatalf("unexpected non-NotFound failures: %d", other)
	}
	if n := h.deadLetterCount(nid); n != 0 {
		t.Errorf("dead letter must be gone after concurrent replays, got %d", n)
	}
	h.waitFor("final delivery", func() bool { return h.jobStatus(nid) == "DELIVERED" })
}

// TestE2EConcurrentExhaustionManyJobs simulates a mass-failure burst: many
// jobs exhausted at once; every one must be dead-lettered exactly once even
// while the scheduler polls concurrently with the worker's activity.
func TestE2EConcurrentExhaustionManyJobs(t *testing.T) {
	h := newHarness(t)
	svcID, key := h.registerService("e2e-burst")

	const jobs = 25
	ids := make([]string, jobs)
	for i := range ids {
		ids[i] = h.send(key, svcID, fmt.Sprintf(`{"event":"burst","i":%d}`, i))
	}
	for _, id := range ids {
		h.waitFor("delivery "+id, func() bool { return h.jobStatus(id) == "DELIVERED" })
	}

	// Exhaust all concurrently to land inside one or two poll windows.
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			h.exhaustRetries(id)
		}(id)
	}
	wg.Wait()

	for _, id := range ids {
		h.waitForDeadLetter(id)
	}
	// Stability check across further polls.
	time.Sleep(5 * pollInterval)
	for _, id := range ids {
		if n := h.deadLetterCount(id); n != 1 {
			t.Errorf("job %s: expected 1 dead letter, got %d", id, n)
		}
	}

	resp, err := h.client.ListDeadLetters(authCtx(key), &notifyv1.ListDeadLettersRequest{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(resp.DeadLetters) != jobs {
		t.Errorf("expected %d dead letters listed, got %d", jobs, len(resp.DeadLetters))
	}
}

// TestE2EReplayWhileSchedulerPolls replays a dead letter at the same moment
// the scheduler may be scanning FAILED jobs — the replayed job must not be
// re-dead-lettered by a stale scheduler pass (it leaves FAILED status before
// the replay publishes).
func TestE2EReplayWhileSchedulerPolls(t *testing.T) {
	h := newHarness(t)
	svcID, key := h.registerService("e2e-replay-vs-sched")

	for i := 0; i < 5; i++ {
		nid := h.send(key, svcID, `{"event":"interleave"}`)
		h.waitFor("delivery", func() bool { return h.jobStatus(nid) == "DELIVERED" })
		h.exhaustRetries(nid)
		dlID := h.waitForDeadLetter(nid)

		// Replay immediately — the scheduler is polling every 200ms.
		if _, err := h.client.ReplayDeadLetter(authCtx(key), &notifyv1.ReplayDeadLetterRequest{Id: dlID}); err != nil {
			t.Fatalf("replay %d: %v", i, err)
		}
		h.waitFor("re-delivery", func() bool { return h.jobStatus(nid) == "DELIVERED" })
		// Give the scheduler extra polls; the job must remain DELIVERED with
		// no resurrected dead letter.
		time.Sleep(3 * pollInterval)
		if got := h.jobStatus(nid); got != "DELIVERED" {
			t.Errorf("iteration %d: job re-entered %s after replay", i, got)
		}
		if n := h.deadLetterCount(nid); n != 0 {
			t.Errorf("iteration %d: dead letter resurrected (%d records)", i, n)
		}
	}
}

// --- Auth and tenancy ---

func TestE2EAuthMatrix(t *testing.T) {
	h := newHarness(t)
	svcID, key := h.registerService("e2e-auth-a")
	_, otherKey := h.registerService("e2e-auth-b")

	nid := h.send(key, svcID, `{"event":"auth"}`)
	h.waitFor("delivery", func() bool { return h.jobStatus(nid) == "DELIVERED" })
	h.exhaustRetries(nid)
	dlID := h.waitForDeadLetter(nid)

	t.Run("list without key is Unauthenticated", func(t *testing.T) {
		_, err := h.client.ListDeadLetters(context.Background(), &notifyv1.ListDeadLettersRequest{})
		if status.Code(err) != codes.Unauthenticated {
			t.Errorf("got %v", err)
		}
	})
	t.Run("list with bogus key is Unauthenticated", func(t *testing.T) {
		_, err := h.client.ListDeadLetters(authCtx("nc_bogus"), &notifyv1.ListDeadLettersRequest{})
		if status.Code(err) != codes.Unauthenticated {
			t.Errorf("got %v", err)
		}
	})
	t.Run("foreign tenant sees empty list", func(t *testing.T) {
		resp, err := h.client.ListDeadLetters(authCtx(otherKey), &notifyv1.ListDeadLettersRequest{})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(resp.DeadLetters) != 0 {
			t.Errorf("tenant isolation broken: foreign service sees %d dead letters", len(resp.DeadLetters))
		}
	})
	t.Run("foreign explicit service_id is PermissionDenied", func(t *testing.T) {
		_, err := h.client.ListDeadLetters(authCtx(otherKey), &notifyv1.ListDeadLettersRequest{ServiceId: svcID})
		if status.Code(err) != codes.PermissionDenied {
			t.Errorf("got %v", err)
		}
	})
	t.Run("foreign replay is PermissionDenied", func(t *testing.T) {
		_, err := h.client.ReplayDeadLetter(authCtx(otherKey), &notifyv1.ReplayDeadLetterRequest{Id: dlID})
		if status.Code(err) != codes.PermissionDenied {
			t.Errorf("got %v", err)
		}
		// And the dead letter must be untouched.
		if n := h.deadLetterCount(nid); n != 1 {
			t.Errorf("foreign replay mutated state: %d dead letters", n)
		}
	})
	t.Run("replay without key is Unauthenticated", func(t *testing.T) {
		_, err := h.client.ReplayDeadLetter(context.Background(), &notifyv1.ReplayDeadLetterRequest{Id: dlID})
		if status.Code(err) != codes.Unauthenticated {
			t.Errorf("got %v", err)
		}
	})
	t.Run("replay nonexistent id is NotFound", func(t *testing.T) {
		_, err := h.client.ReplayDeadLetter(authCtx(key), &notifyv1.ReplayDeadLetterRequest{Id: "no-such-id"})
		if status.Code(err) != codes.NotFound {
			t.Errorf("got %v", err)
		}
	})
	t.Run("replay empty id is InvalidArgument", func(t *testing.T) {
		_, err := h.client.ReplayDeadLetter(authCtx(key), &notifyv1.ReplayDeadLetterRequest{Id: ""})
		if status.Code(err) != codes.InvalidArgument {
			t.Errorf("got %v", err)
		}
	})
}

// --- Persistence edge cases ---

// TestE2EOnConflictIdempotency drives the crash-between-insert-and-update
// window directly: a dead letter exists but the job is still FAILED (as if
// the process died mid-sequence). The next polls must converge without
// duplicating the dead letter.
func TestE2EOnConflictIdempotency(t *testing.T) {
	h := newHarness(t)
	svcID, key := h.registerService("e2e-conflict")

	nid := h.send(key, svcID, `{"event":"conflict"}`)
	h.waitFor("delivery", func() bool { return h.jobStatus(nid) == "DELIVERED" })
	h.exhaustRetries(nid)
	h.waitForDeadLetter(nid)

	// Simulate the crash window: dead letter present, job knocked back to
	// FAILED (status update "lost").
	if _, err := h.db.Pool.Exec(context.Background(),
		`UPDATE notification_jobs SET status='FAILED', next_retry_at=NOW() WHERE request_id=$1`, nid); err != nil {
		t.Fatalf("simulate crash window: %v", err)
	}

	// Scheduler must finish the sequence: ON CONFLICT absorbs the re-insert,
	// status converges to DEAD_LETTERED, and only one record exists.
	h.waitFor("convergence", func() bool { return h.jobStatus(nid) == "DEAD_LETTERED" })
	if n := h.deadLetterCount(nid); n != 1 {
		t.Errorf("expected exactly 1 dead letter after crash-window replay, got %d", n)
	}
}

func TestE2EMigrationIdempotent(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	// newHarness already ran Migrate once on an existing schema; run it
	// several more times — the constraint DO-block and unique index must not
	// error.
	for i := 0; i < 3; i++ {
		if err := h.db.Migrate(ctx); err != nil {
			t.Fatalf("migrate round %d: %v", i, err)
		}
	}
	// The constraint must accept DEAD_LETTERED and reject unknown statuses.
	var conDef string
	if err := h.db.Pool.QueryRow(ctx,
		`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname='notification_jobs_status_check'`).
		Scan(&conDef); err != nil {
		t.Fatalf("constraint lookup: %v", err)
	}
	if !strings.Contains(conDef, "DEAD_LETTERED") {
		t.Errorf("constraint missing DEAD_LETTERED: %s", conDef)
	}
}

func TestE2EPayloadEdgeCases(t *testing.T) {
	h := newHarness(t)
	svcID, key := h.registerService("e2e-payload")

	cases := []struct {
		name    string
		payload string
	}{
		{"empty object", `{}`},
		{"unicode", `{"event":"emoji","msg":"héllo wörld 🚀 null-escaped"}`},
		{"nested", `{"event":"deep","a":{"b":{"c":[1,2,{"d":"e"}]}}}`},
		{"large", `{"event":"big","blob":"` + strings.Repeat("x", 64*1024) + `"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nid := h.send(key, svcID, tc.payload)
			h.waitFor("delivery", func() bool { return h.jobStatus(nid) == "DELIVERED" })
			h.exhaustRetries(nid)
			dlID := h.waitForDeadLetter(nid)

			// Payload must survive the dead-letter round trip and replay.
			var stored []byte
			if err := h.db.Pool.QueryRow(context.Background(),
				`SELECT payload FROM dead_letters WHERE id=$1`, dlID).Scan(&stored); err != nil {
				t.Fatalf("payload fetch: %v", err)
			}
			if len(stored) == 0 {
				t.Fatal("stored payload empty")
			}

			if _, err := h.client.ReplayDeadLetter(authCtx(key), &notifyv1.ReplayDeadLetterRequest{Id: dlID}); err != nil {
				t.Fatalf("replay: %v", err)
			}
			h.waitFor("re-delivery", func() bool { return h.jobStatus(nid) == "DELIVERED" })
		})
	}
}

// TestE2EListIsolationUnderLoad lists dead letters for one tenant while
// another tenant's jobs are being dead-lettered concurrently.
func TestE2EListIsolationUnderLoad(t *testing.T) {
	h := newHarness(t)
	svcA, keyA := h.registerService("e2e-iso-a")
	svcB, keyB := h.registerService("e2e-iso-b")

	nidA := h.send(keyA, svcA, `{"event":"a"}`)
	h.waitFor("delivery A", func() bool { return h.jobStatus(nidA) == "DELIVERED" })
	h.exhaustRetries(nidA)
	h.waitForDeadLetter(nidA)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { // tenant B churns jobs into the DLQ
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			nid := h.send(keyB, svcB, fmt.Sprintf(`{"event":"b","i":%d}`, i))
			h.waitFor("delivery B", func() bool { return h.jobStatus(nid) == "DELIVERED" })
			h.exhaustRetries(nid)
			h.waitForDeadLetter(nid)
		}
	}()

	for i := 0; i < 10; i++ {
		resp, err := h.client.ListDeadLetters(authCtx(keyA), &notifyv1.ListDeadLettersRequest{})
		if err != nil {
			t.Fatalf("list under load: %v", err)
		}
		for _, dl := range resp.DeadLetters {
			if dl.ServiceId != svcA {
				t.Fatalf("tenant isolation broken under load: saw %s's dead letter", dl.ServiceId)
			}
		}
		if len(resp.DeadLetters) != 1 {
			t.Errorf("expected 1 dead letter for tenant A, got %d", len(resp.DeadLetters))
		}
		time.Sleep(pollInterval / 2)
	}
	close(stop)
	wg.Wait()
}
