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
	log.Printf("Worker starting and subscribing to notifications.jobs...")
	_, err := w.nc.Subscribe("notifications.jobs", func(m *nats.Msg) {
		var job domain.NotificationJob
		if err := json.Unmarshal(m.Data, &job); err != nil {
			log.Printf("[ERROR] Failed to unmarshal job from NATS: %v", err)
			return
		}

		log.Printf("[%s] ACTION: Processing job | ID: %s | Service: %s | Status: %s | Attempt: %d",
			time.Now().Format(time.RFC3339), job.RequestID, job.ServiceID, job.Status, job.RetryCount)

		// Update status to DISPATCHED
		log.Printf("[%s] ACTION: Updating job status to DISPATCHED | ID: %s", time.Now().Format(time.RFC3339), job.RequestID)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := w.jobStore.UpdateStatus(ctx, job.RequestID, "DISPATCHED"); err != nil {
			log.Printf("[ERROR] Failed to update status for %s: %v", job.RequestID, err)
		} else {
			log.Printf("[%s] SUCCESS: Job %s status updated to DISPATCHED", time.Now().Format(time.RFC3339), job.RequestID)
		}
	})
	if err != nil {
		return err
	}

	log.Println("Worker ready.")
	return nil
}
