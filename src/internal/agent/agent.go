// Package agent implements the universal Schmutz machine agent.
//
// The agent has no local config. The Ziti identity is the only thing on disk.
// Everything else comes live from the controller on every connection.
//
// Two services, agent always dials:
//
//	telemetry.tango — agent pushes newline-delimited JSON heartbeats
//	config.tango    — agent sends its machine ID as a handshake, controller
//	                  pushes newline-delimited JSON instructions down the pipe
//
// The controller is the source of truth. The agent just sends data and
// listens for instructions. Nothing is cached locally.
package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/openziti/sdk-golang/ziti"
)

// BootConfig is the only config the agent needs locally.
// Everything else comes from the controller.
type BootConfig struct {
	// IdentityPath is the Ziti identity JSON written by schmutz-join.
	// Default: /opt/tango/identity.json
	IdentityPath string

	// FallbackURL is the join API base URL used when Ziti is not yet reachable.
	// Example: "https://your-controller.example"
	FallbackURL string

	// TelemetryService overrides the Ziti service name. Default: "telemetry.tango"
	TelemetryService string

	// ConfigService overrides the Ziti service name. Default: "config.tango"
	ConfigService string

	// DefaultInterval is the heartbeat interval used before the controller
	// sends one. Default: 60s
	DefaultInterval time.Duration
}

func (c *BootConfig) telemetrySvc() string {
	if c.TelemetryService != "" {
		return c.TelemetryService
	}
	return "telemetry.tango"
}

func (c *BootConfig) configSvc() string {
	if c.ConfigService != "" {
		return c.ConfigService
	}
	return "config.tango"
}

func (c *BootConfig) defaultInterval() time.Duration {
	if c.DefaultInterval > 0 {
		return c.DefaultInterval
	}
	return 60 * time.Second
}

// Heartbeat is pushed to telemetry.tango on every tick.
// Short field names — target ~150 bytes over the wire.
type Heartbeat struct {
	MachineID  string  `json:"mid"`
	Hostname   string  `json:"host"`
	OS         string  `json:"os"`
	Arch       string  `json:"arch"`
	UptimeSecs int64   `json:"up"`
	CPUCores   int     `json:"cpu"`
	MemoryMB   int64   `json:"mem,omitempty"`
	LoadAvg1   float64 `json:"load,omitempty"`
	Nickname   string  `json:"nick,omitempty"`
	State      string  `json:"state,omitempty"`
	Profile    string  `json:"profile,omitempty"`
	Timestamp  int64   `json:"ts"`
}

// Instruction is pushed from the controller down the config.tango connection.
type Instruction struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// helloPayload is what the controller sends on connect and in heartbeat responses.
// Contains whatever the controller knows about this machine that's operationally useful.
// Ziti role attributes and policies are controller-only — never sent here.
type helloPayload struct {
	Interval int    `json:"interval"`
	Nickname string `json:"nickname,omitempty"`
	State    string `json:"state,omitempty"`
	Profile  string `json:"profile,omitempty"`
	OSRef    string `json:"os_ref,omitempty"`
	OSVer    string `json:"os_ver,omitempty"`
	Arch     string `json:"arch,omitempty"`
	CPU      string `json:"cpu,omitempty"`
}

// state holds the latest hello payload received from the controller.
// Written on hello, read when building heartbeats.
var state struct {
	mu       sync.Mutex
	hello    helloPayload
	hasHello bool
}

// Run starts the agent and blocks until ctx is cancelled.
// The only local state used is the machine ID from machine.json.
func Run(ctx context.Context, boot BootConfig, logger *slog.Logger) error {
	if boot.IdentityPath == "" {
		boot.IdentityPath = defaultIdentityPath()
	}

	machineID, err := loadMachineID(boot.IdentityPath)
	if err != nil {
		return fmt.Errorf("machine record not found — enrolled? (%w)", err)
	}

	logger = logger.With("mid", machineID)

	zitiCtx, err := ziti.NewContextFromFile(boot.IdentityPath)
	if err != nil {
		logger.Warn("ziti not available, using fallback", "error", err)
		return runFallback(ctx, boot, machineID, logger)
	}
	defer zitiCtx.Close()

	intervalCh := make(chan time.Duration, 1)

	// Wait up to 15s for config.tango to deliver initial config before
	// starting telemetry. If the overlay is slow the telemetry loop starts
	// with the default interval and corrects itself when config arrives.
	configReady := make(chan struct{})
	go runConfigChannel(ctx, zitiCtx, boot, machineID, intervalCh, configReady, logger)

	select {
	case <-configReady:
	case <-time.After(15 * time.Second):
	case <-ctx.Done():
		return nil
	}

	return runTelemetry(ctx, zitiCtx, boot, machineID, intervalCh, logger)
}

// -- Config channel ----------------------------------------------------------

