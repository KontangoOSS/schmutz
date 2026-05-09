package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/KontangoOSS/schmutz/enroll/internal/bao"
)

// fakeOIDCAdmin records every call so tests can assert idempotent
// re-convergence + correct argument plumbing.
type fakeOIDCAdmin struct {
	entitiesByName map[string]*bao.Entity
	roleIDs        map[string]string
	wrapToken      string
	wrapTTL        int
	secretID       string

	// Counters per method to verify idempotent re-converge runs each call.
	calls struct {
		EnsureAuthMethod    int
		EnsureJWTAuthConfig int
		EnsureOIDCKey       int
		PutPolicy           int
		PutApproleRole      int
		PutOIDCRole         int
		PutJWTRole          int
		EnsureEntityAlias   int
		Wrap                int
		GetSecretID         int
		LookupEntity        int
	}
}

func (f *fakeOIDCAdmin) LookupEntityByName(_ context.Context, name string) (*bao.Entity, error) {
	f.calls.LookupEntity++
	if e, ok := f.entitiesByName[name]; ok {
		return e, nil
	}
	return nil, bao.ErrNotFound
}

func (f *fakeOIDCAdmin) UpsertEntity(_ context.Context, name string, md map[string]string) (*bao.Entity, error) {
	if e, ok := f.entitiesByName[name]; ok {
		// merge metadata
		for k, v := range md {
			e.Metadata[k] = v
		}
		return e, nil
	}
	e := &bao.Entity{ID: "ent-" + name, Name: name, Metadata: copyStringMap(md)}
	f.entitiesByName[name] = e
	return e, nil
}

func (f *fakeOIDCAdmin) AuthAccessor(_ context.Context, path string) (string, error) {
	return "auth_" + strings.TrimSuffix(path, "/") + "_test", nil
}

func (f *fakeOIDCAdmin) EnsureAuthMethod(_ context.Context, _, _ string) error {
	f.calls.EnsureAuthMethod++
	return nil
}

func (f *fakeOIDCAdmin) EnsureEntityAlias(_ context.Context, _, _, _ string) error {
	f.calls.EnsureEntityAlias++
	return nil
}

func (f *fakeOIDCAdmin) PutPolicy(_ context.Context, _, _ string) error {
	f.calls.PutPolicy++
	return nil
}

func (f *fakeOIDCAdmin) PutApproleRole(_ context.Context, _ string, _ bao.ApproleRoleOptions) error {
	f.calls.PutApproleRole++
	return nil
}

func (f *fakeOIDCAdmin) ReadApproleRoleID(_ context.Context, role string) (string, error) {
	if rid, ok := f.roleIDs[role]; ok {
		return rid, nil
	}
	rid := "rid-" + role
	f.roleIDs[role] = rid
	return rid, nil
}

func (f *fakeOIDCAdmin) WrapApproleSecretID(_ context.Context, _ string, _ time.Duration) (string, int, error) {
	f.calls.Wrap++
	return f.wrapToken, f.wrapTTL, nil
}

func (f *fakeOIDCAdmin) GetApproleSecretID(_ context.Context, _ string) (string, error) {
	f.calls.GetSecretID++
	if f.secretID != "" {
		return f.secretID, nil
	}
	return "fake-secret-id", nil
}

func (f *fakeOIDCAdmin) EnsureOIDCKey(_ context.Context, _, _ string, _, _ time.Duration, _ string) error {
	f.calls.EnsureOIDCKey++
	return nil
}

func (f *fakeOIDCAdmin) PutOIDCRole(_ context.Context, _ string, _ bao.OIDCRoleOptions) error {
	f.calls.PutOIDCRole++
	return nil
}

func (f *fakeOIDCAdmin) EnsureJWTAuthConfig(_ context.Context, _, _ string) error {
	f.calls.EnsureJWTAuthConfig++
	return nil
}

func (f *fakeOIDCAdmin) PutJWTRole(_ context.Context, _ string, _ bao.JWTRoleOptions) error {
	f.calls.PutJWTRole++
	return nil
}

