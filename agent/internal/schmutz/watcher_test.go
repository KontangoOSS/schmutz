package substrate

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KontangoOSS/schmutz/shared"
)

// fakeBao mimics just the substrate-read endpoint. Concurrency-safe so
// tests can flip the response while the watcher polls.
type fakeBao struct {
	server *httptest.Server

	mu sync.Mutex

	// next response
	status      int
	body        any
	wantNS      string // if non-empty, asserted against X-Vault-Namespace header
	wantToken   string // if non-empty, asserted against X-Vault-Token header
	wantPath    string // if non-empty, asserted against URL path

	// observed
	calls int
	lastNS, lastToken, lastPath string
}

func newFakeBao(t *testing.T) *fakeBao {
	f := &fakeBao{
		status: http.StatusOK,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls++
		f.lastNS = r.Header.Get("X-Vault-Namespace")
		f.lastToken = r.Header.Get("X-Vault-Token")
		f.lastPath = r.URL.Path
		st := f.status
		body := f.body
		wantNS := f.wantNS
		wantToken := f.wantToken
		wantPath := f.wantPath
		f.mu.Unlock()

		if wantNS != "" && f.lastNS != wantNS {
			t.Errorf("namespace header: got %q want %q", f.lastNS, wantNS)
		}
		if wantToken != "" && f.lastToken != wantToken {
			t.Errorf("token header: got %q want %q", f.lastToken, wantToken)
		}
		if wantPath != "" && f.lastPath != wantPath {
			t.Errorf("URL path: got %q want %q", f.lastPath, wantPath)
		}
		if st != http.StatusOK {
			w.WriteHeader(st)
			return
		}
		if body != nil {
			_ = json.NewEncoder(w).Encode(body)
		}
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeBao) addr() string { return f.server.URL }

// kvResponse wraps inner data the way real Bao KV v2 does.
func kvResponse(inner map[string]any) map[string]any {
	return map[string]any{
		"data": map[string]any{
			"data":     inner,
			"metadata": map[string]any{"version": 1},
		},
	}
}

// validInventreeKV returns a typical substrate body that decodeSubstrate
// will accept.
func validInventreeKV() map[string]any {
	return map[string]any{
		"version":       1,
		"tenant":        "kontango",
		"app":           "inventree",
		"deployment":    "prod-01",
		"ziti_identity": "machine-f6f769f1",
		"binds": []any{
			map[string]any{
				"service":    "inventree.tango",
				"local_addr": "127.0.0.1:8000",
				"proto":      "tcp",
			},
		},
	}
}

// writeAgentJSON drops a /etc/schmutz/agent.json equivalent in tempdir.
func writeAgentJSON(t *testing.T, path string, baoAddr, tenant, app, deployment, zitiID string) {
	t.Helper()
	body, err := json.Marshal(map[string]string{
		"bao_addr":      baoAddr,
		"tenant":        tenant,
		"app":           app,
		"deployment":    deployment,
		"ziti_identity": zitiID,
	})
	if err != nil {
		t.Fatalf("marshal agent.json: %v", err)
	}
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatalf("write agent.json: %v", err)
	}
}

func writeToken(t *testing.T, path, token string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(token), 0640); err != nil {
		t.Fatalf("write token: %v", err)
	}
}

func newWatcherWithLogger(t *testing.T, cfg WatcherConfig) (*Watcher, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	cfg.Logger = log.New(&buf, "", 0)
	return NewWatcher(cfg), &buf
}

// Happy path: agent.json + token in place, fake Bao serves a valid
// substrate. Watcher logs a plan, LastSpec is populated, no error.
func TestWatcher_Happy(t *testing.T) {
	dir := t.TempDir()
	agentPath := filepath.Join(dir, "agent.json")
	tokenPath := filepath.Join(dir, "bao-token")

	f := newFakeBao(t)
	f.mu.Lock()
	f.body = kvResponse(validInventreeKV())
	f.wantNS = "kontango/"
	f.wantToken = "scoped-token-x"
	f.wantPath = "/v1/secret/data/apps/inventree/prod-01/substrate"
	f.mu.Unlock()

	writeAgentJSON(t, agentPath, f.addr(), "kontango", "inventree", "prod-01", "machine-f6f769f1")
	writeToken(t, tokenPath, "scoped-token-x")

	w, buf := newWatcherWithLogger(t, WatcherConfig{
		AgentJSONPath: agentPath,
		BaoTokenPath:  tokenPath,
		Interval:      50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan struct{})
	go func() { _ = w.Run(ctx); close(doneCh) }()

	if !waitFor(t, time.Second, func() bool { return w.LastSpec() != nil }) {
		t.Fatalf("expected LastSpec populated; logs:\n%s", buf.String())
	}
	cancel()
	<-doneCh

	if w.LastError() != nil {
		t.Errorf("LastError unexpected: %v", w.LastError())
	}
	spec := w.LastSpec()
	if spec.App != "inventree" || spec.Deployment != "prod-01" {
		t.Errorf("LastSpec wrong: %+v", spec)
	}
	out := buf.String()
	for _, want := range []string{
		"plan for kontango/inventree/prod-01",
		"inventree.tango",
		"127.0.0.1:8000",
		"NO ACTIONS TAKEN",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("plan log missing %q; got:\n%s", want, out)
		}
	}
}

