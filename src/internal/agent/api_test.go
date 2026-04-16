package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAPIHealthReturns200 verifies GET /health returns 200 with status "ok".
func TestAPIHealthReturns200(t *testing.T) {
	h := NewAPIHandler()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("expected status=ok, got %q", body["status"])
	}
}

// TestAPIStatusHasHostname verifies GET /status returns a non-empty hostname field.
func TestAPIStatusHasHostname(t *testing.T) {
	h := NewAPIHandler()
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	hostname, ok := body["hostname"]
	if !ok {
		t.Fatal("hostname field missing from /status response")
	}
	if hostname == "" {
		t.Error("hostname field is empty")
	}
}

// TestAPIInfoHasCPU verifies GET /info returns num_cpu > 0.
func TestAPIInfoHasCPU(t *testing.T) {
	h := NewAPIHandler()
	req := httptest.NewRequest(http.MethodGet, "/info", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	numCPU, ok := body["num_cpu"]
	if !ok {
		t.Fatal("num_cpu field missing from /info response")
	}
	// JSON numbers decode as float64
	cpuVal, ok := numCPU.(float64)
	if !ok {
		t.Fatalf("num_cpu is not a number, got %T", numCPU)
	}
	if cpuVal <= 0 {
		t.Errorf("expected num_cpu > 0, got %v", cpuVal)
	}
}
