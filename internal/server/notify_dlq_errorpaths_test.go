package server

import (
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/lupppig/notifyctl/internal/domain"
	notifyv1 "github.com/lupppig/notifyctl/pkg/grpc/notify/v1"
)

// Failure-ordering edge cases for the replay sequence:
// GetByID -> ownership check -> ResetForReplay -> Publish -> Delete.
// Each step's failure must leave the system replayable: the dead letter
// survives unless the job has fully re-entered the pipeline.

func dlStore(dls ...*domain.DeadLetter) *fakeDeadLetterStore {
	return &fakeDeadLetterStore{letters: dls}
}

func TestReplayDeadLetterMissingIdentity(t *testing.T) {
	s := newReplayServer(dlStore(), &fakeJobStore{}, &fakePublisher{})

	_, err := s.ReplayDeadLetter(t.Context(), &notifyv1.ReplayDeadLetterRequest{Id: "dl-1"})
	if status.Code(err) != codes.Internal {
		t.Errorf("expected Internal when identity missing, got %v", err)
	}
}

func TestReplayDeadLetterGetByIDInternalError(t *testing.T) {
	dlq := dlStore()
	dlq.getErr = errors.New("db down") // non-NotFound error
	jobs := &fakeJobStore{}
	pub := &fakePublisher{}
	s := newReplayServer(dlq, jobs, pub)

	_, err := s.ReplayDeadLetter(authedCtx("svc-1"), &notifyv1.ReplayDeadLetterRequest{Id: "dl-1"})
	if status.Code(err) != codes.Internal {
		t.Errorf("expected Internal on store error, got %v", err)
	}
	if len(jobs.resets()) != 0 || len(pub.published()) != 0 || len(dlq.deleted()) != 0 {
		t.Error("a failed lookup must not reset/publish/delete")
	}
}

func TestReplayDeadLetterResetError(t *testing.T) {
	dlq := dlStore(&domain.DeadLetter{ID: "dl-1", NotificationID: "n-1", ServiceID: "svc-1", Payload: []byte(`{}`)})
	jobs := &fakeJobStore{resetErr: errors.New("reset failed")}
	pub := &fakePublisher{}
	s := newReplayServer(dlq, jobs, pub)

	_, err := s.ReplayDeadLetter(authedCtx("svc-1"), &notifyv1.ReplayDeadLetterRequest{Id: "dl-1"})
	if status.Code(err) != codes.Internal {
		t.Errorf("expected Internal when reset fails, got %v", err)
	}
	// Reset failed before publish/delete: nothing should have escaped, and
	// the dead letter must remain so the replay can be retried.
	if len(pub.published()) != 0 {
		t.Error("must not publish when reset fails")
	}
	if len(dlq.deleted()) != 0 {
		t.Error("must not delete the dead letter when reset fails")
	}
}

func TestReplayDeadLetterPublishError(t *testing.T) {
	dlq := dlStore(&domain.DeadLetter{ID: "dl-1", NotificationID: "n-1", ServiceID: "svc-1", Payload: []byte(`{}`)})
	jobs := &fakeJobStore{}
	pub := &fakePublisher{pubErr: errors.New("nats down")}
	s := newReplayServer(dlq, jobs, pub)

	_, err := s.ReplayDeadLetter(authedCtx("svc-1"), &notifyv1.ReplayDeadLetterRequest{Id: "dl-1"})
	if status.Code(err) != codes.Internal {
		t.Errorf("expected Internal when publish fails, got %v", err)
	}
	// Job reset happens before publish (documents ordering); the dead-letter
	// delete runs AFTER a successful publish, so on publish failure the dead
	// letter survives and the replay is retryable.
	if len(jobs.resets()) != 1 {
		t.Errorf("expected reset to have occurred before publish, got %d", len(jobs.resets()))
	}
	if len(dlq.deleted()) != 0 {
		t.Error("dead letter must not be deleted when publish fails")
	}
}

func TestReplayDeadLetterDeleteError(t *testing.T) {
	// Delete fails after the job already re-entered the pipeline: the caller
	// gets Internal, the publish has happened, and a second replay is safe
	// (reset is an upsert; the stale dead letter is removed on the retry).
	dlq := dlStore(&domain.DeadLetter{ID: "dl-1", NotificationID: "n-1", ServiceID: "svc-1", Payload: []byte(`{}`)})
	dlq.deleteErr = errors.New("delete failed")
	jobs := &fakeJobStore{}
	pub := &fakePublisher{}
	s := newReplayServer(dlq, jobs, pub)

	_, err := s.ReplayDeadLetter(authedCtx("svc-1"), &notifyv1.ReplayDeadLetterRequest{Id: "dl-1"})
	if status.Code(err) != codes.Internal {
		t.Errorf("expected Internal when delete fails, got %v", err)
	}
	if len(pub.published()) != 1 {
		t.Errorf("expected publish to have happened before delete, got %d", len(pub.published()))
	}
}

func TestReplayDeadLetterStatsErrorStillSucceeds(t *testing.T) {
	// IncrementStats is best-effort; a stats error must NOT fail the replay.
	dlq := dlStore(&domain.DeadLetter{ID: "dl-1", NotificationID: "n-1", ServiceID: "svc-1", Payload: []byte(`{"event":"x"}`)})
	jobs := &fakeJobStore{statsErr: errors.New("stats down")}
	pub := &fakePublisher{}
	s := newReplayServer(dlq, jobs, pub)

	resp, err := s.ReplayDeadLetter(authedCtx("svc-1"), &notifyv1.ReplayDeadLetterRequest{Id: "dl-1"})
	if err != nil {
		t.Fatalf("stats failure must not fail replay, got %v", err)
	}
	if resp.NotificationId != "n-1" {
		t.Errorf("expected n-1, got %q", resp.NotificationId)
	}
}

func TestListDeadLettersEmptyReturnsEmptySlice(t *testing.T) {
	dlq := dlStore() // no letters
	s := &NotifyServer{deadLetterStore: dlq}

	resp, err := s.ListDeadLetters(authedCtx("svc-1"), &notifyv1.ListDeadLettersRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.DeadLetters) != 0 {
		t.Errorf("expected empty dead-letter list, got %d", len(resp.DeadLetters))
	}
}
