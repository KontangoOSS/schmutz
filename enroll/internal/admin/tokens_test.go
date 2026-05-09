package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/KontangoOSS/schmutz/enroll/internal/audit"
	"github.com/KontangoOSS/schmutz/enroll/internal/bao"
)

type mockBao struct {
	mu      sync.Mutex
	tokens  map[string]bao.TokenRecord
	deleted []string
}

func newMockBao() *mockBao {
	return &mockBao{tokens: map[string]bao.TokenRecord{}}
}

func (m *mockBao) GetToken(ctx context.Context, t string) (*bao.TokenRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.tokens[t]
	if !ok {
		return nil, bao.ErrNotFound
	}
	return &rec, nil
}
func (m *mockBao) UpdateToken(ctx context.Context, t string, rec *bao.TokenRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens[t] = *rec
	return nil
}
func (m *mockBao) ListTokens(ctx context.Context, includeAll bool) ([]bao.TokenListEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	var out []bao.TokenListEntry
	for k, v := range m.tokens {
		if !includeAll && (v.IsExpired(now) || v.IsConsumed()) {
			continue
		}
		out = append(out, bao.TokenListEntry{Token: k, Record: v})
	}
	return out, nil
}
func (m *mockBao) DeleteToken(ctx context.Context, t string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tokens, t)
	m.deleted = append(m.deleted, t)
	return nil
}
func (m *mockBao) WriteJSON(ctx context.Context, p string, b []byte) error  { return nil }
func (m *mockBao) ReadJSON(ctx context.Context, p string) ([]byte, error)   { return nil, nil }
func (m *mockBao) ListKeys(ctx context.Context, p string) ([]string, error) { return nil, nil }
func (m *mockBao) DeleteKey(ctx context.Context, p string) error            { return nil }

func TestIssueToken(t *testing.T) {
	mb := newMockBao()
	au := &captureAudit{}
	h := NewTokensHandler(mb, au)

	body := bytes.NewBufferString(`{"slug":"proxmox-demo","role":"test","expires":"1h"}`)
	req := httptest.NewRequest("POST", "/api/tokens", body).
		WithContext(adminCtx("alice", "admins"))
	w := httptest.NewRecorder()
	h.Issue().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var resp struct {
		Token     string    `json:"token"`
		Slug      string    `json:"slug"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Slug != "proxmox-demo" {
		t.Errorf("slug = %q", resp.Slug)
	}
	if resp.Token == "" || resp.Token[:3] != "ZE-" {
		t.Errorf("token = %q", resp.Token)
	}
	if _, ok := mb.tokens[resp.Token]; !ok {
		t.Error("token not stored")
	}
	if len(au.events) != 1 || au.events[0].Action != audit.ActionTokenIssue {
		t.Errorf("audit: %+v", au.events)
	}
}

func TestIssueToken_AdminRoleRequiresBreakGlass(t *testing.T) {
	mb := newMockBao()
	au := &captureAudit{}
	h := NewTokensHandler(mb, au)
	body := bytes.NewBufferString(`{"slug":"new-admin","role":"admins","expires":"1h"}`)
	req := httptest.NewRequest("POST", "/api/tokens", body).
		WithContext(adminCtx("alice", "admins"))
	w := httptest.NewRecorder()
	h.Issue().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("status %d, want 403", w.Code)
	}
}

func TestListTokens_FiltersExpired(t *testing.T) {
	mb := newMockBao()
	now := time.Now().UTC()
	mb.tokens["ZE-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"] = bao.TokenRecord{
		Slug: "active", ExpiresAt: now.Add(time.Hour), IssuedBy: "alice",
	}
	mb.tokens["ZE-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"] = bao.TokenRecord{
		Slug: "expired", ExpiresAt: now.Add(-time.Hour), IssuedBy: "alice",
	}
	au := &captureAudit{}
	h := NewTokensHandler(mb, au)
	req := httptest.NewRequest("GET", "/api/tokens", nil).
		WithContext(adminCtx("alice", "admins"))
	w := httptest.NewRecorder()
	h.List().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var resp struct {
		Tokens []bao.TokenListEntry `json:"tokens"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Tokens) != 1 {
		t.Fatalf("got %d, want 1", len(resp.Tokens))
	}
	// Expired token should produce a token.expired audit event.
	expiredCount := 0
	for _, e := range au.events {
		if e.Action == audit.ActionTokenExpired {
			expiredCount++
		}
	}
	if expiredCount != 1 {
		t.Errorf("expected 1 token.expired audit, got %d", expiredCount)
	}
}

func TestRevokeToken(t *testing.T) {
	mb := newMockBao()
	tok := "ZE-cccccccccccccccccccccccccccccccc"
	mb.tokens[tok] = bao.TokenRecord{Slug: "x", ExpiresAt: time.Now().Add(time.Hour)}
	au := &captureAudit{}
	h := NewTokensHandler(mb, au)
	req := httptest.NewRequest("DELETE", "/api/tokens/"+tok, nil).
		WithContext(adminCtx("alice", "admins"))
	req.SetPathValue("token", tok)
	w := httptest.NewRecorder()
	h.Revoke().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if _, ok := mb.tokens[tok]; ok {
		t.Error("token should be deleted")
	}
}

func TestRevokeToken_RefuseConsumed(t *testing.T) {
	mb := newMockBao()
	consumed := time.Now().Add(-time.Minute)
	tok := "ZE-dddddddddddddddddddddddddddddddd"
	mb.tokens[tok] = bao.TokenRecord{
		Slug: "x", ExpiresAt: time.Now().Add(time.Hour),
		ConsumedAt: &consumed, ConsumedBy: "1.2.3.4",
	}
	au := &captureAudit{}
	h := NewTokensHandler(mb, au)
	req := httptest.NewRequest("DELETE", "/api/tokens/"+tok, nil).
		WithContext(adminCtx("alice", "admins"))
	req.SetPathValue("token", tok)
	w := httptest.NewRecorder()
	h.Revoke().ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("status %d, want 409", w.Code)
	}
}
