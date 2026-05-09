package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/KontangoOSS/schmutz/enroll/internal/bao"
	"github.com/KontangoOSS/schmutz/enroll/internal/ziti"
)

// noopAdminKV satisfies bao.AdminKV with no-op implementations so test mocks
// only need to declare the methods they actually exercise.
type noopAdminKV struct{}

// noopZitiAdmin satisfies the admin half of ziti.Client with no-op
// implementations so test mocks only need to declare the methods they
// actually exercise.
type noopZitiAdmin struct{}

func (noopZitiAdmin) ListIdentities(_ context.Context, _ ziti.IdentityFilter) ([]ziti.Identity, error) {
	return nil, nil
}
func (noopZitiAdmin) GetIdentity(_ context.Context, _ string) (*ziti.Identity, error) {
	return nil, nil
}
func (noopZitiAdmin) UpdateIdentity(_ context.Context, _ string, _ ziti.UpdateIdentityRequest) error {
	return nil
}
func (noopZitiAdmin) DeleteIdentityWithSSH(_ context.Context, _ string) error { return nil }
func (noopZitiAdmin) CreateBareIdentity(_ context.Context, _ string, _ []string, _ map[string]string) (*ziti.EnrollmentResult, error) {
	return nil, nil
}
func (noopZitiAdmin) RoleAttributes(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}
func (noopZitiAdmin) CreateService(_ context.Context, _ string, _ []string) error { return nil }

func (noopAdminKV) WriteJSON(_ context.Context, _ string, _ []byte) error  { return nil }
func (noopAdminKV) ReadJSON(_ context.Context, _ string) ([]byte, error)   { return nil, nil }
func (noopAdminKV) ListKeys(_ context.Context, _ string) ([]string, error) { return nil, nil }
func (noopAdminKV) DeleteKey(_ context.Context, _ string) error            { return nil }

type mockBao struct {
	noopAdminKV
	rec       *bao.TokenRecord
	err       error
	updateErr error
	updated   *bao.TokenRecord
}

func (m *mockBao) GetToken(ctx context.Context, token string) (*bao.TokenRecord, error) {
	return m.rec, m.err
}
func (m *mockBao) UpdateToken(ctx context.Context, token string, rec *bao.TokenRecord) error {
	m.updated = rec
	return m.updateErr
}
func (m *mockBao) ListTokens(_ context.Context, _ bool) ([]bao.TokenListEntry, error) { return nil, nil }
func (m *mockBao) DeleteToken(_ context.Context, _ string) error                     { return nil }

type mockZiti struct {
	noopZitiAdmin
	result     *ziti.EnrollmentResult
	err        error
	calledWith ziti.EnrollmentRequest
}

func (m *mockZiti) CreateIdentityWithSSH(ctx context.Context, r ziti.EnrollmentRequest) (*ziti.EnrollmentResult, error) {
	m.calledWith = r
	return m.result, m.err
}
func (m *mockZiti) Health(ctx context.Context) error { return nil }

func TestEnroll_HappyPath(t *testing.T) {
	mb := &mockBao{
		rec: &bao.TokenRecord{
			Slug:           "bao-host-1",
			RoleAttributes: []string{"services", "bao-server"},
			ExpiresAt:      time.Now().Add(1 * time.Hour),
			IssuedBy:       "admin",
		},
	}
	mz := &mockZiti{
		result: &ziti.EnrollmentResult{
			IdentityName: "machine-aabbccdd",
			JWT:          "test-jwt",
		},
	}
	h := NewEnrollHandler(mb, mz, func() string { return "machine-aabbccdd" })
	body := strings.NewReader(`{"token":"ZE-00000000000000000000000000000000"}`)
	req := httptest.NewRequest("POST", "/api/enroll", body)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp EnrollResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Name != "machine-aabbccdd" {
		t.Errorf("Name = %q, want machine-aabbccdd", resp.Name)
	}
	if resp.JWT != "test-jwt" {
		t.Errorf("JWT = %q, want test-jwt", resp.JWT)
	}
	if mb.updated == nil || mb.updated.ConsumedAt == nil {
		t.Fatal("Bao record should have been marked consumed")
	}
	if mz.calledWith.IdentityName != "machine-aabbccdd" {
		t.Errorf("Ziti called with name %q, want machine-aabbccdd", mz.calledWith.IdentityName)
	}
	wantRoles := []string{"quarantine", "machine-machine-aabbccdd-sshhost"}
	if len(mz.calledWith.RoleAttributes) != len(wantRoles) {
		t.Errorf("role attrs = %v, want %v", mz.calledWith.RoleAttributes, wantRoles)
	}
}

