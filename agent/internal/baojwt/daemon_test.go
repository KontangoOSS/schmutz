package baojwt

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestDaemon_RefreshesOnTick verifies the loop wakes up on the ticker
// cadence and produces a fresh token on each tick.
//
// Strategy: short interval (~50ms), wait for at least 3 successful
// refreshes, cancel ctx, assert daemon stops cleanly.
func TestDaemon_RefreshesOnTick(t *testing.T) {
	f := newFakeBao()
	defer f.close()

	dir := t.TempDir()
	agentPath := filepath.Join(dir, "agent.json")
	tokPath := filepath.Join(dir, "bao-token")

	cfg := AgentConfig{
		Role: "x", RoleID: "rid", SecretID: "sid",
		OIDCRole: "x-token", JWTRole: "x-jwt", BaoAddr: f.addr(),
	}
	mustWriteAgent(t, agentPath, cfg)

	var logBuf bytes.Buffer
	d := NewDaemon(DaemonConfig{
		AgentJSONPath: agentPath,
		TokenPath:     tokPath,
		Interval:      50 * time.Millisecond,
		Logger:        log.New(&logBuf, "", 0),
	})

	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan struct{})
	go func() {
		_ = d.Run(ctx)
		close(doneCh)
	}()

	// Wait for at least 3 successful refreshes.
	if !waitFor(t, time.Second, func() bool {
		f.mu.Lock()
		n := f.calls.jwt
		f.mu.Unlock()
		return n >= 3
	}) {
		t.Fatalf("expected >=3 jwt logins; got %d", f.calls.jwt)
	}

	cancel()
	select {
	case <-doneCh:
	case <-time.After(time.Second):
		t.Fatal("daemon did not stop after ctx cancel")
	}

	// Final state on disk: token should match the fake's response.
	got, err := os.ReadFile(tokPath)
	if err != nil {
		t.Fatalf("read token: %v", err)
	}
	if string(got) != f.jwtToken {
		t.Errorf("disk token: got %q want %q", got, f.jwtToken)
	}
	if d.LastResult() == nil {
		t.Error("LastResult should be set")
	}
	if d.LastError() != nil {
		t.Errorf("LastError unexpected: %v", d.LastError())
	}
	if d.LastAttempt() == 0 {
		t.Error("LastAttempt should be set")
	}
}

// TestDaemon_MissingAgentJSON: a host without agent.json (not yet
// bao-enrolled) should log a friendly message and keep retrying. Loop
// stays alive.
func TestDaemon_MissingAgentJSON(t *testing.T) {
	dir := t.TempDir()
	agentPath := filepath.Join(dir, "agent.json") // intentionally absent
	tokPath := filepath.Join(dir, "bao-token")

	var logBuf bytes.Buffer
	d := NewDaemon(DaemonConfig{
		AgentJSONPath: agentPath,
		TokenPath:     tokPath,
		Interval:      30 * time.Millisecond,
		Logger:        log.New(&logBuf, "", 0),
	})

	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan struct{})
	go func() {
		_ = d.Run(ctx)
		close(doneCh)
	}()

	// Wait for >=2 ticks (with 30ms interval, ~100ms is enough).
	time.Sleep(120 * time.Millisecond)
	cancel()
	<-doneCh

	out := logBuf.String()
	if !strings.Contains(out, "not present yet") {
		t.Errorf("expected 'not present yet' log message; got: %s", out)
	}
	if d.LastError() == nil {
		t.Error("LastError should reflect missing agent.json")
	}
	if d.LastAttempt() == 0 {
		t.Error("LastAttempt should be set even on failure")
	}
}

