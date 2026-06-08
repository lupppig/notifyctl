package retry

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lupppig/notifyctl/internal/domain"
)

// fakeJobStore implements store.NotificationJobStore, recording calls.
// All methods are safe for concurrent use so tests can run the scheduler
// from multiple goroutines under -race.
type fakeJobStore struct {
	mu            sync.Mutex
	retryableJobs []*domain.NotificationJob
	drainOnFetch  bool // if set, GetRetryableJobs hands out the batch once

	getErr        error
	updateErr     error
	statsErr      error
	statusUpdates map[string][]string // requestID -> statuses set
	statsCalls    map[string][]string // serviceID -> statuses incremented
}

func newFakeJobStore(jobs ...*domain.NotificationJob) *fakeJobStore {
	return &fakeJobStore{
		retryableJobs: jobs,
		statusUpdates: make(map[string][]string),
		statsCalls:    make(map[string][]string),
	}
}

func (f *fakeJobStore) Create(ctx context.Context, job *domain.NotificationJob) error { return nil }

func (f *fakeJobStore) GetByRequestID(ctx context.Context, requestID string) (*domain.NotificationJob, error) {
	return nil, nil
}

func (f *fakeJobStore) UpdateStatus(ctx context.Context, requestID string, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.updateErr != nil {
		return f.updateErr
	}
	f.statusUpdates[requestID] = append(f.statusUpdates[requestID], status)
	return nil
}

func (f *fakeJobStore) FailJob(ctx context.Context, requestID string, nextRetryAt time.Time) error {
	return nil
}

func (f *fakeJobStore) GetRetryableJobs(ctx context.Context, limit int) ([]*domain.NotificationJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return nil, f.getErr
	}
	jobs := append([]*domain.NotificationJob(nil), f.retryableJobs...)
	if f.drainOnFetch {
		f.retryableJobs = nil
	}
	return jobs, nil
}

func (f *fakeJobStore) List(ctx context.Context, serviceID string) ([]*domain.NotificationJob, error) {
	return nil, nil
}

func (f *fakeJobStore) IncrementStats(ctx context.Context, serviceID, status string, t time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.statsErr != nil {
		return f.statsErr
	}
	f.statsCalls[serviceID] = append(f.statsCalls[serviceID], status)
	return nil
}

func (f *fakeJobStore) GetStats(ctx context.Context, serviceID string) (map[string]int64, error) {
	return nil, nil
}

func (f *fakeJobStore) ResetForReplay(ctx context.Context, job *domain.NotificationJob) error {
	return nil
}

func (f *fakeJobStore) updatesFor(requestID string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.statusUpdates[requestID]...)
}

func (f *fakeJobStore) statsFor(serviceID string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.statsCalls[serviceID]...)
}

// fakeDeadLetterStore implements store.DeadLetterStore, recording creates.
type fakeDeadLetterStore struct {
	mu        sync.Mutex
	created   []*domain.DeadLetter
	createErr error
}

func (f *fakeDeadLetterStore) Create(ctx context.Context, dl *domain.DeadLetter) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return f.createErr
	}
	f.created = append(f.created, dl)
	return nil
}

func (f *fakeDeadLetterStore) List(ctx context.Context, serviceID string) ([]*domain.DeadLetter, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*domain.DeadLetter(nil), f.created...), nil
}

func (f *fakeDeadLetterStore) GetByID(ctx context.Context, id string) (*domain.DeadLetter, error) {
	return nil, nil
}

func (f *fakeDeadLetterStore) DeleteByNotificationID(ctx context.Context, notificationID string) error {
	return nil
}

func (f *fakeDeadLetterStore) all() []*domain.DeadLetter {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*domain.DeadLetter(nil), f.created...)
}

