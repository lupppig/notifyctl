package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DB struct {
	Pool *pgxpool.Pool
}

func New(ctx context.Context, connString string) (*DB, error) {
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{Pool: pool}, nil
}

func (db *DB) Close() {
	db.Pool.Close()
}

func (db *DB) Migrate(ctx context.Context) error {
	schema := `
		CREATE TABLE IF NOT EXISTS services (
			id          TEXT PRIMARY KEY,
			name        TEXT UNIQUE NOT NULL,
			webhook_url TEXT NOT NULL,
			secret      TEXT NOT NULL,
			api_key     TEXT UNIQUE NOT NULL,
			created_at  TIMESTAMPTZ DEFAULT NOW(),
			deleted_at  TIMESTAMPTZ
		);

		CREATE TABLE IF NOT EXISTS notifications (
			id           TEXT PRIMARY KEY,
			service_id   TEXT REFERENCES services(id),
			topic        TEXT NOT NULL,
			payload      BYTEA NOT NULL,
			destinations JSONB NOT NULL,
			created_at   TIMESTAMPTZ DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS delivery_attempts (
			id              TEXT PRIMARY KEY,
			notification_id TEXT REFERENCES notifications(id),
			destination     TEXT NOT NULL,
			status          TEXT NOT NULL,
			status_code     INT,
			response_body   TEXT,
			error           TEXT,
			attempted_at    TIMESTAMPTZ DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS notification_jobs (
			request_id    TEXT PRIMARY KEY,
			service_id    TEXT REFERENCES services(id),
			payload       JSONB NOT NULL,
			status        TEXT NOT NULL CHECK (status IN ('ACCEPTED', 'PENDING', 'DISPATCHED', 'DELIVERED', 'FAILED', 'DEAD_LETTERED')),
			retry_count   INT DEFAULT 0,
			next_retry_at TIMESTAMPTZ,
			created_at    TIMESTAMPTZ DEFAULT NOW(),
			updated_at    TIMESTAMPTZ DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS idx_notification_jobs_service_id ON notification_jobs(service_id);
		CREATE INDEX IF NOT EXISTS idx_notification_jobs_status ON notification_jobs(status);
		CREATE INDEX IF NOT EXISTS idx_notifications_service_id ON notifications(service_id);
		CREATE INDEX IF NOT EXISTS idx_notifications_created_at ON notifications(created_at);
		CREATE INDEX IF NOT EXISTS idx_delivery_attempts_notification_id ON delivery_attempts(notification_id);

		CREATE TABLE IF NOT EXISTS notification_stats (
			service_id  TEXT REFERENCES services(id),
			status      TEXT NOT NULL,
			hour_bucket TIMESTAMPTZ NOT NULL,
			count       BIGINT DEFAULT 0,
			PRIMARY KEY (service_id, status, hour_bucket)
		);

		CREATE TABLE IF NOT EXISTS dead_letters (
			id              TEXT PRIMARY KEY,
			notification_id TEXT NOT NULL,
			service_id      TEXT REFERENCES services(id),
			payload         BYTEA NOT NULL,
			last_error      TEXT,
			attempt_count   INT NOT NULL DEFAULT 0,
			failed_at       TIMESTAMPTZ DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS idx_dead_letters_service_id ON dead_letters(service_id);
		CREATE INDEX IF NOT EXISTS idx_dead_letters_failed_at ON dead_letters(failed_at);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_dead_letters_notification_id ON dead_letters(notification_id);
	`

	_, err := db.Pool.Exec(ctx, schema)
	if err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	// CREATE TABLE IF NOT EXISTS never re-evaluates the status CHECK on
	// existing databases, so re-create the constraint to allow DEAD_LETTERED.
	constraint := `
		ALTER TABLE notification_jobs DROP CONSTRAINT IF EXISTS notification_jobs_status_check;
		ALTER TABLE notification_jobs ADD CONSTRAINT notification_jobs_status_check
			CHECK (status IN ('ACCEPTED', 'PENDING', 'DISPATCHED', 'DELIVERED', 'FAILED', 'DEAD_LETTERED'));
	`
	if _, err := db.Pool.Exec(ctx, constraint); err != nil {
		return fmt.Errorf("failed to migrate status check constraint: %w", err)
	}

	// Soft-delete support for services. CREATE TABLE IF NOT EXISTS never adds the
	// column or swaps constraints on an existing database, so apply them here.
	// The full-column UNIQUE(name)/UNIQUE(api_key) constraints are replaced with
	// partial unique indexes scoped to live rows, so a soft-deleted service frees
	// its name and api_key for reuse.
	softDelete := `
		ALTER TABLE services ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
		ALTER TABLE services DROP CONSTRAINT IF EXISTS services_name_key;
		ALTER TABLE services DROP CONSTRAINT IF EXISTS services_api_key_key;
		CREATE UNIQUE INDEX IF NOT EXISTS idx_services_name_live
			ON services(name)    WHERE deleted_at IS NULL;
		CREATE UNIQUE INDEX IF NOT EXISTS idx_services_api_key_live
			ON services(api_key) WHERE deleted_at IS NULL;
	`
	if _, err := db.Pool.Exec(ctx, softDelete); err != nil {
		return fmt.Errorf("failed to migrate services soft-delete: %w", err)
	}

	return nil
}
