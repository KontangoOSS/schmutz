package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"git.konoss.org/kore/schmutz/controller/internal/service"
)

func newPulseTestAPI() *API {
	api := newTestAPI()
	api.telemetry = service.NewTelemetryService()
	return api
}

// TestPulseState_GET verifies GET /api/pulse/live → 200 JSON with machines array.
func TestPulseState_GET(t *testing.T) {
	api := newPulseTestAPI()
	req := httptest.NewRequest(http.MethodGet, "/api/pulse/live", nil)
	rr := httptest.NewRecorder()
	api.PulseState(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
	if _, ok := resp["machines"]; !ok {
		t.Error("response missing machines key")
	}
	if _, ok := resp["total"]; !ok {
		t.Error("response missing total key")
	}
}

// TestPulseState_EmptyWhenNoMachines verifies total=0 when no machines have reported.
func TestPulseState_EmptyWhenNoMachines(t *testing.T) {
	api := newPulseTestAPI()
	req := httptest.NewRequest(http.MethodGet, "/api/pulse/live", nil)
	rr := httptest.NewRecorder()
	api.PulseState(rr, req)

	var resp map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&resp)
	total, _ := resp["total"].(float64)
	if total != 0 {
		t.Errorf("expected total=0, got %v", total)
	}
}

// TestPulseState_ContentType verifies Content-Type is application/json.
func TestPulseState_ContentType(t *testing.T) {
	api := newPulseTestAPI()
	req := httptest.NewRequest(http.MethodGet, "/api/pulse/live", nil)
	rr := httptest.NewRecorder()
	api.PulseState(rr, req)

	ct := rr.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type=%q, want application/json", ct)
	}
}

// TestPulseMachine_NotFound verifies GET /api/pulse/<id> → 404 when machine has no KV.
func TestPulseMachine_NotFound(t *testing.T) {
	api := newPulseTestAPI()
	req := httptest.NewRequest(http.MethodGet, "/api/pulse/unknown-machine-id", nil)
	req.URL.Path = "/api/pulse/unknown-machine-id"
	rr := httptest.NewRecorder()
	api.PulseMachine(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown machine, got %d", rr.Code)
	}
}

// TestPulseMachine_LiveRoutesToPulseState verifies /api/pulse/live is dispatched to PulseState.
func TestPulseMachine_LiveRoutesToPulseState(t *testing.T) {
	api := newPulseTestAPI()
	req := httptest.NewRequest(http.MethodGet, "/api/pulse/live", nil)
	req.URL.Path = "/api/pulse/live"
	rr := httptest.NewRecorder()
	api.PulseMachine(rr, req)

	// PulseMachine delegates to PulseState for "live" → 200 with machines
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
	if _, ok := resp["machines"]; !ok {
		t.Error("response missing machines key — PulseMachine should delegate to PulseState for 'live'")
	}
}
