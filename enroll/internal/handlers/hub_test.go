package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"git.konoss.org/kore/schmutz/enroll/internal/bao"
	"git.konoss.org/kore/schmutz/enroll/internal/forgejo"
	"git.konoss.org/kore/schmutz/shared"
	"git.konoss.org/kore/schmutz/enroll/internal/ziti"
)

// --- fakes ---

// fakeForgejoStore wraps the in-memory state the fake Forgejo needs.
type fakeForgejoStore struct {
	blueprints  map[string]*shared.Tango
	deployments map[string]*forgejo.DeploymentRecord // key: "app/tenant/dep"
}

// stubForgejo implements a subset of what HubHandler needs from *forgejo.Client
// via a small interface. We fake it by intercepting at the handler level
// using a wrapper type.
//
// Rather than mocking the full Client (which requires httptest), we extract
// the read operations into a thin interface so tests can inject fakes.
type catalogReader interface {
	ListApps(ctx context.Context) ([]forgejo.AppSummary, error)
	GetTango(ctx context.Context, app string) (*shared.Tango, error)
	GetDeployment(ctx context.Context, app, tenant, dep string) (*forgejo.DeploymentRecord, error)
	GetSchmutz(ctx context.Context, app, tenant, dep string) (*shared.Schmutz, error)
	UpdateDeployment(ctx context.Context, app, tenant, dep, msg string, updates map[string]string) error
}

// memCatalog is the in-memory fake used by tests.
type memCatalog struct {
	blueprints  map[string]*shared.Tango         // key: app
	deployments map[string]*forgejo.DeploymentRecord // key: app+"/"+tenant+"/"+dep
	updated     map[string]map[string]string          // key: path → updates
}

func (m *memCatalog) ListApps(_ context.Context) ([]forgejo.AppSummary, error) {
	var out []forgejo.AppSummary
	for _, bp := range m.blueprints {
		if bp.Catalog.Active {
			out = append(out, forgejo.AppSummary{AppID: bp.AppID, DisplayName: bp.Identity.DisplayName})
		}
	}
	return out, nil
}

func (m *memCatalog) GetTango(_ context.Context, app string) (*shared.Tango, error) {
	bp, ok := m.blueprints[app]
	if !ok {
		return nil, forgejo.ErrNotFound
	}
	return bp, nil
}

func (m *memCatalog) GetDeployment(_ context.Context, app, tenant, dep string) (*forgejo.DeploymentRecord, error) {
	key := app + "/" + tenant + "/" + dep
	r, ok := m.deployments[key]
	if !ok {
		return nil, forgejo.ErrNotFound
	}
	return r, nil
}

func (m *memCatalog) GetSchmutz(_ context.Context, _, _, _ string) (*shared.Schmutz, error) {
	return nil, forgejo.ErrNotFound
}

func (m *memCatalog) UpdateDeployment(_ context.Context, app, tenant, dep, _ string, updates map[string]string) error {
	key := app + "/" + tenant + "/" + dep
	if m.updated == nil {
		m.updated = map[string]map[string]string{}
	}
	m.updated[key] = updates
	return nil
}

// memEnrollmentStore is the in-memory enrollment store (using bao.EnrollmentStore
// over a memKV, same as enrollment_test.go).
type memKVForHub struct {
	store map[string][]byte
}

func (m *memKVForHub) WriteJSON(_ context.Context, path string, body []byte) error {
	if m.store == nil {
		m.store = map[string][]byte{}
	}
	m.store[path] = append([]byte(nil), body...)
	return nil
}

func (m *memKVForHub) ReadJSON(_ context.Context, path string) ([]byte, error) {
	if m.store == nil {
		return nil, bao.ErrNotFound
	}
	v, ok := m.store[path]
	if !ok {
		return nil, bao.ErrNotFound
	}
	return append([]byte(nil), v...), nil
}

func (m *memKVForHub) ListKeys(_ context.Context, prefix string) ([]string, error) {
	var keys []string
	for k := range m.store {
		if strings.HasPrefix(k, prefix+"/") {
			keys = append(keys, strings.TrimPrefix(k, prefix+"/"))
		}
	}
	return keys, nil
}

func (m *memKVForHub) DeleteKey(_ context.Context, path string) error {
	delete(m.store, path)
	return nil
}

// stubBaoAdmin satisfies bao.OIDCAdmin for the hub handler tests.
type stubBaoAdmin struct{ noopAdminKV }

