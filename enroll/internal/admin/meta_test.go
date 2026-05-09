package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"git.konoss.org/kore/schmutz/enroll/internal/bao"
)

type healthBao struct{ err error }

func (h *healthBao) GetToken(ctx context.Context, t string) (*bao.TokenRecord, error) {
	if h.err != nil {
		return nil, h.err
	}
	return nil, bao.ErrNotFound
}
func (h *healthBao) UpdateToken(ctx context.Context, t string, r *bao.TokenRecord) error { return nil }
func (h *healthBao) ListTokens(ctx context.Context, all bool) ([]bao.TokenListEntry, error) {
	return nil, nil
}
func (h *healthBao) DeleteToken(ctx context.Context, t string) error             { return nil }
func (h *healthBao) WriteJSON(ctx context.Context, p string, b []byte) error     { return nil }
func (h *healthBao) ReadJSON(ctx context.Context, p string) ([]byte, error)      { return nil, nil }
func (h *healthBao) ListKeys(ctx context.Context, p string) ([]string, error)    { return nil, nil }
func (h *healthBao) DeleteKey(ctx context.Context, p string) error               { return nil }

func TestHealthz_OK(t *testing.T) {
	h := NewMetaHandler(&healthBao{}, healthOK{})
	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	h.Healthz().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status %d", w.Code)
	}
}

func TestHealthz_BaoUnhealthy(t *testing.T) {
	h := NewMetaHandler(&healthBao{err: errors.New("seal")}, healthOK{})
	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	h.Healthz().ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status %d, want 503", w.Code)
	}
}

func TestHealthz_ZitiUnhealthy(t *testing.T) {
	h := NewMetaHandler(&healthBao{}, healthFail{})
	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	h.Healthz().ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status %d, want 503", w.Code)
	}
}

func TestWhoami(t *testing.T) {
	h := NewMetaHandler(&healthBao{}, healthOK{})
	req := httptest.NewRequest("GET", "/api/whoami", nil).
		WithContext(adminCtx("alice", "admins", "admins-break-glass"))
	w := httptest.NewRecorder()
	h.Whoami().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var resp struct {
		Name           string   `json:"name"`
		RoleAttributes []string `json:"role_attributes"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Name != "alice" || len(resp.RoleAttributes) != 2 {
		t.Errorf("got %+v", resp)
	}
}

func TestWhoami_RejectsMissingCaller(t *testing.T) {
	h := NewMetaHandler(&healthBao{}, healthOK{})
	req := httptest.NewRequest("GET", "/api/whoami", nil)
	w := httptest.NewRecorder()
	h.Whoami().ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status %d, want 500", w.Code)
	}
}

type healthOK struct{}

func (healthOK) Health(ctx context.Context) error { return nil }

type healthFail struct{}

func (healthFail) Health(ctx context.Context) error { return errors.New("ziti down") }
