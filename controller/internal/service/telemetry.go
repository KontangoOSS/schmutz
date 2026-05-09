package service

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/vmihailenco/msgpack/v5"
)

// MachineStatus tracks the enrollment state of a machine.
type MachineStatus string

const (
	StatusUnclaimed  MachineStatus = "unclaimed"  // heartbeating, no identity issued
	StatusQuarantine MachineStatus = "quarantine" // enrolled, awaiting approval
	StatusEnrolled   MachineStatus = "enrolled"   // fully on the mesh
)

// Event is the universal telemetry envelope published by agents.
// All messages — heartbeats, net scans, log entries — use this wrapper.
// Wire format: zlib-compressed JSON with short keys {"m","t","ts","d"}.
type Event struct {
	MachineID string          `json:"m"`
	Type      string          `json:"t"`
	Timestamp int64           `json:"ts"`
	Data      json.RawMessage `json:"d"`
}

// DecodeEvent decompresses and deserialises an agent telemetry payload.
// Accepts both zlib-compressed (new agents) and raw JSON (legacy agents).
func DecodeEvent(b []byte) (*Event, error) {
	var raw []byte
	if r, err := zlib.NewReader(bytes.NewReader(b)); err == nil {
		raw, err = io.ReadAll(r)
		r.Close()
		if err != nil {
			return nil, err
		}
	} else {
		raw = b
	}
	var ev Event
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil, err
	}
	return &ev, nil
}

// Heartbeat is the payload received from agents over telemetry.tango or /api/heartbeat.
// Agents wrap this in an Event with Type "heartbeat"; the controller unwraps it.
type Heartbeat struct {
	MachineID  string        `json:"mid"`
	Hostname   string        `json:"host"`
	OS         string        `json:"os"`
	Arch       string        `json:"arch"`
	UptimeSecs int64         `json:"up"`
	CPUCores   int           `json:"cpu"`
	MemoryMB   int64         `json:"mem,omitempty"`
	LoadAvg1   float64       `json:"load,omitempty"`
	Nickname   string        `json:"nick,omitempty"`
	State      string        `json:"state,omitempty"`
	Profile    string        `json:"profile,omitempty"`
	Timestamp  int64         `json:"ts"`
	Status     MachineStatus `json:"status,omitempty"`
	// Set by the controller from the Ziti connection — not agent-supplied.
	IdentityName string    `json:"-"`
	IdentityID   string    `json:"-"`
	SeenAt       time.Time `json:"seen_at"`
}

