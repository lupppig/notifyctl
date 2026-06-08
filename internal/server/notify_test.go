package server

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/lupppig/notifyctl/internal/domain"
	"github.com/lupppig/notifyctl/internal/store"
	notifyv1 "github.com/lupppig/notifyctl/pkg/grpc/notify/v1"
)

type fakeDeadLetterStore struct {
	mu          sync.Mutex
	letters     []*domain.DeadLetter
	listErr     error
	getErr      error
	deleteErr   error
	gotService  string
	deletedNIDs []string
}

func (f *fakeDeadLetterStore) Create(ctx context.Context, dl *domain.DeadLetter) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.letters = append(f.letters, dl)
	return nil
}

func (f *fakeDeadLetterStore) List(ctx context.Context, serviceID string) ([]*domain.DeadLetter, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gotService = serviceID
	if f.listErr != nil {
		return nil, f.listErr
	}
	return append([]*domain.DeadLetter(nil), f.letters...), nil
}

func (f *fakeDeadLetterStore) GetByID(ctx context.Context, id string) (*domain.DeadLetter, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return nil, f.getErr
	}
	for _, dl := range f.letters {
		if dl.ID == id {
			return dl, nil
		}
	}
	return nil, store.ErrNotFound
}

func (f *fakeDeadLetterStore) DeleteByNotificationID(ctx context.Context, notificationID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deletedNIDs = append(f.deletedNIDs, notificationID)
	kept := f.letters[:0]
	for _, dl := range f.letters {
		if dl.NotificationID != notificationID {
			kept = append(kept, dl)
		}
	}
	f.letters = kept
	return nil
}

func (f *fakeDeadLetterStore) deleted() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.deletedNIDs...)
}

// fakeJobStore implements store.NotificationJobStore; only the methods used by
// the replay handler do anything, the rest satisfy the interface.
type fakeJobStore struct {
	mu         sync.Mutex
	resetJobs  []*domain.NotificationJob
	resetErr   error
	statsErr   error
	statsCalls int
}

func (f *fakeJobStore) Create(ctx context.Context, job *domain.NotificationJob) error { return nil }
func (f *fakeJobStore) GetByRequestID(ctx context.Context, requestID string) (*domain.NotificationJob, error) {
	return nil, store.ErrNotFound
}
func (f *fakeJobStore) UpdateStatus(ctx context.Context, requestID, status string) error { return nil }
func (f *fakeJobStore) FailJob(ctx context.Context, requestID string, nextRetryAt time.Time) error {
	return nil
}
func (f *fakeJobStore) GetRetryableJobs(ctx context.Context, limit int) ([]*domain.NotificationJob, error) {
	return nil, nil
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
	f.statsCalls++
	return nil
}
func (f *fakeJobStore) GetStats(ctx context.Context, serviceID string) (map[string]int64, error) {
	return nil, nil
}
func (f *fakeJobStore) ResetForReplay(ctx context.Context, job *domain.NotificationJob) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.resetErr != nil {
		return f.resetErr
	}
	f.resetJobs = append(f.resetJobs, job)
	return nil
}

func (f *fakeJobStore) resets() []*domain.NotificationJob {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*domain.NotificationJob(nil), f.resetJobs...)
}

// fakePublisher captures NATS publishes for assertions.
type fakePublisher struct {
	mu       sync.Mutex
	subjects []string
	datas    [][]byte
	pubErr   error
}

func (p *fakePublisher) Publish(subj string, data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pubErr != nil {
		return p.pubErr
	}
	p.subjects = append(p.subjects, subj)
	p.datas = append(p.datas, data)
	return nil
}

func (p *fakePublisher) published() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.subjects...)
}

func authedCtx(serviceID string) context.Context {
	return context.WithValue(context.Background(), "service", &domain.Service{ID: serviceID, Name: "svc"})
}

func TestListDeadLettersScopedToAuthenticatedService(t *testing.T) {
	dlq := &fakeDeadLetterStore{
		letters: []*domain.DeadLetter{
			{
				ID:             "dl-1",
				NotificationID: "n-1",
				ServiceID:      "svc-1",
				LastError:      "max retries exceeded",
				AttemptCount:   5,
				FailedAt:       time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC),
			},
		},
	}
	s := &NotifyServer{deadLetterStore: dlq}

	resp, err := s.ListDeadLetters(authedCtx("svc-1"), &notifyv1.ListDeadLettersRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dlq.gotService != "svc-1" {
		t.Errorf("expected store query scoped to svc-1, got %q", dlq.gotService)
	}
	if len(resp.DeadLetters) != 1 {
		t.Fatalf("expected 1 dead letter, got %d", len(resp.DeadLetters))
	}
	dl := resp.DeadLetters[0]
	if dl.Id != "dl-1" || dl.NotificationId != "n-1" || dl.AttemptCount != 5 {
		t.Errorf("unexpected dead letter: %+v", dl)
	}
	if dl.FailedAt != "2026-06-08T10:00:00Z" {
		t.Errorf("expected RFC3339 failed_at, got %q", dl.FailedAt)
	}
}

