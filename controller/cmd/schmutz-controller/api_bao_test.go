package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newBaoTestAPI returns an API with a StoreService backed by an unreachable Bao address.
// This exercises error paths (connection refused) rather than nil-panic paths.
func newBaoTestAPI(t *testing.T) *API {
	t.Helper()
	api := newTestAPI()
	api.store = newUnreachableStore(t)
	return api
}

// TestBaoHealth_GET verifies GET → error JSON when Bao is unreachable.
func TestBaoHealth_GET(t *testing.T) {
	api := newBaoTestAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/api/bao/health", nil)
	rr := httptest.NewRecorder()
	api.BaoHealth(rr, req)

	// Bao unreachable → jsonOrErr writes error JSON with 500
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

// TestBaoSealStatus_GET verifies GET → error JSON when Bao is unreachable.
func TestBaoSealStatus_GET(t *testing.T) {
	api := newBaoTestAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/api/bao/seal-status", nil)
	rr := httptest.NewRecorder()
	api.BaoSealStatus(rr, req)

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

// TestBaoKV_NoMount verifies /api/bao/kv/ with no mount segment → 400 usage error.
func TestBaoKV_NoMount(t *testing.T) {
	api := newBaoTestAPI(t)
	// Path strips to "" — only one part → bad request
	req := httptest.NewRequest(http.MethodGet, "/api/bao/kv/", nil)
	req.URL.Path = "/api/bao/kv/"
	rr := httptest.NewRecorder()
	api.BaoKV(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "usage") {
		t.Errorf("expected usage error, got: %s", body)
	}
}

// TestBaoKV_GET_UnreachableBao verifies GET /api/bao/kv/secret/test → error JSON when Bao unreachable.
func TestBaoKV_GET_UnreachableBao(t *testing.T) {
	api := newBaoTestAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/api/bao/kv/secret/test", nil)
	req.URL.Path = "/api/bao/kv/secret/test"
	rr := httptest.NewRecorder()
	api.BaoKV(rr, req)

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

// TestBaoKV_PUT_UnreachableBao verifies PUT → error JSON when Bao unreachable.
func TestBaoKV_PUT_UnreachableBao(t *testing.T) {
	api := newBaoTestAPI(t)
	req := httptest.NewRequest(http.MethodPut, "/api/bao/kv/secret/mypath",
		strings.NewReader(`{"key":"value"}`))
	req.URL.Path = "/api/bao/kv/secret/mypath"
	rr := httptest.NewRecorder()
	api.BaoKV(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}

// TestBaoKV_DELETE_UnreachableBao verifies DELETE → error JSON when Bao unreachable.
func TestBaoKV_DELETE_UnreachableBao(t *testing.T) {
	api := newBaoTestAPI(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/bao/kv/secret/mypath", nil)
	req.URL.Path = "/api/bao/kv/secret/mypath"
	rr := httptest.NewRecorder()
	api.BaoKV(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}

// TestBaoKV_MethodNotAllowed verifies PATCH (unsupported) → 405.
func TestBaoKV_MethodNotAllowed(t *testing.T) {
	api := newBaoTestAPI(t)
	req := httptest.NewRequest(http.MethodPatch, "/api/bao/kv/secret/mypath", nil)
	req.URL.Path = "/api/bao/kv/secret/mypath"
	rr := httptest.NewRecorder()
	api.BaoKV(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

// TestBaoPolicies_GET_UnreachableBao verifies GET → error JSON when Bao unreachable.
func TestBaoPolicies_GET_UnreachableBao(t *testing.T) {
	api := newBaoTestAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/api/bao/policies", nil)
	rr := httptest.NewRecorder()
	api.BaoPolicies(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
}

// TestBaoPolicies_DELETE_MethodNotAllowed verifies DELETE → 405 (only GET/POST).
func TestBaoPolicies_DELETE_MethodNotAllowed(t *testing.T) {
	api := newBaoTestAPI(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/bao/policies", nil)
	rr := httptest.NewRecorder()
	api.BaoPolicies(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

// TestBaoAppRoles_GET_UnreachableBao verifies GET → error JSON when Bao unreachable.
func TestBaoAppRoles_GET_UnreachableBao(t *testing.T) {
	api := newBaoTestAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/api/bao/approles", nil)
	rr := httptest.NewRecorder()
	api.BaoAppRoles(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}

// TestBaoAppRoles_DELETE_MethodNotAllowed verifies DELETE on collection → 405.
func TestBaoAppRoles_DELETE_MethodNotAllowed(t *testing.T) {
	api := newBaoTestAPI(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/bao/approles", nil)
	rr := httptest.NewRecorder()
	api.BaoAppRoles(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

// TestBaoTokenCreate_GET_MethodNotAllowed verifies GET on token create → 405.
func TestBaoTokenCreate_GET_MethodNotAllowed(t *testing.T) {
	api := newBaoTestAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/api/bao/tokens", nil)
	rr := httptest.NewRecorder()
	api.BaoTokenCreate(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

// TestBaoKV_ListSubpath verifies /api/bao/kv/{mount}/list/{path} triggers list logic.
func TestBaoKV_ListSubpath(t *testing.T) {
	api := newBaoTestAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/api/bao/kv/secret/list/applications/", nil)
	req.URL.Path = "/api/bao/kv/secret/list/applications/"
	rr := httptest.NewRecorder()
	api.BaoKV(rr, req)

	// List hits Bao → unreachable → error JSON
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
}
