package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"git.konoss.org/kore/schmutz/controller/internal/service"
)

func testAPI() *API {
	ts := service.NewTestStore()
	return &API{hpStore: ts}
}

func TestAuthCheckUnknown(t *testing.T) {
	api := testAPI()
	req := httptest.NewRequest("GET", "/api/auth/check?fingerprint=unknown123", nil)
	w := httptest.NewRecorder()
	api.AuthCheck(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "denied" {
		t.Errorf("want denied, got %q", resp["status"])
	}
}

func TestAuthCheckMissingFingerprint(t *testing.T) {
	api := testAPI()
	req := httptest.NewRequest("GET", "/api/auth/check", nil)
	w := httptest.NewRecorder()
	api.AuthCheck(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestAuthCheckPending(t *testing.T) {
	api := testAPI()
	api.hpStore.PutHoneypot("fp-abc", map[string]interface{}{
		"id":    "machine-001",
		"state": "pending",
	})

	req := httptest.NewRequest("GET", "/api/auth/check?fingerprint=fp-abc", nil)
	w := httptest.NewRecorder()
	api.AuthCheck(w, req)

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "pending" {
		t.Errorf("want pending, got %q", resp["status"])
	}
	if resp["id"] != "machine-001" {
		t.Errorf("want machine-001, got %q", resp["id"])
	}
}

func TestAuthCheckApproved(t *testing.T) {
	api := testAPI()
	api.hpStore.PutHoneypot("fp-xyz", map[string]interface{}{
		"id":    "machine-002",
		"state": "approved",
	})

	req := httptest.NewRequest("GET", "/api/auth/check?fingerprint=fp-xyz", nil)
	w := httptest.NewRecorder()
	api.AuthCheck(w, req)

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "approved" {
		t.Errorf("want approved, got %q", resp["status"])
	}
}

func TestHoneypotPush(t *testing.T) {
	api := testAPI()
	body := `{"type":"enrollment","controller":"ctrl-1","ip":"1.2.3.4","data":{"fingerprint":"fp-new","node":"ctrl-1"}}`
	req := httptest.NewRequest("POST", "/api/honeypot/push", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.HoneypotPush(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d: %s", w.Code, w.Body.String())
	}

	rec, err := api.hpStore.GetHoneypot("fp-new")
	if err != nil {
		t.Fatalf("GetHoneypot: %v", err)
	}
	if rec == nil {
		t.Fatal("expected record to be stored, got nil")
	}
}

func TestAuthCheckApprovedWithOTT(t *testing.T) {
	api := testAPI()
	api.hpStore.PutHoneypot("fp-ott", map[string]interface{}{
		"id":    "machine-003",
		"state": "approved",
		"ott":   "eyJhbGciOiJSUzI1NiJ9.test",
	})

	req := httptest.NewRequest("GET", "/api/auth/check?fingerprint=fp-ott", nil)
	w := httptest.NewRecorder()
	api.AuthCheck(w, req)

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "approved" {
		t.Errorf("want approved, got %q", resp["status"])
	}
	if resp["ott"] == "" {
		t.Error("want ott in response, got empty")
	}
}

func TestHoneypotApproveNotFound(t *testing.T) {
	api := testAPI()
	req := httptest.NewRequest("POST", "/api/honeypot/approve/no-such-fp", nil)
	w := httptest.NewRecorder()
	api.HoneypotApprove(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

func TestHoneypotApproveNoZiti(t *testing.T) {
	api := testAPI() // ziti is nil — approve without provisioning
	api.hpStore.PutHoneypot("fp-approve", map[string]interface{}{
		"id":    "machine-004",
		"state": "pending",
	})

	req := httptest.NewRequest("POST", "/api/honeypot/approve/fp-approve", nil)
	w := httptest.NewRecorder()
	api.HoneypotApprove(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "approved" {
		t.Errorf("want approved, got %q", resp["status"])
	}

	// Verify state persisted
	rec, _ := api.hpStore.GetHoneypot("fp-approve")
	if rec["state"] != "approved" {
		t.Errorf("want state=approved in store, got %q", rec["state"])
	}
}

func TestHoneypotApproveIdempotent(t *testing.T) {
	api := testAPI()
	api.hpStore.PutHoneypot("fp-idem", map[string]interface{}{
		"id":    "machine-005",
		"state": "approved",
		"ott":   "existing-jwt",
	})

	req := httptest.NewRequest("POST", "/api/honeypot/approve/fp-idem", nil)
	w := httptest.NewRecorder()
	api.HoneypotApprove(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["ott"] != "existing-jwt" {
		t.Errorf("want existing-jwt preserved, got %q", resp["ott"])
	}
}

func TestHoneypotPushNonEnrollmentIgnored(t *testing.T) {
	api := testAPI()
	body := `{"type":"beacon","controller":"ctrl-1","ip":"1.2.3.4","data":{}}`
	req := httptest.NewRequest("POST", "/api/honeypot/push", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.HoneypotPush(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d", w.Code)
	}
}
