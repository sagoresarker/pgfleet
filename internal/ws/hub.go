// Package ws provides a simple in-memory pub/sub hub for streaming
// provisioning/lifecycle events to WebSocket clients.
package ws

import "sync"

// Event is a progress/lifecycle update for an instance.
type Event struct {
	InstanceID string `json:"instance_id"`
	Step       string `json:"step"`
	Detail     string `json:"detail"`
}

const subBuffer = 64

// Hub fans out events to all current subscribers. Slow subscribers drop events
// rather than blocking publishers.
type Hub struct {
	mu   sync.Mutex
	subs map[chan Event]struct{}
}

// NewHub creates an empty Hub.
func NewHub() *Hub {
	return &Hub{subs: map[chan Event]struct{}{}}
}

// Subscribe registers a new subscriber and returns its channel plus an
// unsubscribe function that closes the channel.
func (h *Hub) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, subBuffer)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, ch)
			h.mu.Unlock()
			close(ch)
		})
	}
	return ch, cancel
}

// Publish broadcasts an event to all subscribers, dropping it for any
// subscriber whose buffer is full (never blocks).
func (h *Hub) Publish(instanceID, step, detail string) {
	ev := Event{InstanceID: instanceID, Step: step, Detail: detail}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- ev:
		default: // drop for slow subscriber
		}
	}
}
