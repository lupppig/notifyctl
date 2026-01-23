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
			created_at  TIMESTAMPTZ DEFAULT NOW()
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
			request_id  TEXT PRIMARY KEY,
			service_id  TEXT REFERENCES services(id),
			payload     JSONB NOT NULL,
			status      TEXT NOT NULL CHECK (status IN ('ACCEPTED', 'PENDING', 'DISPATCHED', 'DELIVERED', 'FAILED')),
			retry_count INT DEFAULT 0,
			created_at  TIMESTAMPTZ DEFAULT NOW(),
			updated_at  TIMESTAMPTZ DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS idx_notification_jobs_service_id ON notification_jobs(service_id);
		CREATE INDEX IF NOT EXISTS idx_notification_jobs_status ON notification_jobs(status);
		CREATE INDEX IF NOT EXISTS idx_notifications_service_id ON notifications(service_id);
		CREATE INDEX IF NOT EXISTS idx_notifications_created_at ON notifications(created_at);
		CREATE INDEX IF NOT EXISTS idx_delivery_attempts_notification_id ON delivery_attempts(notification_id);
	`

	_, err := db.Pool.Exec(ctx, schema)
	if err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}
