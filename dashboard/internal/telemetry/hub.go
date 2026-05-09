// Package telemetry provides SSE fan-out for live telemetry frames from relay.tango.
package telemetry

import (
	"encoding/json"
	"sync"
)

// Frame is a decoded telemetry message ready for SSE delivery.
type Frame struct {
	MachineID string          `json:"machine_id"`
	Type      string          `json:"type"`
	Timestamp int64           `json:"ts"`
	Payload   json.RawMessage `json:"payload"`
}

// Hub distributes incoming telemetry frames to all registered SSE clients.
type Hub struct {
	mu      sync.RWMutex
	clients map[chan Frame]struct{}

	snapMu   sync.RWMutex
	snapshot map[string]json.RawMessage // key: "machineID/type"
}

// NewHub creates an empty hub.
func NewHub() *Hub {
	return &Hub{
		clients:  make(map[chan Frame]struct{}),
		snapshot: make(map[string]json.RawMessage),
	}
}

// Subscribe registers a client channel. Caller must Unsubscribe when done.
func (h *Hub) Subscribe() chan Frame {
	ch := make(chan Frame, 64)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

// Unsubscribe removes and closes a client channel.
func (h *Hub) Unsubscribe(ch chan Frame) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
	close(ch)
}

// Publish sends a frame to all subscribers and updates the snapshot.
func (h *Hub) Publish(f Frame) {
	h.snapMu.Lock()
	h.snapshot[f.MachineID+"/"+f.Type] = f.Payload
	h.snapMu.Unlock()

	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.clients {
		select {
		case ch <- f:
		default: // slow client — drop rather than block
		}
	}
}

// Snapshot returns all latest frames keyed by "machineID/type".
func (h *Hub) Snapshot() map[string]json.RawMessage {
	h.snapMu.RLock()
	defer h.snapMu.RUnlock()
	out := make(map[string]json.RawMessage, len(h.snapshot))
	for k, v := range h.snapshot {
		out[k] = v
	}
	return out
}
