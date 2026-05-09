package gateway_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"git.konoss.org/kore/schmutz/agent/internal/gateway"
	"git.konoss.org/kore/schmutz/shared"
)

// fakeSpecServer returns a minimal OpenAPI 3.0 spec at /openapi.json.
func fakeSpecServer(t *testing.T) *httptest.Server {
	t.Helper()
	spec := map[string]any{
		"openapi": "3.0.3",
		"info":    map[string]any{"title": "Test API", "version": "1.0"},
		"paths":   map[string]any{"/health": map[string]any{}},
	}
	b, _ := json.Marshal(spec)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/openapi.json" {
			w.Header().Set("Content-Type", "application/json")
			w.Write(b)
			return
		}
		http.NotFound(w, r)
	}))
}

func TestDiscover_FindsOpenAPISpec(t *testing.T) {
	srv := fakeSpecServer(t)
	defer srv.Close()

	var p int
	fmt.Sscanf(srv.Listener.Addr().String(), "127.0.0.1:%d", &p)
	port := uint16(p)

	cfg := gateway.DiscoverConfig{
		AgentJSON: gateway.AgentInfo{
			Tenant: "kontango", App: "test", Deployment: "prod-01",
			BaoAddr: "http://127.0.0.1:1",
		},
		GatewayConfig: &shared.GatewayConfig{
			Enabled:  true,
			Services: []shared.GatewayService{{Name: "testapp", Port: port}},
		},
		SkipBaoWrite:     true,
		SkipForgejoWrite: true,
	}

	entries, err := gateway.Discover(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least 1 entry")
	}
	if entries[0].Name != "testapp" {
		t.Errorf("Name: got %q want testapp", entries[0].Name)
	}
	if entries[0].SpecSource != "openapi" {
		t.Errorf("SpecSource: got %q want openapi", entries[0].SpecSource)
	}
	if len(entries[0].SpecJSON) == 0 {
		t.Error("SpecJSON should not be empty")
	}
}

func TestDiscover_GeneratesStubWhenNoSpec(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	var p int
	fmt.Sscanf(srv.Listener.Addr().String(), "127.0.0.1:%d", &p)

	cfg := gateway.DiscoverConfig{
		AgentJSON: gateway.AgentInfo{Tenant: "kontango", App: "test", Deployment: "prod-01"},
		GatewayConfig: &shared.GatewayConfig{
			Enabled:  true,
			Services: []shared.GatewayService{{Name: "nospec", Port: uint16(p)}},
		},
		SkipBaoWrite:     true,
		SkipForgejoWrite: true,
	}

	entries, err := gateway.Discover(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected 1 entry")
	}
	if entries[0].SpecSource != "stub" {
		t.Errorf("SpecSource: got %q want stub", entries[0].SpecSource)
	}
	if len(entries[0].SpecJSON) == 0 {
		t.Error("stub SpecJSON should not be empty")
	}
}

func TestWriteSpecs_WritesToBao(t *testing.T) {
	var written []string
	baoSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/v1/secret/data/") {
			written = append(written, r.URL.Path)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data":{}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer baoSrv.Close()

	entries := []gateway.ServiceEntry{
		{Name: "myapp", Port: 9090, SpecSource: "openapi",
			SpecJSON: []byte(`{"openapi":"3.0.3","info":{"title":"T","version":"1"},"paths":{}}`)},
	}

	cfg := gateway.BaoWriteConfig{
		BaoAddr:    baoSrv.URL,
		BaoToken:   "test-token",
		Tenant:     "kontango",
		App:        "myapp",
		Deployment: "prod-01",
	}

	if err := gateway.WriteSpecs(context.Background(), cfg, entries); err != nil {
		t.Fatalf("WriteSpecs: %v", err)
	}

	found := false
	for _, p := range written {
		if strings.Contains(p, "apps/myapp/prod-01/api-spec") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected write to apps/myapp/prod-01/api-spec, got: %v", written)
	}
}

