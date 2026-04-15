package register_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// TestRegisterAgainstMockAPI verifies the full registration flow against a
// local TLS test server.
func TestRegisterAgainstMockAPI(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/edge/management/v1/version":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"version": "v2.0.0-pre5"},
			})
		case "/edge/management/v1/identities":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"id": "test-id",
					"enrollment": map[string]any{
						"ott": map[string]any{"jwt": "test"},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	s := register.New()
	s.ApiBase = srv.URL + "/edge/management/v1"
	s.Insecure = true

	ctx := pipeline.NewContext()
	ctx.Hostname = "testhost"

	if err := s.Run(ctx); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if !ctx.Registered {
		t.Error("expected ctx.Registered to be true after successful Run")
	}
}
