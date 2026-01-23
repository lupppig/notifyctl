package worker

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/lupppig/notifyctl/internal/domain"
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
	_, err := w.nc.Subscribe("notifications.jobs", func(m *nats.Msg) {
		var job domain.NotificationJob
		if err := json.Unmarshal(m.Data, &job); err != nil {
			log.Printf("Error unmarshaling job: %v", err)
			return
		}

		log.Printf("[%s] Worker received job: request_id=%s service_id=%s status=%s retry_count=%d",
			time.Now().Format(time.RFC3339), job.RequestID, job.ServiceID, job.Status, job.RetryCount)

		// Update status to DISPATCHED
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := w.jobStore.UpdateStatus(ctx, job.RequestID, "DISPATCHED"); err != nil {
			log.Printf("Error updating job status for %s: %v", job.RequestID, err)
		}
	})
	if err != nil {
		return err
	}

	log.Println("Worker started and subscribed to notifications.jobs")
	return nil
}
