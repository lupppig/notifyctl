package postgres

import (
	"context"
	"fmt"

	"github.com/lupppig/notifyctl/internal/domain"
)

type DeliveryAttemptStore struct {
	db *DB
}

func NewDeliveryAttemptStore(db *DB) *DeliveryAttemptStore {
	return &DeliveryAttemptStore{db: db}
}

func (s *DeliveryAttemptStore) Create(ctx context.Context, attempt *domain.DeliveryAttempt) error {
	query := `
		INSERT INTO delivery_attempts (id, notification_id, destination, status, status_code, response_body, error, attempted_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`

	_, err := s.db.Pool.Exec(ctx, query,
		attempt.ID,
		attempt.NotificationID,
		attempt.Destination,
		attempt.Status,
		attempt.StatusCode,
		attempt.ResponseBody,
		attempt.Error,
		attempt.AttemptedAt,
	)
	if err != nil {
		return fmt.Errorf("insert delivery attempt: %w", err)
	}

	return nil
}
