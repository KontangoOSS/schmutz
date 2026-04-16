package agent

import (
	"fmt"
	"log/slog"
	"os"
	"sort"
	"sync"
	"time"
)

// Config holds the configuration for the agent daemon.
// All Ziti SDK integration (dial/listen) is handled in Tasks 5-8.
type Config struct {
	IdentityPath    string        // path to Ziti identity JSON
	TelemetryTarget string        // Ziti service name to dial for telemetry (e.g. "telemetry-stream.tango")
	APIPort         int           // local port for API service (default 8080)
	SSHPort         int           // local port for SSH proxy (default 22)
	CollectInterval time.Duration // telemetry collection interval (default 5s)
}

// DefaultConfig returns a Config with sensible defaults applied.
func DefaultConfig() *Config {
	return &Config{
		TelemetryTarget: "telemetry-stream.tango",
		APIPort:         8080,
		SSHPort:         22,
		CollectInterval: 5 * time.Second,
	}
}

// Daemon manages the agent lifecycle. It coordinates service goroutines and
// owns the Ziti SDK context (added in Tasks 5-8). Call Run() to start and
// Stop() to shut down cleanly.
type Daemon struct {
	cfg      *Config
	services map[string]bool // tracks active services by name
	stop     chan struct{}
	stopped  chan struct{}
	running  bool
	mu       sync.Mutex
}

// NewDaemon creates a new Daemon with the given config. It validates that the
// identity file path exists (via os.Stat) but does not load the Ziti SDK yet.
// Returns an error if cfg is nil, IdentityPath is empty, or the file is absent.
func NewDaemon(cfg *Config) (*Daemon, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config must not be nil")
	}
	if cfg.IdentityPath == "" {
		return nil, fmt.Errorf("IdentityPath must not be empty")
	}
	if _, err := os.Stat(cfg.IdentityPath); err != nil {
		return nil, fmt.Errorf("identity file not found at %s: %w", cfg.IdentityPath, err)
	}
	return &Daemon{
		cfg:      cfg,
		services: make(map[string]bool),
		stop:     make(chan struct{}),
		stopped:  make(chan struct{}),
	}, nil
}

// Run starts the daemon and blocks until Stop() is called or a fatal error
// occurs. Ziti SDK integration (dialing telemetry, listening for API/SSH) will
// be wired in here during Tasks 5-8. For now Run simply signals that it is
// alive and waits for a stop signal.
func (d *Daemon) Run() error {
	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		return fmt.Errorf("daemon is already running")
	}
	d.running = true
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		d.running = false
		d.mu.Unlock()
		close(d.stopped)
	}()

	slog.Info("daemon running", "identity", d.cfg.IdentityPath, "telemetry", d.cfg.TelemetryTarget)

	<-d.stop
	slog.Info("daemon stopping")
	return nil
}

// Stop signals Run() to return and waits for the daemon to finish cleanup.
// Calling Stop on a daemon that is not running is a no-op.
func (d *Daemon) Stop() {
	d.mu.Lock()
	running := d.running
	d.mu.Unlock()

	if !running {
		return
	}

	// Signal the run loop to exit (idempotent close guard via recover).
	func() {
		defer func() { recover() }() //nolint:errcheck
		close(d.stop)
	}()

	<-d.stopped
}

// IsRunning returns true if Run() is currently executing.
func (d *Daemon) IsRunning() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.running
}

// ActiveServices returns a sorted list of service names currently tracked as
// active. Services are registered by the Ziti integration added in Tasks 5-8.
func (d *Daemon) ActiveServices() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	names := make([]string, 0, len(d.services))
	for name, active := range d.services {
		if active {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
