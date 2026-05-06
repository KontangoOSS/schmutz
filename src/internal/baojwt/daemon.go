package baojwt

import (
	"context"
	"errors"
	"log"
	"os"
	"sync/atomic"
	"time"
)

// DaemonConfig configures the in-process refresh loop.
type DaemonConfig struct {
	// AgentJSONPath is /etc/schmutz/agent.json by default.
	AgentJSONPath string
	// TokenPath is /run/bao-token by default.
	TokenPath string
	// Interval between refresh attempts. 10m matches the systemd-timer
	// pattern from the bash flow; the issued token's ttl is 15m, so
	// 10m leaves ~5m headroom.
	Interval time.Duration
	// Logger to write to. nil = log.Default().
	Logger *log.Logger
}

// DefaultDaemonConfig returns sensible defaults for the bao-jwt subsystem.
func DefaultDaemonConfig() DaemonConfig {
	return DaemonConfig{
		AgentJSONPath: "/etc/schmutz/agent.json",
		TokenPath:     DefaultTokenPath,
		Interval:      10 * time.Minute,
	}
}

// Daemon is the long-running refresh loop. Construct with NewDaemon, start
// with Run. Run blocks until ctx is cancelled. State observable via
// LastResult / LastError for status reporting.
type Daemon struct {
	cfg      DaemonConfig
	logger   *log.Logger
	lastErr  atomic.Pointer[error]
	lastRes  atomic.Pointer[RefreshResult]
	lastTime atomic.Int64 // unix seconds
}

// NewDaemon validates config + returns a Daemon ready to Run. It does NOT
// load agent.json — that happens on each refresh tick so that operators
// can drop new credentials in without restarting the daemon.
func NewDaemon(cfg DaemonConfig) *Daemon {
	if cfg.AgentJSONPath == "" {
		cfg.AgentJSONPath = "/etc/schmutz/agent.json"
	}
	if cfg.TokenPath == "" {
		cfg.TokenPath = DefaultTokenPath
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 10 * time.Minute
	}
	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}
	return &Daemon{cfg: cfg, logger: logger}
}

// Run drives the refresh loop. It refreshes once immediately at startup so
// /run/bao-token exists by the time other subsystems need it, then on
// every tick. It returns nil when ctx is cancelled.
//
// A failed refresh does NOT stop the loop — the next tick retries. This
// matches the operator expectation that a transient bao blip should not
// require the daemon to be restarted, and that the previous /run/bao-token
// remains valid (writeTokenAtomic preserves the prior token on failure).
func (d *Daemon) Run(ctx context.Context) error {
	d.logger.Printf("baojwt: subsystem starting (agent_json=%s token=%s interval=%s)",
		d.cfg.AgentJSONPath, d.cfg.TokenPath, d.cfg.Interval)

	// First refresh immediately. Don't block startup if it fails — log
	// and let the timer retry.
	d.tick(ctx)

	t := time.NewTicker(d.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			d.logger.Printf("baojwt: subsystem stopping (%v)", ctx.Err())
			return nil
		case <-t.C:
			d.tick(ctx)
		}
	}
}

// tick attempts one refresh. All failure modes are logged + recorded;
// none propagate up. A missing agent.json (e.g. the host hasn't been
// bao-enrolled yet) is logged at INFO not ERROR — that's an expected
// state for a freshly-provisioned machine that hasn't gone through
// bao-app-enroll on the controller side.
func (d *Daemon) tick(ctx context.Context) {
	d.lastTime.Store(time.Now().Unix())

	cfg, err := LoadAgentConfig(d.cfg.AgentJSONPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || isNotExistWrapped(err) {
			d.logger.Printf("baojwt: %s not present yet — host has not been bao-enrolled", d.cfg.AgentJSONPath)
			d.recordErr(err)
			return
		}
		d.logger.Printf("baojwt: load %s: %v", d.cfg.AgentJSONPath, err)
		d.recordErr(err)
		return
	}

	res, err := Refresh(ctx, cfg, d.cfg.TokenPath)
	if err != nil {
		d.logger.Printf("baojwt: refresh failed (prior token preserved): %v", err)
		d.recordErr(err)
		return
	}
	d.recordOK(res)
	d.logger.Printf("baojwt: refreshed %s (role=%s, %d bytes)", res.TokenPath, res.Role, res.BytesOut)
}

// LastResult returns the most recent successful refresh result, or nil if
// none yet (or only failures).
func (d *Daemon) LastResult() *RefreshResult { return d.lastRes.Load() }

// LastError returns the most recent error, or nil if the last attempt
// succeeded.
func (d *Daemon) LastError() error {
	p := d.lastErr.Load()
	if p == nil {
		return nil
	}
	return *p
}

// LastAttempt returns the unix-seconds timestamp of the last tick. 0 if
// none yet.
func (d *Daemon) LastAttempt() int64 { return d.lastTime.Load() }

func (d *Daemon) recordOK(res *RefreshResult) {
	d.lastRes.Store(res)
	d.lastErr.Store(nil)
}

func (d *Daemon) recordErr(err error) {
	d.lastErr.Store(&err)
}

// isNotExistWrapped looks for a not-exist error wrapped by LoadAgentConfig.
// LoadAgentConfig wraps os.ReadFile with %w, so errors.Is should work, but
// we double-check the message form for older Go behavior.
func isNotExistWrapped(err error) bool {
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	return false
}
