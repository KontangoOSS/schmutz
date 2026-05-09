package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"git.konoss.org/kore/schmutz/enroll/internal/identity"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
}

func TestRequireAdmin_AllowsAdmin(t *testing.T) {
	req := httptest.NewRequest("GET", "/x", nil)
	req = req.WithContext(identity.WithCaller(req.Context(),
		identity.Caller{Name: "alice", RoleAttributes: []string{"admins"}}))
	w := httptest.NewRecorder()
	RequireAdmin(okHandler()).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestRequireAdmin_RejectsNonAdmin(t *testing.T) {
	req := httptest.NewRequest("GET", "/x", nil)
	req = req.WithContext(identity.WithCaller(req.Context(),
		identity.Caller{Name: "bob", RoleAttributes: []string{"test"}}))
	w := httptest.NewRecorder()
	RequireAdmin(okHandler()).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestRequireAdmin_RejectsMissingCaller(t *testing.T) {
	req := httptest.NewRequest("GET", "/x", nil)
	w := httptest.NewRecorder()
	RequireAdmin(okHandler()).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

func TestRequireBreakGlass_AllowsBreakGlass(t *testing.T) {
	req := httptest.NewRequest("POST", "/x", nil)
	req = req.WithContext(identity.WithCaller(req.Context(),
		identity.Caller{Name: "break-glass-admin", RoleAttributes: []string{"admins", "admins-break-glass"}}))
	w := httptest.NewRecorder()
	RequireBreakGlass(okHandler()).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestRequireBreakGlass_RejectsPlainAdmin(t *testing.T) {
	req := httptest.NewRequest("POST", "/x", nil)
	req = req.WithContext(identity.WithCaller(req.Context(),
		identity.Caller{Name: "alice", RoleAttributes: []string{"admins"}}))
	w := httptest.NewRecorder()
	RequireBreakGlass(okHandler()).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}