func TestEnroll_UnknownToken(t *testing.T) {
	mb := &mockBao{err: bao.ErrNotFound}
	mz := &mockZiti{}
	h := NewEnrollHandler(mb, mz, func() string { return "machine-x" })
	req := httptest.NewRequest("POST", "/api/enroll",
		strings.NewReader(`{"token":"ZE-00000000000000000000000000000000"}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestEnroll_ExpiredToken(t *testing.T) {
	mb := &mockBao{rec: &bao.TokenRecord{
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}}
	mz := &mockZiti{}
	h := NewEnrollHandler(mb, mz, func() string { return "machine-x" })
	req := httptest.NewRequest("POST", "/api/enroll",
		strings.NewReader(`{"token":"ZE-00000000000000000000000000000000"}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusGone {
		t.Errorf("status = %d, want 410", w.Code)
	}
}

func TestEnroll_ConsumedToken(t *testing.T) {
	now := time.Now()
	mb := &mockBao{rec: &bao.TokenRecord{
		ExpiresAt:  time.Now().Add(1 * time.Hour),
		ConsumedAt: &now,
		ConsumedBy: "1.2.3.4",
	}}
	mz := &mockZiti{}
	h := NewEnrollHandler(mb, mz, func() string { return "machine-x" })
	req := httptest.NewRequest("POST", "/api/enroll",
		strings.NewReader(`{"token":"ZE-00000000000000000000000000000000"}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", w.Code)
	}
}

func TestEnroll_MalformedToken(t *testing.T) {
	mb := &mockBao{}
	mz := &mockZiti{}
	h := NewEnrollHandler(mb, mz, func() string { return "machine-x" })
	req := httptest.NewRequest("POST", "/api/enroll",
		strings.NewReader(`{"token":"not-a-real-token"}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestEnroll_ZitiFailureLeavesTokenConsumed(t *testing.T) {
	mb := &mockBao{rec: &bao.TokenRecord{
		Slug:           "test",
		RoleAttributes: []string{"test"},
		ExpiresAt:      time.Now().Add(1 * time.Hour),
	}}
	mz := &mockZiti{err: errFromString("ziti exploded")}
	h := NewEnrollHandler(mb, mz, func() string { return "machine-x" })
	req := httptest.NewRequest("POST", "/api/enroll",
		strings.NewReader(`{"token":"ZE-00000000000000000000000000000000"}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
	if mb.updated == nil || mb.updated.ConsumedAt == nil {
		t.Error("token should be consumed even if ziti failed (orphan trade-off)")
	}
}

type stringErr string

func (s stringErr) Error() string  { return string(s) }
func errFromString(s string) error { return stringErr(s) }

type mockHealthBao struct {
	noopAdminKV
	err error
}

func (m *mockHealthBao) GetToken(ctx context.Context, token string) (*bao.TokenRecord, error) {
	return nil, m.err
}
func (m *mockHealthBao) UpdateToken(ctx context.Context, token string, rec *bao.TokenRecord) error {
	return m.err
}
func (m *mockHealthBao) ListTokens(_ context.Context, _ bool) ([]bao.TokenListEntry, error) { return nil, nil }
func (m *mockHealthBao) DeleteToken(_ context.Context, _ string) error                     { return nil }

type mockHealthZiti struct {
	noopZitiAdmin
	err error
}

func (m *mockHealthZiti) CreateIdentityWithSSH(ctx context.Context, r ziti.EnrollmentRequest) (*ziti.EnrollmentResult, error) {
	return nil, m.err
}
func (m *mockHealthZiti) Health(ctx context.Context) error { return m.err }

func TestHealth_Healthy(t *testing.T) {
	h := NewHealthHandler(&mockHealthBao{}, &mockHealthZiti{})
	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestHealth_BaoDown(t *testing.T) {
	h := NewHealthHandler(&mockHealthBao{err: errFromString("bao down")}, &mockHealthZiti{})
	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

func TestHealth_ZitiDown(t *testing.T) {
	h := NewHealthHandler(&mockHealthBao{}, &mockHealthZiti{err: errFromString("ziti down")})
	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}