// fakeKV implements bao.AdminKV for the ziti-identity index lookup.
type fakeKV struct {
	store map[string][]byte
	// Returning err for ReadJSON simulates missing/Bao-down.
	readErr error
}

func (k *fakeKV) WriteJSON(_ context.Context, path string, body []byte) error {
	if k.store == nil {
		k.store = map[string][]byte{}
	}
	k.store[path] = append([]byte(nil), body...)
	return nil
}

func (k *fakeKV) ReadJSON(_ context.Context, path string) ([]byte, error) {
	if k.readErr != nil {
		return nil, k.readErr
	}
	if v, ok := k.store[path]; ok {
		return v, nil
	}
	return nil, errors.New("not found")
}

func (k *fakeKV) ListKeys(_ context.Context, _ string) ([]string, error) { return nil, nil }
func (k *fakeKV) DeleteKey(_ context.Context, _ string) error            { return nil }

func newFakes() (*fakeOIDCAdmin, *fakeKV) {
	return &fakeOIDCAdmin{
			entitiesByName: map[string]*bao.Entity{},
			roleIDs:        map[string]string{},
			wrapToken:      "s.testwrap",
			wrapTTL:        300,
		}, &fakeKV{
			store: map[string][]byte{},
		}
}

func copyStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func seedInventree(admin *fakeOIDCAdmin, kv *fakeKV) {
	admin.entitiesByName["kontango-inventree-prod-01"] = &bao.Entity{
		ID:   "ent-1",
		Name: "kontango-inventree-prod-01",
		Metadata: map[string]string{
			"tenant":        "kontango",
			"app":           "inventree",
			"deployment":    "prod-01",
			"flavor":        "app-host",
			"ziti_identity": "machine-f6f769f1",
		},
	}
	idx, _ := json.Marshal(map[string]string{"entity": "kontango-inventree-prod-01"})
	kv.store["schmutz/ziti-index/machine-f6f769f1"] = idx
}

// Happy path: known identity → 200 with full bundle.
func TestBaoBundle_Happy(t *testing.T) {
	admin, kv := newFakes()
	seedInventree(admin, kv)

	h := NewBaoBundleHandler(admin, kv, "http://bao.tango:8200", DefaultBaoBundleConfig())
	req := httptest.NewRequest("POST", "/api/bao-bundle", nil)
	req.Header.Set("X-Ziti-Identity", "machine-f6f769f1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got status %d, body=%s", w.Code, w.Body.String())
	}
	var got BaoBundleResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.Tenant != "kontango" || got.App != "inventree" || got.Deployment != "prod-01" {
		t.Errorf("unexpected tenant/app/deployment: %+v", got)
	}
	if got.SecretID == "" {
		t.Errorf("secret_id not populated: %+v", got)
	}
	if got.RoleID != "rid-kontango-inventree-prod-01" {
		t.Errorf("role_id not threaded through: %s", got.RoleID)
	}
	if got.OIDCRole != "kontango-inventree-prod-01-token" {
		t.Errorf("unexpected oidc_role: %s", got.OIDCRole)
	}
	if got.JWTRole != "kontango-inventree-prod-01-jwt-app" {
		t.Errorf("unexpected jwt_role: %s", got.JWTRole)
	}
	if got.BaoAddr != "http://bao.tango:8200" {
		t.Errorf("bao_addr not propagated: %s", got.BaoAddr)
	}
	if got.EntityID != "ent-1" {
		t.Errorf("entity_id not propagated: %s", got.EntityID)
	}

	// Each idempotent step must run exactly once on this call.
	if admin.calls.GetSecretID != 1 {
		t.Errorf("expected 1 GetApproleSecretID call, got %d", admin.calls.GetSecretID)
	}
	if admin.calls.EnsureOIDCKey != 1 || admin.calls.PutJWTRole != 1 {
		t.Errorf("idempotent steps did not run exactly once: %+v", admin.calls)
	}
}

