package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/KontangoOSS/schmutz/enroll/internal/audit"
	"github.com/KontangoOSS/schmutz/enroll/internal/identity"
	"github.com/KontangoOSS/schmutz/enroll/internal/ziti"
)

type mockZiti struct {
	mu         sync.Mutex
	identities map[string]ziti.Identity
	updated    map[string]ziti.UpdateIdentityRequest
	deleted    []string
	createErr  error
}

func newMockZiti() *mockZiti {
	return &mockZiti{
		identities: map[string]ziti.Identity{},
		updated:    map[string]ziti.UpdateIdentityRequest{},
	}
}

func (m *mockZiti) CreateIdentityWithSSH(ctx context.Context, r ziti.EnrollmentRequest) (*ziti.EnrollmentResult, error) {
	return nil, nil
}
func (m *mockZiti) Health(ctx context.Context) error { return nil }
func (m *mockZiti) ListIdentities(ctx context.Context, f ziti.IdentityFilter) ([]ziti.Identity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []ziti.Identity
	for _, id := range m.identities {
		out = append(out, id)
	}
	return out, nil
}
func (m *mockZiti) GetIdentity(ctx context.Context, name string) (*ziti.Identity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.identities[name]
	if !ok {
		return nil, errIdentityNotFound{name}
	}
	return &id, nil
}
func (m *mockZiti) UpdateIdentity(ctx context.Context, name string, r ziti.UpdateIdentityRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.identities[name]
	if !ok {
		return errIdentityNotFound{name}
	}
	id.RoleAttributes = r.RoleAttributes
	if r.Tags != nil {
		id.Tags = r.Tags
	}
	m.identities[name] = id
	m.updated[name] = r
	return nil
}
func (m *mockZiti) DeleteIdentityWithSSH(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.identities[name]; !ok {
		return errIdentityNotFound{name}
	}
	delete(m.identities, name)
	m.deleted = append(m.deleted, name)
	return nil
}
func (m *mockZiti) RoleAttributes(ctx context.Context, name string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id, ok := m.identities[name]; ok {
		return id.RoleAttributes, nil
	}
	return nil, errIdentityNotFound{name}
}

func (m *mockZiti) CreateBareIdentity(ctx context.Context, name string, attrs []string, tags map[string]string) (*ziti.EnrollmentResult, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.identities[name] = ziti.Identity{ID: "id-" + name, Name: name, RoleAttributes: attrs, Tags: tags}
	return &ziti.EnrollmentResult{IdentityID: "id-" + name, IdentityName: name, JWT: "jwt-" + name}, nil
}

func (m *mockZiti) CreateService(_ context.Context, _ string, _ []string) error { return nil }

type errIdentityNotFound struct{ name string }

func (e errIdentityNotFound) Error() string { return "identity " + e.name + " not found" }

type captureAudit struct {
	mu     sync.Mutex
	events []audit.Event
}

func (c *captureAudit) Record(ctx context.Context, ev audit.Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev)
	return nil
}

func adminCtx(name string, roles ...string) context.Context {
	return identity.WithCaller(context.Background(),
		identity.Caller{Name: name, RoleAttributes: roles})
}

