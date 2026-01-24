package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/lupppig/notifyctl/internal/domain"
	"github.com/lupppig/notifyctl/internal/logging"
	"github.com/lupppig/notifyctl/internal/store"
	"github.com/nats-io/nats.go"
)

type Worker struct {
	nc       *nats.Conn
	jobStore store.NotificationJobStore
}

func NewWorker(nc *nats.Conn, jobStore store.NotificationJobStore) *Worker {
	return &Worker{
		nc:       nc,
		jobStore: jobStore,
	}
}

func (w *Worker) Start() error {
	slog.Info("worker starting and subscribing to notifications.jobs", slog.String("code", "SYS_STARTUP"))
	_, err := w.nc.Subscribe("notifications.jobs", func(m *nats.Msg) {
		var job domain.NotificationJob
		if err := json.Unmarshal(m.Data, &job); err != nil {
			slog.Error("failed to unmarshal job from NATS", slog.String("code", "BROKER_ERROR"), slog.Any("error", err))
			return
		}

		ctx := context.Background()
		ctx = logging.WithEventID(ctx, job.RequestID)
		ctx = logging.WithService(ctx, job.ServiceID, "") // We don't have service name here yet, but we have ID
		l := logging.FromContext(ctx)

		l.Info("processing job", slog.String("code", "JOB_PROC_START"), slog.Int("attempt", job.RetryCount))

		if err := w.jobStore.UpdateStatus(ctx, job.RequestID, "DISPATCHED"); err != nil {
			l.Error("failed to update job status", slog.String("code", "DB_ERROR"), slog.Any("error", err))
		} else {
			l.Info("job status updated to DISPATCHED", slog.String("code", "JOB_DISPATCHED"))
			if err := w.jobStore.IncrementStats(ctx, job.ServiceID, "DISPATCHED", time.Now()); err != nil {
				l.Warn("failed to increment stats", slog.String("code", "DB_ERROR"), slog.Any("error", err))
			}

			// Simulate delivery for tracking verification
			go func(serviceID, requestID string) {
				time.Sleep(1 * time.Second)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				ctx = logging.WithEventID(ctx, requestID)
				ctx = logging.WithService(ctx, serviceID, "")
				l := logging.FromContext(ctx)

				_ = w.jobStore.UpdateStatus(ctx, requestID, "DELIVERED")
				_ = w.jobStore.IncrementStats(ctx, serviceID, "DELIVERED", time.Now())
				l.Info("job delivered successfully", slog.String("code", "DEL_SUCCESS"))
			}(job.ServiceID, job.RequestID)
		}
	})
	if err != nil {
		return err
	}

	slog.Info("worker ready", slog.String("code", "SYS_READY"))
	return nil
}