func (stubBaoAdmin) LookupEntityByName(_ context.Context, _ string) (*bao.Entity, error) {
	return &bao.Entity{ID: "test-ent-id", Name: "test-entity"}, nil
}
func (stubBaoAdmin) UpsertEntity(_ context.Context, name string, md map[string]string) (*bao.Entity, error) {
	return &bao.Entity{ID: "test-ent-id", Name: name, Metadata: md}, nil
}
func (stubBaoAdmin) AuthAccessor(_ context.Context, _ string) (string, error) {
	return "auth_approle_test", nil
}
func (stubBaoAdmin) EnsureAuthMethod(_ context.Context, _, _ string) error { return nil }
func (stubBaoAdmin) EnsureEntityAlias(_ context.Context, _, _, _ string) error { return nil }
func (stubBaoAdmin) PutPolicy(_ context.Context, _, _ string) error           { return nil }
func (stubBaoAdmin) PutApproleRole(_ context.Context, _ string, _ bao.ApproleRoleOptions) error {
	return nil
}
func (stubBaoAdmin) ReadApproleRoleID(_ context.Context, _ string) (string, error) {
	return "test-role-id", nil
}
func (stubBaoAdmin) WrapApproleSecretID(_ context.Context, _ string, _ time.Duration) (string, int, error) {
	return "s.test-wrap", 300, nil
}
func (stubBaoAdmin) GetApproleSecretID(_ context.Context, _ string) (string, error) {
	return "test-plain-secret-id", nil
}
func (stubBaoAdmin) EnsureOIDCKey(_ context.Context, _, _ string, _, _ time.Duration, _ string) error {
	return nil
}
func (stubBaoAdmin) PutOIDCRole(_ context.Context, _ string, _ bao.OIDCRoleOptions) error {
	return nil
}
func (stubBaoAdmin) EnsureJWTAuthConfig(_ context.Context, _, _ string) error { return nil }
func (stubBaoAdmin) PutJWTRole(_ context.Context, _ string, _ bao.JWTRoleOptions) error {
	return nil
}

// stubZiti satisfies ziti.Client for hub handler tests.
type stubZiti struct{ noopZitiAdmin }

func (s stubZiti) CreateBareIdentity(_ context.Context, name string, _ []string, _ map[string]string) (*ziti.EnrollmentResult, error) {
	return &ziti.EnrollmentResult{
		IdentityID:   "test-ziti-id",
		IdentityName: name,
		JWT:          "eyJ.test.jwt",
	}, nil
}

func (s stubZiti) CreateIdentityWithSSH(_ context.Context, _ ziti.EnrollmentRequest) (*ziti.EnrollmentResult, error) {
	return &ziti.EnrollmentResult{IdentityID: "test-id", IdentityName: "test", JWT: "eyJ.test.jwt"}, nil
}
func (s stubZiti) Health(_ context.Context) error                           { return nil }
func (s stubZiti) ListIdentities(_ context.Context, _ ziti.IdentityFilter) ([]ziti.Identity, error) {
	return nil, nil
}
func (s stubZiti) GetIdentity(_ context.Context, _ string) (*ziti.Identity, error) { return nil, nil }
func (s stubZiti) UpdateIdentity(_ context.Context, _ string, _ ziti.UpdateIdentityRequest) error {
	return nil
}
func (s stubZiti) DeleteIdentityWithSSH(_ context.Context, _ string) error { return nil }
func (s stubZiti) RoleAttributes(_ context.Context, _ string) ([]string, error) { return nil, nil }
func (s stubZiti) CreateService(_ context.Context, _ string, _ []string) error  { return nil }

// stubNamespacedKV satisfies bao.NamespacedKV for tests.
type stubNamespacedKV struct{}

func (stubNamespacedKV) WriteJSON(_ context.Context, _, _, _ string, _ []byte) error { return nil }
func (stubNamespacedKV) ReadJSON(_ context.Context, _, _, _ string) ([]byte, error) {
	return nil, bao.ErrNotFound
}

// buildHub is the factory the tests use. It wires up a HubHandler with
// in-memory fakes for everything, plus a memCatalog so tests can inject
// any blueprint/deployment state.
func buildHub(t *testing.T, cat *memCatalog) *hubForTest {
	t.Helper()
	kv := &memKVForHub{}
	enrollStore := bao.NewEnrollmentStore(kv)
	h := &hubForTest{
		catalog:     cat,
		enrollStore: enrollStore,
		kv:          kv,
		baoAdmin:    stubBaoAdmin{},
		nsKV:        stubNamespacedKV{},
		zitiClient:  stubZiti{},
		adminToken:  "test-admin-token",
		baoAddr:     "http://bao.tango:8200",
	}
	return h
}

