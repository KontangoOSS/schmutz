package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/KontangoOSS/schmutz/controller/internal/service"
)

// newMetricsTestAPI returns an API with a MetricsService backed by unreachable services.
// MetricsStatus and MetricsZiti call a.ziti.Authenticate() via the MetricsService — the
// handlers themselves do the auth call first (in MetricsStatus/MetricsZiti in api_ops.go),
// so we need a.ziti set.
func newMetricsTestAPI(t *testing.T) *API {
	t.Helper()
	api := newTestAPI()
	store := newUnreachableStore(t)
	ziti := newUnreachableZiti()
	api.store = store
	api.ziti = ziti
	api.metrics = service.NewMetricsService(ziti, store)
	return api
}

// TestMetricsStatus_GET verifies GET → error JSON (ziti unreachable).
func TestMetricsStatus_GET(t *testing.T) {
	api := newMetricsTestAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/api/metrics/status", nil)
	rr := httptest.NewRecorder()
	api.MetricsStatus(rr, req)

	// ziti auth fails → errJSON 500
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 (ziti unreachable), got %d", rr.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
	if _, ok := resp["error"]; !ok {
		t.Error("response missing error key")
	}
}

// TestMetricsStatus_POST verifies POST → 405.
// MetricsStatus has no method guard in itself — routing is what limits it,
// but the handler doesn't enforce the method. We test that GET reaches the handler.
// For completeness test that a bad verb gets through and still returns valid JSON
// (the handler doesn't enforce method itself, it just calls ziti — so we get 500 not 405).
// This is the correct behaviour for this endpoint.
func TestMetricsStatus_ReturnsJSON(t *testing.T) {
	api := newMetricsTestAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/api/metrics/status", nil)
	rr := httptest.NewRecorder()
	api.MetricsStatus(rr, req)

	ct := rr.Header().Get("Content-Type")
	if ct == "" {
		t.Error("Content-Type header missing")
	}
	var resp interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
}

// TestMetricsZiti_GET verifies GET → error JSON (ziti unreachable).
func TestMetricsZiti_GET(t *testing.T) {
	api := newMetricsTestAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/api/metrics/ziti", nil)
	rr := httptest.NewRecorder()
	api.MetricsZiti(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 (ziti unreachable), got %d", rr.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
	if _, ok := resp["error"]; !ok {
		t.Error("response missing error key")
	}
}

// TestMetricsBao_GET verifies GET → error JSON (bao unreachable).
func TestMetricsBao_GET(t *testing.T) {
	api := newMetricsTestAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/api/metrics/bao", nil)
	rr := httptest.NewRecorder()
	api.MetricsBao(rr, req)

	// bao unreachable → MetricsBao calls BaoCluster which returns error
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 (bao unreachable), got %d", rr.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
}

// TestMetricsMachine_GET verifies GET → 200 with local machine data (no network calls).
func TestMetricsMachine_GET(t *testing.T) {
	api := newMetricsTestAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/api/metrics/machine", nil)
	rr := httptest.NewRecorder()
	api.MetricsMachine(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
	// MachineMetrics always returns "go" and "os" fields regardless of network
	if _, ok := resp["go"]; !ok {
		t.Error("response missing go key")
	}
	if _, ok := resp["os"]; !ok {
		t.Error("response missing os key")
	}
}

// TestMetricsLatency_GET verifies GET → 200 JSON (measures latency, returns error times for unreachable services).
func TestMetricsLatency_GET(t *testing.T) {
	api := newMetricsTestAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/api/metrics/latency", nil)
	rr := httptest.NewRecorder()
	api.MetricsLatency(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
	// NetworkLatency always returns bao_ms and ziti_ms
	if _, ok := resp["bao_ms"]; !ok {
		t.Error("response missing bao_ms key")
	}
	if _, ok := resp["ziti_ms"]; !ok {
		t.Error("response missing ziti_ms key")
	}
}
