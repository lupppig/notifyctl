package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/lupppig/notifyctl/internal/domain"
	"github.com/lupppig/notifyctl/internal/store"
)

type DeadLetterStore struct {
	db *DB
}

func NewDeadLetterStore(db *DB) *DeadLetterStore {
	return &DeadLetterStore{db: db}
}

func (s *DeadLetterStore) Create(ctx context.Context, dl *domain.DeadLetter) error {
	query := `
		INSERT INTO dead_letters (id, notification_id, service_id, payload, last_error, attempt_count, failed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (notification_id) DO NOTHING
	`

	_, err := s.db.Pool.Exec(ctx, query,
		dl.ID,
		dl.NotificationID,
		dl.ServiceID,
		dl.Payload,
		dl.LastError,
		dl.AttemptCount,
		dl.FailedAt,
	)
	if err != nil {
		return fmt.Errorf("insert dead letter: %w", err)
	}

	return nil
}

func (s *DeadLetterStore) List(ctx context.Context, serviceID string) ([]*domain.DeadLetter, error) {
	query := `
		SELECT id, notification_id, service_id, payload, last_error, attempt_count, failed_at
		FROM dead_letters
		WHERE ($1 = '' OR service_id = $1)
		ORDER BY failed_at DESC
		LIMIT 100
	`

	rows, err := s.db.Pool.Query(ctx, query, serviceID)
	if err != nil {
		return nil, fmt.Errorf("query dead letters: %w", err)
	}
	defer rows.Close()

	var letters []*domain.DeadLetter
	for rows.Next() {
		var (
			dl        domain.DeadLetter
			serviceID *string
			lastError *string
		)
		if err := rows.Scan(
			&dl.ID,
			&dl.NotificationID,
			&serviceID,
			&dl.Payload,
			&lastError,
			&dl.AttemptCount,
			&dl.FailedAt,
		); err != nil {
			return nil, fmt.Errorf("scan dead letter: %w", err)
		}
		if serviceID != nil {
			dl.ServiceID = *serviceID
		}
		if lastError != nil {
			dl.LastError = *lastError
		}
		letters = append(letters, &dl)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dead letters: %w", err)
	}

	return letters, nil
}

func (s *DeadLetterStore) GetByID(ctx context.Context, id string) (*domain.DeadLetter, error) {
	query := `
		SELECT id, notification_id, service_id, payload, last_error, attempt_count, failed_at
		FROM dead_letters
		WHERE id = $1
	`

	var (
		dl        domain.DeadLetter
		serviceID *string
		lastError *string
	)
	err := s.db.Pool.QueryRow(ctx, query, id).Scan(
		&dl.ID,
		&dl.NotificationID,
		&serviceID,
		&dl.Payload,
		&lastError,
		&dl.AttemptCount,
		&dl.FailedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("get dead letter: %w", err)
	}
	if serviceID != nil {
		dl.ServiceID = *serviceID
	}
	if lastError != nil {
		dl.LastError = *lastError
	}

	return &dl, nil
}

func (s *DeadLetterStore) DeleteByNotificationID(ctx context.Context, notificationID string) error {
	_, err := s.db.Pool.Exec(ctx, `DELETE FROM dead_letters WHERE notification_id = $1`, notificationID)
	if err != nil {
		return fmt.Errorf("delete dead letter: %w", err)
	}
	return nil
}