func TestListIdentities_OK(t *testing.T) {
	mz := newMockZiti()
	mz.identities["a"] = ziti.Identity{ID: "1", Name: "a", RoleAttributes: []string{"test"}}
	mz.identities["b"] = ziti.Identity{ID: "2", Name: "b", RoleAttributes: []string{"admins"}}
	au := &captureAudit{}
	h := NewIdentitiesHandler(mz, au)
	req := httptest.NewRequest("GET", "/api/identities", nil).WithContext(adminCtx("alice", "admins"))
	w := httptest.NewRecorder()
	h.List().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var resp struct {
		Identities []ziti.Identity `json:"identities"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Identities) != 2 {
		t.Errorf("got %d identities", len(resp.Identities))
	}
}

func TestApprove_LiftsQuarantineAndAddsRole(t *testing.T) {
	mz := newMockZiti()
	mz.identities["machine-x"] = ziti.Identity{
		ID:             "1",
		Name:           "machine-x",
		RoleAttributes: []string{"machine-machine-x-sshhost", "quarantine"},
	}
	au := &captureAudit{}
	h := NewIdentitiesHandler(mz, au)

	body := bytes.NewBufferString(`{"role":"test"}`)
	req := httptest.NewRequest("POST", "/api/identities/machine-x/approve", body).
		WithContext(adminCtx("alice", "admins"))
	req.SetPathValue("name", "machine-x")
	w := httptest.NewRecorder()
	h.Approve().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	got := mz.identities["machine-x"]
	if hasRole(got.RoleAttributes, "quarantine") {
		t.Error("quarantine should be removed")
	}
	if !hasRole(got.RoleAttributes, "test") {
		t.Error("test role should be present")
	}
	if !hasRole(got.RoleAttributes, "machine-machine-x-sshhost") {
		t.Error("ssh-host role should be retained")
	}
	if len(au.events) != 1 || au.events[0].Action != audit.ActionIdentityApprove {
		t.Errorf("audit events: %+v", au.events)
	}
}

func TestApprove_RefusesAlreadyApproved(t *testing.T) {
	mz := newMockZiti()
	mz.identities["m"] = ziti.Identity{Name: "m", RoleAttributes: []string{"test"}}
	au := &captureAudit{}
	h := NewIdentitiesHandler(mz, au)
	req := httptest.NewRequest("POST", "/api/identities/m/approve",
		bytes.NewBufferString(`{"role":"test"}`)).WithContext(adminCtx("alice", "admins"))
	req.SetPathValue("name", "m")
	w := httptest.NewRecorder()
	h.Approve().ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("status %d, want 409", w.Code)
	}
}

func TestApprove_BreakGlassProtected(t *testing.T) {
	mz := newMockZiti()
	mz.identities[BreakGlassIdentityName] = ziti.Identity{Name: BreakGlassIdentityName,
		RoleAttributes: []string{"admins", "admins-break-glass"}}
	au := &captureAudit{}
	h := NewIdentitiesHandler(mz, au)
	req := httptest.NewRequest("POST", "/api/identities/break-glass-admin/approve",
		bytes.NewBufferString(`{"role":"admins"}`)).WithContext(adminCtx("bg", "admins-break-glass"))
	req.SetPathValue("name", BreakGlassIdentityName)
	w := httptest.NewRecorder()
	h.Approve().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("status %d, want 403", w.Code)
	}
}

func TestDelete_RegularIdentity(t *testing.T) {
	mz := newMockZiti()
	mz.identities["m"] = ziti.Identity{Name: "m", RoleAttributes: []string{"test"}}
	au := &captureAudit{}
	h := NewIdentitiesHandler(mz, au)
	req := httptest.NewRequest("DELETE", "/api/identities/m", nil).
		WithContext(adminCtx("alice", "admins"))
	req.SetPathValue("name", "m")
	w := httptest.NewRecorder()
	h.Delete().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if _, ok := mz.identities["m"]; ok {
		t.Error("identity should be deleted")
	}
}

func TestDelete_AdminRequiresBreakGlass(t *testing.T) {
	mz := newMockZiti()
	mz.identities["alice"] = ziti.Identity{Name: "alice", RoleAttributes: []string{"admins"}}
	au := &captureAudit{}
	h := NewIdentitiesHandler(mz, au)
	req := httptest.NewRequest("DELETE", "/api/identities/alice", nil).
		WithContext(adminCtx("bob", "admins"))
	req.SetPathValue("name", "alice")
	w := httptest.NewRecorder()
	h.Delete().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("status %d, want 403 (admin without break-glass cannot delete admin)", w.Code)
	}
}

func TestCreate_RegularIdentity(t *testing.T) {
	mz := newMockZiti()
	au := &captureAudit{}
	h := NewIdentitiesHandler(mz, au)
	body := bytes.NewBufferString(`{"name":"router-x","roles":["routers"]}`)
	req := httptest.NewRequest("POST", "/api/identities", body).
		WithContext(adminCtx("alice", "admins"))
	w := httptest.NewRecorder()
	h.Create().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var resp struct {
		JWT string `json:"jwt"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !strings.HasPrefix(resp.JWT, "jwt-") {
		t.Errorf("JWT = %q", resp.JWT)
	}
}

func TestCreate_AdminRoleRequiresBreakGlass(t *testing.T) {
	mz := newMockZiti()
	au := &captureAudit{}
	h := NewIdentitiesHandler(mz, au)
	body := bytes.NewBufferString(`{"name":"new-admin","roles":["admins"]}`)
	req := httptest.NewRequest("POST", "/api/identities", body).
		WithContext(adminCtx("alice", "admins"))
	w := httptest.NewRecorder()
	h.Create().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("status %d, want 403", w.Code)
	}
}

func hasRole(roles []string, want string) bool {
	for _, r := range roles {
		if r == want {
			return true
		}
	}
	return false
}