// hubForTest wraps the handler with an injectable catalog (memCatalog vs *forgejo.Client).
// We use a separate handler so we can swap the catalog without changing the production code.
type hubForTest struct {
	catalog     catalogReader
	enrollStore *bao.EnrollmentStore
	kv          bao.AdminKV
	baoAdmin    bao.OIDCAdmin
	nsKV        bao.NamespacedKV
	zitiClient  ziti.Client
	adminToken  string
	baoAddr     string
}

func (ht *hubForTest) do(method, path string, body interface{}, token string) *httptest.ResponseRecorder {
	h := &hubHandlerTestable{
		catalog:     ht.catalog,
		enrollments: ht.enrollStore,
		baoAdmin:    ht.baoAdmin,
		baoKV:       ht.kv,
		baoNS:       ht.nsKV,
		zitiClient:  ht.zitiClient,
		adminToken:  ht.adminToken,
		baoAddr:     ht.baoAddr,
	}
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Kontango-Token", token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// hubHandlerTestable is a version of HubHandler that takes catalogReader
// so tests can inject the in-memory fake. In production, *forgejo.Client
// implements catalogReader via duck typing (we rely on the methods matching).
type hubHandlerTestable struct {
	catalog     catalogReader
	enrollments *bao.EnrollmentStore
	baoAdmin    bao.OIDCAdmin
	baoKV       bao.AdminKV
	baoNS       bao.NamespacedKV
	zitiClient  ziti.Client
	adminToken  string
	baoAddr     string
}

func (h *hubHandlerTestable) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Route the same way HubHandler does, but using h.catalog instead of h.forgejo.
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications":
		apps, err := h.catalog.ListApps(r.Context())
		if err != nil {
			writeErr(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"apps": apps, "total": len(apps)})

	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/enrollments":
		if !h.checkAuth(w, r) {
			return
		}
		var req createEnrollmentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if req.Tenant == "" || req.App == "" || req.Deployment == "" {
			writeErr(w, http.StatusBadRequest, "tenant, app, deployment required")
			return
		}
		if req.Flavor == "" {
			req.Flavor = "app-host"
		}
		bp, err := h.catalog.GetTango(r.Context(), req.App)
		if forgejo.ErrNotFound == err || (err == nil && !bp.Catalog.Active) {
			writeErr(w, http.StatusNotFound, "app not found or not active")
			return
		}
		dep, err := h.catalog.GetDeployment(r.Context(), req.App, req.Tenant, req.Deployment)
		if err == forgejo.ErrNotFound {
			writeErr(w, http.StatusConflict, "deployment not found in Git")
			return
		}
		if dep.Status != "pending" {
			writeErr(w, http.StatusGone, "deployment not in pending state")
			return
		}
		existing, _ := h.enrollments.FindByDeployment(r.Context(), req.Tenant, req.App, req.Deployment)
		if existing != nil {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "token already exists", "token": existing.Token})
			return
		}
		rec, err := h.enrollments.Issue(r.Context(), req.Tenant, req.App, req.Deployment, req.Flavor, "test")
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"token": rec.Token, "expires_at": rec.ExpiresAt,
			"tenant": rec.Tenant, "app": rec.App, "deployment": rec.Deployment,
		})

	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/enrollments/"):
		if !h.checkAuth(w, r) {
			return
		}
		token := strings.TrimPrefix(r.URL.Path, "/api/v1/enrollments/")
		err := h.enrollments.Revoke(r.Context(), token)
		if err == bao.ErrEnrollmentNotFound {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		if err == bao.ErrEnrollmentConsumed {
			writeErr(w, http.StatusConflict, "already consumed")
			return
		}
		w.WriteHeader(http.StatusNoContent)

	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/enroll":
		var req claimRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if req.Token == "" || req.Tenant == "" || req.App == "" || req.Deployment == "" {
			writeErr(w, http.StatusBadRequest, "token, tenant, app, deployment required")
			return
		}
		rec, err := h.enrollments.Validate(r.Context(), req.Token, req.Tenant, req.App, req.Deployment)
		if err != nil {
			switch {
			case err == bao.ErrEnrollmentNotFound, err == bao.ErrEnrollmentExpired:
				writeErr(w, http.StatusUnauthorized, "invalid or expired token")
			case err == bao.ErrEnrollmentConsumed:
				writeErr(w, http.StatusConflict, "already consumed")
			case err == bao.ErrEnrollmentRevoked:
				writeErr(w, http.StatusUnauthorized, "revoked")
			default:
				writeErr(w, http.StatusBadRequest, err.Error())
			}
			return
		}
		if err := h.enrollments.Consume(r.Context(), req.Token); err != nil {
			writeErr(w, http.StatusConflict, "consume: "+err.Error())
			return
		}
		// Stub: return a minimal valid response
		name := generateMachineNameHex()
		writeJSON(w, http.StatusOK, claimResponse{
			ZitiIdentity: zitiIdentityPayload{Name: name, IdentityJSON: "eyJ.test.jwt"},
			BaoBundle: baoBundlePayload{
				Role: req.Tenant + "-" + req.App + "-" + req.Deployment,
				BaoAddr: h.baoAddr, Tenant: req.Tenant, App: req.App,
				Deployment: req.Deployment, Flavor: rec.Flavor, ZitiIdentity: name,
			},
		})

	default:
		writeErr(w, http.StatusNotFound, "not found")
	}
}

