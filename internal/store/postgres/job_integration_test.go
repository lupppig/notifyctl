//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/lupppig/notifyctl/internal/domain"
	"github.com/lupppig/notifyctl/internal/store/postgres"
	"github.com/lupppig/notifyctl/internal/store/postgres/testdb"
)

func newJobStore(t *testing.T) (*postgres.NotificationJobStore, *postgres.DB) {
	db := testdb.Open(t)
	testdb.Truncate(t, db)
	testdb.SeedService(t, db, "svc-1", "")
	return postgres.NewNotificationJobStore(db), db
}

func TestResetForReplayInsertsWhenAbsent(t *testing.T) {
	s, db := newJobStore(t)
	ctx := context.Background()

	job := &domain.NotificationJob{
		RequestID: "n-1", ServiceID: "svc-1",
		Payload: []byte(`{"event":"x"}`), Status: "PENDING",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := s.ResetForReplay(ctx, job); err != nil {
		t.Fatalf("reset (insert path): %v", err)
	}
	if got := testdb.JobStatus(t, db, "n-1"); got != "PENDING" {
		t.Errorf("expected inserted job PENDING, got %q", got)
	}
}

func TestResetForReplayUpsertsWhenPresent(t *testing.T) {
	s, db := newJobStore(t)
	ctx := context.Background()

	// Seed an exhausted FAILED job.
	retryAt := time.Now().Add(time.Hour)
	testdb.InsertJob(t, db, &domain.NotificationJob{
		RequestID: "n-1", ServiceID: "svc-1", Payload: []byte(`{"event":"x"}`),
		Status: "FAILED", RetryCount: 5, NextRetryAt: &retryAt,
		CreatedAt: time.Now().Add(-time.Hour), UpdatedAt: time.Now().Add(-time.Hour),
	})

	if err := s.ResetForReplay(ctx, &domain.NotificationJob{
		RequestID: "n-1", ServiceID: "svc-1", Payload: []byte(`{"event":"x"}`), Status: "PENDING",
	}); err != nil {
		t.Fatalf("reset (upsert path): %v", err)
	}

	// Verify the row was reset: status PENDING, retry_count 0, next_retry_at NULL.
	var status string
	var retryCount int
	var nextRetry *time.Time
	err := db.Pool.QueryRow(ctx,
		`SELECT status, retry_count, next_retry_at FROM notification_jobs WHERE request_id=$1`, "n-1").
		Scan(&status, &retryCount, &nextRetry)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if status != "PENDING" || retryCount != 0 || nextRetry != nil {
		t.Errorf("expected reset row (PENDING,0,NULL), got (%s,%d,%v)", status, retryCount, nextRetry)
	}
}

func TestDeadLetteredStatusAcceptedByCheckConstraint(t *testing.T) {
	s, _ := newJobStore(t)
	ctx := context.Background()
	// Insert an ACCEPTED job, then transition to DEAD_LETTERED (added by the
	// constraint migration). Must not violate the CHECK constraint.
	if err := s.Create(ctx, &domain.NotificationJob{
		RequestID: "n-1", ServiceID: "svc-1", Payload: []byte(`{}`), Status: "ACCEPTED",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.UpdateStatus(ctx, "n-1", "DEAD_LETTERED"); err != nil {
		t.Errorf("DEAD_LETTERED must be allowed by the migrated CHECK constraint, got: %v", err)
	}
}

func TestBogusStatusRejectedByCheckConstraint(t *testing.T) {
	s, _ := newJobStore(t)
	ctx := context.Background()
	if err := s.Create(ctx, &domain.NotificationJob{
		RequestID: "n-1", ServiceID: "svc-1", Payload: []byte(`{}`), Status: "ACCEPTED",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.UpdateStatus(ctx, "n-1", "BOGUS_STATUS"); err == nil {
		t.Error("expected CHECK constraint to reject an unknown status")
	}
}

func TestGetRetryableJobsSelectsOnlyDueFailed(t *testing.T) {
	s, db := newJobStore(t)
	ctx := context.Background()
	past := time.Now().Add(-time.Minute)
	future := time.Now().Add(time.Hour)

	testdb.InsertJob(t, db, &domain.NotificationJob{RequestID: "due", ServiceID: "svc-1", Payload: []byte(`{}`), Status: "FAILED", RetryCount: 1, NextRetryAt: &past, CreatedAt: time.Now(), UpdatedAt: time.Now()})
	testdb.InsertJob(t, db, &domain.NotificationJob{RequestID: "future", ServiceID: "svc-1", Payload: []byte(`{}`), Status: "FAILED", RetryCount: 1, NextRetryAt: &future, CreatedAt: time.Now(), UpdatedAt: time.Now()})
	testdb.InsertJob(t, db, &domain.NotificationJob{RequestID: "pending", ServiceID: "svc-1", Payload: []byte(`{}`), Status: "PENDING", RetryCount: 0, NextRetryAt: &past, CreatedAt: time.Now(), UpdatedAt: time.Now()})

	jobs, err := s.GetRetryableJobs(ctx, 50)
	if err != nil {
		t.Fatalf("get retryable: %v", err)
	}
	if len(jobs) != 1 || jobs[0].RequestID != "due" {
		t.Errorf("expected only the due FAILED job, got %+v", jobs)
	}
}

func TestMigrateIdempotent(t *testing.T) {
	db := testdb.Open(t)
	// Open already migrated once; a second Migrate (which DROPs/ADDs the status
	// constraint) must not error.
	if err := db.Migrate(context.Background()); err != nil {
		t.Errorf("second Migrate should be idempotent, got: %v", err)
	}
}
