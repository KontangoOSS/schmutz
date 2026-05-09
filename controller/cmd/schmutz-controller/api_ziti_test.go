package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"git.konoss.org/kore/schmutz/controller/internal/service"
)

// newZitiTestAPI returns an API with a ZitiService pointing at an unreachable address.
// All Ziti handlers call zitiToken → Authenticate, which will fail with a connection error.
// This lets us verify error-path behaviour (correct error JSON, no panics) without mocking.
func newZitiTestAPI() *API {
	api := newTestAPI()
	api.ziti = &service.ZitiService{
		Addr: "127.0.0.1:19998",
		User: "admin",
		Pass: "x",
	}
	return api
}

// assertZitiError verifies the response is a 500 JSON error (auth failed).
func assertZitiError(t *testing.T, rr *httptest.ResponseRecorder) {
	t.Helper()
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 (ziti auth failed), got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
	if _, ok := resp["error"]; !ok {
		t.Error("response missing error key")
	}
}

// TestZitiVersion_AuthFail verifies GET with unreachable Ziti → 500 error JSON.
func TestZitiVersion_AuthFail(t *testing.T) {
	api := newZitiTestAPI()
	req := httptest.NewRequest(http.MethodGet, "/api/ziti/version", nil)
	rr := httptest.NewRecorder()
	api.ZitiVersion(rr, req)
	assertZitiError(t, rr)
}

// TestZitiIdentities_AuthFail verifies GET with unreachable Ziti → 500 error JSON.
func TestZitiIdentities_AuthFail(t *testing.T) {
	api := newZitiTestAPI()
	req := httptest.NewRequest(http.MethodGet, "/api/ziti/identities", nil)
	rr := httptest.NewRecorder()
	api.ZitiIdentities(rr, req)
	assertZitiError(t, rr)
}

// TestZitiIdentities_POST_AuthFail verifies POST with unreachable Ziti → 500.
func TestZitiIdentities_POST_AuthFail(t *testing.T) {
	api := newZitiTestAPI()
	req := httptest.NewRequest(http.MethodPost, "/api/ziti/identities", nil)
	rr := httptest.NewRecorder()
	api.ZitiIdentities(rr, req)
	assertZitiError(t, rr)
}

// TestZitiServices_AuthFail verifies GET with unreachable Ziti → 500 error JSON.
func TestZitiServices_AuthFail(t *testing.T) {
	api := newZitiTestAPI()
	req := httptest.NewRequest(http.MethodGet, "/api/ziti/services", nil)
	rr := httptest.NewRecorder()
	api.ZitiServices(rr, req)
	assertZitiError(t, rr)
}

// TestZitiServices_DELETE_MethodNotAllowed verifies DELETE is rejected (only GET/POST).
// ZitiServices calls zitiToken first — auth fails → 500 before method check would fire.
// The method not allowed is for the per-resource ZitiService handler, not the collection.
// For ZitiService (singular), DELETE is valid. For ZitiServices (plural), DELETE → 405.
// Since auth runs first in all handlers, an auth failure returns 500 for any method.
// Test that invalid method on ZitiService (resource) returns 405 after a successful token.
// Without live Ziti we can only verify auth failure (500), not method enforcement (405).
// We test the 500 path here and document the intended method restriction.
func TestZitiServices_DELETE_ReturnsError(t *testing.T) {
	api := newZitiTestAPI()
	req := httptest.NewRequest(http.MethodDelete, "/api/ziti/services", nil)
	rr := httptest.NewRecorder()
	// ZitiServices collection: DELETE → goes through zitiToken (fails) → 500
	api.ZitiServices(rr, req)
	// Auth failure takes precedence over method check
	if rr.Code != http.StatusInternalServerError && rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 500 or 405, got %d", rr.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
}

// TestZitiSummary_AuthFail verifies GET with unreachable Ziti → 500 error JSON.
func TestZitiSummary_AuthFail(t *testing.T) {
	api := newZitiTestAPI()
	req := httptest.NewRequest(http.MethodGet, "/api/ziti/summary", nil)
	rr := httptest.NewRecorder()
	api.ZitiSummary(rr, req)
	assertZitiError(t, rr)
}

// TestZitiFind_AuthFail verifies GET /api/ziti/find/identities/foo → 500 error JSON.
func TestZitiFind_AuthFail(t *testing.T) {
	api := newZitiTestAPI()
	req := httptest.NewRequest(http.MethodGet, "/api/ziti/find/identities/testname", nil)
	req.URL.Path = "/api/ziti/find/identities/testname"
	rr := httptest.NewRecorder()
	api.ZitiFind(rr, req)
	assertZitiError(t, rr)
}

// TestZitiFind_MissingType verifies /api/ziti/find/ with no type/name → 400.
func TestZitiFind_MissingType(t *testing.T) {
	api := newZitiTestAPI()
	// path after stripping "/api/ziti/find/" → "" → splits into 1 part → bad request
	req := httptest.NewRequest(http.MethodGet, "/api/ziti/find/", nil)
	req.URL.Path = "/api/ziti/find/"
	rr := httptest.NewRecorder()
	api.ZitiFind(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing type/name, got %d", rr.Code)
	}
}

// TestZitiRouters_AuthFail verifies GET with unreachable Ziti → 500 error JSON.
func TestZitiRouters_AuthFail(t *testing.T) {
	api := newZitiTestAPI()
	req := httptest.NewRequest(http.MethodGet, "/api/ziti/routers", nil)
	rr := httptest.NewRecorder()
	api.ZitiRouters(rr, req)
	assertZitiError(t, rr)
}

// TestZitiEnrollments_AuthFail verifies GET with unreachable Ziti → 500 error JSON.
func TestZitiEnrollments_AuthFail(t *testing.T) {
	api := newZitiTestAPI()
	req := httptest.NewRequest(http.MethodGet, "/api/ziti/enrollments", nil)
	rr := httptest.NewRecorder()
	api.ZitiEnrollments(rr, req)
	assertZitiError(t, rr)
}

// TestZitiCAs_AuthFail verifies GET with unreachable Ziti → 500 error JSON.
func TestZitiCAs_AuthFail(t *testing.T) {
	api := newZitiTestAPI()
	req := httptest.NewRequest(http.MethodGet, "/api/ziti/cas", nil)
	rr := httptest.NewRecorder()
	api.ZitiCAs(rr, req)
	assertZitiError(t, rr)
}
