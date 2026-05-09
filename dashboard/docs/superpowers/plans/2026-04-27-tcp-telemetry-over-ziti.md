# TCP Telemetry over Ziti — Remove NATS Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the embedded NATS broker and NATS client with raw TCP streams over Ziti, using the already-designed `internal/stream` wire protocol (`schmutz` repo). Three repos change in lockstep: `schmutz` (agent), `schmutz-controller` (server), `ziti-dash` (dashboard).

**Architecture:** Each enrolled agent dials `telemetry.tango` — a Ziti service hosted by the controller. The connection carries length-prefixed, zstd-compressed, typed frames (the existing `stream.Encoder`/`Decoder`). The controller fans received frames into its in-process `TelemetryService`. `ziti-dash` also dials `telemetry.tango` and fans frames to browser clients via SSE (`text/event-stream`). NATS disappears from all three repos. `config.tango` stays unchanged.

**Wire format (already exists in `schmutz/src/internal/stream/protocol.go`):**
```
[1 byte: MsgType][4 bytes: payload_len BE][8 bytes: ts_nanos BE][payload_len bytes: zstd(JSON)]
```
Types: `0x01`=heartbeat, `0x02`=system, `0x03`=network, `0x04`=process, `0x05`=disk, `0x06`=service, `0x07`=log, `0x08`=event

**Tech Stack:** Go 1.25/1.26, `openziti/sdk-golang`, `klauspost/compress/zstd` (already in schmutz), `shirou/gopsutil/v3` (already in schmutz). No new dependencies in any repo.

**Repos:**
- `~/git/kore/schmutz/src/` — module `github.com/KontangoOSS/schmutz`
- `~/git/kore/schmutz-controller/src/` — module `github.com/KontangoOSS/schmutz-controller`
- `~/git/kore/ziti-dash/` — module `git.kontango.io/kore/ziti-dash`

---

## File Map

### schmutz (agent)
**Created:**
- `src/internal/telemetry/dialer.go` — dials `telemetry.tango`, reconnects with backoff, runs encode loop

**Modified:**
- `src/cmd/schmutz/main.go` — start `telemetry.Dialer` goroutine alongside `agent.Run()`
- `src/internal/collector/collector.go` — add `CollectAll()` convenience function
- `src/agent/host.go` — remove `"nats-"` service port mapping

### schmutz-controller
**Created:**
- `src/internal/service/tcptelemetry.go` — `TCPTelemetryService`: `net.Listener` on `telemetry.tango`, reads frames, dispatches to `TelemetryService`

**Modified:**
- `src/cmd/schmutz-controller/main.go` — replace NATS block with `TCPTelemetryService`, remove NATS imports
- `src/cmd/schmutz-controller/selfmonitor.go` — publish direct to `TelemetryService` (no NATS)
- `src/internal/controller/enroll/enroll.go` — remove `nats` from default service list
- `src/internal/service/nats.go` — **deleted**

### ziti-dash
**Created:**
- `internal/telemetry/listener.go` — dials `telemetry.tango` over Ziti, reads frames, fans to SSE hub
- `internal/telemetry/hub.go` — in-process SSE fan-out: register/unregister browser clients, broadcast frames
- `internal/telemetry/plugin.go` — BFF plugin: `GET /api/telemetry/stream` (SSE), `GET /api/telemetry/live` (snapshot)

**Deleted:**
- `internal/nats/subscriber.go` — entire package removed

**Modified:**
- `cmd/ziti-dash/main.go` — remove NATS wiring, add telemetry plugin
- `go.mod` — remove `nats-io/nats.go` direct dep
- `frontend/app.js` — replace polling `loadTelemetry()` with `EventSource` SSE connection

---

## Task 1: Add `CollectAll()` to schmutz collector + wire `telemetry.Dialer` in agent start

**Why first:** The agent is the leaf — no other code depends on it. We can build and test the agent side independently.

**Repo:** `~/git/kore/schmutz/src/`

**Files:**
- Modify: `internal/collector/collector.go`
- Create: `internal/telemetry/dialer.go`
- Modify: `cmd/schmutz/main.go`
- Modify: `agent/host.go`

- [ ] **Step 1: Add `CollectAll()` to `src/internal/collector/collector.go`**

Append to the end of the existing file:

```go
// Snapshot is a full point-in-time collection of all telemetry data.
type Snapshot struct {
	System  *stream.SystemData  `json:"system,omitempty"`
	Network *stream.NetworkData `json:"network,omitempty"`
	Disk    *stream.DiskData    `json:"disk,omitempty"`
	Process *stream.ProcessData `json:"process,omitempty"`
}

// CollectAll gathers all available telemetry in one call.
// Partial failures are silently skipped — best effort.
func CollectAll() *Snapshot {
	s := &Snapshot{}
	s.System, _ = CollectSystem()
	s.Network, _ = CollectNetwork()
	s.Disk, _ = CollectDisk()
	s.Process, _ = CollectProcess()
	return s
}
```

- [ ] **Step 2: Create `src/internal/telemetry/dialer.go`**

```go
// Package telemetry dials telemetry.tango and streams typed frames to the controller.
package telemetry

import (
	"log"
	"net"
	"time"

	"github.com/KontangoOSS/schmutz/internal/collector"
	"github.com/KontangoOSS/schmutz/internal/stream"
	"github.com/openziti/sdk-golang/ziti"
)

// Dialer connects to telemetry.tango and publishes telemetry frames on a fixed interval.
// It reconnects automatically with exponential backoff on any failure.
type Dialer struct {
	identityPath string
	interval     time.Duration
	stop         chan struct{}
}

// NewDialer creates a Dialer using the given Ziti identity file.
// interval is how often a full telemetry snapshot is sent.
func NewDialer(identityPath string, interval time.Duration) *Dialer {
	return &Dialer{
		identityPath: identityPath,
		interval:     interval,
		stop:         make(chan struct{}),
	}
}

// Run starts the dial-and-send loop. Blocks until Stop() is called.
func (d *Dialer) Run() {
	backoff := 5 * time.Second
	const maxBackoff = 5 * time.Minute

	for {
		select {
		case <-d.stop:
			return
		default:
		}

		if err := d.runOnce(); err != nil {
			log.Printf("telemetry: disconnected (%v) — retry in %s", err, backoff)
			select {
			case <-d.stop:
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, maxBackoff)
			continue
		}
		backoff = 5 * time.Second
	}
}

// Stop signals the dialer to exit cleanly.
func (d *Dialer) Stop() {
	close(d.stop)
}

func (d *Dialer) runOnce() error {
	cfg, err := ziti.NewConfigFromFile(d.identityPath)
	if err != nil {
		return err
	}
	ctx, err := ziti.NewContext(cfg)
	if err != nil {
		return err
	}
	defer ctx.Close()

	conn, err := ctx.Dial("telemetry.tango")
	if err != nil {
		return err
	}
	defer conn.Close()

	log.Printf("telemetry: connected to telemetry.tango")

	enc, err := stream.NewEncoder(conn)
	if err != nil {
		return err
	}

	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	// Send an immediate heartbeat on connect so the controller knows we're alive.
	if err := enc.Send(stream.MsgHeartbeat, nil); err != nil {
		return err
	}

	for {
		select {
		case <-d.stop:
			return nil
		case <-ticker.C:
			if err := d.sendSnapshot(enc, conn); err != nil {
				return err
			}
		}
	}
}

func (d *Dialer) sendSnapshot(enc *stream.Encoder, conn net.Conn) error {
	snap := collector.CollectAll()

	if snap.System != nil {
		if err := enc.Send(stream.MsgSystem, snap.System); err != nil {
			return err
		}
	}
	if snap.Network != nil {
		if err := enc.Send(stream.MsgNetwork, snap.Network); err != nil {
			return err
		}
	}
	if snap.Disk != nil {
		if err := enc.Send(stream.MsgDisk, snap.Disk); err != nil {
			return err
		}
	}
	if snap.Process != nil {
		if err := enc.Send(stream.MsgProcess, snap.Process); err != nil {
			return err
		}
	}

	return nil
}

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
```