func TestListDeadLettersIgnoresRequestedForeignService(t *testing.T) {
	dlq := &fakeDeadLetterStore{}
	s := &NotifyServer{deadLetterStore: dlq}

	_, err := s.ListDeadLetters(authedCtx("svc-1"), &notifyv1.ListDeadLettersRequest{ServiceId: "svc-2"})
	if status.Code(err) != codes.PermissionDenied {
		t.Errorf("expected PermissionDenied for foreign service_id, got %v", err)
	}
}

func TestListDeadLettersMatchingServiceIDAllowed(t *testing.T) {
	dlq := &fakeDeadLetterStore{}
	s := &NotifyServer{deadLetterStore: dlq}

	_, err := s.ListDeadLetters(authedCtx("svc-1"), &notifyv1.ListDeadLettersRequest{ServiceId: "svc-1"})
	if err != nil {
		t.Errorf("expected no error for own service_id, got %v", err)
	}
}

func TestListDeadLettersMissingIdentity(t *testing.T) {
	s := &NotifyServer{deadLetterStore: &fakeDeadLetterStore{}}

	_, err := s.ListDeadLetters(context.Background(), &notifyv1.ListDeadLettersRequest{})
	if status.Code(err) != codes.Internal {
		t.Errorf("expected Internal when identity missing, got %v", err)
	}
}

func TestListDeadLettersStoreError(t *testing.T) {
	dlq := &fakeDeadLetterStore{listErr: errors.New("db down")}
	s := &NotifyServer{deadLetterStore: dlq}

	_, err := s.ListDeadLetters(authedCtx("svc-1"), &notifyv1.ListDeadLettersRequest{})
	if status.Code(err) != codes.Internal {
		t.Errorf("expected Internal on store error, got %v", err)
	}
}

func newReplayServer(dlq *fakeDeadLetterStore, jobs *fakeJobStore, pub *fakePublisher) *NotifyServer {
	return &NotifyServer{deadLetterStore: dlq, jobStore: jobs, nc: pub}
}

func TestReplayDeadLetterReenqueues(t *testing.T) {
	dlq := &fakeDeadLetterStore{
		letters: []*domain.DeadLetter{
			{ID: "dl-1", NotificationID: "n-1", ServiceID: "svc-1", Payload: []byte(`{"event":"x"}`), AttemptCount: 5},
		},
	}
	jobs := &fakeJobStore{}
	pub := &fakePublisher{}
	s := newReplayServer(dlq, jobs, pub)

	resp, err := s.ReplayDeadLetter(authedCtx("svc-1"), &notifyv1.ReplayDeadLetterRequest{Id: "dl-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.NotificationId != "n-1" {
		t.Errorf("expected re-enqueued notification id n-1, got %q", resp.NotificationId)
	}

	// Job reset back into the pipeline under the original ID, PENDING, fresh budget.
	if len(jobs.resetJobs) != 1 {
		t.Fatalf("expected 1 ResetForReplay call, got %d", len(jobs.resetJobs))
	}
	rj := jobs.resetJobs[0]
	if rj.RequestID != "n-1" || rj.ServiceID != "svc-1" || rj.Status != "PENDING" || rj.RetryCount != 0 {
		t.Errorf("unexpected reset job: %+v", rj)
	}

	// Dead letter removed so a re-failure can re-dead-letter cleanly.
	if len(dlq.deletedNIDs) != 1 || dlq.deletedNIDs[0] != "n-1" {
		t.Errorf("expected dead letter for n-1 to be deleted, got %v", dlq.deletedNIDs)
	}

	// Published to the dispatcher subject.
	if len(pub.subjects) != 1 || pub.subjects[0] != "notifications.jobs" {
		t.Errorf("expected publish to notifications.jobs, got %v", pub.subjects)
	}
}

func TestReplayDeadLetterNotFound(t *testing.T) {
	s := newReplayServer(&fakeDeadLetterStore{}, &fakeJobStore{}, &fakePublisher{})

	_, err := s.ReplayDeadLetter(authedCtx("svc-1"), &notifyv1.ReplayDeadLetterRequest{Id: "missing"})
	if status.Code(err) != codes.NotFound {
		t.Errorf("expected NotFound, got %v", err)
	}
}

func TestReplayDeadLetterForeignServiceDenied(t *testing.T) {
	dlq := &fakeDeadLetterStore{
		letters: []*domain.DeadLetter{
			{ID: "dl-1", NotificationID: "n-1", ServiceID: "svc-2", Payload: []byte(`{}`)},
		},
	}
	jobs := &fakeJobStore{}
	pub := &fakePublisher{}
	s := newReplayServer(dlq, jobs, pub)

	_, err := s.ReplayDeadLetter(authedCtx("svc-1"), &notifyv1.ReplayDeadLetterRequest{Id: "dl-1"})
	if status.Code(err) != codes.PermissionDenied {
		t.Errorf("expected PermissionDenied, got %v", err)
	}
	// Nothing should have been mutated or published.
	if len(jobs.resetJobs) != 0 || len(pub.subjects) != 0 || len(dlq.deletedNIDs) != 0 {
		t.Error("foreign replay must not reset/delete/publish")
	}
}

func TestReplayDeadLetterMissingID(t *testing.T) {
	s := newReplayServer(&fakeDeadLetterStore{}, &fakeJobStore{}, &fakePublisher{})

	_, err := s.ReplayDeadLetter(authedCtx("svc-1"), &notifyv1.ReplayDeadLetterRequest{Id: ""})
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument for empty id, got %v", err)
	}
}

