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
		INSERT INTO notification_jobs (request_id, service_id, payload, status, retry_count, next_retry_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`

	_, err := s.db.Pool.Exec(ctx, query,
		job.RequestID,
		job.ServiceID,
		job.Payload,
		job.Status,
		job.RetryCount,
		job.NextRetryAt,
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
		SELECT request_id, service_id, payload, status, retry_count, next_retry_at, created_at, updated_at
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
		&job.NextRetryAt,
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

func (s *NotificationJobStore) FailJob(ctx context.Context, requestID string, nextRetryAt time.Time) error {
	query := `
		UPDATE notification_jobs
		SET status = 'FAILED', retry_count = retry_count + 1, next_retry_at = $1, updated_at = $2
		WHERE request_id = $3
	`
	_, err := s.db.Pool.Exec(ctx, query, nextRetryAt, time.Now(), requestID)
	if err != nil {
		return fmt.Errorf("fail notification job: %w", err)
	}
	return nil
}

func (s *NotificationJobStore) GetRetryableJobs(ctx context.Context, limit int) ([]*domain.NotificationJob, error) {
	query := `
		SELECT request_id, service_id, payload, status, retry_count, next_retry_at, created_at, updated_at
		FROM notification_jobs
		WHERE status = 'FAILED' AND next_retry_at <= $1
		ORDER BY next_retry_at ASC
		LIMIT $2
	`
	rows, err := s.db.Pool.Query(ctx, query, time.Now(), limit)
	if err != nil {
		return nil, fmt.Errorf("query retryable jobs: %w", err)
	}
	defer rows.Close()

	var jobs []*domain.NotificationJob
	for rows.Next() {
		var job domain.NotificationJob
		err := rows.Scan(
			&job.RequestID,
			&job.ServiceID,
			&job.Payload,
			&job.Status,
			&job.RetryCount,
			&job.NextRetryAt,
			&job.CreatedAt,
			&job.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan retryable job: %w", err)
		}
		jobs = append(jobs, &job)
	}
	return jobs, nil
}

func (s *NotificationJobStore) List(ctx context.Context, serviceID string) ([]*domain.NotificationJob, error) {
	query := `
		SELECT request_id, service_id, payload, status, retry_count, next_retry_at, created_at, updated_at
		FROM notification_jobs
		WHERE ($1 = '' OR service_id = $1)
		ORDER BY created_at DESC
		LIMIT 100
	`
	rows, err := s.db.Pool.Query(ctx, query, serviceID)
	if err != nil {
		return nil, fmt.Errorf("query notification jobs: %w", err)
	}
	defer rows.Close()

	var jobs []*domain.NotificationJob
	for rows.Next() {
		var job domain.NotificationJob
		err := rows.Scan(
			&job.RequestID,
			&job.ServiceID,
			&job.Payload,
			&job.Status,
			&job.RetryCount,
			&job.NextRetryAt,
			&job.CreatedAt,
			&job.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan notification job: %w", err)
		}
		jobs = append(jobs, &job)
	}
	return jobs, nil
}

func (s *NotificationJobStore) IncrementStats(ctx context.Context, serviceID, status string, t time.Time) error {
	hourBucket := t.Truncate(time.Hour)

	query := `
		INSERT INTO notification_stats (service_id, status, hour_bucket, count)
		VALUES ($1, $2, $3, 1)
		ON CONFLICT (service_id, status, hour_bucket)
		DO UPDATE SET count = notification_stats.count + 1
	`
	_, err := s.db.Pool.Exec(ctx, query, serviceID, status, hourBucket)
	if err != nil {
		return fmt.Errorf("increment status stats: %w", err)
	}
	return nil
}

func (s *NotificationJobStore) GetStats(ctx context.Context, serviceID string) (map[string]int64, error) {
	query := `
		SELECT status, SUM(count) as total
		FROM notification_stats
		WHERE service_id = $1
		GROUP BY status
	`
	rows, err := s.db.Pool.Query(ctx, query, serviceID)
	if err != nil {
		return nil, fmt.Errorf("query notification stats: %w", err)
	}
	defer rows.Close()

	stats := make(map[string]int64)
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("scan stat: %w", err)
		}
		stats[status] = count
	}
	return stats, nil
}