- [ ] **Step 3: Wire the dialer into `src/cmd/schmutz/main.go` `startCmd()`**

In `startCmd()`, after `log.Printf("schmutz: starting — slug=%s services=%v", slug, services)` and before `return a.Run()`, add:

```go
// Start telemetry stream over Ziti.
tel := telemetry.NewDialer(identityPath, 30*time.Second)
go tel.Run()
defer tel.Stop()
```

Add import at top of file:
```go
"time"
"github.com/KontangoOSS/schmutz/internal/telemetry"
```

- [ ] **Step 4: Remove `nats-` from `src/agent/host.go` service port map**

In `servicePort` map, remove this entry:
```go
"nats-":  "127.0.0.1:4222",
```

- [ ] **Step 5: Build**

```bash
cd ~/git/kore/schmutz/src && go build ./...
```

Expected: no errors. The telemetry package imports are resolved via the existing openziti/sdk-golang dep.

- [ ] **Step 6: Run tests**

```bash
cd ~/git/kore/schmutz/src && go test ./internal/stream/... ./internal/collector/... -v 2>&1 | tail -20
```

Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
cd ~/git/kore/schmutz
git add src/internal/telemetry/ src/internal/collector/collector.go src/cmd/schmutz/main.go src/agent/host.go
git commit -m "feat: add telemetry.tango dialer — stream typed frames over Ziti, remove nats- service"
```

No Co-Authored-By. No Claude references.

---

## Task 2: Replace NATS with `TCPTelemetryService` in schmutz-controller

**Why:** Controller side — replace the NATS embedded broker with a `telemetry.tango` listener. The controller already has `ZitiTransport.ListenEdge()` which gives us `GetDialerIdentityName()` so we know which machine each connection is from. The existing `TelemetryService` (`internal/service/telemetry.go`) already handles in-memory storage — we just feed it differently.

**Repo:** `~/git/kore/schmutz-controller/src/`

**Files:**
- Create: `internal/service/tcptelemetry.go`
- Modify: `cmd/schmutz-controller/main.go`
- Modify: `cmd/schmutz-controller/selfmonitor.go`
- Modify: `internal/controller/enroll/enroll.go`
- Delete: `internal/service/nats.go`

- [ ] **Step 1: Read `src/internal/service/telemetry.go` to understand `TelemetryService` API**

```bash
grep -n "^func\|^type" ~/git/kore/schmutz-controller/src/internal/service/telemetry.go | head -30
```

Specifically find: how to call `RecordEvent`, `RecordHeartbeat`, and what the `Event`/`Heartbeat` structs look like.

- [ ] **Step 2: Create `src/internal/service/tcptelemetry.go`**

```go
// Package service — TCPTelemetryService replaces the embedded NATS broker.
// It accepts raw TCP connections on a net.Listener (backed by a Ziti edge listener),
// reads typed stream frames, and dispatches them into TelemetryService.
package service

import (
	"encoding/json"
	"io"
	"log"
	"net"

	"github.com/KontangoOSS/schmutz-controller/internal/stream"
	"github.com/openziti/sdk-golang/ziti/edge"
)

// TCPTelemetryService accepts telemetry connections on a Ziti edge listener
// and fans frames into the in-process TelemetryService.
type TCPTelemetryService struct {
	listener edge.Listener
	tel      *TelemetryService
	stop     chan struct{}
}

// NewTCPTelemetryService creates a service that reads from ln (a Ziti edge listener)
// and dispatches frames to tel.
func NewTCPTelemetryService(ln edge.Listener, tel *TelemetryService) *TCPTelemetryService {
	return &TCPTelemetryService{
		listener: ln,
		tel:      tel,
		stop:     make(chan struct{}),
	}
}

// Start begins accepting connections. Non-blocking — runs in background goroutines.
func (s *TCPTelemetryService) Start() {
	go s.acceptLoop()
	log.Printf("tcptelemetry: listening on telemetry.tango")
}

// Stop closes the listener and signals all goroutines to exit.
func (s *TCPTelemetryService) Stop() {
	close(s.stop)
	s.listener.Close()
}

func (s *TCPTelemetryService) acceptLoop() {
	for {
		conn, err := s.listener.AcceptEdge()
		if err != nil {
			select {
			case <-s.stop:
				return
			default:
				log.Printf("tcptelemetry: accept: %v", err)
				return
			}
		}
		go s.handleConn(conn)
	}
}

func (s *TCPTelemetryService) handleConn(conn edge.Conn) {
	defer conn.Close()

	machineID := conn.GetDialerIdentityName()
	log.Printf("tcptelemetry: connected: %s", machineID)

	dec, err := stream.NewDecoder(conn)
	if err != nil {
		log.Printf("tcptelemetry: decoder: %v", err)
		return
	}

	for {
		select {
		case <-s.stop:
			return
		default:
		}

		msg, err := dec.Receive()
		if err != nil {
			if err != io.EOF {
				log.Printf("tcptelemetry: %s: read: %v", machineID, err)
			}
			return
		}

		s.dispatch(machineID, msg)
	}
}