func exhaustedJob(requestID, serviceID string, retryCount int) *domain.NotificationJob {
	return &domain.NotificationJob{
		RequestID:  requestID,
		ServiceID:  serviceID,
		Payload:    json.RawMessage(`{"event":"order.created"}`),
		Status:     "FAILED",
		RetryCount: retryCount,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
}

func TestProcessRetriesDeadLettersExhaustedJob(t *testing.T) {
	jobStore := newFakeJobStore(exhaustedJob("req-1", "svc-1", 5))
	dlq := &fakeDeadLetterStore{}

	s := NewScheduler(DefaultConfig()).
		WithStore(jobStore).
		WithDeadLetterStore(dlq)

	s.processRetries(context.Background())

	created := dlq.all()
	if len(created) != 1 {
		t.Fatalf("expected 1 dead letter, got %d", len(created))
	}
	dl := created[0]
	if dl.NotificationID != "req-1" || dl.ServiceID != "svc-1" || dl.AttemptCount != 5 {
		t.Errorf("dead letter fields mismatch: %+v", dl)
	}
	if string(dl.Payload) != `{"event":"order.created"}` {
		t.Errorf("payload not preserved: %s", dl.Payload)
	}
	if dl.ID == "" {
		t.Error("expected dead letter ID to be set")
	}

	if got := jobStore.updatesFor("req-1"); len(got) != 1 || got[0] != "DEAD_LETTERED" {
		t.Errorf("expected status update to DEAD_LETTERED, got %v", got)
	}
	if got := jobStore.statsFor("svc-1"); len(got) != 1 || got[0] != "FAILED" {
		t.Errorf("expected one FAILED stats increment, got %v", got)
	}
}

func TestProcessRetriesDLQInsertFailureKeepsJobFailed(t *testing.T) {
	jobStore := newFakeJobStore(exhaustedJob("req-1", "svc-1", 5))
	dlq := &fakeDeadLetterStore{createErr: errors.New("db down")}

	s := NewScheduler(DefaultConfig()).
		WithStore(jobStore).
		WithDeadLetterStore(dlq)

	s.processRetries(context.Background())

	if got := jobStore.updatesFor("req-1"); len(got) != 0 {
		t.Errorf("expected no status update when DLQ insert fails, got %v", got)
	}
	if got := jobStore.statsFor("svc-1"); len(got) != 0 {
		t.Errorf("expected no stats increment when DLQ insert fails, got %v", got)
	}
}

func TestProcessRetriesDeadLettersWithoutDLQStore(t *testing.T) {
	jobStore := newFakeJobStore(exhaustedJob("req-1", "svc-1", 5))

	s := NewScheduler(DefaultConfig()).WithStore(jobStore)

	s.processRetries(context.Background())

	// The terminal status fix must not depend on DLQ wiring.
	if got := jobStore.updatesFor("req-1"); len(got) != 1 || got[0] != "DEAD_LETTERED" {
		t.Errorf("expected status update to DEAD_LETTERED without DLQ store, got %v", got)
	}
}

func TestProcessRetriesDeadLettersOnlyOnce(t *testing.T) {
	// After dead-lettering, the job leaves FAILED status, so the next poll
	// must not see it again. Simulate two polls: second returns no jobs.
	jobStore := newFakeJobStore(exhaustedJob("req-1", "svc-1", 5))
	dlq := &fakeDeadLetterStore{}

	s := NewScheduler(DefaultConfig()).
		WithStore(jobStore).
		WithDeadLetterStore(dlq)

	s.processRetries(context.Background())
	jobStore.mu.Lock()
	jobStore.retryableJobs = nil // job no longer FAILED
	jobStore.mu.Unlock()
	s.processRetries(context.Background())

	if got := dlq.all(); len(got) != 1 {
		t.Errorf("expected exactly 1 dead letter across polls, got %d", len(got))
	}
	if got := jobStore.statsFor("svc-1"); len(got) != 1 {
		t.Errorf("expected exactly 1 stats increment across polls, got %v", got)
	}
}

func TestProcessRetriesUpdateStatusFailureSkipsStats(t *testing.T) {
	// If the DEAD_LETTERED status flip fails, stats must NOT be incremented:
	// the job remains FAILED and will be re-processed (and re-counted) next
	// poll, so counting now would double-count.
	jobStore := newFakeJobStore(exhaustedJob("req-1", "svc-1", 5))
	jobStore.updateErr = errors.New("db down")
	dlq := &fakeDeadLetterStore{}

	s := NewScheduler(DefaultConfig()).
		WithStore(jobStore).
		WithDeadLetterStore(dlq)

	s.processRetries(context.Background())

	if got := jobStore.statsFor("svc-1"); len(got) != 0 {
		t.Errorf("expected no stats increment when status update fails, got %v", got)
	}
	// The DLQ insert is idempotent, so it having happened is fine.
	if got := dlq.all(); len(got) != 1 {
		t.Errorf("expected DLQ insert before failed status update, got %d", len(got))
	}
}

func TestProcessRetriesStatsFailureTolerated(t *testing.T) {
	// A stats failure must not undo or block dead-lettering.
	jobStore := newFakeJobStore(exhaustedJob("req-1", "svc-1", 5))
	jobStore.statsErr = errors.New("stats table locked")
	dlq := &fakeDeadLetterStore{}

	s := NewScheduler(DefaultConfig()).
		WithStore(jobStore).
		WithDeadLetterStore(dlq)

	s.processRetries(context.Background())

	if got := dlq.all(); len(got) != 1 {
		t.Errorf("expected dead letter despite stats failure, got %d", len(got))
	}
	if got := jobStore.updatesFor("req-1"); len(got) != 1 || got[0] != "DEAD_LETTERED" {
		t.Errorf("expected DEAD_LETTERED despite stats failure, got %v", got)
	}
}

func TestProcessRetriesFetchErrorGraceful(t *testing.T) {
	// A failing GetRetryableJobs must not panic or produce side effects.
	jobStore := newFakeJobStore()
	jobStore.getErr = errors.New("db down")

	s := NewScheduler(DefaultConfig()).
		WithStore(jobStore).
		WithDeadLetterStore(&fakeDeadLetterStore{})

	s.processRetries(context.Background()) // must not panic
}

func TestProcessRetriesBatchOfExhaustedJobs(t *testing.T) {
	// A whole batch of exhausted jobs (mass-failure scenario) must each be
	// dead-lettered exactly once, independently.
	jobs := []*domain.NotificationJob{
		exhaustedJob("req-1", "svc-1", 5),
		exhaustedJob("req-2", "svc-1", 7),
		exhaustedJob("req-3", "svc-2", 6),
	}
	jobStore := newFakeJobStore(jobs...)
	dlq := &fakeDeadLetterStore{}

	s := NewScheduler(DefaultConfig()).
		WithStore(jobStore).
		WithDeadLetterStore(dlq)

	s.processRetries(context.Background())

	created := dlq.all()
	if len(created) != 3 {
		t.Fatalf("expected 3 dead letters, got %d", len(created))
	}
	seen := map[string]bool{}
	for _, dl := range created {
		seen[dl.NotificationID] = true
	}
	for _, id := range []string{"req-1", "req-2", "req-3"} {
		if !seen[id] {
			t.Errorf("missing dead letter for %s", id)
		}
		if got := jobStore.updatesFor(id); len(got) != 1 || got[0] != "DEAD_LETTERED" {
			t.Errorf("job %s: expected single DEAD_LETTERED update, got %v", id, got)
		}
	}
	if got := jobStore.statsFor("svc-1"); len(got) != 2 {
		t.Errorf("expected 2 FAILED stats for svc-1, got %v", got)
	}
	if got := jobStore.statsFor("svc-2"); len(got) != 1 {
		t.Errorf("expected 1 FAILED stat for svc-2, got %v", got)
	}
}

func TestProcessRetriesBoundaryRetryCount(t *testing.T) {
	// retry_count == MaxAttempts is exhausted; MaxAttempts-1 is not.
	cfg := DefaultConfig() // MaxAttempts = 5
	jobStore := newFakeJobStore(exhaustedJob("req-exact", "svc-1", cfg.MaxAttempts))
	dlq := &fakeDeadLetterStore{}

	s := NewScheduler(cfg).WithStore(jobStore).WithDeadLetterStore(dlq)
	s.processRetries(context.Background())

	if got := dlq.all(); len(got) != 1 {
		t.Errorf("retry_count == MaxAttempts must dead-letter, got %d dead letters", len(got))
	}
}

func TestConcurrentSchedulersDeadLetterIdempotently(t *testing.T) {
	// Two scheduler instances polling the same store (multi-replica
	// deployment) race on the same exhausted job. The fake store, like the
	// real ON CONFLICT-protected store, absorbs duplicate inserts at the
	// uniqueness layer; this test asserts the scheduler itself does not
	// corrupt state or panic under -race, and that the duplicate volume is
	// bounded by the number of racers (not amplified).
	const racers = 8
	job := exhaustedJob("req-race", "svc-1", 5)
	jobStore := newFakeJobStore(job)
	jobStore.drainOnFetch = true // first fetch wins the batch, like SKIP LOCKED would
	dlq := &fakeDeadLetterStore{}

	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := NewScheduler(DefaultConfig()).
				WithStore(jobStore).
				WithDeadLetterStore(dlq)
			s.processRetries(context.Background())
		}()
	}
	wg.Wait()

	// drainOnFetch hands the job to exactly one racer.
	if got := dlq.all(); len(got) != 1 {
		t.Errorf("expected exactly 1 dead letter with drained fetch, got %d", len(got))
	}
	if got := jobStore.updatesFor("req-race"); len(got) != 1 {
		t.Errorf("expected exactly 1 status update, got %v", got)
	}
}

func TestConcurrentSchedulersWithoutDrainBoundedDuplicates(t *testing.T) {
	// Without SKIP LOCKED semantics (current production reality), every
	// racer sees the same FAILED batch. The DB's ON CONFLICT makes the
	// duplicate inserts harmless; here we assert each racer performs at most
	// one insert attempt per job and nothing panics under -race.
	const racers = 8
	jobStore := newFakeJobStore(exhaustedJob("req-race", "svc-1", 5))
	dlq := &fakeDeadLetterStore{}

	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := NewScheduler(DefaultConfig()).
				WithStore(jobStore).
				WithDeadLetterStore(dlq)
			s.processRetries(context.Background())
		}()
	}
	wg.Wait()

	if got := dlq.all(); len(got) > racers {
		t.Errorf("dead-letter inserts amplified beyond racer count: %d > %d", len(got), racers)
	}
}

func TestSchedulerStartLogicOnlyModeReturns(t *testing.T) {
	// Without a NATS connection, Start must return immediately (logic-only
	// mode) instead of polling with a nil publisher.
	s := NewScheduler(DefaultConfig()).WithStore(newFakeJobStore())

	done := make(chan struct{})
	go func() {
		s.Start(context.Background())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return in logic-only mode")
	}
}
