package memory

import (
	"encoding/json"
	"sync"
	"time"
)

const defaultRingSize = 500

type Entry struct {
	MachineID  string          `json:"machine_id"`
	Slug       string          `json:"slug"`
	Payload    json.RawMessage `json:"payload"`
	ReceivedAt time.Time       `json:"received_at"`
}

type stateKey struct{ machineID, slug string }

type Layer struct {
	mu        sync.RWMutex
	live      map[stateKey]json.RawMessage
	lastSeen  map[stateKey]string
	lastFlush map[stateKey]time.Time
	ring      []Entry
	ringSize  int
	ringIdx   int
	FlushFn   func(machineID, slug string, payload json.RawMessage)
}

func New(ringSize int) *Layer {
	if ringSize <= 0 {
		ringSize = defaultRingSize
	}
	return &Layer{
		live:      make(map[stateKey]json.RawMessage),
		lastSeen:  make(map[stateKey]string),
		lastFlush: make(map[stateKey]time.Time),
		ring:      make([]Entry, ringSize),
		ringSize:  ringSize,
	}
}

func (l *Layer) Record(machineID, slug string, payload json.RawMessage) {
	k := stateKey{machineID, slug}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.live[k] = payload

	l.ring[l.ringIdx%l.ringSize] = Entry{
		MachineID:  machineID,
		Slug:       slug,
		Payload:    payload,
		ReceivedAt: time.Now(),
	}
	l.ringIdx++

	shouldFlush := false

	_, seen := l.lastFlush[k]
	if !seen {
		shouldFlush = true
	}

	if !shouldFlush {
		var parsed map[string]any
		if json.Unmarshal(payload, &parsed) == nil {
			if status, ok := parsed["status"].(string); ok {
				if status != l.lastSeen[k] {
					shouldFlush = true
					l.lastSeen[k] = status
				}
			}
		}
	}

	if !shouldFlush && time.Since(l.lastFlush[k]) > 15*time.Minute {
		shouldFlush = true
	}

	if shouldFlush && l.FlushFn != nil {
		l.lastFlush[k] = time.Now()
		go l.FlushFn(machineID, slug, payload)
	}
}

func (l *Layer) LiveState(machineID, slug string) json.RawMessage {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.live[stateKey{machineID, slug}]
}

func (l *Layer) AllLive() map[string]json.RawMessage {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make(map[string]json.RawMessage, len(l.live))
	for k, v := range l.live {
		out[k.machineID+"/"+k.slug] = v
	}
	return out
}

func (l *Layer) ForceFlush() {
	if l.FlushFn == nil {
		return
	}
	l.mu.Lock()
	snapshot := make(map[stateKey]json.RawMessage, len(l.live))
	now := time.Now()
	for k, v := range l.live {
		snapshot[k] = v
		l.lastFlush[k] = now
	}
	l.mu.Unlock()

	for k, v := range snapshot {
		l.FlushFn(k.machineID, k.slug, v)
	}
}
