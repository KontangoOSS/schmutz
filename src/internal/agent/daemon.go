package agent

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/openziti/edge-api/rest_model"
	"github.com/openziti/sdk-golang/ziti"
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

// Daemon manages the agent lifecycle. Connects to the Ziti overlay,
// streams telemetry, and hosts API + SSH services.
type Daemon struct {
	cfg      *Config
	zitiCtx  ziti.Context
	services map[string]bool
	stop     chan struct{}
	stopped  chan struct{}
	cancel   context.CancelFunc
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

// Run starts the daemon and blocks until Stop() is called or a fatal error occurs.
// Connects to the Ziti overlay, starts telemetry streaming, binds API + SSH services,
// and listens for service change events (tier promotion/demotion).
func (d *Daemon) Run() error {
	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		return fmt.Errorf("daemon is already running")
	}
	d.running = true
	d.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel

	defer func() {
		cancel()
		d.mu.Lock()
		d.running = false
		d.mu.Unlock()
		close(d.stopped)
	}()

	// Load Ziti identity and create SDK context
	slog.Info("loading Ziti identity", "path", d.cfg.IdentityPath)
	zitiCfg, err := ziti.NewConfigFromFile(d.cfg.IdentityPath)
	if err != nil {
		return fmt.Errorf("load identity: %w", err)
	}

	d.zitiCtx, err = ziti.NewContext(zitiCfg)
	if err != nil {
		return fmt.Errorf("create Ziti context: %w", err)
	}

	slog.Info("connected to Ziti overlay")

	// Register service change listeners
	d.zitiCtx.Events().AddServiceAddedListener(func(_ ziti.Context, svc *rest_model.ServiceDetail) {
		d.HandleServiceChange(ServiceEvent{Name: *svc.Name, Added: true})
	})
	d.zitiCtx.Events().AddServiceRemovedListener(func(_ ziti.Context, svc *rest_model.ServiceDetail) {
		d.HandleServiceChange(ServiceEvent{Name: *svc.Name, Added: false})
	})

	// Start telemetry stream in background
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			slog.Info("dialing telemetry service", "service", d.cfg.TelemetryTarget)
			conn, err := d.zitiCtx.Dial(d.cfg.TelemetryTarget)
			if err != nil {
				slog.Error("telemetry dial failed, retrying in 30s", "error", err)
				select {
				case <-time.After(30 * time.Second):
					continue
				case <-ctx.Done():
					return
				}
			}

			slog.Info("telemetry stream connected")
			d.addService(d.cfg.TelemetryTarget)
			err = StreamTelemetry(ctx, conn, d.cfg.CollectInterval)
			d.removeService(d.cfg.TelemetryTarget)
			if err != nil {
				slog.Error("telemetry stream error, reconnecting in 10s", "error", err)
			}
			conn.Close()

			select {
			case <-time.After(10 * time.Second):
			case <-ctx.Done():
				return
			}
		}
	}()

	// Start API service if we can listen
	go func() {
		hostname, _ := os.Hostname()
		apiService := fmt.Sprintf("%s-api.tango", hostname)
		slog.Info("listening for API service", "service", apiService)
		ln, err := d.zitiCtx.Listen(apiService)
		if err != nil {
			slog.Error("API listen failed", "error", err, "service", apiService)
			return
		}
		d.addService(apiService)
		slog.Info("API service bound", "service", apiService)
		ServeAPI(ln)
	}()

	// Start SSH proxy if we can listen
	go func() {
		hostname, _ := os.Hostname()
		sshService := fmt.Sprintf("%s-ssh.tango", hostname)
		slog.Info("listening for SSH service", "service", sshService)
		ln, err := d.zitiCtx.Listen(sshService)
		if err != nil {
			slog.Error("SSH listen failed", "error", err, "service", sshService)
			return
		}
		d.addService(sshService)
		slog.Info("SSH service bound", "service", sshService)
		ServeSSHProxy(ln, d.cfg.SSHPort)
	}()

	slog.Info("daemon running", "identity", d.cfg.IdentityPath)

	// Block until stop signal
	<-d.stop
	slog.Info("daemon stopping")
	return nil
}

func (d *Daemon) addService(name string) {
	d.mu.Lock()
	d.services[name] = true
	d.mu.Unlock()
}

func (d *Daemon) removeService(name string) {
	d.mu.Lock()
	delete(d.services, name)
	d.mu.Unlock()
}

// DialService dials a Ziti service by name. Returns a net.Conn through the overlay.
func (d *Daemon) DialService(service string) (net.Conn, error) {
	if d.zitiCtx == nil {
		return nil, fmt.Errorf("not connected to Ziti overlay")
	}
	return d.zitiCtx.Dial(service)
}

// ListenService binds a Ziti service. Returns a net.Listener on the overlay.
func (d *Daemon) ListenService(service string) (net.Listener, error) {
	if d.zitiCtx == nil {
		return nil, fmt.Errorf("not connected to Ziti overlay")
	}
	return d.zitiCtx.Listen(service)
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
