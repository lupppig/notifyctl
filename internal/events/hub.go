package events

import (
	"sync"
)

type Subscriber struct {
	ID             string
	NotificationID string // Filter by notification ID (empty = all)
	ServiceID      string // Filter by service ID (empty = all)
	Events         chan DeliveryEvent
}

type Hub struct {
	subscribers map[string]*Subscriber
	mu          sync.RWMutex
}

func NewHub() *Hub {
	return &Hub{
		subscribers: make(map[string]*Subscriber),
	}
}

func (h *Hub) Subscribe(sub *Subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.subscribers[sub.ID] = sub
}

func (h *Hub) Unsubscribe(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if sub, ok := h.subscribers[id]; ok {
		close(sub.Events)
		delete(h.subscribers, id)
	}
}

func (h *Hub) Publish(event DeliveryEvent) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, sub := range h.subscribers {
		if h.matchesFilter(sub, event) {
			select {
			case sub.Events <- event:
			default:
				// Non-blocking: skip if subscriber buffer is full
			}
		}
	}
}

func (h *Hub) matchesFilter(sub *Subscriber, event DeliveryEvent) bool {
	if sub.NotificationID != "" && sub.NotificationID != event.NotificationID {
		return false
	}
	if sub.ServiceID != "" && sub.ServiceID != event.ServiceID {
		return false
	}
	return true
}

func (h *Hub) SubscriberCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subscribers)
}