func (s *TCPTelemetryService) dispatch(machineID string, msg *stream.Message) {
	slug := stream.MsgName(msg.Type)

	// Build an Event envelope matching the existing TelemetryService contract.
	ev := &Event{
		MachineID: machineID,
		Type:      slug,
		Timestamp: msg.Timestamp.Unix(),
		Data:      json.RawMessage(msg.Payload),
	}
	s.tel.RecordEvent(ev)
}

// RecordSelf allows the controller's selfmonitor to publish directly
// without going through a network socket.
func (s *TCPTelemetryService) RecordSelf(machineID, slug string, data interface{}) {
	payload, err := json.Marshal(data)
	if err != nil {
		return
	}
	ev := &Event{
		MachineID: machineID,
		Type:      slug,
		Data:      json.RawMessage(payload),
	}
	s.tel.RecordEvent(ev)
}

// PublishRaw is a no-op compatibility shim so call sites that used
// NATSService.Publish() compile without changes during the migration.
func (s *TCPTelemetryService) PublishRaw(subject string, data []byte) {}
```

Note: This requires the `stream` package from the schmutz repo. Check if schmutz-controller already has the stream package or if it needs to be copied. Run:

```bash
grep -r "stream" ~/git/kore/schmutz-controller/src/go.mod
find ~/git/kore/schmutz-controller/src -path "*/stream/*.go" | head -5
```

If the stream package doesn't exist in schmutz-controller, copy it from schmutz:

```bash
mkdir -p ~/git/kore/schmutz-controller/src/internal/stream
cp ~/git/kore/schmutz/src/internal/stream/protocol.go ~/git/kore/schmutz-controller/src/internal/stream/protocol.go
# Update the package path in the copied file if needed
head -3 ~/git/kore/schmutz-controller/src/internal/stream/protocol.go
```

The package declaration stays `package stream`. Adjust the import path to match the controller module: `github.com/KontangoOSS/schmutz-controller/internal/stream`.

- [ ] **Step 3: Update `selfmonitor.go` to use `TCPTelemetryService` directly**

Read `selfmonitor.go`:
```bash
cat ~/git/kore/schmutz-controller/src/cmd/schmutz-controller/selfmonitor.go
```

Replace the function signature and NATS publish with direct `RecordSelf` call:

Change:
```go
func startSelfMonitor(nats *service.NATSService, store *service.StoreService, ziti *service.ZitiService, nodeName string) {
```

To:
```go
func startSelfMonitor(tel *service.TCPTelemetryService, store *service.StoreService, ziti *service.ZitiService, nodeName string) {
```

Replace the `nats.Publish(subject, data)` call at the end of `publishControllerPulse` with:

```go
machineID := "controller-" + nodeName
tel.RecordSelf(machineID, "system", kv)
```

Remove the `msgpack` import if it's only used for NATS publishing. Remove the `subject` variable.

- [ ] **Step 4: Update `main.go` — replace NATS block with TCPTelemetryService**

Read the NATS block in main.go:
```bash
grep -n "nats\|NATS\|natsSvc\|NATSService" ~/git/kore/schmutz-controller/src/cmd/schmutz-controller/main.go | head -20
```

Find the block that starts with `// NATS bus` and replace it entirely with:

```go
// TCP telemetry — agents dial telemetry.tango and stream typed frames directly.
if telLn, err := zt.ListenEdge("telemetry.tango"); err != nil {
    log.Printf("tcptelemetry: listener failed: %v", err)
} else {
    tcpTel := service.NewTCPTelemetryService(telLn, telSvc)
    tcpTel.Start()
    defer tcpTel.Stop()
    startSelfMonitor(tcpTel, store, ziti, cfg.NodeName)
    log.Printf("tcptelemetry: listening on telemetry.tango")
}
```

Remove `natsSvc` from the `api` struct initialization and the `nats` field from the API struct. Remove all `natsclient` imports.

- [ ] **Step 5: Remove `nats` from enrolled service list in `enroll.go`**

In `createZitiBasic()`, find and remove:
```go
{name: "nats", port: 4222, attrs: []string{"nats-" + nickname, "nats-services"}, dial: svcDialAttrs},
```

- [ ] **Step 6: Remove `internal/service/nats.go`**

```bash
rm ~/git/kore/schmutz-controller/src/internal/service/nats.go
```

- [ ] **Step 7: Remove NATS from go.mod**

```bash
cd ~/git/kore/schmutz-controller/src
go mod tidy
```

Verify `nats-io/nats-server` and `nats-io/nats.go` and `nats-io/jetstream` are gone from go.mod.

- [ ] **Step 8: Build**

```bash
cd ~/git/kore/schmutz-controller/src && go build ./...
```

Expected: no errors. Fix any remaining compile errors from leftover NATS references:
```bash
grep -rn "natsSvc\|NATSService\|nats\." ~/git/kore/schmutz-controller/src/cmd/ --include="*.go" | grep -v "_test\|// " | head -20
```

- [ ] **Step 9: Run tests**

```bash
cd ~/git/kore/schmutz-controller/src && go test ./... 2>&1 | tail -20
```

Expected: all PASS (nats_test.go is deleted, remaining tests pass).

- [ ] **Step 10: Commit**

```bash
cd ~/git/kore/schmutz-controller
git add src/internal/service/tcptelemetry.go src/internal/stream/ src/cmd/schmutz-controller/ src/internal/controller/enroll/enroll.go src/go.mod src/go.sum
git rm src/internal/service/nats.go src/internal/service/nats_test.go 2>/dev/null || true
git commit -m "feat: replace NATS with raw TCP on telemetry.tango, remove nats from enrollment services"
```

No Co-Authored-By. No Claude references.

---

## Task 3: Create `telemetry.tango` Ziti service in the controller's enrollment flow

**Why:** The agent now dials `telemetry.tango`, but that service doesn't exist in Ziti yet. We need to create it during enrollment (or on controller startup) with the right policies so enrolled machines can dial it and the controller can host it.

**Repo:** `~/git/kore/schmutz-controller/src/`

**Files:**
- Modify: `internal/controller/enroll/ziti_services.go`
- Modify: `cmd/schmutz-controller/main.go`

- [ ] **Step 1: Read how `createProgressService` works to understand the pattern**

```bash
cat ~/git/kore/schmutz-controller/src/internal/controller/enroll/ziti_services.go
```

- [ ] **Step 2: Add `EnsureTelemetryService()` to `ziti_services.go`**

Append to `ziti_services.go`:

```go
// EnsureTelemetryService creates the telemetry.tango Ziti service if it doesn't exist.
// This is a controller-hosted service — enrolled machines dial it, controller listens.
// Idempotent: safe to call on every startup.
//
// Policy: any identity with role "#enrolled" can dial; the controller identity hosts.
func EnsureTelemetryService(c *common.Clients, token string) {
	const svcName = "telemetry.tango"
	const dialRole = "#enrolled"

	if c.Ziti.GetServiceIDByName(token, svcName) != "" {
		return // already exists
	}

	// Create the service (no host/intercept configs needed — raw Ziti bind/dial)
	svcID := c.Ziti.CreateService(token, svcName, []string{"telemetry-services"})
	if svcID == "" {
		log.Printf("telemetry-svc: failed to create service %s", svcName)
		return
	}

	// Service policy — enrolled machines can dial
	c.Ziti.CreateServicePolicy(token, svcName+"-dial", "Dial",
		[]string{dialRole}, []string{"@" + svcName})

	// Service policy — controller can bind (host) the service
	// Controller identity name is the Ziti identity that the controller enrolled with.
	c.Ziti.CreateServicePolicy(token, svcName+"-bind", "Bind",
		[]string{"#controllers"}, []string{"@" + svcName})

	log.Printf("telemetry-svc: created %s (dial=%s)", svcName, dialRole)
}
```

- [ ] **Step 3: Wire `EnsureTelemetryService` into controller startup in `main.go`**

After the Ziti transport is initialized and the admin token is obtained, add a call to ensure the service exists. Find where `agentmod.StartConfigListener` is called and add before it:

```go
// Ensure telemetry.tango service exists in Ziti
enrollmod.EnsureTelemetryService(clients, adminToken)
```

- [ ] **Step 4: Add `CreateService` and `CreateServicePolicy` helpers if they don't exist**

Check what Ziti helpers exist:
```bash
grep -n "^func.*CreateService\|^func.*ServicePolicy" ~/git/kore/schmutz-controller/src/internal/service/ziti.go | head -10
```

If `CreateService` doesn't exist, add to `service/ziti.go`:

```go
// CreateService creates a plain Ziti service with no configs and returns its ID.
// Returns empty string on failure.
func (z *ZitiService) CreateService(token, name string, attrs []string) string {
	// Use the existing REST helper pattern in this file
	// (mirror how other create calls work in ziti.go)
	id := z.createServiceREST(token, name, attrs)
	return id
}
```

Look at the existing pattern in `ziti.go` and `ziti_extended.go` — mirror whatever helper is used by `createZitiBasic`. The pattern is well-established in the codebase.

- [ ] **Step 5: Build**

```bash
cd ~/git/kore/schmutz-controller/src && go build ./...
```

- [ ] **Step 6: Commit**

```bash
cd ~/git/kore/schmutz-controller
git add src/internal/controller/enroll/ziti_services.go src/cmd/schmutz-controller/main.go src/internal/service/ziti.go
git commit -m "feat: create telemetry.tango Ziti service on controller startup"
```

No Co-Authored-By. No Claude references.

---

## Task 4: Remove NATS from ziti-dash, add telemetry SSE plugin

**Why:** ziti-dash currently subscribes to NATS to receive telemetry. Replace with a Ziti dial to `telemetry.tango` + in-process SSE fan-out. The existing polling `loadTelemetry()` stays as fallback but the primary path becomes `EventSource`.

**Repo:** `~/git/kore/ziti-dash/`

**Files:**
- Create: `internal/telemetry/hub.go`
- Create: `internal/telemetry/listener.go`
- Create: `internal/telemetry/plugin.go`
- Modify: `cmd/ziti-dash/main.go`
- Modify: `go.mod` (remove nats deps)
- Modify: `frontend/app.js` (add SSE EventSource)
- Delete: `internal/nats/subscriber.go`

- [ ] **Step 1: Create `internal/telemetry/hub.go`**

```go
// Package telemetry provides SSE fan-out for live telemetry frames.
package telemetry

import (
	"encoding/json"
	"sync"
)

// Frame is a decoded telemetry message ready for SSE delivery.
type Frame struct {
	MachineID string          `json:"machine_id"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	Timestamp int64           `json:"ts"`
}

// Hub distributes incoming telemetry frames to all registered SSE clients.
type Hub struct {
	mu      sync.RWMutex
	clients map[chan Frame]struct{}

	// Snapshot: last frame per machine+type for polling fallback.
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

// Subscribe registers a client channel. The caller must call Unsubscribe when done.
func (h *Hub) Subscribe() chan Frame {
	ch := make(chan Frame, 64)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

// Unsubscribe removes a client channel.
func (h *Hub) Unsubscribe(ch chan Frame) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
	close(ch)
}

// Publish sends a frame to all subscribers and updates the snapshot.
func (h *Hub) Publish(f Frame) {
	// Update snapshot
	h.snapMu.Lock()
	h.snapshot[f.MachineID+"/"+f.Type] = f.Payload
	h.snapMu.Unlock()

	// Fan out
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
```

- [ ] **Step 2: Create `internal/telemetry/listener.go`**

```go
package telemetry

import (
	"crypto/tls"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"git.kontango.io/kore/ziti-dash/pkg/browzer/ziti"
	sdkziti "github.com/openziti/sdk-golang/ziti"
)

// Listener dials telemetry.tango over Ziti and publishes received frames to Hub.
// It reconnects automatically with backoff on disconnect.
type Listener struct {
	identityPath string
	hub          *Hub
	stop         chan struct{}
}

// NewListener creates a listener using the given Ziti identity file.
func NewListener(identityPath string, hub *Hub) *Listener {
	return &Listener{
		identityPath: identityPath,
		hub:          hub,
		stop:         make(chan struct{}),
	}
}

// Run starts the dial loop. Blocks until Stop() is called.
func (l *Listener) Run() {
	backoff := 10 * time.Second
	const maxBackoff = 5 * time.Minute

	for {
		select {
		case <-l.stop:
			return
		default:
		}

		if err := l.runOnce(); err != nil {
			log.Printf("telemetry listener: disconnected (%v) — retry in %s", err, backoff)
			select {
			case <-l.stop:
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, maxBackoff)
			continue
		}
		backoff = 10 * time.Second
	}
}

// Stop signals the listener to exit.
func (l *Listener) Stop() {
	close(l.stop)
}

func (l *Listener) runOnce() error {
	cfg, err := sdkziti.NewConfigFromFile(l.identityPath)
	if err != nil {
		return err
	}
	ctx, err := sdkziti.NewContext(cfg)
	if err != nil {
		return err
	}
	defer ctx.Close()

	conn, err := ctx.Dial("telemetry.tango")
	if err != nil {
		return err
	}
	defer conn.Close()

	log.Printf("telemetry listener: connected to telemetry.tango")

	// Use the stream decoder from schmutz — but we can't import that module directly.
	// Instead read frames using the same wire format: [1B type][4B len][8B ts_nanos][payload]
	// This keeps ziti-dash dependency-free from the schmutz module.
	return readFrames(conn, l.hub, l.stop)
}

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// msgNames maps stream protocol type bytes to human-readable slugs.
var msgNames = map[uint8]string{
	0x01: "heartbeat",
	0x02: "system",
	0x03: "network",
	0x04: "process",
	0x05: "disk",
	0x06: "service",
	0x07: "log",
	0x08: "event",
}

// readFrames reads the stream wire format and publishes frames to hub.
// Wire: [1B type][4B len BE][8B ts_nanos BE][len bytes: zstd(JSON)]
func readFrames(r io.Reader, hub *Hub, stop chan struct{}) error {
	// Import zstd decoder
	// Using the klauspost/compress library which is already in go.mod via edge-api transitives
	// If not available, fall back to raw JSON (pre-compression path).
	// Check go.mod first: grep klauspost ~/git/kore/ziti-dash/go.mod
	//
	// For now implement without zstd — the stream encoder compresses, we need to decompress.
	// Add klauspost/compress to go.mod if not present.

	import_zstd_here := true // placeholder — see Step 3
	_ = import_zstd_here

	header := make([]byte, 13) // 1+4+8
	for {
		select {
		case <-stop:
			return nil
		default:
		}

		if _, err := io.ReadFull(r, header); err != nil {
			return err
		}

		msgType := header[0]
		payloadLen := uint32(header[1])<<24 | uint32(header[2])<<16 | uint32(header[3])<<8 | uint32(header[4])
		tsNanos := int64(header[5])<<56 | int64(header[6])<<48 | int64(header[7])<<40 | int64(header[8])<<32 |
			int64(header[9])<<24 | int64(header[10])<<16 | int64(header[11])<<8 | int64(header[12])

		compressed := make([]byte, payloadLen)
		if _, err := io.ReadFull(r, compressed); err != nil {
			return err
		}

		// Decompress zstd
		payload, err := decompressZstd(compressed)
		if err != nil {
			log.Printf("telemetry listener: decompress: %v (skipping frame)", err)
			continue
		}

		typeName := msgNames[msgType]
		if typeName == "" {
			typeName = "unknown"
		}

		// MachineID comes from the connection identity (set by the controller when
		// it relays frames). For now use a placeholder — Task 5 adds machineID injection.
		// The frame payload itself contains the machine data.
		_ = tsNanos
		hub.Publish(Frame{
			MachineID: "unknown", // refined in Task 5
			Type:      typeName,
			Payload:   json.RawMessage(payload),
			Timestamp: tsNanos / 1e9,
		})
	}
}

// httpClientViaZiti returns an http.Client that dials through Ziti.
// Kept for BFF compatibility — not used in the stream path.
func httpClientViaZiti(z *ziti.Client) *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
}
```

**Note on zstd:** After writing the file, check if `klauspost/compress` is already in go.mod:

```bash
grep klauspost ~/git/kore/ziti-dash/go.mod
```

If not, add it:
```bash
cd ~/git/kore/ziti-dash && go get github.com/klauspost/compress@latest
```

Then implement `decompressZstd` in a separate file `internal/telemetry/zstd.go`:

```go
package telemetry

import "github.com/klauspost/compress/zstd"

var zstdDecoder, _ = zstd.NewReader(nil)

func decompressZstd(compressed []byte) ([]byte, error) {
	return zstdDecoder.DecodeAll(compressed, nil)
}
```

- [ ] **Step 3: Create `internal/telemetry/plugin.go`**

```go
package telemetry

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"git.kontango.io/kore/ziti-dash/pkg/browzer/bff"
)

// Plugin is a BFF plugin exposing SSE stream and snapshot endpoints.
type Plugin struct {
	Hub *Hub
}

func (p *Plugin) Name() string        { return "telemetry-stream" }
func (p *Plugin) Description() string { return "Live telemetry via SSE from telemetry.tango" }

func (p *Plugin) Register(router *bff.Router) {
	// SSE stream — browser connects and receives live frames
	router.HandleRaw("GET /api/telemetry/stream", p.sseHandler)

	// Snapshot — existing polling fallback, now served from hub snapshot
	router.HandleRaw("GET /api/telemetry/live", p.snapshotHandler)
}

func (p *Plugin) sseHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ch := p.Hub.Subscribe()
	defer p.Hub.Unsubscribe(ch)

	// Send current snapshot immediately so the browser has data before the first frame
	snap := p.Hub.Snapshot()
	for key, payload := range snap {
		fmt.Fprintf(w, "data: {\"key\":%q,\"payload\":%s}\n\n", key, payload)
	}
	flusher.Flush()

	// Keepalive ticker — SSE connections die silently without periodic data
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		case frame, ok := <-ch:
			if !ok {
				return
			}
			b, err := json.Marshal(frame)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
		}
	}
}

