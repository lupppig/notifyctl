package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/lupppig/notifyctl/internal/domain"
)

type NotificationJobStore struct {
	db *DB
}

func NewNotificationJobStore(db *DB) *NotificationJobStore {
	return &NotificationJobStore{db: db}
}

func (s *NotificationJobStore) Create(ctx context.Context, job *domain.NotificationJob) error {
	query := `
		INSERT INTO notification_jobs (request_id, service_id, payload, status, retry_count, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, err := s.db.Pool.Exec(ctx, query,
		job.RequestID,
		job.ServiceID,
		job.Payload,
		job.Status,
		job.RetryCount,
		job.CreatedAt,
		job.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create notification job: %w", err)
	}

	return nil
}

func (s *NotificationJobStore) GetByRequestID(ctx context.Context, requestID string) (*domain.NotificationJob, error) {
	query := `
		SELECT request_id, service_id, payload, status, retry_count, created_at, updated_at
		FROM notification_jobs
		WHERE request_id = $1
	`
	var job domain.NotificationJob
	err := s.db.Pool.QueryRow(ctx, query, requestID).Scan(
		&job.RequestID,
		&job.ServiceID,
		&job.Payload,
		&job.Status,
		&job.RetryCount,
		&job.CreatedAt,
		&job.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get notification job: %w", err)
	}
	return &job, nil
}

func (s *NotificationJobStore) UpdateStatus(ctx context.Context, requestID string, status string) error {
	query := `
		UPDATE notification_jobs
		SET status = $1, updated_at = $2
		WHERE request_id = $3
	`
	_, err := s.db.Pool.Exec(ctx, query, status, time.Now(), requestID)
	if err != nil {
		return fmt.Errorf("update notification job status: %w", err)
	}
	return nil
}
