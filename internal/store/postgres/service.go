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
func (s *ServiceStore) List(ctx context.Context) ([]*domain.Service, error) {
	query := `
		SELECT id, name, webhook_url, secret, api_key, created_at
		FROM services
		ORDER BY created_at DESC
	`

	rows, err := s.db.Pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query services: %w", err)
	}
	defer rows.Close()

	var services []*domain.Service
	for rows.Next() {
		var svc domain.Service
		err := rows.Scan(
			&svc.ID,
			&svc.Name,
			&svc.WebhookURL,
			&svc.Secret,
			&svc.APIKey,
			&svc.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan service: %w", err)
		}
		services = append(services, &svc)
	}

	return services, nil
}

func (s *ServiceStore) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM services WHERE id = $1`
	_, err := s.db.Pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("delete service %s: %w", id, err)
	}
	return nil
}
