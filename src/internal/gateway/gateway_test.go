package gateway_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/KontangoOSS/schmutz/internal/gateway"
	"github.com/KontangoOSS/schmutz/internal/shared"
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
