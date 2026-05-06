package substrate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/KontangoOSS/schmutz/internal/shared"
)

// WatcherConfig configures the in-process poll loop.
type WatcherConfig struct {
	// AgentJSONPath is /etc/schmutz/agent.json by default. Carries
	// tenant + app + deployment + bao_addr — the watcher needs all of
	// these to know what substrate path to read.
	AgentJSONPath string

	// BaoTokenPath is /run/bao-token by default. The bao-jwt subsystem
	// keeps this fresh; the watcher reads it on every poll so token
	// rotations propagate immediately.
	BaoTokenPath string

	// Interval between polls. 24h matches the user's stated cadence:
	// substrate changes infrequently (operator updates).
	Interval time.Duration

	// Logger to write to. nil = log.Default().
	Logger *log.Logger
}

// DefaultWatcherConfig returns sane v1 defaults.
func DefaultWatcherConfig() WatcherConfig {
	return WatcherConfig{
		AgentJSONPath: "/etc/schmutz/agent.json",
		BaoTokenPath:  "/run/bao-token",
		Interval:      24 * time.Hour,
	}
}

// Watcher polls Bao for substrate changes and logs a plan describing
// what it would reconcile. v1 takes NO actions on the host — the
// reconciler is a separate subsystem that arrives once we trust the
// plan output.
//
// Observable state (concurrency-safe via atomic.Pointer):
//
//	LastSpec    — most recent successfully-parsed substrate
//	LastError   — most recent error (cleared on next success)
//	LastAttempt — unix seconds of last poll attempt
type Watcher struct {
	cfg    WatcherConfig
	logger *log.Logger

	lastSpec    atomic.Pointer[shared.Substrate]
	lastErr     atomic.Pointer[error]
	lastAttempt atomic.Int64
}

// NewWatcher returns a watcher ready to Run. Defaults are filled in for
// any zero-valued WatcherConfig fields.
func NewWatcher(cfg WatcherConfig) *Watcher {
	if cfg.AgentJSONPath == "" {
		cfg.AgentJSONPath = "/etc/schmutz/agent.json"
	}
	if cfg.BaoTokenPath == "" {
		cfg.BaoTokenPath = "/run/bao-token"
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 24 * time.Hour
	}
	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}
	return &Watcher{cfg: cfg, logger: logger}
}

// Run drives the watcher. Polls once at startup, then every Interval
// thereafter. Returns nil when ctx is cancelled. A failed poll does NOT
// stop the loop — the next tick retries.
func (w *Watcher) Run(ctx context.Context) error {
	w.logger.Printf("substrate: watcher starting (agent_json=%s token=%s interval=%s)",
		w.cfg.AgentJSONPath, w.cfg.BaoTokenPath, w.cfg.Interval)

	w.tick(ctx)

	t := time.NewTicker(w.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			w.logger.Printf("substrate: watcher stopping (%v)", ctx.Err())
			return nil
		case <-t.C:
			w.tick(ctx)
		}
	}
}