func (p *Plugin) snapshotHandler(w http.ResponseWriter, r *http.Request) {
	snap := p.Hub.Snapshot()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(snap)
}
```

- [ ] **Step 4: Update `cmd/ziti-dash/main.go`**

Remove the NATS block:
```go
if natsURL := envOr("NATS_URL", "nats://nats.tango:4222"); natsURL != "" && st != nil {
    ...
}
```

Add telemetry listener + plugin. After `identityPath := os.Getenv("ZITI_IDENTITY")`:

```go
// Telemetry stream — dial telemetry.tango and fan frames to SSE clients
hub := telemetry.NewHub()
if identityPath := os.Getenv("ZITI_IDENTITY"); identityPath != "" {
    lis := telemetry.NewListener(identityPath, hub)
    go lis.Run()
    defer lis.Stop()
    log.Printf("telemetry: dialing telemetry.tango")
}
```

And add to `app.Use(...)` calls:
```go
a.Use(&telemetry.Plugin{Hub: hub})
```

Add import:
```go
"git.kontango.io/kore/ziti-dash/internal/telemetry"
```

Remove the `intnats` import.

- [ ] **Step 5: Delete the NATS subscriber**

```bash
rm ~/git/kore/ziti-dash/internal/nats/subscriber.go
rmdir ~/git/kore/ziti-dash/internal/nats/ 2>/dev/null || true
```

- [ ] **Step 6: Clean up go.mod**

```bash
cd ~/git/kore/ziti-dash && go mod tidy
```

Verify nats deps are gone:
```bash
grep nats ~/git/kore/ziti-dash/go.mod
```

Expected: no output.

- [ ] **Step 7: Build**

```bash
cd ~/git/kore/ziti-dash && go build ./...
```

Fix any remaining compile errors.

- [ ] **Step 8: Commit**

```bash
cd ~/git/kore/ziti-dash
git add internal/telemetry/ cmd/ziti-dash/main.go go.mod go.sum
git rm internal/nats/subscriber.go 2>/dev/null || true
git commit -m "feat: replace NATS subscriber with Ziti TCP stream listener and SSE fan-out"
```

No Co-Authored-By. No Claude references.

---

## Task 5: Inject machineID into frames — controller tags each connection

**Why:** In Task 4 the listener has `machineID: "unknown"` because the frame payload doesn't carry an identity — that identity comes from the Ziti connection (`GetDialerIdentityName()`). The controller knows who each connection is from. We need to inject the machineID into outgoing frames so ziti-dash can attribute them.

The cleanest approach: the controller prepends a machineID header frame to each connection. First frame the controller sends back on accept: `[0x00 type: "identity"][4B len][machineID string]`. The listener (ziti-dash) reads this first frame to learn the machineID for all subsequent frames.

Alternatively — and simpler — have the controller serve a **multiplexing proxy**: ziti-dash dials `telemetry.tango`, the controller accepts and relays all agent frames adding a machineID prefix to each. This makes ziti-dash a passive consumer.

**Simplest implementation:** On `TCPTelemetryService.handleConn()`, after accepting each agent connection, relay frames to any registered ziti-dash relay connections with machineID prepended in the JSON payload.

Actually the simplest: add a **relay endpoint** — `GET /api/telemetry/stream` on the controller (not ziti-dash) that does SSE. ziti-dash dials that HTTP endpoint over Ziti and proxies it to the browser. But that adds an HTTP layer we don't want.

**Chosen approach:** Add `MachineID` as a field injected by the `TCPTelemetryService` dispatcher. The `Event` struct already has `MachineID`. The SSE hub receives `Frame` with machineID set by the controller-side dispatcher, and that gets relayed to browsers. ziti-dash just needs the machineID in the JSON body of each SSE event — which it already has since `Frame.MachineID` is set in `dispatch()`.

**The fix in Task 4's listener.go:** Instead of ziti-dash dialing `telemetry.tango` as a dumb frame reader, the controller's `TCPTelemetryService` gets a **hub relay** — it publishes to an in-process channel that ziti-dash reads if co-located, OR ziti-dash reads from a `relay.tango` endpoint the controller exposes.

**For the current deployment (ziti-dash on same network):** Add a `relay.tango` Ziti service on the controller. The controller fans all tagged frames (`{machineID, type, payload}`) as newline-delimited JSON to any `relay.tango` dialer. ziti-dash dials `relay.tango`, reads NDJSON, publishes to hub.

**Files:**
- Create: `src/internal/service/relay.go` (schmutz-controller)
- Modify: `src/cmd/schmutz-controller/main.go` (schmutz-controller)
- Modify: `internal/telemetry/listener.go` (ziti-dash — replace frame reader with NDJSON reader)

- [ ] **Step 1: Create `src/internal/service/relay.go` in schmutz-controller**

```go
// Package service — RelayService fans tagged telemetry frames to relay.tango dials.
// This is how ziti-dash (or any consumer) receives live telemetry with machineID context.
// Wire: newline-delimited JSON, one object per line:
//   {"machine_id":"<id>","type":"<slug>","ts":<unix>,"payload":{...}}
package service

