package retry

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/lupppig/notifyctl/internal/domain"
)

// concurrentJobStore is a mutex-safe NotificationJobStore that always returns
// the same exhausted job from GetRetryableJobs and counts status updates and
// stat increments. Used to drive two processRetries goroutines at once.
type concurrentJobStore struct {
	mu            sync.Mutex
	job           *domain.NotificationJob
	statusUpdates int
	statsCalls    int
}

func (s *concurrentJobStore) Create(ctx context.Context, job *domain.NotificationJob) error {
	return nil
}
func (s *concurrentJobStore) GetByRequestID(ctx context.Context, requestID string) (*domain.NotificationJob, error) {
	return nil, nil
}
func (s *concurrentJobStore) UpdateStatus(ctx context.Context, requestID, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statusUpdates++
	return nil
}
func (s *concurrentJobStore) FailJob(ctx context.Context, requestID string, nextRetryAt time.Time) error {
	return nil
}
func (s *concurrentJobStore) GetRetryableJobs(ctx context.Context, limit int) ([]*domain.NotificationJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Each poll sees the same still-FAILED job (no SKIP LOCKED in the real
	// query, so concurrent pollers can both observe it).
	return []*domain.NotificationJob{s.job}, nil
}
func (s *concurrentJobStore) List(ctx context.Context, serviceID string) ([]*domain.NotificationJob, error) {
	return nil, nil
}
func (s *concurrentJobStore) IncrementStats(ctx context.Context, serviceID, status string, t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statsCalls++
	return nil
}
func (s *concurrentJobStore) GetStats(ctx context.Context, serviceID string) (map[string]int64, error) {
	return nil, nil
}
func (s *concurrentJobStore) ResetForReplay(ctx context.Context, job *domain.NotificationJob) error {
	return nil
}

// dedupeDeadLetterStore is a mutex-safe DeadLetterStore that emulates the real
// table's UNIQUE(notification_id) + ON CONFLICT DO NOTHING: a second Create for
// the same notification_id is a silent no-op. This is what guarantees
// exactly-one dead letter under concurrency even though the scheduler has no
// SKIP LOCKED.
type dedupeDeadLetterStore struct {
	mu      sync.Mutex
	byNID   map[string]*domain.DeadLetter
	creates int
}

func newDedupeDeadLetterStore() *dedupeDeadLetterStore {
	return &dedupeDeadLetterStore{byNID: make(map[string]*domain.DeadLetter)}
}

func (s *dedupeDeadLetterStore) Create(ctx context.Context, dl *domain.DeadLetter) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.creates++
	if _, exists := s.byNID[dl.NotificationID]; exists {
		return nil // ON CONFLICT DO NOTHING
	}
	s.byNID[dl.NotificationID] = dl
	return nil
}
func (s *dedupeDeadLetterStore) List(ctx context.Context, serviceID string) ([]*domain.DeadLetter, error) {
	return nil, nil
}
func (s *dedupeDeadLetterStore) GetByID(ctx context.Context, id string) (*domain.DeadLetter, error) {
	return nil, nil
}
func (s *dedupeDeadLetterStore) DeleteByNotificationID(ctx context.Context, notificationID string) error {
	return nil
}
func (s *dedupeDeadLetterStore) rowCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.byNID)
}

// TestProcessRetriesConcurrentSameJob drives two concurrent processRetries over
// the same exhausted job. It documents race finding F1: the unique-index
// emulation guarantees exactly ONE dead-letter row, but because GetRetryableJobs
// has no FOR UPDATE SKIP LOCKED and the stat increment is unconditional, the
// FAILED stat (and DEAD_LETTERED status write) MAY be applied more than once.
// We assert the durable invariant (one DL row) strictly and the stat/status
// counts loosely (1..2), and run clean under `go test -race`.
func TestProcessRetriesConcurrentSameJob(t *testing.T) {
	job := exhaustedJob("req-1", "svc-1", 5)
	jobStore := &concurrentJobStore{job: job}
	dlq := newDedupeDeadLetterStore()

	s := NewScheduler(DefaultConfig()).
		WithStore(jobStore).
		WithDeadLetterStore(dlq)

	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			s.processRetries(context.Background())
		}()
	}
	wg.Wait()

	// Durable invariant: exactly one dead letter despite concurrent processing.
	if got := dlq.rowCount(); got != 1 {
		t.Fatalf("expected exactly 1 dead-letter row under concurrency, got %d", got)
	}

	// F1 (known): without SKIP LOCKED, status/stat writes can double-apply.
	if jobStore.statusUpdates < 1 || jobStore.statusUpdates > 2 {
		t.Errorf("expected 1..2 status updates (F1 over-count tolerated), got %d", jobStore.statusUpdates)
	}
	if jobStore.statsCalls < 1 || jobStore.statsCalls > 2 {
		t.Errorf("expected 1..2 stats increments (F1 over-count tolerated), got %d", jobStore.statsCalls)
	}
}

// TestProcessRetriesStatsExactlyOnce_WantFix pins the desired post-fix behavior:
// concurrent processing should record the FAILED stat exactly once. Skipped
// until GetRetryableJobs claims rows (FOR UPDATE SKIP LOCKED) or the stat
// increment is gated on a real FAILED->DEAD_LETTERED transition.
func TestProcessRetriesStatsExactlyOnce_WantFix(t *testing.T) {
	t.Skip("want-fix: scheduler should not double-count FAILED stats under concurrency (needs SKIP LOCKED or conditional update)")
}
