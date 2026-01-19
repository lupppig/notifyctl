package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lupppig/notifyctl/internal/domain"
)

type NotificationStore struct {
	db *DB
}

func NewNotificationStore(db *DB) *NotificationStore {
	return &NotificationStore{db: db}
}

func (s *NotificationStore) Create(ctx context.Context, n *domain.Notification) error {
	destinations, err := json.Marshal(n.Destinations)
	if err != nil {
		return fmt.Errorf("failed to marshal destinations: %w", err)
	}

	query := `
		INSERT INTO notifications (id, service_id, topic, payload, destinations, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`

	_, err = s.db.Pool.Exec(ctx, query,
		n.ID,
		n.ServiceID,
		n.Topic,
		n.Payload,
		destinations,
		n.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create notification: %w", err)
	}

	return nil
}