// Re-issue: same identity, second call → fresh wrap, no errors, calls
// roughly double.
func TestBaoBundle_Idempotent(t *testing.T) {
	admin, kv := newFakes()
	seedInventree(admin, kv)
	h := NewBaoBundleHandler(admin, kv, "http://bao.tango:8200", DefaultBaoBundleConfig())

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("POST", "/api/bao-bundle", nil)
		req.Header.Set("X-Ziti-Identity", "machine-f6f769f1")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("call %d: status %d body=%s", i, w.Code, w.Body.String())
		}
	}
	if admin.calls.GetSecretID != 2 {
		t.Errorf("expected 2 GetApproleSecretID calls across 2 requests, got %d", admin.calls.GetSecretID)
	}
	if admin.calls.PutJWTRole != 2 || admin.calls.PutApproleRole != 2 {
		t.Errorf("expected each upsert to run once per call, got %+v", admin.calls)
	}
}

// Unknown ziti identity → 404 with a message hinting at operator action.
func TestBaoBundle_NotFound(t *testing.T) {
	admin, kv := newFakes()
	// Don't seed anything.
	h := NewBaoBundleHandler(admin, kv, "http://bao.tango:8200", DefaultBaoBundleConfig())

	req := httptest.NewRequest("POST", "/api/bao-bundle", nil)
	req.Header.Set("X-Ziti-Identity", "machine-deadbeef")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("got status %d, want 404", w.Code)
	}
	var resp EnrollErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !strings.Contains(resp.Error, "machine-deadbeef") {
		t.Errorf("error message lacks identity hint: %q", resp.Error)
	}
}

// Bad header forms get rejected before any Bao calls.
func TestBaoBundle_BadIdentity(t *testing.T) {
	cases := map[string]string{
		"missing":           "",
		"random string":     "hello",
		"wrong prefix":      "vm-12345678",
		"too short suffix":  "machine-1",
		"non-hex suffix":    "machine-zzzzzzzz",
	}
	for name, hdr := range cases {
		t.Run(name, func(t *testing.T) {
			admin, kv := newFakes()
			h := NewBaoBundleHandler(admin, kv, "http://bao.tango:8200", DefaultBaoBundleConfig())
			req := httptest.NewRequest("POST", "/api/bao-bundle", nil)
			if hdr != "" {
				req.Header.Set("X-Ziti-Identity", hdr)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("got %d, want 401, body=%s", w.Code, w.Body.String())
			}
			if admin.calls.LookupEntity != 0 {
				t.Errorf("Bao called despite bad header: %+v", admin.calls)
			}
		})
	}
}

// Wrong method → 405, no Bao calls.
func TestBaoBundle_WrongMethod(t *testing.T) {
	admin, kv := newFakes()
	h := NewBaoBundleHandler(admin, kv, "http://bao.tango:8200", DefaultBaoBundleConfig())
	req := httptest.NewRequest("GET", "/api/bao-bundle", nil)
	req.Header.Set("X-Ziti-Identity", "machine-12345678")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got %d, want 405", w.Code)
	}
	if admin.calls.LookupEntity != 0 {
		t.Errorf("Bao called on wrong-method request: %+v", admin.calls)
	}
}

// Index points at an entity that no longer exists → still 404 (drift recovery
// behavior; admin must reconcile).
func TestBaoBundle_OrphanIndex(t *testing.T) {
	admin, kv := newFakes()
	idx, _ := json.Marshal(map[string]string{"entity": "ghost-entity"})
	kv.store["schmutz/ziti-index/machine-f6f769f1"] = idx
	// Note: admin.entitiesByName has no "ghost-entity"

	h := NewBaoBundleHandler(admin, kv, "http://bao.tango:8200", DefaultBaoBundleConfig())
	req := httptest.NewRequest("POST", "/api/bao-bundle", nil)
	req.Header.Set("X-Ziti-Identity", "machine-f6f769f1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404 for orphan index, body=%s", w.Code, w.Body.String())
	}
}

// Helper: assert response body decodes to the right error shape.
var _ = bytes.Buffer{} // silence unused import if helper grows
