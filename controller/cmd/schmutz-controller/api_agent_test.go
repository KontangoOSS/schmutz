package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"git.konoss.org/kore/schmutz/controller/internal/service"
)

// newAgentTestAPI returns an API with telemetry wired up (required by Heartbeat/LiveMachines/PushInstruction).
func newAgentTestAPI() *API {
	api := newTestAPI()
	api.telemetry = service.NewTelemetryService()
	return api
}

// TestHeartbeat_WrongMethod verifies GET → 405.
func TestHeartbeat_WrongMethod(t *testing.T) {
	api := newAgentTestAPI()
	req := httptest.NewRequest(http.MethodGet, "/api/heartbeat", nil)
	rr := httptest.NewRecorder()
	api.Heartbeat(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

// TestHeartbeat_NoBody verifies that an empty body returns 400.
// Empty body fails JSON decode → bad request.
func TestHeartbeat_NoBody(t *testing.T) {
	api := newAgentTestAPI()
	req := httptest.NewRequest(http.MethodPost, "/api/heartbeat", strings.NewReader(""))
	rr := httptest.NewRecorder()
	api.Heartbeat(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// TestHeartbeat_ValidLegacyJSON verifies a bare Heartbeat JSON object → 200.
func TestHeartbeat_ValidLegacyJSON(t *testing.T) {
	api := newAgentTestAPI()
	body := `{"mid":"test-machine","type":"heartbeat","host":"testbox","os":"linux"}`
	req := httptest.NewRequest(http.MethodPost, "/api/heartbeat", strings.NewReader(body))
	rr := httptest.NewRecorder()
	api.Heartbeat(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestHeartbeat_MissingMachineID verifies that a body with no machine_id and no header → 400.
func TestHeartbeat_MissingMachineID(t *testing.T) {
	api := newAgentTestAPI()
	// Neither X-Machine-ID header nor mid field in body
	body := `{"type":"heartbeat"}`
	req := httptest.NewRequest(http.MethodPost, "/api/heartbeat", strings.NewReader(body))
	rr := httptest.NewRecorder()
	api.Heartbeat(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// TestHeartbeat_MachineIDFromHeader verifies machine_id can come from the X-Machine-ID header.
func TestHeartbeat_MachineIDFromHeader(t *testing.T) {
	api := newAgentTestAPI()
	body := `{"type":"heartbeat","host":"testbox","os":"linux"}`
	req := httptest.NewRequest(http.MethodPost, "/api/heartbeat", strings.NewReader(body))
	req.Header.Set("X-Machine-ID", "hdr-machine-01")
	rr := httptest.NewRecorder()
	api.Heartbeat(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestLiveMachines_GET verifies GET /api/machines/live → 200 JSON with machines key.
func TestLiveMachines_GET(t *testing.T) {
	api := newAgentTestAPI()
	req := httptest.NewRequest(http.MethodGet, "/api/machines/live", nil)
	rr := httptest.NewRecorder()
	api.LiveMachines(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type=%q, want application/json", ct)
	}
}

// TestLiveMachines_WrongMethod verifies POST → 405.
func TestLiveMachines_WrongMethod(t *testing.T) {
	api := newAgentTestAPI()
	req := httptest.NewRequest(http.MethodPost, "/api/machines/live", nil)
	rr := httptest.NewRecorder()
	api.LiveMachines(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

// TestLiveMachines_EmptyResult verifies machines array is present even when empty.
func TestLiveMachines_EmptyResult(t *testing.T) {
	api := newAgentTestAPI()
	req := httptest.NewRequest(http.MethodGet, "/api/machines/live", nil)
	rr := httptest.NewRecorder()
	api.LiveMachines(rr, req)

	if rr.Body.Len() == 0 {
		t.Error("empty response body")
	}
	// Body must be valid JSON
	var resp map[string]interface{}
	if err2 := json.NewDecoder(rr.Body).Decode(&resp); err2 != nil {
		t.Fatalf("response not valid JSON: %v", err2)
	}
	if _, ok := resp["machines"]; !ok {
		t.Error("response missing machines key")
	}
}

// TestPushInstruction_WrongMethod verifies GET → 405.
func TestPushInstruction_WrongMethod(t *testing.T) {
	api := newAgentTestAPI()
	req := httptest.NewRequest(http.MethodGet, "/api/machines/push/m1", nil)
	rr := httptest.NewRecorder()
	api.PushInstruction(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

// TestPushInstruction_NoMachineID verifies POST with empty machine ID → 400.
func TestPushInstruction_NoMachineID(t *testing.T) {
	api := newAgentTestAPI()
	// Path strips "/api/machines/push/" → empty string
	req := httptest.NewRequest(http.MethodPost, "/api/machines/push/", strings.NewReader(`{"type":"reload"}`))
	rr := httptest.NewRecorder()
	api.PushInstruction(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// TestPushInstruction_EmptyType verifies POST with empty type → 400.
func TestPushInstruction_EmptyType(t *testing.T) {
	api := newAgentTestAPI()
	req := httptest.NewRequest(http.MethodPost, "/api/machines/push/m1", strings.NewReader(`{"type":""}`))
	rr := httptest.NewRecorder()
	api.PushInstruction(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// TestPushInstruction_NoConnectedAgent verifies POST with valid type but no connected agent → 404.
func TestPushInstruction_NoConnectedAgent(t *testing.T) {
	api := newAgentTestAPI()
	req := httptest.NewRequest(http.MethodPost, "/api/machines/push/nonexistent-machine", strings.NewReader(`{"type":"reload"}`))
	rr := httptest.NewRecorder()
	api.PushInstruction(rr, req)

	// telemetry.Push returns error for unknown machine → 404
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

// TestPushInstruction_BadJSON verifies POST with bad JSON → 400.
func TestPushInstruction_BadJSON(t *testing.T) {
	api := newAgentTestAPI()
	req := httptest.NewRequest(http.MethodPost, "/api/machines/push/m1", strings.NewReader(`{bad`))
	rr := httptest.NewRecorder()
	api.PushInstruction(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}