import (
	"encoding/json"
	"io"
	"log"
	"sync"

	"github.com/openziti/sdk-golang/ziti/edge"
)

// RelayFrame is the wire format sent to relay consumers.
type RelayFrame struct {
	MachineID string          `json:"machine_id"`
	Type      string          `json:"type"`
	Timestamp int64           `json:"ts"`
	Payload   json.RawMessage `json:"payload"`
}

// RelayService accepts connections on relay.tango and fans RelayFrames to them.
type RelayService struct {
	listener edge.Listener
	mu       sync.RWMutex
	clients  map[chan RelayFrame]struct{}
	stop     chan struct{}
}

// NewRelayService creates a relay bound to ln.
func NewRelayService(ln edge.Listener) *RelayService {
	return &RelayService{
		listener: ln,
		clients:  make(map[chan RelayFrame]struct{}),
		stop:     make(chan struct{}),
	}
}

// Start begins accepting relay consumer connections.
func (s *RelayService) Start() {
	go s.acceptLoop()
	log.Printf("relay: listening on relay.tango")
}

// Stop shuts down the relay.
func (s *RelayService) Stop() {
	close(s.stop)
	s.listener.Close()
}

// Publish sends a frame to all connected relay consumers.
func (s *RelayService) Publish(f RelayFrame) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for ch := range s.clients {
		select {
		case ch <- f:
		default: // drop on slow consumer
		}
	}
}

