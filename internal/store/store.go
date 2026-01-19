package store

import (
	"context"

	"github.com/lupppig/notifyctl/internal/domain"
)

type ServiceStore interface {
	Create(ctx context.Context, svc *domain.Service) error
}

type NotificationStore interface {
	Create(ctx context.Context, n *domain.Notification) error
}
