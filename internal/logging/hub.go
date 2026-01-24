package logging

import (
	"sync"
)

type LogHub struct {
	mu          sync.RWMutex
	subscribers map[string]chan string
}

var globalHub = &LogHub{
	subscribers: make(map[string]chan string),
}

func GetHub() *LogHub {
	return globalHub
}

func (h *LogHub) Subscribe(id string) chan string {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan string, 100)
	h.subscribers[id] = ch
	return ch
}

func (h *LogHub) Unsubscribe(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ch, ok := h.subscribers[id]; ok {
		close(ch)
		delete(h.subscribers, id)
	}
}

func (h *LogHub) Broadcast(line string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, ch := range h.subscribers {
		select {
		case ch <- line:
		default:
			// Subscriber is too slow, skip this line
		}
	}
}