// TestDaemon_TransientFailureRecovers: a refresh that fails (bao 503)
// should be logged but not stop the loop. When bao recovers, the next
// tick succeeds and the prior token is overwritten.
func TestDaemon_TransientFailureRecovers(t *testing.T) {
	f := newFakeBao()
	defer f.close()

	dir := t.TempDir()
	agentPath := filepath.Join(dir, "agent.json")
	tokPath := filepath.Join(dir, "bao-token")

	cfg := AgentConfig{
		Role: "x", RoleID: "rid", SecretID: "sid",
		OIDCRole: "x-token", JWTRole: "x-jwt", BaoAddr: f.addr(),
	}
	mustWriteAgent(t, agentPath, cfg)

	// Start with bao unhealthy.
	f.mu.Lock()
	f.healthStatus = http.StatusServiceUnavailable
	f.mu.Unlock()

	var logBuf bytes.Buffer
	d := NewDaemon(DaemonConfig{
		AgentJSONPath: agentPath,
		TokenPath:     tokPath,
		Interval:      30 * time.Millisecond,
		Logger:        log.New(&logBuf, "", 0),
	})

	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan struct{})
	go func() {
		_ = d.Run(ctx)
		close(doneCh)
	}()

	// Wait for at least one failed refresh.
	if !waitFor(t, time.Second, func() bool {
		return d.LastError() != nil
	}) {
		t.Fatal("expected LastError after bao unhealthy")
	}

	// Recover bao.
	f.mu.Lock()
	f.healthStatus = http.StatusOK
	f.mu.Unlock()

	// Next tick should succeed.
	if !waitFor(t, time.Second, func() bool {
		return d.LastError() == nil && d.LastResult() != nil
	}) {
		t.Fatalf("daemon did not recover; lastErr=%v lastRes=%v", d.LastError(), d.LastResult())
	}

	cancel()
	<-doneCh

	if !strings.Contains(logBuf.String(), "refresh failed") {
		t.Errorf("expected 'refresh failed' log on bao 503; got: %s", logBuf.String())
	}
}

// TestDaemon_PreservesTokenAcrossFailure: if the daemon successfully
// writes a token, then bao goes 503, the prior token must remain on
// disk. (writeTokenAtomic guarantees this; this just exercises the
// daemon path.)
func TestDaemon_PreservesTokenAcrossFailure(t *testing.T) {
	f := newFakeBao()
	defer f.close()

	dir := t.TempDir()
	agentPath := filepath.Join(dir, "agent.json")
	tokPath := filepath.Join(dir, "bao-token")

	cfg := AgentConfig{
		Role: "x", RoleID: "rid", SecretID: "sid",
		OIDCRole: "x-token", JWTRole: "x-jwt", BaoAddr: f.addr(),
	}
	mustWriteAgent(t, agentPath, cfg)

	d := NewDaemon(DaemonConfig{
		AgentJSONPath: agentPath,
		TokenPath:     tokPath,
		Interval:      30 * time.Millisecond,
		Logger:        log.New(io.Discard, "", 0),
	})

	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan struct{})
	go func() {
		_ = d.Run(ctx)
		close(doneCh)
	}()

	// Wait for first success.
	if !waitFor(t, time.Second, func() bool {
		return d.LastResult() != nil
	}) {
		t.Fatal("no first refresh success")
	}
	firstToken, _ := os.ReadFile(tokPath)

	// Make bao fail.
	f.mu.Lock()
	f.approleStatus = http.StatusForbidden
	f.mu.Unlock()

	// Wait for the failure to be observed.
	if !waitFor(t, time.Second, func() bool {
		return d.LastError() != nil
	}) {
		t.Fatal("expected error after approle 403")
	}

	// Token on disk must still match the first refresh.
	got, _ := os.ReadFile(tokPath)
	if string(got) != string(firstToken) {
		t.Errorf("prior token clobbered on failure: got %q want %q", got, firstToken)
	}

	cancel()
	<-doneCh
}

// helpers

func mustWriteAgent(t *testing.T, path string, cfg AgentConfig) {
	t.Helper()
	body, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal cfg: %v", err)
	}
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func waitFor(t *testing.T, max time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}

// Add a mutex around mutable fakeBao fields so tests can flip them while
// the daemon runs concurrently. The original tests are single-goroutine,
// so this lock is no-op for them.
var _ = sync.Mutex{}