// HeartbeatResponse is returned to agents on every POST /api/heartbeat.
// Normally {"cmd":"noop"}. When the controller has something to say, it carries
// a command that the agent should act on immediately.
type HeartbeatResponse struct {
	Cmd     string          `json:"cmd"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Instruction is a message pushed down a config channel to an agent.
type Instruction struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// configConn represents an agent's open config.tango connection.
type configConn struct {
	machineID string
	send      chan []byte // controller writes instructions here
	done      chan struct{}
}

// recentEvent is a time-stamped raw event stored per machine per type.
// We keep the last N events in a ring buffer.
type recentEvent struct {
	ReceivedAt time.Time       `json:"received_at"`
	Data       json.RawMessage `json:"data"`
}

const maxRecentEvents = 50

// KV is a flat map of string keys to string values. No schema.
// Keys use dot notation for namespacing: "kore/ticketarr.status"
type KV map[string]string

// System key registry — same as agent. Position = metric.
const (
	SysHostname = iota
	SysOS
	SysArch
	SysCPUs
	SysLoad // load * 100 as uint16
	SysMemMB
	SysUptime
	SysNick
	SysState
	SysProfile
	SysMemAvail // 10
)

// DecodeMsgpackKV decodes a MessagePack payload — handles both
// arrays (system pulses, compact) and maps (app pulses, flexible).
func DecodeMsgpackKV(data []byte) (KV, error) {
	// Try map first (app pulses)
	var kv KV
	if err := msgpack.Unmarshal(data, &kv); err == nil && len(kv) > 0 {
		return kv, nil
	}

	// Try array (system pulses — positional key registry)
	var arr []interface{}
	if err := msgpack.Unmarshal(data, &arr); err != nil {
		return nil, err
	}
	if len(arr) == 0 {
		return nil, fmt.Errorf("empty array")
	}

	result := make(KV)
	get := func(i int) string {
		if i >= len(arr) || arr[i] == nil {
			return ""
		}
		return fmt.Sprintf("%v", arr[i])
	}
	if v := get(SysHostname); v != "" { result["hostname"] = v }
	if v := get(SysOS); v != "" { result["os"] = v }
	if v := get(SysArch); v != "" { result["arch"] = v }
	if v := get(SysCPUs); v != "" { result["cpus"] = v }
	if v := get(SysLoad); v != "" {
		// Decode load * 100 back to float
		if n, ok := toUint(arr[SysLoad]); ok {
			result["load"] = fmt.Sprintf("%.2f", float64(n)/100)
		} else {
			result["load"] = v
		}
	}
	if v := get(SysMemMB); v != "" { result["mem_mb"] = v }
	if v := get(SysMemAvail); v != "" { result["mem_avail_mb"] = v }
	if v := get(SysUptime); v != "" { result["up"] = v }
	if v := get(SysNick); v != "" { result["nick"] = v }
	if v := get(SysState); v != "" { result["state"] = v }
	if v := get(SysProfile); v != "" { result["profile"] = v }
	return result, nil
}

func toUint(v interface{}) (uint64, bool) {
	switch n := v.(type) {
	case uint8: return uint64(n), true
	case uint16: return uint64(n), true
	case uint32: return uint64(n), true
	case uint64: return n, true
	case int8: return uint64(n), true
	case int16: return uint64(n), true
	case int32: return uint64(n), true
	case int64: return uint64(n), true
	default: return 0, false
	}
}

// machineState holds all KV pairs for a machine, keyed by tag+key.
type machineState struct {
	KV       map[string]string // "tag:key" → value
	LastSeen time.Time
}

// TelemetryService holds live machine state in memory.
// Pulse-based — each machine has a KV map updated by incoming pulses.
type TelemetryService struct {
	mu      sync.RWMutex
	beats   map[string]*Heartbeat               // machineID → last heartbeat (legacy)
	machines map[string]*machineState            // machineID → live KV state
	configs map[string]*configConn              // machineID → open config channel
	pending map[string]*HeartbeatResponse       // machineID → next response to deliver
	events  map[string]map[string][]recentEvent // machineID → type → ring
}

func NewTelemetryService() *TelemetryService {
	return &TelemetryService{
		beats:    make(map[string]*Heartbeat),
		machines: make(map[string]*machineState),
		configs:  make(map[string]*configConn),
		pending:  make(map[string]*HeartbeatResponse),
		events:   make(map[string]map[string][]recentEvent),
	}
}

// RecordEvent routes an incoming Event envelope to the appropriate handler.
func (t *TelemetryService) RecordEvent(ev *Event) {
	if ev.MachineID == "" {
		return
	}
	switch ev.Type {
	case "hb", "heartbeat":
		var hb Heartbeat
		if err := json.Unmarshal(ev.Data, &hb); err != nil {
			return
		}
		if hb.MachineID == "" {
			hb.MachineID = ev.MachineID
		}
		t.RecordHeartbeat(&hb)
	default:
		t.mu.Lock()
		if t.events[ev.MachineID] == nil {
			t.events[ev.MachineID] = make(map[string][]recentEvent)
		}
		ring := t.events[ev.MachineID][ev.Type]
		ring = append(ring, recentEvent{ReceivedAt: time.Now(), Data: ev.Data})
		if len(ring) > maxRecentEvents {
			ring = ring[len(ring)-maxRecentEvents:]
		}
		t.events[ev.MachineID][ev.Type] = ring
		t.mu.Unlock()
	}
}

// RecordKV merges a KV map into a machine's live state. Last value wins.
func (t *TelemetryService) RecordKV(machineID string, kv KV) {
	t.mu.Lock()
	defer t.mu.Unlock()

	ms, ok := t.machines[machineID]
	if !ok {
		ms = &machineState{KV: make(map[string]string)}
		t.machines[machineID] = ms
	}
	ms.LastSeen = time.Now()

	for k, v := range kv {
		ms.KV[k] = v
	}
}

// MachineKV returns the live KV state for a machine.
func (t *TelemetryService) MachineKV(machineID string) (map[string]string, time.Time) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	ms, ok := t.machines[machineID]
	if !ok {
		return nil, time.Time{}
	}
	// Copy to avoid holding lock
	kv := make(map[string]string, len(ms.KV))
	for k, v := range ms.KV {
		kv[k] = v
	}
	return kv, ms.LastSeen
}

// AllMachineKV returns the live KV state for all machines.
func (t *TelemetryService) AllMachineKV() map[string]map[string]string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make(map[string]map[string]string, len(t.machines))
	for id, ms := range t.machines {
		kv := make(map[string]string, len(ms.KV))
		for k, v := range ms.KV {
			kv[k] = v
		}
		result[id] = kv
	}
	return result
}

// RecentEvents returns the last N events of a given type for a machine.
func (t *TelemetryService) RecentEvents(machineID, evType string) []recentEvent {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if m, ok := t.events[machineID]; ok {
		return m[evType]
	}
	return nil
}

// RecordHeartbeat stores the latest heartbeat for a machine.
// If the machine has no prior record it is marked unclaimed.
func (t *TelemetryService) RecordHeartbeat(hb *Heartbeat) {
	hb.SeenAt = time.Now()
	t.mu.Lock()
	existing, seen := t.beats[hb.MachineID]
	if hb.Status == "" {
		if seen {
			hb.Status = existing.Status
		} else {
			hb.Status = StatusUnclaimed
		}
	}
	t.beats[hb.MachineID] = hb
	t.mu.Unlock()
}

// NextResponse returns the pending command for a machine and clears it.
// Returns a noop response if nothing is queued.
func (t *TelemetryService) NextResponse(machineID string) HeartbeatResponse {
	t.mu.Lock()
	defer t.mu.Unlock()
	if r, ok := t.pending[machineID]; ok {
		delete(t.pending, machineID)
		return *r
	}
	return HeartbeatResponse{Cmd: "noop"}
}

// QueueCommand queues a command to be delivered on the machine's next heartbeat.
// Overwrites any existing queued command for that machine.
func (t *TelemetryService) QueueCommand(machineID string, resp HeartbeatResponse) {
	t.mu.Lock()
	t.pending[machineID] = &resp
	t.mu.Unlock()
}

// SetStatus updates the enrollment status for a machine.
func (t *TelemetryService) SetStatus(machineID string, status MachineStatus) {
	t.mu.Lock()
	if hb, ok := t.beats[machineID]; ok {
		hb.Status = status
	}
	t.mu.Unlock()
}

// LiveMachines returns all machines with a heartbeat in the last ttl duration.
func (t *TelemetryService) LiveMachines(ttl time.Duration) []*Heartbeat {
	cutoff := time.Now().Add(-ttl)
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]*Heartbeat, 0, len(t.beats))
	for _, hb := range t.beats {
		if hb.SeenAt.After(cutoff) {
			out = append(out, hb)
		}
	}
	return out
}

// LastHeartbeat returns the most recent heartbeat for a specific machine.
func (t *TelemetryService) LastHeartbeat(machineID string) (*Heartbeat, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	hb, ok := t.beats[machineID]
	return hb, ok
}

// RegisterConfigConn registers an open config channel for a machine.
// Returns a receive-only channel the loop should read instructions from,
// and a done channel to signal when the connection closes.
func (t *TelemetryService) RegisterConfigConn(machineID string) (<-chan []byte, chan struct{}) {
	conn := &configConn{
		machineID: machineID,
		send:      make(chan []byte, 16),
		done:      make(chan struct{}),
	}
	t.mu.Lock()
	// Close any existing connection for this machine before replacing.
	if old, ok := t.configs[machineID]; ok {
		select {
		case <-old.done:
		default:
			close(old.done)
		}
	}
	t.configs[machineID] = conn
	t.mu.Unlock()
	return conn.send, conn.done
}

// UnregisterConfigConn removes the config channel for a machine.
func (t *TelemetryService) UnregisterConfigConn(machineID string) {
	t.mu.Lock()
	delete(t.configs, machineID)
	t.mu.Unlock()
}

// Push sends an instruction to a connected machine over its config channel.
// Returns an error if the machine has no open config channel.
func (t *TelemetryService) Push(machineID string, instr Instruction) error {
	t.mu.RLock()
	conn, ok := t.configs[machineID]
	t.mu.RUnlock()
	if !ok {
		return fmt.Errorf("machine %s has no active config channel", machineID)
	}
	payload, err := json.Marshal(instr)
	if err != nil {
		return err
	}
	select {
	case conn.send <- append(payload, '\n'):
		return nil
	default:
		return fmt.Errorf("machine %s config channel is full", machineID)
	}
}

// ConnectedMachines returns the IDs of machines with open config channels.
func (t *TelemetryService) ConnectedMachines() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	ids := make([]string, 0, len(t.configs))
	for id := range t.configs {
		ids = append(ids, id)
	}
	return ids
}