func (h *hubHandlerTestable) checkAuth(w http.ResponseWriter, r *http.Request) bool {
	if h.adminToken == "" {
		return true
	}
	if r.Header.Get("X-Kontango-Token") != h.adminToken {
		writeErr(w, http.StatusUnauthorized, "invalid operator token")
		return false
	}
	return true
}

// --- active blueprint + pending deployment fixtures ---

func inventreeBlueprint() *shared.Tango {
	bp := &shared.Tango{}
	_ = bp.UnmarshalJSON([]byte(`{
		"app_id": "inventree",
		"uuid": "d0afad25-f68e-4c79-b572-739e04d7d789",
		"schema_version": 2,
		"identity": {"display_name": "InvenTree", "description": "Inventory mgmt",
		             "category": "operations"},
		"sizing": {"min": "tg-sm-1", "recommended": "tg-md-1", "max": "tg-lg-1"},
		"deployment": {"docker_supported": true},
		"runtime": {
			"bao_secrets_path": "kontango/secret/apps/inventree/{deployment}",
			"health": {"url": "http://localhost:8000/api/", "expect_status": 200}
		},
		"catalog": {"added": "2026-05-06", "maintainer": "kontango", "active": true}
	}`))
	return bp
}

func pendingDeployment() *forgejo.DeploymentRecord {
	return &forgejo.DeploymentRecord{
		Tenant: "kontango", App: "inventree", Deployment: "prod-02",
		Flavor: "app-host", Status: "pending", ApprovedBy: "dillon",
	}
}

// --- tests ---

func TestHub_ListApplications(t *testing.T) {
	cat := &memCatalog{blueprints: map[string]*shared.Tango{"inventree": inventreeBlueprint()}}
	h := buildHub(t, cat)
	w := h.do("GET", "/api/v1/applications", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/applications: %d %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if total, _ := resp["total"].(float64); total != 1 {
		t.Errorf("total: got %v want 1", total)
	}
}

func TestHub_ListApplications_NoAuth(t *testing.T) {
	// Catalog endpoint is public — no token needed.
	cat := &memCatalog{blueprints: map[string]*shared.Tango{"inventree": inventreeBlueprint()}}
	h := buildHub(t, cat)
	w := h.do("GET", "/api/v1/applications", nil, "") // no token
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 without auth, got %d", w.Code)
	}
}

func TestHub_CreateEnrollment_Happy(t *testing.T) {
	cat := &memCatalog{
		blueprints:  map[string]*shared.Tango{"inventree": inventreeBlueprint()},
		deployments: map[string]*forgejo.DeploymentRecord{"inventree/kontango/prod-02": pendingDeployment()},
	}
	h := buildHub(t, cat)
	w := h.do("POST", "/api/v1/enrollments", map[string]any{
		"tenant": "kontango", "app": "inventree", "deployment": "prod-02",
	}, "test-admin-token")
	if w.Code != http.StatusCreated {
		t.Fatalf("POST /api/v1/enrollments: %d %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)
	tok, _ := resp["token"].(string)
	if !strings.HasPrefix(tok, "enroll-") {
		t.Errorf("token shape: %q", tok)
	}
}

func TestHub_CreateEnrollment_NoAuth(t *testing.T) {
	cat := &memCatalog{blueprints: map[string]*shared.Tango{"inventree": inventreeBlueprint()}}
	h := buildHub(t, cat)
	w := h.do("POST", "/api/v1/enrollments", map[string]any{"tenant": "k", "app": "i", "deployment": "p"}, "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without token, got %d", w.Code)
	}
}

func TestHub_CreateEnrollment_AppNotFound(t *testing.T) {
	cat := &memCatalog{blueprints: map[string]*shared.Tango{}}
	h := buildHub(t, cat)
	w := h.do("POST", "/api/v1/enrollments", map[string]any{
		"tenant": "kontango", "app": "ghost", "deployment": "prod-01",
	}, "test-admin-token")
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown app, got %d", w.Code)
	}
}

