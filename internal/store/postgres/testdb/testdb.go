// Package testdb provides helpers for tests that need a real Postgres database.
//
// It is import-safe from any test (it adds no build constraints), but every
// helper that touches a database calls t.Skip when DATABASE_URL is unset or the
// database is unreachable, so suites that don't run integration/e2e stay green
// with zero infrastructure.
//
// Tables are TRUNCATE'd between tests via Truncate / t.Cleanup so cases don't
// leak state into each other.
package testdb

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/lupppig/notifyctl/internal/domain"
	"github.com/lupppig/notifyctl/internal/store/postgres"
)

// allTables is truncated (RESTART IDENTITY CASCADE) to reset state. Order is
// irrelevant under CASCADE but listed child-first for clarity.
const truncateSQL = `TRUNCATE dead_letters, notification_stats, delivery_attempts, notification_jobs, notifications, services RESTART IDENTITY CASCADE`

// Open connects to DATABASE_URL, runs migrations, and returns the DB. If
// DATABASE_URL is unset or the database can't be pinged, the test is skipped.
// The pool is closed automatically via t.Cleanup.
func Open(t *testing.T) *postgres.DB {
	t.Helper()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Retry the initial connect a few times so a cold/just-started database
	// (common in CI right after `docker compose up`) doesn't cause a spurious
	// failure or skip.
	var (
		db  *postgres.DB
		err error
	)
	for attempt := 0; attempt < 5; attempt++ {
		db, err = postgres.New(ctx, dsn)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Skipf("DATABASE_URL set but database unreachable (%v); skipping", err)
		case <-time.After(time.Duration(attempt+1) * 500 * time.Millisecond):
		}
	}
	if err != nil {
		t.Skipf("DATABASE_URL set but database unreachable (%v); skipping", err)
	}
	t.Cleanup(db.Close)

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// Truncate clears all tables. Call at the top of each test (and/or register via
// t.Cleanup) so tests are isolated.
func Truncate(t *testing.T, db *postgres.DB) {
	t.Helper()
	if _, err := db.Pool.Exec(context.Background(), truncateSQL); err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

// SeedService inserts a minimal services row so foreign keys on
// notification_jobs / dead_letters / notification_stats are satisfied. The
// apiKeyHash is optional; pass "" to auto-generate a unique placeholder.
func SeedService(t *testing.T, db *postgres.DB, id, apiKeyHash string) {
	t.Helper()
	if apiKeyHash == "" {
		apiKeyHash = "hash-" + id
	}
	_, err := db.Pool.Exec(context.Background(),
		`INSERT INTO services (id, name, webhook_url, secret, api_key)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (id) DO NOTHING`,
		id, "svc-"+id, "http://example.test/hook", "secret-"+id, apiKeyHash,
	)
	if err != nil {
		t.Fatalf("seed service %q: %v", id, err)
	}
}

// InsertDeadLetter writes a dead_letters row directly (bypassing the store) so
// tests can set up arbitrary scenarios, including controlled failed_at values
// and NULL service_id/last_error.
func InsertDeadLetter(t *testing.T, db *postgres.DB, dl *domain.DeadLetter) {
	t.Helper()
	var serviceID *string
	if dl.ServiceID != "" {
		serviceID = &dl.ServiceID
	}
	var lastErr *string
	if dl.LastError != "" {
		lastErr = &dl.LastError
	}
	_, err := db.Pool.Exec(context.Background(),
		`INSERT INTO dead_letters (id, notification_id, service_id, payload, last_error, attempt_count, failed_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		dl.ID, dl.NotificationID, serviceID, dl.Payload, lastErr, dl.AttemptCount, dl.FailedAt,
	)
	if err != nil {
		t.Fatalf("insert dead letter %q: %v", dl.ID, err)
	}
}

// InsertJob writes a notification_jobs row directly. payload must be valid JSON
// (the column is JSONB).
func InsertJob(t *testing.T, db *postgres.DB, job *domain.NotificationJob) {
	t.Helper()
	_, err := db.Pool.Exec(context.Background(),
		`INSERT INTO notification_jobs (request_id, service_id, payload, status, retry_count, next_retry_at, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		job.RequestID, job.ServiceID, job.Payload, job.Status, job.RetryCount,
		job.NextRetryAt, job.CreatedAt, job.UpdatedAt,
	)
	if err != nil {
		t.Fatalf("insert job %q: %v", job.RequestID, err)
	}
}

// CountDeadLetters returns the number of dead_letters rows (optionally scoped to
// a notification_id when nid != "").
func CountDeadLetters(t *testing.T, db *postgres.DB, nid string) int {
	t.Helper()
	var n int
	var err error
	if nid == "" {
		err = db.Pool.QueryRow(context.Background(), `SELECT count(*) FROM dead_letters`).Scan(&n)
	} else {
		err = db.Pool.QueryRow(context.Background(), `SELECT count(*) FROM dead_letters WHERE notification_id = $1`, nid).Scan(&n)
	}
	if err != nil {
		t.Fatalf("count dead letters: %v", err)
	}
	return n
}

// JobStatus returns the status of a notification_jobs row.
func JobStatus(t *testing.T, db *postgres.DB, requestID string) string {
	t.Helper()
	var s string
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT status FROM notification_jobs WHERE request_id = $1`, requestID).Scan(&s); err != nil {
		t.Fatalf("job status %q: %v", requestID, err)
	}
	return s
}

// StatCount returns the summed notification_stats count for a (service,status).
func StatCount(t *testing.T, db *postgres.DB, serviceID, status string) int64 {
	t.Helper()
	var n int64
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(count),0) FROM notification_stats WHERE service_id=$1 AND status=$2`,
		serviceID, status).Scan(&n); err != nil {
		t.Fatalf("stat count: %v", err)
	}
	return n
}
