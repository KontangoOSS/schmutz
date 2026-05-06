package baojwt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeBao stands up a tiny HTTP server that mimics just the four
// endpoints the agent calls. Each endpoint can be programmed per-test to
// return a fixed response or an error code.
type fakeBao struct {
	server *httptest.Server

	healthStatus int

	unwrapStatus  int
	unwrapPayload map[string]any

	approleStatus int
	approleToken  string

	oidcStatus int
	oidcToken  string

	jwtStatus int
	jwtToken  string

	calls struct {
		health, unwrap, approle, oidc, jwt int
	}

	lastApproleBody, lastJWTBody map[string]any
	lastOIDCAuthHeader           string
	lastUnwrapAuthHeader         string
}

func newFakeBao() *fakeBao {
	f := &fakeBao{
		healthStatus:  http.StatusOK,
		unwrapStatus:  http.StatusOK,
		unwrapPayload: map[string]any{"secret_id": "fake-secret-id"},
		approleStatus: http.StatusOK,
		approleToken:  "fake-app-token",
		oidcStatus:    http.StatusOK,
		oidcToken:     "fake.oidc.jwt",
		jwtStatus:     http.StatusOK,
		jwtToken:      "fake-scoped-token",
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/sys/health", func(w http.ResponseWriter, r *http.Request) {
		f.calls.health++
		w.WriteHeader(f.healthStatus)
	})
	mux.HandleFunc("/v1/sys/wrapping/unwrap", func(w http.ResponseWriter, r *http.Request) {
		f.calls.unwrap++
		f.lastUnwrapAuthHeader = r.Header.Get("X-Vault-Token")
		if f.unwrapStatus != http.StatusOK {
			w.WriteHeader(f.unwrapStatus)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": f.unwrapPayload})
	})
	mux.HandleFunc("/v1/auth/approle/login", func(w http.ResponseWriter, r *http.Request) {
		f.calls.approle++
		_ = json.NewDecoder(r.Body).Decode(&f.lastApproleBody)
		if f.approleStatus != http.StatusOK {
			w.WriteHeader(f.approleStatus)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"auth": map[string]any{"client_token": f.approleToken},
		})
	})
	mux.HandleFunc("/v1/identity/oidc/token/", func(w http.ResponseWriter, r *http.Request) {
		f.calls.oidc++
		f.lastOIDCAuthHeader = r.Header.Get("X-Vault-Token")
		if f.oidcStatus != http.StatusOK {
			w.WriteHeader(f.oidcStatus)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"token": f.oidcToken},
		})
	})
	mux.HandleFunc("/v1/auth/jwt/login", func(w http.ResponseWriter, r *http.Request) {
		f.calls.jwt++
		_ = json.NewDecoder(r.Body).Decode(&f.lastJWTBody)
		if f.jwtStatus != http.StatusOK {
			w.WriteHeader(f.jwtStatus)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"auth": map[string]any{"client_token": f.jwtToken},
		})
	})
	f.server = httptest.NewServer(mux)
	return f
}

func (f *fakeBao) close() { f.server.Close() }
func (f *fakeBao) addr() string { return f.server.URL }

// Happy path: refresh runs the full chain and writes the scoped token.
func TestRefresh_Happy(t *testing.T) {
	f := newFakeBao()
	defer f.close()

	dir := t.TempDir()
	tokPath := filepath.Join(dir, "bao-token")

	cfg := &AgentConfig{
		Role: "kontango-inventree-prod-01", RoleID: "rid", SecretID: "sid",
		OIDCRole: "kontango-inventree-prod-01-token",
		JWTRole:  "kontango-inventree-jwt-app",
		BaoAddr:  f.addr(),
	}
	res, err := Refresh(context.Background(), cfg, tokPath)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if res.Role != cfg.Role {
		t.Errorf("role mismatch: %s", res.Role)
	}

	got, err := os.ReadFile(tokPath)
	if err != nil {
		t.Fatalf("read token: %v", err)
	}
	if string(got) != f.jwtToken {
		t.Errorf("token mismatch: got %q want %q", got, f.jwtToken)
	}

	// All four calls should have happened exactly once.
	if f.calls.health != 1 || f.calls.approle != 1 || f.calls.oidc != 1 || f.calls.jwt != 1 {
		t.Errorf("call counts: %+v", f.calls)
	}

	// Approle body carries role_id and secret_id.
	if f.lastApproleBody["role_id"] != "rid" || f.lastApproleBody["secret_id"] != "sid" {
		t.Errorf("approle body: %v", f.lastApproleBody)
	}
	// OIDC mint must use the app token, not the agent's own bearer.
	if f.lastOIDCAuthHeader != f.approleToken {
		t.Errorf("oidc auth header: got %q want %q", f.lastOIDCAuthHeader, f.approleToken)
	}
	// JWT login body carries the JWT we minted.
	if f.lastJWTBody["jwt"] != f.oidcToken || f.lastJWTBody["role"] != cfg.JWTRole {
		t.Errorf("jwt body: %v", f.lastJWTBody)
	}

	// File mode is 0640.
	st, _ := os.Stat(tokPath)
	if st.Mode().Perm() != 0640 {
		t.Errorf("token mode: got %o want 0640", st.Mode().Perm())
	}
}

