package preflight_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/KontangoOSS/schmutz/internal/preflight"
)

func TestName(t *testing.T) {
	s := preflight.New("")
	if s.Name() != "Preflight Check" {
		t.Errorf("expected %q, got %q", "Preflight Check", s.Name())
	}
}

func TestNeverSkips(t *testing.T) {
	s := preflight.New("")
	if s.Skip() {
		t.Error("preflight step should never skip")
	}
}

func TestConnectivityOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := preflight.CheckConnectivity(srv.URL); err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}
}

func TestConnectivityFail(t *testing.T) {
	err := preflight.CheckConnectivity("http://192.0.2.1:1")
	if err == nil {
		t.Error("expected error for unreachable URL, got nil")
	}
}

func TestScoreBelowThreshold(t *testing.T) {
	if preflight.ShouldAbort(15, 50) {
		t.Error("score 15 with threshold 50 should not abort")
	}
}

func TestScoreAboveThreshold(t *testing.T) {
	if !preflight.ShouldAbort(75, 50) {
		t.Error("score 75 with threshold 50 should abort")
	}
}

func TestScoreAtThreshold(t *testing.T) {
	if !preflight.ShouldAbort(50, 50) {
		t.Error("score 50 with threshold 50 should abort")
	}
}

func TestPublicIPFromMock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "203.0.113.42\n")
	}))
	defer srv.Close()

	ip, err := preflight.GetPublicIPFrom(srv.URL)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if ip != "203.0.113.42" {
		t.Errorf("expected %q, got %q", "203.0.113.42", ip)
	}
}