// runConfigChannel dials config.tango, sends the machine ID, and reads
// instructions pushed down by the controller. Reconnects on disconnect.
func runConfigChannel(ctx context.Context, zitiCtx ziti.Context, boot BootConfig, machineID string, intervalCh chan<- time.Duration, configReady chan struct{}, logger *slog.Logger) {
	backoff := 5 * time.Second
	for {
		if ctx.Err() != nil {
			return
		}

		conn, err := zitiCtx.Dial(boot.configSvc())
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
				if backoff < 5*time.Minute {
					backoff *= 2
				}
			}
			continue
		}
		backoff = 5 * time.Second

		readInstructions(ctx, conn, machineID, intervalCh, configReady, logger)
		conn.Close()

		select {
		case <-ctx.Done():
			return
		case <-time.After(15 * time.Second):
		}
	}
}

func readInstructions(ctx context.Context, conn io.ReadWriteCloser, machineID string, intervalCh chan<- time.Duration, configReady chan struct{}, logger *slog.Logger) {
	go func() { <-ctx.Done(); conn.Close() }()

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var instr Instruction
		if err := json.Unmarshal(line, &instr); err != nil {
			continue
		}

		switch instr.Type {
		case "hello":
			// Controller pushes this immediately on connect and via heartbeat
			// response. Contains operational params only — interval, profile
			// name for logging. Ziti identity and role attributes are immutable
			// from the agent's perspective; the controller owns those.
			var cfg helloPayload
			if err := json.Unmarshal(instr.Payload, &cfg); err == nil {
				state.mu.Lock()
				state.hello = cfg
				state.hasHello = true
				state.mu.Unlock()
				if cfg.Interval > 0 {
					select {
					case intervalCh <- time.Duration(cfg.Interval) * time.Second:
					default:
					}
				}
			}
			// Signal that initial config has been received — unblocks telemetry start.
			select {
			case configReady <- struct{}{}:
			default:
			}
		case "set_interval":
			var p struct {
				Seconds int `json:"seconds"`
			}
			if err := json.Unmarshal(instr.Payload, &p); err == nil && p.Seconds > 0 {
				select {
				case intervalCh <- time.Duration(p.Seconds) * time.Second:
				default:
				}
			}
		case "reload":
			return
		}
	}
}

// -- Telemetry (NATS over Ziti) ----------------------------------------------

// runTelemetry starts all collectors and fans their output into a single NATS
// publisher on "tango.telemetry.<machineID>". Each collector runs independently
// in its own goroutine — net, logs, heartbeat — and sends as soon as data is
// ready. The NATS connection is restarted on failure; collectors keep running
// and buffering into the shared channel during the reconnect window.
func runTelemetry(ctx context.Context, zitiCtx ziti.Context, boot BootConfig, machineID string, intervalCh chan time.Duration, logger *slog.Logger) error {
	subject := "tango.telemetry." + machineID
	backoff := 5 * time.Second

	// eventCh is shared across all collectors and the publisher loop.
	// Buffer of 256 absorbs bursts (e.g. log flood on boot) without blocking collectors.
	eventCh := make(chan []byte, 256)

	// Start collectors — they run for the lifetime of ctx.
	go (&heartbeatCollector{intervalCh: intervalCh, initial: boot.defaultInterval()}).collect(ctx, machineID, eventCh)
	go (&netCollector{interval: 5 * time.Minute}).collect(ctx, machineID, eventCh)
	go (&logCollector{}).collect(ctx, machineID, eventCh)

	interval := boot.defaultInterval()
	for {
		if ctx.Err() != nil {
			return nil
		}

		nc, err := connectNATS(zitiCtx, boot.telemetrySvc())
		if err != nil {
			// NATS unavailable — run HTTP fallback at the configured interval until
			// the overlay comes back. NATS reconnect is attempted after each tick.
			if boot.FallbackURL != "" {
				fallbackTicker := time.NewTicker(interval)
			fallbackLoop:
				for {
					hb := buildHeartbeat(machineID)
					ev, _ := encodeEvent(machineID, "hb", hb)
					if cmd, _ := httpHeartbeat(ctx, boot.FallbackURL, machineID, ev); cmd != nil {
						handleCmd(cmd, intervalCh)
					}
					select {
					case <-ctx.Done():
						fallbackTicker.Stop()
						return nil
					case d := <-intervalCh:
						interval = d
						fallbackTicker.Reset(d)
					case <-fallbackTicker.C:
						// Try NATS again on each tick
						fallbackTicker.Stop()
						break fallbackLoop
					}
				}
			} else {
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(backoff):
					if backoff < 5*time.Minute {
						backoff *= 2
					}
				}
			}
			continue
		}

		backoff = 5 * time.Second
		interval = publishEvents(ctx, nc, subject, eventCh, intervalCh, interval)
		nc.Close()

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(5 * time.Second):
		}
	}
}

// connectNATS dials the NATS service through Ziti and returns a connected
// *nats.Conn. The Ziti conn is used as the custom dialer — NATS sees a
// normal net.Conn underneath.
func connectNATS(zitiCtx ziti.Context, serviceName string) (*natsgo.Conn, error) {
	return natsgo.Connect("nats://nats.tango",
		natsgo.SetCustomDialer(&zitiDialer{ctx: zitiCtx, service: serviceName}),
		natsgo.MaxReconnects(0), // we handle reconnects ourselves
		natsgo.Timeout(10*time.Second),
	)
}

