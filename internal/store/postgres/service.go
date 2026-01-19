package postgres

import (
	"context"
	"fmt"

	"github.com/lupppig/notifyctl/internal/domain"
)

type ServiceStore struct {
	db *DB
}

func NewServiceStore(db *DB) *ServiceStore {
	return &ServiceStore{db: db}
}

func (s *ServiceStore) Create(ctx context.Context, svc *domain.Service) error {
	query := `
		INSERT INTO services (id, name, webhook_url, secret, api_key, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`

	_, err := s.db.Pool.Exec(ctx, query,
		svc.ID,
		svc.Name,
		svc.WebhookURL,
		svc.Secret,
		svc.APIKey,
		svc.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create service: %w", err)
	}

	return nil
}
