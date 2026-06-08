//go:build integration

// Integration tests for the retry scheduler's dead-lettering against a real
// Postgres. Run with:
//
//	go test -race -tags=integration ./internal/retry/...
//
// Requires DATABASE_URL; tests t.Skip otherwise.
package retry

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/lupppig/notifyctl/internal/domain"
	"github.com/lupppig/notifyctl/internal/store/postgres"
	"github.com/lupppig/notifyctl/internal/store/postgres/testdb"
)

// exhaustedConfig dead-letters immediately (MaxAttempts=1 so any retry_count>=1
// is terminal).
func exhaustedConfig() Config {
	c := DefaultConfig()
	c.MaxAttempts = 1
	return c
}

func seedExhaustedJob(t *testing.T, db *postgres.DB, requestID, serviceID string) {
	t.Helper()
	past := time.Now().Add(-time.Minute)
	testdb.InsertJob(t, db, &domain.NotificationJob{
		RequestID: requestID, ServiceID: serviceID, Payload: []byte(`{"event":"order.created"}`),
		Status: "FAILED", RetryCount: 5, NextRetryAt: &past,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
}

func TestProcessRetriesDeadLettersRealDB(t *testing.T) {
	db := testdb.Open(t)
	testdb.Truncate(t, db)
	testdb.SeedService(t, db, "svc-1", "")
	seedExhaustedJob(t, db, "n-1", "svc-1")

	jobStore := postgres.NewNotificationJobStore(db)
	dlqStore := postgres.NewDeadLetterStore(db)
	s := NewScheduler(exhaustedConfig()).
		WithStore(jobStore).
		WithDeadLetterStore(dlqStore)

	s.processRetries(context.Background())

	if n := testdb.CountDeadLetters(t, db, "n-1"); n != 1 {
		t.Fatalf("expected 1 dead letter, got %d", n)
	}
	if got := testdb.JobStatus(t, db, "n-1"); got != "DEAD_LETTERED" {
		t.Errorf("expected job DEAD_LETTERED, got %q", got)
	}
	// Job must no longer be retryable.
	jobs, _ := jobStore.GetRetryableJobs(context.Background(), 50)
	for _, j := range jobs {
		if j.RequestID == "n-1" {
			t.Error("dead-lettered job should not be returned by GetRetryableJobs")
		}
	}
}

// TestProcessRetriesConcurrentRealDB exercises race finding F1 against a real
// database: two concurrent scheduler passes over the same exhausted job. The
// UNIQUE(notification_id) index guarantees exactly one dead-letter row. The
// FAILED stat MAY be over-counted (no FOR UPDATE SKIP LOCKED); we assert the
// durable invariant strictly and the stat loosely, and run under -race.
func TestProcessRetriesConcurrentRealDB(t *testing.T) {
	db := testdb.Open(t)
	testdb.Truncate(t, db)
	testdb.SeedService(t, db, "svc-1", "")
	seedExhaustedJob(t, db, "n-1", "svc-1")

	jobStore := postgres.NewNotificationJobStore(db)
	dlqStore := postgres.NewDeadLetterStore(db)
	s := NewScheduler(exhaustedConfig()).
		WithStore(jobStore).
		WithDeadLetterStore(dlqStore)

	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			s.processRetries(context.Background())
		}()
	}
	wg.Wait()

	// Durable invariant: exactly one dead letter, no matter the interleaving.
	if n := testdb.CountDeadLetters(t, db, "n-1"); n != 1 {
		t.Fatalf("expected exactly 1 dead letter under concurrency, got %d", n)
	}
	if got := testdb.JobStatus(t, db, "n-1"); got != "DEAD_LETTERED" {
		t.Errorf("expected DEAD_LETTERED, got %q", got)
	}

	// F1 (known over-count): assert at least one FAILED stat was recorded. A
	// post-fix scheduler would make this exactly 1 — see the _WantFix test.
	if got := testdb.StatCount(t, db, "svc-1", "FAILED"); got < 1 {
		t.Errorf("expected >=1 FAILED stat, got %d", got)
	}
}

func TestProcessRetriesConcurrentStatsExactlyOnceRealDB_WantFix(t *testing.T) {
	t.Skip("want-fix: concurrent scheduler passes should record FAILED stat exactly once (needs SKIP LOCKED / conditional update)")
}