func (s *RelayService) acceptLoop() {
	for {
		conn, err := s.listener.AcceptEdge()
		if err != nil {
			select {
			case <-s.stop:
				return
			default:
				log.Printf("relay: accept: %v", err)
				return
			}
		}
		go s.handleRelay(conn)
	}
}

func (s *RelayService) handleRelay(conn edge.Conn) {
	defer conn.Close()

	consumer := conn.GetDialerIdentityName()
	log.Printf("relay: consumer connected: %s", consumer)

	ch := make(chan RelayFrame, 256)
	s.mu.Lock()
	s.clients[ch] = struct{}{}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.clients, ch)
		s.mu.Unlock()
		close(ch)
	}()

	enc := json.NewEncoder(conn)
	for {
		select {
		case <-s.stop:
			return
		case frame, ok := <-ch:
			if !ok {
				return
			}
			if err := enc.Encode(frame); err != nil {
				if err != io.EOF {
					log.Printf("relay: %s: write: %v", consumer, err)
				}
				return
			}
		}
	}
}
```

- [ ] **Step 2: Wire relay into `TCPTelemetryService.dispatch()`**

In `tcptelemetry.go`, add a `relay *RelayService` field to `TCPTelemetryService` and set it optionally:

```go
type TCPTelemetryService struct {
	listener edge.Listener
	tel      *TelemetryService
	relay    *RelayService // optional — set after construction
	stop     chan struct{}
}

