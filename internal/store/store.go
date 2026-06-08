package store

import (
	"context"
	"errors"
	"time"

	"github.com/lupppig/notifyctl/internal/domain"
)

var (
	ErrAlreadyExists = errors.New("already exists")
	ErrNotFound      = errors.New("not found")
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
	FailJob(ctx context.Context, requestID string, nextRetryAt time.Time) error
	GetRetryableJobs(ctx context.Context, limit int) ([]*domain.NotificationJob, error)
	List(ctx context.Context, serviceID string) ([]*domain.NotificationJob, error)
	IncrementStats(ctx context.Context, serviceID, status string, t time.Time) error
	GetStats(ctx context.Context, serviceID string) (map[string]int64, error)
	// ResetForReplay upserts the job back to PENDING with a fresh retry
	// budget so a replayed dead letter re-enters the dispatch pipeline.
	ResetForReplay(ctx context.Context, job *domain.NotificationJob) error
}

type DeliveryAttemptStore interface {
	Create(ctx context.Context, attempt *domain.DeliveryAttempt) error
}

type DeadLetterStore interface {
	Create(ctx context.Context, dl *domain.DeadLetter) error
	List(ctx context.Context, serviceID string) ([]*domain.DeadLetter, error)
	GetByID(ctx context.Context, id string) (*domain.DeadLetter, error)
	DeleteByNotificationID(ctx context.Context, notificationID string) error
}