// tick performs one poll. Failure modes are logged and recorded; none
// propagate up.
func (w *Watcher) tick(ctx context.Context) {
	w.lastAttempt.Store(time.Now().Unix())

	agent, err := loadAgent(w.cfg.AgentJSONPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			w.logger.Printf("substrate: %s not present yet — host has not been bao-enrolled", w.cfg.AgentJSONPath)
		} else {
			w.logger.Printf("substrate: load %s: %v", w.cfg.AgentJSONPath, err)
		}
		w.recordErr(err)
		return
	}
	if agent.Tenant == "" || agent.App == "" || agent.Deployment == "" {
		err := fmt.Errorf("agent.json missing tenant/app/deployment (saw %q/%q/%q)",
			agent.Tenant, agent.App, agent.Deployment)
		w.logger.Printf("substrate: %v", err)
		w.recordErr(err)
		return
	}

	tokenBytes, err := os.ReadFile(w.cfg.BaoTokenPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			w.logger.Printf("substrate: %s not present yet — bao-jwt subsystem hasn't refreshed yet", w.cfg.BaoTokenPath)
		} else {
			w.logger.Printf("substrate: read %s: %v", w.cfg.BaoTokenPath, err)
		}
		w.recordErr(err)
		return
	}
	token := strings.TrimSpace(string(tokenBytes))
	if token == "" {
		w.recordErr(fmt.Errorf("substrate: %s is empty", w.cfg.BaoTokenPath))
		w.logger.Printf("substrate: %s is empty", w.cfg.BaoTokenPath)
		return
	}

	cli := NewClient(agent.BaoAddr, 0)
	kv, err := cli.ReadSubstrateKV(ctx, token, agent.Tenant+"/", agent.App, agent.Deployment)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			w.logger.Printf("substrate: no substrate at %s/secret/data/apps/%s/%s/substrate yet — controller has not provisioned this host's substrate",
				agent.Tenant, agent.App, agent.Deployment)
		} else {
			w.logger.Printf("substrate: fetch failed: %v", err)
		}
		w.recordErr(err)
		return
	}

	spec, err := decodeSubstrate(kv)
	if err != nil {
		w.logger.Printf("substrate: decode: %v", err)
		w.recordErr(err)
		return
	}
	if err := spec.Validate(); err != nil {
		w.logger.Printf("substrate: validate: %v", err)
		w.recordErr(err)
		return
	}
	if err := spec.MatchesPath(agent.Tenant, agent.App, agent.Deployment); err != nil {
		// Body's identity fields don't match the path we read from. The
		// controller wrote the wrong record, OR something tampered with
		// the data. Surface loudly; do not act.
		w.logger.Printf("substrate: SECURITY: body identity doesn't match path: %v", err)
		w.recordErr(err)
		return
	}
	if spec.ZitiIdentity != agent.ZitiIdentity {
		err := fmt.Errorf("ziti_identity %q in substrate doesn't match agent identity %q",
			spec.ZitiIdentity, agent.ZitiIdentity)
		w.logger.Printf("substrate: SECURITY: %v", err)
		w.recordErr(err)
		return
	}

	w.recordOK(spec)
	w.logger.Printf("substrate: read %s/secret/data/apps/%s/%s/substrate (%d binds, %d routes)",
		agent.Tenant, agent.App, agent.Deployment, len(spec.Binds), len(spec.Routes))
	w.logger.Print(planLog(spec))
}

// LastSpec returns the most recently parsed substrate, or nil if no
// successful poll has happened yet.
func (w *Watcher) LastSpec() *shared.Substrate { return w.lastSpec.Load() }

// LastError returns the most recent error, or nil if the last attempt
// succeeded. Always reflects the most recent tick.
func (w *Watcher) LastError() error {
	p := w.lastErr.Load()
	if p == nil {
		return nil
	}
	return *p
}

// LastAttempt returns the unix-seconds timestamp of the last poll. 0 if
// none yet.
func (w *Watcher) LastAttempt() int64 { return w.lastAttempt.Load() }

func (w *Watcher) recordOK(spec *shared.Substrate) {
	w.lastSpec.Store(spec)
	w.lastErr.Store(nil)
}

func (w *Watcher) recordErr(err error) {
	w.lastErr.Store(&err)
}

// agentSummary is the subset of /etc/schmutz/agent.json the watcher needs.
// We read the file fresh on every tick rather than passing it in so an
// operator can update the file (e.g. point at a new bao_addr) without
// restarting the daemon.
type agentSummary struct {
	BaoAddr      string `json:"bao_addr"`
	Tenant       string `json:"tenant"`
	App          string `json:"app"`
	Deployment   string `json:"deployment"`
	ZitiIdentity string `json:"ziti_identity"`
}

func loadAgent(path string) (*agentSummary, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var a agentSummary
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &a, nil
}

// decodeSubstrate converts the KV map (string keys, untyped values) into
// a typed shared.Substrate. We round-trip via JSON so the existing
// json/yaml tags on the struct do the work — Bao gives us
// `map[string]any` where nested arrays/maps are already typed.
func decodeSubstrate(kv map[string]any) (*shared.Substrate, error) {
	body, err := json.Marshal(kv)
	if err != nil {
		return nil, fmt.Errorf("re-marshal kv: %w", err)
	}
	var spec shared.Substrate
	if err := json.Unmarshal(body, &spec); err != nil {
		return nil, fmt.Errorf("unmarshal substrate: %w", err)
	}
	if spec.Version == 0 {
		// Tolerate v1 records written before the schema added an
		// explicit Version. Bao-app-enroll always writes Version=1
		// today, so this branch is purely defensive.
		spec.Version = shared.SubstrateSchemaVersion
	}
	return &spec, nil
}