func (s *TCPTelemetryService) SetRelay(r *RelayService) {
	s.relay = r
}
```

In `dispatch()`, after `s.tel.RecordEvent(ev)`, add:

```go
if s.relay != nil {
	s.relay.Publish(RelayFrame{
		MachineID: machineID,
		Type:      slug,
		Timestamp: msg.Timestamp.Unix(),
		Payload:   json.RawMessage(msg.Payload),
	})
}
```

Also fan selfmonitor frames through relay in `RecordSelf()`:
```go
func (s *TCPTelemetryService) RecordSelf(machineID, slug string, data interface{}) {
	payload, _ := json.Marshal(data)
	ev := &Event{MachineID: machineID, Type: slug, Data: json.RawMessage(payload)}
	s.tel.RecordEvent(ev)
	if s.relay != nil {
		s.relay.Publish(RelayFrame{MachineID: machineID, Type: slug, Payload: json.RawMessage(payload)})
	}
}
```

- [ ] **Step 3: Create `relay.tango` Ziti service alongside `telemetry.tango` in `ziti_services.go`**

Add `EnsureRelayService()` following the same pattern as `EnsureTelemetryService()`:

```go
// EnsureRelayService creates the relay.tango Ziti service.
// Only trusted consumers (e.g. ziti-dash identity) can dial.
// The controller identity hosts it.
func EnsureRelayService(c *common.Clients, token string) {
	const svcName = "relay.tango"
	if c.Ziti.GetServiceIDByName(token, svcName) != "" {
		return
	}
	svcID := c.Ziti.CreateService(token, svcName, []string{"relay-services"})
	if svcID == "" {
		log.Printf("relay-svc: failed to create %s", svcName)
		return
	}
	c.Ziti.CreateServicePolicy(token, svcName+"-dial", "Dial",
		[]string{"#dashboards"}, []string{"@" + svcName})
	c.Ziti.CreateServicePolicy(token, svcName+"-bind", "Bind",
		[]string{"#controllers"}, []string{"@" + svcName})
	log.Printf("relay-svc: created %s (dial=#dashboards)", svcName)
}
```

Wire it into controller startup in `main.go`:
```go
enrollmod.EnsureRelayService(clients, adminToken)

if relayLn, err := zt.ListenEdge("relay.tango"); err != nil {
    log.Printf("relay: listener failed: %v", err)
} else {
    relaySvc := service.NewRelayService(relayLn)
    relaySvc.Start()
    defer relaySvc.Stop()
    tcpTel.SetRelay(relaySvc)
}
```

- [ ] **Step 4: Update `internal/telemetry/listener.go` in ziti-dash to dial `relay.tango` instead**

Replace the `readFrames` function with NDJSON reading from `relay.tango`:

```go
func (l *Listener) runOnce() error {
	cfg, err := sdkziti.NewConfigFromFile(l.identityPath)
	if err != nil {
		return err
	}
	ctx, err := sdkziti.NewContext(cfg)
	if err != nil {
		return err
	}
	defer ctx.Close()

	conn, err := ctx.Dial("relay.tango")
	if err != nil {
		return err
	}
	defer conn.Close()

	log.Printf("telemetry listener: connected to relay.tango")

	dec := json.NewDecoder(conn)
	for {
		select {
		case <-l.stop:
			return nil
		default:
		}

		var frame Frame
		if err := dec.Decode(&frame); err != nil {
			if err != io.EOF {
				log.Printf("telemetry listener: decode: %v", err)
			}
			return err
		}
		l.hub.Publish(frame)
	}
}
```

Remove the `readFrames`, `decompressZstd`, `msgNames`, and zstd imports — no longer needed. Remove `internal/telemetry/zstd.go` if created.

Also remove `klauspost/compress` from go.mod if it was added only for zstd:
```bash
cd ~/git/kore/ziti-dash && go mod tidy
```

- [ ] **Step 5: Build both repos**

```bash
cd ~/git/kore/schmutz-controller/src && go build ./...
cd ~/git/kore/ziti-dash && go build ./...
```

- [ ] **Step 6: Commit both repos**

```bash
cd ~/git/kore/schmutz-controller
git add src/internal/service/relay.go src/internal/service/tcptelemetry.go src/cmd/schmutz-controller/main.go src/internal/controller/enroll/ziti_services.go
git commit -m "feat: add relay.tango service — fans tagged telemetry frames to dashboard consumers"

cd ~/git/kore/ziti-dash
git add internal/telemetry/
git commit -m "feat: dial relay.tango for live telemetry with machineID context"
```

No Co-Authored-By. No Claude references.

---

## Task 6: Wire SSE into the frontend — replace polling with EventSource

**Why:** The telemetry page currently polls `GET /api/telemetry/live` every 5 seconds. Replace with a persistent `EventSource` connection to `GET /api/telemetry/stream`. The existing card rendering logic stays, just driven by push instead of pull.

**Repo:** `~/git/kore/ziti-dash/`

**Files:**
- Modify: `frontend/app.js`

- [ ] **Step 1: Read the current telemetry section in app.js**

```bash
grep -n "loadTelemetry\|renderTelemetry\|telemetryCache\|page.*telemetry\|EventSource\|auto-refresh" ~/git/kore/ziti-dash/frontend/app.js | head -30
```

- [ ] **Step 2: Add SSE connection management to `app.js`**

Find the existing `telemetryCache` variable and `loadTelemetry` function. Add before `loadTelemetry`:

```js
// ---- Telemetry SSE ----
let telemetrySource = null;

function startTelemetrySSE() {
  if (telemetrySource) return; // already connected

  telemetrySource = new EventSource('/api/telemetry/stream');

  telemetrySource.onmessage = (e) => {
    try {
      const frame = JSON.parse(e.data);
      if (!frame.machine_id || !frame.type) return; // skip keepalive comments
      const key = frame.machine_id + '/' + frame.type;
      telemetryCache[key] = frame.payload;
      // Only re-render if telemetry page is active — avoid work in background
      const activePage = document.querySelector('.page.active');
      if (activePage && activePage.id === 'page-telemetry') {
        renderTelemetry();
      }
    } catch (err) {
      // ignore parse errors (keepalive lines)
    }
  };

  telemetrySource.onerror = () => {
    // Browser auto-reconnects EventSource — just log
    console.log('telemetry: SSE reconnecting...');
  };
}

function stopTelemetrySSE() {
  if (telemetrySource) {
    telemetrySource.close();
    telemetrySource = null;
  }
}
```

- [ ] **Step 3: Update `loadTelemetry()` to seed from snapshot then start SSE**

Replace the existing `loadTelemetry` function body:

```js
async function loadTelemetry() {
  // Seed from snapshot (handles initial load and page refresh)
  try {
    const snap = await api('/api/telemetry/live');
    if (snap && typeof snap === 'object') {
      Object.assign(telemetryCache, snap);
    }
  } catch (e) {
    // ignore — SSE will populate
  }
  renderTelemetry();
  startTelemetrySSE();
}
```

- [ ] **Step 4: Remove the 5-second auto-refresh timer for telemetry**

Find and remove (or comment) the auto-refresh interval that calls `loadTelemetry()` on a timer. Keep the `loadTelemetry` call on page switch (when user clicks the Telemetry nav item).

```bash
grep -n "setInterval\|5.*telemetry\|telemetry.*5" ~/git/kore/ziti-dash/frontend/app.js | head -10
```

Find the auto-refresh block (around line 1438 based on earlier grep) and remove the interval entirely — SSE handles updates.

- [ ] **Step 5: Stop SSE when navigating away from telemetry page (optional but clean)**

In the nav click handler (the `document.querySelectorAll('.nav-item').forEach` block), add:

```js
// Stop telemetry SSE when leaving the telemetry page
if (currentPage === 'telemetry' && item.dataset.page !== 'telemetry') {
  stopTelemetrySSE();
}
// Start it when entering
if (item.dataset.page === 'telemetry') {
  startTelemetrySSE();
}
```

- [ ] **Step 6: Rebuild container and verify SSE endpoint**

```bash
cd ~/git/kore/ziti-dash
go build -o build/ziti-dash ./cmd/ziti-dash
docker rm -f ziti-dash 2>/dev/null || true