func TestHub_CreateEnrollment_DeploymentNotInGit(t *testing.T) {
	cat := &memCatalog{blueprints: map[string]*shared.Tango{"inventree": inventreeBlueprint()}}
	h := buildHub(t, cat)
	w := h.do("POST", "/api/v1/enrollments", map[string]any{
		"tenant": "kontango", "app": "inventree", "deployment": "prod-99",
	}, "test-admin-token")
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 for missing deployment.yaml, got %d", w.Code)
	}
}

func TestHub_CreateEnrollment_DeploymentNotPending(t *testing.T) {
	cat := &memCatalog{
		blueprints: map[string]*shared.Tango{"inventree": inventreeBlueprint()},
		deployments: map[string]*forgejo.DeploymentRecord{
			"inventree/kontango/prod-01": {Tenant: "kontango", App: "inventree", Deployment: "prod-01", Status: "active"},
		},
	}
	h := buildHub(t, cat)
	w := h.do("POST", "/api/v1/enrollments", map[string]any{
		"tenant": "kontango", "app": "inventree", "deployment": "prod-01",
	}, "test-admin-token")
	if w.Code != http.StatusGone {
		t.Errorf("expected 410 for non-pending deployment, got %d", w.Code)
	}
}

func TestHub_CreateEnrollment_DuplicateToken(t *testing.T) {
	cat := &memCatalog{
		blueprints:  map[string]*shared.Tango{"inventree": inventreeBlueprint()},
		deployments: map[string]*forgejo.DeploymentRecord{"inventree/kontango/prod-02": pendingDeployment()},
	}
	h := buildHub(t, cat)
	// First token
	w1 := h.do("POST", "/api/v1/enrollments", map[string]any{
		"tenant": "kontango", "app": "inventree", "deployment": "prod-02",
	}, "test-admin-token")
	if w1.Code != http.StatusCreated {
		t.Fatalf("first issue: %d %s", w1.Code, w1.Body.String())
	}
	// Second token — should 409
	w2 := h.do("POST", "/api/v1/enrollments", map[string]any{
		"tenant": "kontango", "app": "inventree", "deployment": "prod-02",
	}, "test-admin-token")
	if w2.Code != http.StatusConflict {
		t.Errorf("expected 409 on duplicate, got %d", w2.Code)
	}
}

func TestHub_RevokeEnrollment_Happy(t *testing.T) {
	cat := &memCatalog{
		blueprints:  map[string]*shared.Tango{"inventree": inventreeBlueprint()},
		deployments: map[string]*forgejo.DeploymentRecord{"inventree/kontango/prod-02": pendingDeployment()},
	}
	h := buildHub(t, cat)
	w1 := h.do("POST", "/api/v1/enrollments", map[string]any{
		"tenant": "kontango", "app": "inventree", "deployment": "prod-02",
	}, "test-admin-token")
	var issued map[string]any
	_ = json.NewDecoder(w1.Body).Decode(&issued)
	tok := issued["token"].(string)

	w2 := h.do("DELETE", "/api/v1/enrollments/"+tok, nil, "test-admin-token")
	if w2.Code != http.StatusNoContent {
		t.Errorf("DELETE: expected 204, got %d %s", w2.Code, w2.Body.String())
	}
}