func TestWriteSpecToForgejo_CreatesFileWhenAbsent(t *testing.T) {
	var posted bool
	forgeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"not found"}`))
			return
		}
		if r.Method == http.MethodPost {
			posted = true
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"commit":{"sha":"abc123"}}`))
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer forgeSrv.Close()

	cfg := gateway.ForgejoWriteConfig{
		ForgejoURL:   forgeSrv.URL,
		ForgejoToken: "test-token",
		ForgejoOrg:   "public",
		App:          "myapp",
	}
	spec := []byte(`{"openapi":"3.0.3","info":{"title":"T","version":"1"},"paths":{}}`)

	if err := gateway.WriteSpecToForgejo(context.Background(), cfg, spec); err != nil {
		t.Fatalf("WriteSpecToForgejo: %v", err)
	}
	if !posted {
		t.Error("expected POST to create api.json")
	}
}

func TestWriteSpecToForgejo_SkipsWhenFileExists(t *testing.T) {
	var postCalled bool
	forgeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"sha":"existing","content":"e30K","encoding":"base64"}`))
			return
		}
		postCalled = true
		w.WriteHeader(http.StatusCreated)
	}))
	defer forgeSrv.Close()

	cfg := gateway.ForgejoWriteConfig{
		ForgejoURL: forgeSrv.URL, ForgejoToken: "tok",
		ForgejoOrg: "public", App: "myapp",
	}
	if err := gateway.WriteSpecToForgejo(context.Background(), cfg,
		[]byte(`{"openapi":"3.0.3"}`)); err != nil {
		t.Fatalf("WriteSpecToForgejo: %v", err)
	}
	if postCalled {
		t.Error("should not POST when file already exists")
	}
}

func TestServer_IndexReturnsServices(t *testing.T) {
	entries := []gateway.ServiceEntry{
		{Name: "ticketarr", Port: 9090, Protocol: "http",
			SpecSource: "openapi", EndpointCount: 42,
			SpecJSON: []byte(`{"openapi":"3.0.3"}`)},
	}
	info := gateway.HostInfo{
		ZitiIdentity: "machine-abc123",
		App:          "ticketarr",
		Deployment:   "prod-01",
		Tenant:       "kontango",
	}
	srv := gateway.NewServer(info, entries)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d", w.Code)
	}
	var resp struct {
		Host     string           `json:"host"`
		Services []map[string]any `json:"services"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Services) != 1 {
		t.Fatalf("services: got %d want 1", len(resp.Services))
	}
	if resp.Services[0]["name"] != "ticketarr" {
		t.Errorf("name: got %v", resp.Services[0]["name"])
	}
}

func TestServer_SpecEndpointReturnsJSON(t *testing.T) {
	spec := []byte(`{"openapi":"3.0.3","info":{"title":"Test","version":"1"},"paths":{}}`)
	entries := []gateway.ServiceEntry{
		{Name: "ticketarr", Port: 9090, SpecSource: "openapi", SpecJSON: spec},
	}
	srv := gateway.NewServer(gateway.HostInfo{App: "ticketarr"}, entries)

	req := httptest.NewRequest(http.MethodGet, "/services/ticketarr/spec", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: got %q want application/json", ct)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("openapi")) {
		t.Error("body should contain openapi spec")
	}
}

func TestServer_UnknownServiceReturns404(t *testing.T) {
	srv := gateway.NewServer(gateway.HostInfo{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/services/doesnotexist/spec", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d want 404", w.Code)
	}
}

func TestServer_HealthReturnsOK(t *testing.T) {
	srv := gateway.NewServer(gateway.HostInfo{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status: got %d", w.Code)
	}
}

func TestServer_DocsServesScalarHTML(t *testing.T) {
	entries := []gateway.ServiceEntry{
		{Name: "ticketarr", Port: 9090, SpecSource: "openapi",
			SpecJSON: []byte(`{"openapi":"3.0.3"}`)},
	}
	srv := gateway.NewServer(gateway.HostInfo{App: "ticketarr"}, entries)

	req := httptest.NewRequest(http.MethodGet, "/docs/ticketarr", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "ticketarr") {
		t.Error("body should contain service title")
	}
	if !strings.Contains(body, "/services/ticketarr/spec") {
		t.Error("body should contain spec URL")
	}
	if !strings.Contains(body, "scalar") {
		t.Error("body should reference scalar")
	}
}