REFRESH_TOKEN=$(cat ~/.config/ziti/ziti-cli.json | python3 -c "import json,sys; d=json.load(sys.stdin); print(d['edgeIdentities']['default']['apiSession']['oidcRefreshToken'])")

docker run -d --name ziti-dash --network host \
  -e ZITI_CTRL_ADDR=ctrl-1.tango:1280 \
  -e ZITI_REFRESH_TOKEN="$REFRESH_TOKEN" \
  -e LISTEN_ADDR=:9090 \
  git.kontango.io/kore/ziti-dash:latest

sleep 5
# Verify SSE endpoint exists
curl -s -N --max-time 3 http://localhost:9090/api/telemetry/stream | head -5
```

Expected: SSE response headers + `: keepalive` line within 3 seconds, or actual frame data.

- [ ] **Step 7: Commit**

```bash
cd ~/git/kore/ziti-dash
git add frontend/app.js
git commit -m "feat: replace telemetry polling with EventSource SSE — live push from relay.tango"
```

No Co-Authored-By. No Claude references.

---

## Task 7: Final build, integration smoke test, cleanup

- [ ] **Step 1: Build all three repos cleanly**

```bash
cd ~/git/kore/schmutz/src && go build ./... && go test ./... 2>&1 | grep -E "FAIL|ok"
cd ~/git/kore/schmutz-controller/src && go build ./... && go test ./... 2>&1 | grep -E "FAIL|ok"
cd ~/git/kore/ziti-dash && go build ./... && go vet ./... 2>&1
```

Expected: no FAILs, no vet errors.

- [ ] **Step 2: Verify go.mod is clean in all repos**

```bash
grep -i nats ~/git/kore/ziti-dash/go.mod && echo "NATS STILL PRESENT" || echo "ziti-dash: clean"
grep -i nats ~/git/kore/schmutz-controller/src/go.mod && echo "NATS STILL PRESENT" || echo "controller: clean"
```

- [ ] **Step 3: Verify no NATS imports remain in source**

```bash
grep -rn "\"github.com/nats-io" ~/git/kore/ziti-dash/ --include="*.go" | grep -v ".git"
grep -rn "\"github.com/nats-io" ~/git/kore/schmutz-controller/src/ --include="*.go" | grep -v ".git" | grep -v "_test"
```

Expected: no output from either.

- [ ] **Step 4: Build and push Docker image for ziti-dash**

```bash
cd ~/git/kore/ziti-dash
docker build -t git.kontango.io/kore/ziti-dash:latest .
```

- [ ] **Step 5: Run ziti-dash container and verify telemetry stream endpoint**

```bash
REFRESH_TOKEN=$(cat ~/.config/ziti/ziti-cli.json | python3 -c "import json,sys; d=json.load(sys.stdin); print(d['edgeIdentities']['default']['apiSession']['oidcRefreshToken'])")

docker rm -f ziti-dash 2>/dev/null || true
docker run -d --name ziti-dash --network host \
  -e ZITI_CTRL_ADDR=ctrl-1.tango:1280 \
  -e ZITI_REFRESH_TOKEN="$REFRESH_TOKEN" \
  -e LISTEN_ADDR=:9090 \
  git.kontango.io/kore/ziti-dash:latest

sleep 5
docker logs ziti-dash 2>&1 | grep -E "plugin loaded|listening|telemetry|relay"
curl -s -o /dev/null -w "%{http_code}" http://localhost:9090/api/telemetry/live
curl -s -o /dev/null -w "%{http_code}" http://localhost:9090/api/telemetry/stream
```

Expected:
- Logs show `plugin loaded: telemetry-stream`, `listening on :9090`
- `/api/telemetry/live` → 200
- `/api/telemetry/stream` → 200 (SSE)

- [ ] **Step 6: Final commits for all three repos**

```bash
# Verify git log looks clean
git -C ~/git/kore/schmutz log --oneline -5
git -C ~/git/kore/schmutz-controller log --oneline -5
git -C ~/git/kore/ziti-dash log --oneline -5
```

---

## Self-Review

**Spec coverage:**
- ✅ NATS removed from ziti-dash (`internal/nats/` deleted, go.mod cleaned)
- ✅ NATS removed from schmutz-controller (`internal/service/nats.go` deleted, NATS deps removed)
- ✅ `nats-<nick>` removed from enrolled service list
- ✅ `telemetry.tango` Ziti service created on controller startup
- ✅ `relay.tango` Ziti service created — fans tagged frames with machineID to consumers
- ✅ Agent dials `telemetry.tango` and sends typed stream frames (zstd-compressed JSON)
- ✅ Controller accepts frames, tags with machineID, fans through RelayService
- ✅ ziti-dash dials `relay.tango`, reads NDJSON, publishes to SSE hub
- ✅ Browser connects via EventSource — live push replaces 5s polling
- ✅ `config.tango` untouched throughout

**Architecture:**
```
agent → [telemetry.tango] → controller (TCPTelemetryService)
                                    ↓ (tags machineID)
                            controller (RelayService)
                                    ↓ [relay.tango]
                            ziti-dash (Listener → Hub)
                                    ↓ SSE
                            browser (EventSource)
```

**What didn't change:** `config.tango` (already raw TCP, already correct), the `stream` wire protocol (existed, just wired up), all enrollment flows, all Bao/Ziti management APIs, the BFF plugin architecture.

**Known follow-ups:**
- selfmonitor frames currently go `RecordSelf` → `TelemetryService` + `RelayService` — verify the controller's own pulse data appears in ziti-dash telemetry grid
- Agent reconnection on controller restart should be seamless (backoff in `Dialer.Run()`)
- `relay.tango` dial policy uses `#dashboards` role — ensure ziti-dash's enrolled identity has that role attribute set
