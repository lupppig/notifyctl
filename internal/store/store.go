package store

import (
	"context"
	"errors"

	"github.com/lupppig/notifyctl/internal/domain"
)

var (
	ErrAlreadyExists = errors.New("already exists")
)

type ServiceStore interface {
	Create(ctx context.Context, svc *domain.Service) error
	List(ctx context.Context) ([]*domain.Service, error)
	GetByAPIKeyHash(ctx context.Context, hash string) (*domain.Service, error)
	Delete(ctx context.Context, id string) error
}

type NotificationStore interface {
	Create(ctx context.Context, n *domain.Notification) error
	GetByID(ctx context.Context, id string) (*domain.Notification, error)
}

type NotificationJobStore interface {
	Create(ctx context.Context, job *domain.NotificationJob) error
	GetByRequestID(ctx context.Context, requestID string) (*domain.NotificationJob, error)
	UpdateStatus(ctx context.Context, requestID string, status string) error
}

type DeliveryAttemptStore interface {
	Create(ctx context.Context, attempt *domain.DeliveryAttempt) error
}
