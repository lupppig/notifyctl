package events

import (
	"sync"
	"testing"
	"time"
)

func TestHubSubscribeAndPublish(t *testing.T) {
	hub := NewHub()

	sub := &Subscriber{
		ID:     "test-sub-1",
		Events: make(chan DeliveryEvent, 10),
	}
	hub.Subscribe(sub)

	event := DeliveryEvent{
		NotificationID: "notif-1",
		ServiceID:      "service-1",
		Destination:    "https://example.com",
		Status:         DeliveryStatusDelivered,
		Message:        "Success",
		Attempt:        1,
		Timestamp:      time.Now(),
	}

	hub.Publish(event)

	select {
	case received := <-sub.Events:
		if received.NotificationID != event.NotificationID {
			t.Errorf("expected notification ID %s, got %s", event.NotificationID, received.NotificationID)
		}
		if received.Status != event.Status {
			t.Errorf("expected status %s, got %s", event.Status, received.Status)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for event")
	}
}

func TestHubBroadcastToMultipleSubscribers(t *testing.T) {
	hub := NewHub()

	sub1 := &Subscriber{ID: "sub-1", Events: make(chan DeliveryEvent, 10)}
	sub2 := &Subscriber{ID: "sub-2", Events: make(chan DeliveryEvent, 10)}
	sub3 := &Subscriber{ID: "sub-3", Events: make(chan DeliveryEvent, 10)}

	hub.Subscribe(sub1)
	hub.Subscribe(sub2)
	hub.Subscribe(sub3)

	event := DeliveryEvent{
		NotificationID: "notif-broadcast",
		Status:         DeliveryStatusDelivered,
	}

	hub.Publish(event)

	for _, sub := range []*Subscriber{sub1, sub2, sub3} {
		select {
		case received := <-sub.Events:
			if received.NotificationID != event.NotificationID {
				t.Errorf("subscriber %s: expected notification ID %s, got %s", sub.ID, event.NotificationID, received.NotificationID)
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("subscriber %s: timeout waiting for event", sub.ID)
		}
	}
}

func TestHubFilterByNotificationID(t *testing.T) {
	hub := NewHub()

	// Subscriber filtering for specific notification
	sub := &Subscriber{
		ID:             "filtered-sub",
		NotificationID: "target-notif",
		Events:         make(chan DeliveryEvent, 10),
	}
	hub.Subscribe(sub)

	// Publish matching event
	hub.Publish(DeliveryEvent{NotificationID: "target-notif", Status: DeliveryStatusDelivered})

	// Publish non-matching event
	hub.Publish(DeliveryEvent{NotificationID: "other-notif", Status: DeliveryStatusFailed})

	// Should only receive the matching event
	select {
	case received := <-sub.Events:
		if received.NotificationID != "target-notif" {
			t.Errorf("expected target-notif, got %s", received.NotificationID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for matching event")
	}

	// Channel should be empty (non-matching event was filtered)
	select {
	case <-sub.Events:
		t.Error("should not receive non-matching event")
	case <-time.After(50 * time.Millisecond):
		// Expected - no more events
	}
}

func TestHubFilterByServiceID(t *testing.T) {
	hub := NewHub()

	sub := &Subscriber{
		ID:        "service-filtered-sub",
		ServiceID: "target-service",
		Events:    make(chan DeliveryEvent, 10),
	}
	hub.Subscribe(sub)

	hub.Publish(DeliveryEvent{ServiceID: "target-service", NotificationID: "notif-1"})
	hub.Publish(DeliveryEvent{ServiceID: "other-service", NotificationID: "notif-2"})

	select {
	case received := <-sub.Events:
		if received.ServiceID != "target-service" {
			t.Errorf("expected target-service, got %s", received.ServiceID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for matching event")
	}

	select {
	case <-sub.Events:
		t.Error("should not receive non-matching event")
	case <-time.After(50 * time.Millisecond):
		// Expected
	}
}

func TestHubUnsubscribe(t *testing.T) {
	hub := NewHub()

	sub := &Subscriber{
		ID:     "unsub-test",
		Events: make(chan DeliveryEvent, 10),
	}
	hub.Subscribe(sub)

	if hub.SubscriberCount() != 1 {
		t.Errorf("expected 1 subscriber, got %d", hub.SubscriberCount())
	}

	hub.Unsubscribe(sub.ID)

	if hub.SubscriberCount() != 0 {
		t.Errorf("expected 0 subscribers after unsubscribe, got %d", hub.SubscriberCount())
	}

	// Channel should be closed
	_, ok := <-sub.Events
	if ok {
		t.Error("expected channel to be closed after unsubscribe")
	}
}

func TestHubNonBlockingPublish(t *testing.T) {
	hub := NewHub()

	// Subscriber with small buffer
	sub := &Subscriber{
		ID:     "slow-sub",
		Events: make(chan DeliveryEvent, 1), // Only 1 buffer
	}
	hub.Subscribe(sub)

	// Publish more events than buffer can hold
	for i := 0; i < 10; i++ {
		hub.Publish(DeliveryEvent{NotificationID: "notif", Attempt: i})
	}

	// Should not block - first event should be in buffer
	select {
	case <-sub.Events:
		// Good - received first event
	default:
		t.Error("expected at least one event in buffer")
	}
}

func TestHubConcurrentAccess(t *testing.T) {
	hub := NewHub()

	var wg sync.WaitGroup

	// Concurrent subscribers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			sub := &Subscriber{
				ID:     string(rune('A' + id)),
				Events: make(chan DeliveryEvent, 100),
			}
			hub.Subscribe(sub)
			time.Sleep(10 * time.Millisecond)
			hub.Unsubscribe(sub.ID)
		}(i)
	}

	// Concurrent publishers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				hub.Publish(DeliveryEvent{NotificationID: "concurrent-test"})
			}
		}(i)
	}

	wg.Wait()
	// Test passes if no race conditions or deadlocks
}

func BenchmarkHubPublish(b *testing.B) {
	hub := NewHub()

	sub := &Subscriber{
		ID:     "bench-sub",
		Events: make(chan DeliveryEvent, 1000),
	}
	hub.Subscribe(sub)

	event := DeliveryEvent{
		NotificationID: "bench-notif",
		Status:         DeliveryStatusDelivered,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hub.Publish(event)
	}
}

func BenchmarkHubPublishManySubscribers(b *testing.B) {
	hub := NewHub()

	for i := 0; i < 100; i++ {
		sub := &Subscriber{
			ID:     string(rune(i)),
			Events: make(chan DeliveryEvent, 1000),
		}
		hub.Subscribe(sub)
	}

	event := DeliveryEvent{
		NotificationID: "bench-notif",
		Status:         DeliveryStatusDelivered,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hub.Publish(event)
	}
}
