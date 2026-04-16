package register_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KontangoOSS/schmutz/internal/pipeline"
	"github.com/KontangoOSS/schmutz/internal/register"
)

// TestName verifies the step returns the correct display name.
func TestName(t *testing.T) {
	s := register.New()
	if s.Name() != "Register Device" {
		t.Errorf("expected %q, got %q", "Register Device", s.Name())
	}
}

// TestSkipsWhenRegistered verifies Skip returns true when ctx.Registered is set.
func TestSkipsWhenRegistered(t *testing.T) {
	s := register.New()
	ctx := pipeline.NewContext()
	ctx.Registered = true
	if !s.Skip(ctx) {
		t.Error("expected Skip to return true when ctx.Registered is true")
	}
}

// TestSkipsWhenIdentityExists verifies Skip returns true when ctx.Identity is set.
func TestSkipsWhenIdentityExists(t *testing.T) {
	s := register.New()
	ctx := pipeline.NewContext()
	ctx.Identity = "/some/path/to/identity.json"
	if !s.Skip(ctx) {
		t.Error("expected Skip to return true when ctx.Identity is non-empty")
	}
}

// TestDoesNotSkipWhenFresh verifies Skip returns false for a fresh context.
func TestDoesNotSkipWhenFresh(t *testing.T) {
	s := register.New()
	ctx := pipeline.NewContext()
	if s.Skip(ctx) {
		t.Error("expected Skip to return false for a fresh context")
	}
}

// TestRegisterApproved verifies that an approved response sets ctx.Registered and ctx.JWT.
func TestRegisterApproved(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/enroll" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(register.EnrollResponse{
			Status:     "approved",
			IdentityID: "test-identity-id",
			JWT:        "test-jwt-token",
			Tier:       "sandbox",
			Message:    "welcome",
		})
	}))
	defer srv.Close()

	s := register.New()
	s.ApiBase = srv.URL

	ctx := pipeline.NewContext()
	ctx.Hostname = "testhost"
	ctx.OS = "linux"
	ctx.Arch = "amd64"
	ctx.Platform = "linux/amd64"

	if err := s.Run(ctx); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if !ctx.Registered {
		t.Error("expected ctx.Registered to be true after approved enrollment")
	}
	if ctx.JWT != "test-jwt-token" {
		t.Errorf("expected ctx.JWT = %q, got %q", "test-jwt-token", ctx.JWT)
	}
	if ctx.Tier != "sandbox" {
		t.Errorf("expected ctx.Tier = %q, got %q", "sandbox", ctx.Tier)
	}
}

// TestRegisterAgentRequired verifies that a 428 response returns an appropriate error.
func TestRegisterAgentRequired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/enroll" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPreconditionRequired)
		json.NewEncoder(w).Encode(register.EnrollResponse{
			Status:  "agent_required",
			Message: "install the schmutz agent first",
		})
	}))
	defer srv.Close()

	s := register.New()
	s.ApiBase = srv.URL

	ctx := pipeline.NewContext()
	ctx.Hostname = "testhost"

	err := s.Run(ctx)
	if err == nil {
		t.Fatal("expected an error for agent_required, got nil")
	}
	if !strings.Contains(err.Error(), "agent install") {
		t.Errorf("expected error to mention agent install, got: %v", err)
	}
	if ctx.Registered {
		t.Error("ctx.Registered should remain false on agent_required")
	}
}

// TestRegisterDenied verifies that a denied response returns an appropriate error.
func TestRegisterDenied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/enroll" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(register.EnrollResponse{
			Status:  "denied",
			Message: "this device is not authorized",
		})
	}))
	defer srv.Close()

	s := register.New()
	s.ApiBase = srv.URL

	ctx := pipeline.NewContext()
	ctx.Hostname = "testhost"

	err := s.Run(ctx)
	if err == nil {
		t.Fatal("expected an error for denied enrollment, got nil")
	}
	if !strings.Contains(err.Error(), "denied") {
		t.Errorf("expected error to mention denied, got: %v", err)
	}
	if ctx.Registered {
		t.Error("ctx.Registered should remain false on denied enrollment")
	}
}
