package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KontangoOSS/schmutz/controller/internal/service"
)

// newOpsTestAPI returns an API wired up with all services needed for ops handlers.
func newOpsTestAPI(t *testing.T) *API {
	t.Helper()
	api := newTestAPI()
	api.ziti = newUnreachableZiti()
	api.store = newTestStore()
	api.hpStore = service.NewTestStore()
	api.security = service.NewSecurityService(nil)
	api.discovery = service.NewDiscoveryService(nil)
	api.telemetry = service.NewTelemetryService()
	return api
}

// --- Approve ---

func TestApprove_WrongMethod(t *testing.T) {
	api := newOpsTestAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/api/ops/approve", nil)
	rr := httptest.NewRecorder()
	api.Approve(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

func TestApprove_MissingMachineID(t *testing.T) {
	api := newOpsTestAPI(t)
	req := httptest.NewRequest(http.MethodPost, "/api/ops/approve", strings.NewReader(`{}`))
	rr := httptest.NewRecorder()
	api.Approve(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestApprove_ZitiAuthFailure(t *testing.T) {
	api := newOpsTestAPI(t)
	req := httptest.NewRequest(http.MethodPost, "/api/ops/approve",
		strings.NewReader(`{"machine_id":"m1"}`))
	rr := httptest.NewRecorder()
	api.Approve(rr, req)

	// Ziti is unreachable → 502 bad gateway
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rr.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
}

// --- Deny ---

func TestDeny_WrongMethod(t *testing.T) {
	api := newOpsTestAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/api/ops/deny", nil)
	rr := httptest.NewRecorder()
	api.Deny(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

func TestDeny_MissingMachineID(t *testing.T) {
	api := newOpsTestAPI(t)
	req := httptest.NewRequest(http.MethodPost, "/api/ops/deny", strings.NewReader(`{}`))
	rr := httptest.NewRecorder()
	api.Deny(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestDeny_ZitiAuthFailure(t *testing.T) {
	api := newOpsTestAPI(t)
	req := httptest.NewRequest(http.MethodPost, "/api/ops/deny",
		strings.NewReader(`{"machine_id":"m1","reason":"test"}`))
	rr := httptest.NewRecorder()
	api.Deny(rr, req)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rr.Code)
	}
}

// --- Machine ---

func TestMachine_WrongMethod(t *testing.T) {
	api := newOpsTestAPI(t)
	// Machine only allows GET
	req := httptest.NewRequest(http.MethodPost, "/api/machines/some-id", nil)
	req.URL.Path = "/api/machines/some-id"
	rr := httptest.NewRecorder()
	api.Machine(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

func TestMachine_NilStore_NotFound(t *testing.T) {
	api := newOpsTestAPI(t)
	// store is nil → Machine falls through to GetMachine → 404
	req := httptest.NewRequest(http.MethodGet, "/api/machines/test-id", nil)
	req.URL.Path = "/api/machines/test-id"
	rr := httptest.NewRecorder()
	api.Machine(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

// --- Profiles ---

func TestProfiles_WrongMethod(t *testing.T) {
	api := newOpsTestAPI(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/profiles", nil)
	rr := httptest.NewRecorder()
	api.Profiles(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

// --- Profile ---

func TestProfile_WrongMethod(t *testing.T) {
	api := newOpsTestAPI(t)
	req := httptest.NewRequest(http.MethodPost, "/api/profiles/myprofile", nil)
	req.URL.Path = "/api/profiles/myprofile"
	rr := httptest.NewRecorder()
	api.Profile(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

// --- KnownMachine ---

func TestKnownMachine_NilStore_NotFound(t *testing.T) {
	api := newOpsTestAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/api/known/testhost", nil)
	req.URL.Path = "/api/known/testhost"
	rr := httptest.NewRecorder()
	api.KnownMachine(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

// --- DebugEvents ---

func TestDebugEvents_AllTypes(t *testing.T) {
	api := newOpsTestAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/api/debug/events/machine-1", nil)
	req.URL.Path = "/api/debug/events/machine-1"
	rr := httptest.NewRecorder()
	api.DebugEvents(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
	if _, ok := resp["machine_id"]; !ok {
		t.Error("response missing machine_id key")
	}
}

func TestDebugEvents_SpecificType(t *testing.T) {
	api := newOpsTestAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/api/debug/events/machine-1/hb", nil)
	req.URL.Path = "/api/debug/events/machine-1/hb"
	rr := httptest.NewRecorder()
	api.DebugEvents(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
	if resp["type"] != "hb" {
		t.Errorf("type=%q, want hb", resp["type"])
	}
}

// --- SecurityCheckDuplicate ---

func TestSecurityCheckDuplicate_ValidJSON(t *testing.T) {
	api := newOpsTestAPI(t)
	body := `{"hostname":"host1","machine_id":"m1","macs":["aa:bb:cc:dd:ee:ff"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/security/check-duplicate", strings.NewReader(body))
	rr := httptest.NewRecorder()
	api.SecurityCheckDuplicate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
	if _, ok := resp["duplicate"]; !ok {
		t.Error("response missing duplicate key")
	}
}

func TestSecurityCheckDuplicate_BadJSON(t *testing.T) {
	api := newOpsTestAPI(t)
	req := httptest.NewRequest(http.MethodPost, "/api/security/check-duplicate", strings.NewReader(`{bad`))
	rr := httptest.NewRecorder()
	api.SecurityCheckDuplicate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// --- SecurityValidateFingerprint ---

func TestSecurityValidateFingerprint_ValidJSON(t *testing.T) {
	api := newOpsTestAPI(t)
	body := `{"machine_id":"m1","hardware_hash":"abc123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/security/validate-fingerprint", strings.NewReader(body))
	rr := httptest.NewRecorder()
	api.SecurityValidateFingerprint(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
	if _, ok := resp["valid"]; !ok {
		t.Error("response missing valid key")
	}
}

// --- SecurityAudit ---

func TestSecurityAudit_ValidJSON(t *testing.T) {
	api := newOpsTestAPI(t)
	body := `{"machine_id":"m1","event":"test","source_ip":"1.2.3.4"}`
	req := httptest.NewRequest(http.MethodPost, "/api/security/audit", strings.NewReader(body))
	rr := httptest.NewRecorder()
	api.SecurityAudit(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
}

func TestSecurityAudit_BadJSON(t *testing.T) {
	api := newOpsTestAPI(t)
	req := httptest.NewRequest(http.MethodPost, "/api/security/audit", strings.NewReader(`{bad`))
	rr := httptest.NewRecorder()
	api.SecurityAudit(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// --- DiscoveryMatch ---

func TestDiscoveryMatch_ValidJSON(t *testing.T) {
	api := newOpsTestAPI(t)
	body := `{"hostname":"host1","machine_id":"m1","hardware_hash":"hash1","macs":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/discovery/match", strings.NewReader(body))
	rr := httptest.NewRecorder()
	api.DiscoveryMatch(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
}

func TestDiscoveryMatch_BadJSON(t *testing.T) {
	api := newOpsTestAPI(t)
	req := httptest.NewRequest(http.MethodPost, "/api/discovery/match", strings.NewReader(`{bad`))
	rr := httptest.NewRecorder()
	api.DiscoveryMatch(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// --- DiscoveryAutoClaim ---

func TestDiscoveryAutoClaim_ZitiAuthFailure(t *testing.T) {
	api := newOpsTestAPI(t)
	body := `{"ziti_id":"some-id","match":{"entity_id":"e1","match_type":"hostname","confidence":"high"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/discovery/auto-claim", strings.NewReader(body))
	rr := httptest.NewRecorder()
	api.DiscoveryAutoClaim(rr, req)

	// Ziti unreachable → 500
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
	if _, ok := resp["error"]; !ok {
		t.Error("response missing error key")
	}
}

func TestDiscoveryAutoClaim_BadJSON(t *testing.T) {
	api := newOpsTestAPI(t)
	req := httptest.NewRequest(http.MethodPost, "/api/discovery/auto-claim", strings.NewReader(`{bad`))
	rr := httptest.NewRecorder()
	api.DiscoveryAutoClaim(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}
