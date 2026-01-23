package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/lupppig/notifyctl/internal/broker"
	"github.com/lupppig/notifyctl/internal/domain"
	"github.com/lupppig/notifyctl/internal/events"
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
	eventHub       *events.Hub
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
	eventHub *events.Hub,
) *WebhookWorker {
	return &WebhookWorker{
		notifications:  notifications,
		attempts:       attempts,
		httpClient:     httpClient,
		consumer:       consumer,
		publisher:      publisher,
		retryScheduler: retryScheduler,
		eventHub:       eventHub,
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

	failedDests := 0
	for _, dest := range notification.Destinations {
		if dest.Type == domain.DestinationTypeWebhook {
			if !w.deliverWebhook(ctx, notification, dest, nm.AttemptCount) {
				failedDests++
			}
		}
	}

	if failedDests == 0 {
		msg.Ack()
		return
	}

	nm.AttemptCount++
	if w.retryScheduler.ShouldRetry(nm.AttemptCount) {
		delay := w.retryScheduler.NextDelay(nm.AttemptCount)
		w.emitEvent(notification, "", events.DeliveryStatusRetrying, "Retrying", nm.AttemptCount)
		msg.NakWithDelay(delay)
	} else {
		w.moveToDLQ(ctx, nm)
		msg.Ack()
	}
}

func (w *WebhookWorker) deliverWebhook(ctx context.Context, n *domain.Notification, dest domain.Destination, attemptCount int) bool {
	attempt := &domain.DeliveryAttempt{
		ID:             uuid.New().String(),
		NotificationID: n.ID,
		Destination:    dest.Target,
		AttemptedAt:    time.Now(),
		Status:         domain.DeliveryStatusFailed,
	}

	defer func() {
		if err := w.attempts.Create(ctx, attempt); err != nil {
			log.Printf("failed to record delivery attempt: %v", err)
		}
	}()

	w.emitEvent(n, dest.Target, events.DeliveryStatusPending, "Queued", attemptCount)
	w.emitEvent(n, dest.Target, events.DeliveryStatusDelivering, "Sending", attemptCount)

	resp, err := w.httpClient.Post(ctx, dest.Target, n.Payload)
	if err != nil {
		attempt.Error = err.Error()
		w.emitEvent(n, dest.Target, events.DeliveryStatusFailed, err.Error(), attemptCount)
		return false
	}

	attempt.StatusCode = resp.StatusCode
	attempt.ResponseBody = resp.Body

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		attempt.Status = domain.DeliveryStatusSuccess
		w.emitEvent(n, dest.Target, events.DeliveryStatusDelivered, "OK", attemptCount)
		return true
	}

	w.emitEvent(n, dest.Target, events.DeliveryStatusFailed, fmt.Sprintf("HTTP %d", resp.StatusCode), attemptCount)
	return false
}

func (w *WebhookWorker) moveToDLQ(ctx context.Context, nm notificationMessage) {
	log.Printf("moving notification %s to DLQ", nm.NotificationID)
	data, _ := json.Marshal(nm)
	if err := w.publisher.PublishToDLQ(ctx, data); err != nil {
		log.Printf("failed to publish to DLQ: %v", err)
	}
}

func (w *WebhookWorker) emitEvent(n *domain.Notification, destination string, status events.DeliveryStatus, message string, attempt int) {
	if w.eventHub == nil {
		return
	}

	w.eventHub.Publish(events.DeliveryEvent{
		NotificationID: n.ID,
		ServiceID:      n.ServiceID,
		Destination:    destination,
		Status:         status,
		Message:        message,
		Attempt:        attempt,
		Timestamp:      time.Now(),
	})
}