// Failure-ordering edge cases for each replay step live in
// notify_dlq_errorpaths_test.go; below are payload edge cases and races.

func TestReplayDeadLetterEmptyPayload(t *testing.T) {
	// Nil/empty payload must round-trip without error.
	dlq := &fakeDeadLetterStore{
		letters: []*domain.DeadLetter{{ID: "dl-1", NotificationID: "n-1", ServiceID: "svc-1", Payload: nil}},
	}
	jobs := &fakeJobStore{}
	pub := &fakePublisher{}
	s := newReplayServer(dlq, jobs, pub)

	resp, err := s.ReplayDeadLetter(authedCtx("svc-1"), &notifyv1.ReplayDeadLetterRequest{Id: "dl-1"})
	if err != nil {
		t.Fatalf("unexpected error for empty payload: %v", err)
	}
	if resp.NotificationId != "n-1" {
		t.Errorf("unexpected notification id: %q", resp.NotificationId)
	}
}

// --- Race conditions ---

func TestConcurrentReplaySameDeadLetter(t *testing.T) {
	// N clients replay the same dead letter simultaneously (double-click,
	// retried RPC, two operators). All goroutines must be race-free; at
	// least one replay must succeed; the job reset is an upsert and the
	// delete idempotent, so duplicate successes are acceptable — corruption
	// or panic is not.
	const racers = 16
	dlq := &fakeDeadLetterStore{
		letters: []*domain.DeadLetter{{ID: "dl-1", NotificationID: "n-1", ServiceID: "svc-1", Payload: []byte(`{}`)}},
	}
	jobs := &fakeJobStore{}
	pub := &fakePublisher{}
	s := newReplayServer(dlq, jobs, pub)

	var wg sync.WaitGroup
	var successes int64
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.ReplayDeadLetter(authedCtx("svc-1"), &notifyv1.ReplayDeadLetterRequest{Id: "dl-1"})
			if err == nil {
				atomic.AddInt64(&successes, 1)
			} else if status.Code(err) != codes.NotFound {
				t.Errorf("unexpected error code: %v", err)
			}
		}()
	}
	wg.Wait()

	if successes == 0 {
		t.Fatal("expected at least one successful replay")
	}
	// Every success implies a reset+publish pair; they must match.
	if int64(len(jobs.resets())) != successes || int64(len(pub.published())) != successes {
		t.Errorf("reset/publish/success counts diverge: resets=%d publishes=%d successes=%d",
			len(jobs.resets()), len(pub.published()), successes)
	}
}

func TestConcurrentListAndReplay(t *testing.T) {
	// Listing while replaying must be race-free and never observe corrupt
	// state (the fake store guards with a mutex like the DB guards with MVCC).
	dlq := &fakeDeadLetterStore{}
	for i := 0; i < 20; i++ {
		dlq.letters = append(dlq.letters, &domain.DeadLetter{
			ID: fmt.Sprintf("dl-%d", i), NotificationID: fmt.Sprintf("n-%d", i), ServiceID: "svc-1",
		})
	}
	jobs := &fakeJobStore{}
	pub := &fakePublisher{}
	s := newReplayServer(dlq, jobs, pub)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, _ = s.ReplayDeadLetter(authedCtx("svc-1"), &notifyv1.ReplayDeadLetterRequest{Id: fmt.Sprintf("dl-%d", n)})
		}(i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := s.ListDeadLetters(authedCtx("svc-1"), &notifyv1.ListDeadLettersRequest{})
			if err != nil {
				t.Errorf("list during replay failed: %v", err)
				return
			}
			for _, dl := range resp.DeadLetters {
				if dl.Id == "" || dl.NotificationId == "" {
					t.Error("observed corrupt dead letter during concurrent replay")
				}
			}
		}()
	}
	wg.Wait()

	if got := len(dlq.deleted()); got != 20 {
		t.Errorf("expected all 20 dead letters replayed, got %d", got)
	}
}