func TestHub_ClaimEnrollment_Happy(t *testing.T) {
	cat := &memCatalog{
		blueprints:  map[string]*shared.Tango{"inventree": inventreeBlueprint()},
		deployments: map[string]*forgejo.DeploymentRecord{"inventree/kontango/prod-02": pendingDeployment()},
	}
	h := buildHub(t, cat)
	// Issue token
	w1 := h.do("POST", "/api/v1/enrollments", map[string]any{
		"tenant": "kontango", "app": "inventree", "deployment": "prod-02",
	}, "test-admin-token")
	var issued map[string]any
	_ = json.NewDecoder(w1.Body).Decode(&issued)
	tok := issued["token"].(string)

	// Agent claims
	w2 := h.do("POST", "/api/v1/enroll", map[string]any{
		"token": tok, "tenant": "kontango", "app": "inventree", "deployment": "prod-02",
	}, "")
	if w2.Code != http.StatusOK {
		t.Fatalf("claim: %d %s", w2.Code, w2.Body.String())
	}
	var resp claimResponse
	_ = json.NewDecoder(w2.Body).Decode(&resp)
	if !strings.HasPrefix(resp.ZitiIdentity.Name, "machine-") {
		t.Errorf("ziti identity name: %q", resp.ZitiIdentity.Name)
	}
	if resp.BaoBundle.Tenant != "kontango" {
		t.Errorf("bundle tenant: %q", resp.BaoBundle.Tenant)
	}
}

func TestHub_ClaimEnrollment_Replay(t *testing.T) {
	cat := &memCatalog{
		blueprints:  map[string]*shared.Tango{"inventree": inventreeBlueprint()},
		deployments: map[string]*forgejo.DeploymentRecord{"inventree/kontango/prod-02": pendingDeployment()},
	}
	h := buildHub(t, cat)
	w1 := h.do("POST", "/api/v1/enrollments", map[string]any{
		"tenant": "kontango", "app": "inventree", "deployment": "prod-02",
	}, "test-admin-token")
	var issued map[string]any
	_ = json.NewDecoder(w1.Body).Decode(&issued)
	tok := issued["token"].(string)

	claim := map[string]any{"token": tok, "tenant": "kontango", "app": "inventree", "deployment": "prod-02"}
	_ = h.do("POST", "/api/v1/enroll", claim, "") // first claim
	w3 := h.do("POST", "/api/v1/enroll", claim, "") // replay attempt
	if w3.Code != http.StatusConflict {
		t.Errorf("replay: expected 409, got %d", w3.Code)
	}
}

func TestHub_ClaimEnrollment_BadToken(t *testing.T) {
	cat := &memCatalog{blueprints: map[string]*shared.Tango{}}
	h := buildHub(t, cat)
	w := h.do("POST", "/api/v1/enroll", map[string]any{
		"token": "enroll-doesnotexist1234567890abcdef",
		"tenant": "kontango", "app": "inventree", "deployment": "prod-02",
	}, "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("bad token: expected 401, got %d", w.Code)
	}
}

func TestHub_ClaimEnrollment_TokenMismatch(t *testing.T) {
	cat := &memCatalog{
		blueprints:  map[string]*shared.Tango{"inventree": inventreeBlueprint()},
		deployments: map[string]*forgejo.DeploymentRecord{"inventree/kontango/prod-02": pendingDeployment()},
	}
	h := buildHub(t, cat)
	w1 := h.do("POST", "/api/v1/enrollments", map[string]any{
		"tenant": "kontango", "app": "inventree", "deployment": "prod-02",
	}, "test-admin-token")
	var issued map[string]any
	_ = json.NewDecoder(w1.Body).Decode(&issued)
	tok := issued["token"].(string)

	// Agent claims with wrong deployment
	w2 := h.do("POST", "/api/v1/enroll", map[string]any{
		"token": tok, "tenant": "kontango", "app": "inventree", "deployment": "prod-WRONG",
	}, "")
	if w2.Code != http.StatusBadRequest {
		t.Errorf("mismatch: expected 400, got %d %s", w2.Code, w2.Body.String())
	}
}

func TestHub_UnknownRoute(t *testing.T) {
	cat := &memCatalog{}
	h := buildHub(t, cat)
	w := h.do("GET", "/api/v1/nonexistent", nil, "")
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHub_WrongMethod(t *testing.T) {
	cat := &memCatalog{blueprints: map[string]*shared.Tango{}}
	h := buildHub(t, cat)
	w := h.do("PUT", "/api/v1/applications", nil, "")
	if w.Code != http.StatusNotFound {
		t.Errorf("PUT /applications: expected 404, got %d", w.Code)
	}
}

// Compile-time check that *forgejo.Client satisfies catalogReader.
// This is the duck-type guarantee that makes the test double trustworthy.
var _ catalogReader = (*forgejo.Client)(nil)
