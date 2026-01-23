package worker

import (
	"encoding/json"
	"log"

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

		log.Printf("Worker received job: request_id=%s service_id=%s", job.RequestID, job.ServiceID)

		// Placeholder for real delivery logic in future phases
		// For now, we just log and move on.
	})
	if err != nil {
		return err
	}

	log.Println("Worker started and subscribed to notifications.jobs")
	return nil
}