// On any failure, the existing /run/bao-token must NOT be touched.
func TestRefresh_PreservesExistingTokenOnFailure(t *testing.T) {
	f := newFakeBao()
	f.approleStatus = http.StatusForbidden
	defer f.close()

	dir := t.TempDir()
	tokPath := filepath.Join(dir, "bao-token")
	prior := []byte("previous-token-do-not-clobber")
	if err := os.WriteFile(tokPath, prior, 0640); err != nil {
		t.Fatalf("seed prior token: %v", err)
	}

	cfg := &AgentConfig{
		Role: "x", RoleID: "rid", SecretID: "sid",
		OIDCRole: "x-token", JWTRole: "x-jwt-app", BaoAddr: f.addr(),
	}
	if _, err := Refresh(context.Background(), cfg, tokPath); err == nil {
		t.Fatal("expected approle login error")
	}

	got, _ := os.ReadFile(tokPath)
	if string(got) != string(prior) {
		t.Errorf("prior token clobbered: got %q want %q", got, prior)
	}
}

// Validate() catches missing fields before any network call.
func TestRefresh_ConfigValidation(t *testing.T) {
	f := newFakeBao()
	defer f.close()

	cfg := &AgentConfig{Role: "x"} // intentionally minimal
	if _, err := Refresh(context.Background(), cfg, "/tmp/bao-token-nope"); err == nil {
		t.Fatal("expected validation error on incomplete config")
	}
	if f.calls.health+f.calls.approle+f.calls.oidc+f.calls.jwt != 0 {
		t.Errorf("unexpected calls on invalid config: %+v", f.calls)
	}
}

// Health 429 is treated as healthy (standby OK for reads).
func TestRefresh_Health429StandbyOK(t *testing.T) {
	f := newFakeBao()
	f.healthStatus = http.StatusTooManyRequests
	defer f.close()

	dir := t.TempDir()
	cfg := &AgentConfig{
		Role: "x", RoleID: "rid", SecretID: "sid",
		OIDCRole: "x-token", JWTRole: "x-jwt", BaoAddr: f.addr(),
	}
	if _, err := Refresh(context.Background(), cfg, filepath.Join(dir, "tok")); err != nil {
		t.Errorf("standby (429) should be healthy: %v", err)
	}
}

// Health 503 is a hard failure.
func TestRefresh_Health503Fails(t *testing.T) {
	f := newFakeBao()
	f.healthStatus = http.StatusServiceUnavailable
	defer f.close()

	dir := t.TempDir()
	cfg := &AgentConfig{
		Role: "x", RoleID: "rid", SecretID: "sid",
		OIDCRole: "x-token", JWTRole: "x-jwt", BaoAddr: f.addr(),
	}
	_, err := Refresh(context.Background(), cfg, filepath.Join(dir, "tok"))
	if err == nil || !strings.Contains(err.Error(), "bao unreachable") {
		t.Errorf("expected unreachable error, got %v", err)
	}
}

// FetchBundle round-trips the controller response.
func TestFetchBundle(t *testing.T) {
	expected := Bundle{
		Role: "kontango-x-prod-01", RoleID: "rid",
		SecretIDWrapToken: "s.wraptoken", WrapTTLSeconds: 300,
		OIDCRole: "kontango-x-prod-01-token", JWTRole: "kontango-x-jwt-app",
		Tenant: "kontango", App: "x", Deployment: "prod-01", Flavor: "app-host",
		ZitiIdentity: "machine-12345678", EntityID: "ent-1",
		BaoAddr: "http://bao.tango:8200",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/bao-bundle" || r.Method != "POST" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("X-Ziti-Identity"); got != "machine-12345678" {
			t.Errorf("missing/wrong X-Ziti-Identity: %q", got)
		}
		_ = json.NewEncoder(w).Encode(expected)
	}))
	defer srv.Close()

	got, err := FetchBundle(context.Background(), srv.URL, "machine-12345678", nil)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if *got != expected {
		t.Errorf("bundle mismatch:\n got %+v\nwant %+v", *got, expected)
	}
}

// Controller error → typed error containing the body.
func TestFetchBundle_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"no enrollment for machine-12345678"}`))
	}))
	defer srv.Close()

	_, err := FetchBundle(context.Background(), srv.URL, "machine-12345678", nil)
	if err == nil || !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "no enrollment") {
		t.Errorf("expected 404 + body, got %v", err)
	}
}

// InstallBundle: unwrap + login validate + atomic write of /etc/schmutz/agent.json.
func TestInstallBundle(t *testing.T) {
	f := newFakeBao()
	defer f.close()

	dir := t.TempDir()
	agentPath := filepath.Join(dir, "agent.json")

	b := &Bundle{
		Role: "kontango-x-prod-01", RoleID: "rid",
		SecretIDWrapToken: "s.wraptok",
		OIDCRole:          "x-tok", JWTRole: "x-jwt",
		BaoAddr: f.addr(),
		Tenant:  "kontango", App: "x", Deployment: "prod-01", Flavor: "app-host",
		ZitiIdentity: "machine-12345678",
	}
	cfg, err := InstallBundle(context.Background(), b, agentPath)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if cfg.SecretID != "fake-secret-id" {
		t.Errorf("secret_id not threaded from unwrap: %q", cfg.SecretID)
	}
	if f.calls.unwrap != 1 || f.calls.approle != 1 {
		t.Errorf("expected one unwrap + one validate-login, got %+v", f.calls)
	}
	if f.lastUnwrapAuthHeader != "s.wraptok" {
		t.Errorf("unwrap auth header: %q", f.lastUnwrapAuthHeader)
	}

	// On-disk: 0600, parsable, matches.
	st, _ := os.Stat(agentPath)
	if st.Mode().Perm() != 0600 {
		t.Errorf("agent.json mode: got %o want 0600", st.Mode().Perm())
	}
	loaded, err := LoadAgentConfig(agentPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.SecretID != cfg.SecretID || loaded.Role != cfg.Role || loaded.BaoAddr != cfg.BaoAddr {
		t.Errorf("disk config mismatch: %+v vs %+v", loaded, cfg)
	}
}