// agent.json missing → log "not present yet", LastError reflects ENOENT,
// loop keeps running.
func TestWatcher_MissingAgentJSON(t *testing.T) {
	dir := t.TempDir()
	w, buf := newWatcherWithLogger(t, WatcherConfig{
		AgentJSONPath: filepath.Join(dir, "missing.json"),
		BaoTokenPath:  filepath.Join(dir, "missing.token"),
		Interval:      30 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan struct{})
	go func() { _ = w.Run(ctx); close(doneCh) }()

	if !waitFor(t, time.Second, func() bool { return w.LastError() != nil }) {
		t.Fatal("expected LastError after missing agent.json")
	}
	cancel()
	<-doneCh

	if !strings.Contains(buf.String(), "not present yet") {
		t.Errorf("expected 'not present yet' log; got:\n%s", buf.String())
	}
}

// agent.json present but token file missing → log appropriately.
func TestWatcher_MissingToken(t *testing.T) {
	dir := t.TempDir()
	agentPath := filepath.Join(dir, "agent.json")
	writeAgentJSON(t, agentPath, "http://127.0.0.1:1", "kontango", "inventree", "prod-01", "machine-f6f769f1")

	w, buf := newWatcherWithLogger(t, WatcherConfig{
		AgentJSONPath: agentPath,
		BaoTokenPath:  filepath.Join(dir, "missing.token"),
		Interval:      30 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan struct{})
	go func() { _ = w.Run(ctx); close(doneCh) }()

	if !waitFor(t, time.Second, func() bool { return w.LastError() != nil }) {
		t.Fatal("expected LastError after missing token")
	}
	cancel()
	<-doneCh

	if !strings.Contains(buf.String(), "not present yet") {
		t.Errorf("expected 'not present yet' log mentioning bao-jwt; got:\n%s", buf.String())
	}
}

// Bao returns 404 → ErrNotFound branch logs "no substrate at... yet."
func TestWatcher_NotFound(t *testing.T) {
	dir := t.TempDir()
	agentPath := filepath.Join(dir, "agent.json")
	tokenPath := filepath.Join(dir, "bao-token")

	f := newFakeBao(t)
	f.mu.Lock()
	f.status = http.StatusNotFound
	f.mu.Unlock()

	writeAgentJSON(t, agentPath, f.addr(), "kontango", "inventree", "prod-01", "machine-f6f769f1")
	writeToken(t, tokenPath, "tok")

	w, buf := newWatcherWithLogger(t, WatcherConfig{
		AgentJSONPath: agentPath,
		BaoTokenPath:  tokenPath,
		Interval:      30 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan struct{})
	go func() { _ = w.Run(ctx); close(doneCh) }()

	if !waitFor(t, time.Second, func() bool {
		return w.LastError() != nil
	}) {
		t.Fatal("expected error on 404")
	}
	cancel()
	<-doneCh

	if !strings.Contains(buf.String(), "controller has not provisioned") {
		t.Errorf("expected provisioning hint in log; got:\n%s", buf.String())
	}
}

// Identity mismatch — substrate body's ziti_identity != agent's. Must
// log SECURITY and NOT populate LastSpec.
func TestWatcher_IdentityMismatch(t *testing.T) {
	dir := t.TempDir()
	agentPath := filepath.Join(dir, "agent.json")
	tokenPath := filepath.Join(dir, "bao-token")

	body := validInventreeKV()
	body["ziti_identity"] = "machine-deadbeef" // wrong

	f := newFakeBao(t)
	f.mu.Lock()
	f.body = kvResponse(body)
	f.mu.Unlock()

	writeAgentJSON(t, agentPath, f.addr(), "kontango", "inventree", "prod-01", "machine-f6f769f1")
	writeToken(t, tokenPath, "tok")

	w, buf := newWatcherWithLogger(t, WatcherConfig{
		AgentJSONPath: agentPath,
		BaoTokenPath:  tokenPath,
		Interval:      30 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan struct{})
	go func() { _ = w.Run(ctx); close(doneCh) }()

	if !waitFor(t, time.Second, func() bool { return w.LastError() != nil }) {
		t.Fatal("expected LastError on identity mismatch")
	}
	cancel()
	<-doneCh

	if w.LastSpec() != nil {
		t.Error("LastSpec should NOT be set on identity mismatch")
	}
	if !strings.Contains(buf.String(), "SECURITY") {
		t.Errorf("expected SECURITY in log; got:\n%s", buf.String())
	}
}

// Path mismatch — body's tenant/app/deployment doesn't match agent.
// Identity matches, but path identity doesn't. Same SECURITY treatment.
func TestWatcher_PathMismatch(t *testing.T) {
	dir := t.TempDir()
	agentPath := filepath.Join(dir, "agent.json")
	tokenPath := filepath.Join(dir, "bao-token")

	body := validInventreeKV()
	body["deployment"] = "staging" // wrong; agent.json says prod-01

	f := newFakeBao(t)
	f.mu.Lock()
	f.body = kvResponse(body)
	f.mu.Unlock()

	writeAgentJSON(t, agentPath, f.addr(), "kontango", "inventree", "prod-01", "machine-f6f769f1")
	writeToken(t, tokenPath, "tok")

	w, buf := newWatcherWithLogger(t, WatcherConfig{
		AgentJSONPath: agentPath,
		BaoTokenPath:  tokenPath,
		Interval:      30 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan struct{})
	go func() { _ = w.Run(ctx); close(doneCh) }()

	if !waitFor(t, time.Second, func() bool { return w.LastError() != nil }) {
		t.Fatal("expected LastError on path mismatch")
	}
	cancel()
	<-doneCh
	if w.LastSpec() != nil {
		t.Error("LastSpec should NOT be set on path mismatch")
	}
	if !strings.Contains(buf.String(), "SECURITY") {
		t.Errorf("expected SECURITY in log; got:\n%s", buf.String())
	}
}

// Refresh: previously-good substrate, then later poll fails — LastSpec
// must remain (last-known-good), LastError must reflect the new failure.
func TestWatcher_PreservesLastGoodSpecOnFailure(t *testing.T) {
	dir := t.TempDir()
	agentPath := filepath.Join(dir, "agent.json")
	tokenPath := filepath.Join(dir, "bao-token")

	f := newFakeBao(t)
	f.mu.Lock()
	f.body = kvResponse(validInventreeKV())
	f.mu.Unlock()

	writeAgentJSON(t, agentPath, f.addr(), "kontango", "inventree", "prod-01", "machine-f6f769f1")
	writeToken(t, tokenPath, "tok")

	w, _ := newWatcherWithLogger(t, WatcherConfig{
		AgentJSONPath: agentPath,
		BaoTokenPath:  tokenPath,
		Interval:      30 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan struct{})
	go func() { _ = w.Run(ctx); close(doneCh) }()

	if !waitFor(t, time.Second, func() bool { return w.LastSpec() != nil }) {
		t.Fatal("first poll should have succeeded")
	}
	first := w.LastSpec()

	// Now make Bao fail
	f.mu.Lock()
	f.status = http.StatusInternalServerError
	f.body = nil
	f.mu.Unlock()

	if !waitFor(t, time.Second, func() bool { return w.LastError() != nil }) {
		t.Fatal("expected LastError after Bao 500")
	}

	cancel()
	<-doneCh

	if w.LastSpec() != first {
		t.Errorf("LastSpec should still equal the previous-good spec; got %p want %p",
			w.LastSpec(), first)
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

// decodeSubstrate is reused by the watcher; verify it round-trips
// shared.Schmutz too (sanity check that JSON tags match).
func TestDecodeSubstrate_RoundTrip(t *testing.T) {
	in := &shared.Schmutz{
		Version: 1, Tenant: "x", App: "y", Deployment: "z",
		ZitiIdentity: "machine-12345678",
		Binds: []shared.Bind{
			{Service: "y.tango", LocalAddr: "127.0.0.1:1", Proto: "tcp"},
		},
	}
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var kv map[string]any
	if err := json.Unmarshal(body, &kv); err != nil {
		t.Fatal(err)
	}
	out, err := decodeSubstrate(kv)
	if err != nil {
		t.Fatal(err)
	}
	if out.App != in.App || len(out.Binds) != 1 || out.Binds[0].Service != "y.tango" {
		t.Errorf("round-trip mismatch: %+v", out)
	}
}