// zitiDialer implements nats.CustomDialer — dials through the Ziti overlay.
type zitiDialer struct {
	ctx     ziti.Context
	service string
}

func (d *zitiDialer) Dial(_, _ string) (net.Conn, error) {
	return d.ctx.Dial(d.service)
}

// publishEvents drains eventCh and publishes each message to NATS until the
// connection drops or ctx is cancelled. Returns the current interval.
func publishEvents(ctx context.Context, nc *natsgo.Conn, subject string, eventCh <-chan []byte, intervalCh <-chan time.Duration, interval time.Duration) time.Duration {
	for {
		select {
		case <-ctx.Done():
			return interval
		case d := <-intervalCh:
			interval = d
		case payload, ok := <-eventCh:
			if !ok {
				return interval
			}
			if !nc.IsConnected() {
				return interval
			}
			nc.Publish(subject, payload)
		}
	}
}

// -- Fallback ----------------------------------------------------------------

func runFallback(ctx context.Context, boot BootConfig, machineID string, logger *slog.Logger) error {
	if boot.FallbackURL == "" {
		return fmt.Errorf("ziti unavailable and no fallback URL configured")
	}
	intervalCh := make(chan time.Duration, 1)
	interval := boot.defaultInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	beat := func() {
		hb := buildHeartbeat(machineID)
		ev, _ := encodeEvent(machineID, "hb", hb)
		if cmd, _ := httpHeartbeat(ctx, boot.FallbackURL, machineID, ev); cmd != nil {
			handleCmd(cmd, intervalCh)
		}
	}

	beat()

	for {
		select {
		case <-ctx.Done():
			return nil
		case d := <-intervalCh:
			interval = d
			ticker.Reset(d)
		case <-ticker.C:
			beat()
		}
	}
}

// handleCmd acts on a command returned in a heartbeat response.
func handleCmd(cmd *HeartbeatCmd, intervalCh chan<- time.Duration) {
	switch cmd.Cmd {
	case "hello":
		var cfg helloPayload
		if err := json.Unmarshal(cmd.Payload, &cfg); err == nil {
			state.mu.Lock()
			state.hello = cfg
			state.hasHello = true
			state.mu.Unlock()
			if cfg.Interval > 0 {
				select {
				case intervalCh <- time.Duration(cfg.Interval) * time.Second:
				default:
				}
			}
		}
	case "enroll":
		if JoinFunc == nil {
			return
		}
		var p struct {
			URL      string `json:"url"`
			Token    string `json:"token"`
			RoleID   string `json:"role_id"`
			SecretID string `json:"secret_id"`
		}
		if err := json.Unmarshal(cmd.Payload, &p); err != nil || p.URL == "" {
			return
		}
		go JoinFunc(p.URL, p.Token, p.RoleID, p.SecretID)
	}
}

// JoinFunc is called when the agent receives an enroll command. Injected at
// startup so the agent package doesn't import the join command package.
var JoinFunc func(url, session, roleID, secretID string) error

// HeartbeatCmd is the command returned by the controller in a heartbeat response.
type HeartbeatCmd struct {
	Cmd     string          `json:"cmd"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

func httpHeartbeat(ctx context.Context, baseURL, machineID string, payload []byte) (*HeartbeatCmd, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/api/heartbeat", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Machine-ID", machineID)
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var cmd HeartbeatCmd
	if err := json.NewDecoder(resp.Body).Decode(&cmd); err != nil {
		return nil, nil
	}
	return &cmd, nil
}

// -- Payload -----------------------------------------------------------------

func buildHeartbeat(machineID string) Heartbeat {
	h, _ := os.Hostname()
	hb := Heartbeat{
		MachineID:  machineID,
		Hostname:   h,
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		UptimeSecs: uptimeSeconds(),
		CPUCores:   runtime.NumCPU(),
		MemoryMB:   memoryMB(),
		LoadAvg1:   loadAvg1(),
		Timestamp:  time.Now().Unix(),
	}
	state.mu.Lock()
	if state.hasHello {
		hb.Nickname = state.hello.Nickname
		hb.State = state.hello.State
		hb.Profile = state.hello.Profile
	}
	state.mu.Unlock()
	return hb
}

// -- Helpers -----------------------------------------------------------------

func loadMachineID(identityPath string) (string, error) {
	data, err := os.ReadFile(filepath.Join(filepath.Dir(identityPath), "machine.json"))
	if err != nil {
		return "", err
	}
	var rec struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &rec); err != nil || rec.ID == "" {
		return "", fmt.Errorf("id missing from machine.json")
	}
	return rec.ID, nil
}

func defaultIdentityPath() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("ProgramData"), "tango", "identity.json")
	case "darwin":
		return "/usr/local/tango/identity.json"
	default:
		return "/opt/tango/identity.json"
	}
}
