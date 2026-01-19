package worker

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/lupppig/notifyctl/internal/broker"
	"github.com/lupppig/notifyctl/internal/domain"
	"github.com/lupppig/notifyctl/internal/httpclient"
	"github.com/lupppig/notifyctl/internal/retry"
	"github.com/lupppig/notifyctl/internal/store"
)

type WebhookWorker struct {
	notifications  store.NotificationStore
	attempts       store.DeliveryAttemptStore
	httpClient     *httpclient.Client
	consumer       jetstream.Consumer
	publisher      broker.Publisher
	retryScheduler *retry.Scheduler
}

type notificationMessage struct {
	NotificationID string `json:"notification_id"`
	AttemptCount   int    `json:"attempt_count"`
}

func NewWebhookWorker(
	notifications store.NotificationStore,
	attempts store.DeliveryAttemptStore,
	httpClient *httpclient.Client,
	consumer jetstream.Consumer,
	publisher broker.Publisher,
	retryScheduler *retry.Scheduler,
) *WebhookWorker {
	return &WebhookWorker{
		notifications:  notifications,
		attempts:       attempts,
		httpClient:     httpClient,
		consumer:       consumer,
		publisher:      publisher,
		retryScheduler: retryScheduler,
	}
}

func (w *WebhookWorker) Start(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			msgs, err := w.consumer.Fetch(10, jetstream.FetchMaxWait(5*time.Second))
			if err != nil {
				log.Printf("error fetching messages: %v", err)
				continue
			}

			for msg := range msgs.Messages() {
				w.processMessage(ctx, msg)
			}
		}
	}
}

func (w *WebhookWorker) processMessage(ctx context.Context, msg jetstream.Msg) {
	var nm notificationMessage
	if err := json.Unmarshal(msg.Data(), &nm); err != nil {
		log.Printf("failed to unmarshal message: %v", err)
		msg.Ack()
		return
	}

	notification, err := w.notifications.GetByID(ctx, nm.NotificationID)
	if err != nil {
		log.Printf("failed to get notification %s: %v", nm.NotificationID, err)
		msg.Ack()
		return
	}

	allSucceeded := true
	for _, dest := range notification.Destinations {
		if dest.Type != domain.DestinationTypeWebhook {
			continue
		}

		success := w.deliverWebhook(ctx, notification, dest, nm.AttemptCount)
		if !success {
			allSucceeded = false
		}
	}

	if allSucceeded {
		msg.Ack()
		return
	}

	// Handle retry or DLQ for failed deliveries
	nm.AttemptCount++
	if w.retryScheduler.ShouldRetry(nm.AttemptCount) {
		delay := w.retryScheduler.NextDelay(nm.AttemptCount)
		log.Printf("scheduling retry %d for notification %s in %v", nm.AttemptCount, nm.NotificationID, delay)
		msg.NakWithDelay(delay)
	} else {
		log.Printf("max retries reached for notification %s, moving to DLQ", nm.NotificationID)
		dlqData, _ := json.Marshal(nm)
		if err := w.publisher.PublishToDLQ(ctx, dlqData); err != nil {
			log.Printf("failed to publish to DLQ: %v", err)
		}
		msg.Ack()
	}
}

func (w *WebhookWorker) deliverWebhook(ctx context.Context, n *domain.Notification, dest domain.Destination, attemptCount int) bool {
	attempt := &domain.DeliveryAttempt{
		ID:             uuid.New().String(),
		NotificationID: n.ID,
		Destination:    dest.Target,
		AttemptedAt:    time.Now(),
	}

	resp, err := w.httpClient.Post(ctx, dest.Target, n.Payload)
	if err != nil {
		attempt.Status = domain.DeliveryStatusFailed
		attempt.Error = err.Error()
	} else {
		attempt.StatusCode = resp.StatusCode
		attempt.ResponseBody = resp.Body

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			attempt.Status = domain.DeliveryStatusSuccess
		} else {
			attempt.Status = domain.DeliveryStatusFailed
		}
	}

	if err := w.attempts.Create(ctx, attempt); err != nil {
		log.Printf("failed to record delivery attempt: %v", err)
	}

	return attempt.Status == domain.DeliveryStatusSuccess
}
