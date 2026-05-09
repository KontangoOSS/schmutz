package bao_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"git.konoss.org/kore/schmutz/enroll/internal/bao"
)

// memKV is an in-memory AdminKV for testing.
type memKV struct {
	mu    sync.Mutex
	store map[string][]byte
}

func newMemKV() *memKV { return &memKV{store: map[string][]byte{}} }

func (m *memKV) WriteJSON(_ context.Context, path string, body []byte) error {
	m.mu.Lock()
	m.store[path] = append([]byte(nil), body...)
	m.mu.Unlock()
	return nil
}

func (m *memKV) ReadJSON(_ context.Context, path string) ([]byte, error) {
	m.mu.Lock()
	v, ok := m.store[path]
	m.mu.Unlock()
	if !ok {
		return nil, bao.ErrNotFound
	}
	return append([]byte(nil), v...), nil
}

func (m *memKV) ListKeys(_ context.Context, prefix string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var keys []string
	for k := range m.store {
		if strings.HasPrefix(k, prefix+"/") {
			keys = append(keys, strings.TrimPrefix(k, prefix+"/"))
		}
	}
	return keys, nil
}

func (m *memKV) DeleteKey(_ context.Context, path string) error {
	m.mu.Lock()
	delete(m.store, path)
	m.mu.Unlock()
	return nil
}

func newStore() *bao.EnrollmentStore {
	return bao.NewEnrollmentStore(newMemKV())
}

// Issue + Get round-trip.
func TestEnrollmentStore_IssueAndGet(t *testing.T) {
	s := newStore()
	rec, err := s.Issue(context.Background(), "kontango", "ticketarr", "prod-01", "app-host", "dillon")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if !strings.HasPrefix(rec.Token, "enroll-") || len(rec.Token) != 7+32 {
		t.Errorf("token shape: %q", rec.Token)
	}
	if rec.Status != bao.StatusPending {
		t.Errorf("status: %q", rec.Status)
	}
	if rec.ExpiresAt.Before(time.Now()) {
		t.Error("ExpiresAt should be in the future")
	}

	got, err := s.Get(context.Background(), rec.Token)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Token != rec.Token || got.App != "ticketarr" {
		t.Errorf("round-trip: %+v", got)
	}
}

// Validate happy path.
func TestEnrollmentStore_ValidateHappy(t *testing.T) {
	s := newStore()
	rec, _ := s.Issue(context.Background(), "kontango", "ticketarr", "prod-01", "app-host", "dillon")

	got, err := s.Validate(context.Background(), rec.Token, "kontango", "ticketarr", "prod-01")
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if got.Token != rec.Token {
		t.Error("token mismatch")
	}
}

// Validate rejects token/deployment mismatches.
func TestEnrollmentStore_ValidateMismatch(t *testing.T) {
	s := newStore()
	rec, _ := s.Issue(context.Background(), "kontango", "ticketarr", "prod-01", "app-host", "dillon")

	_, err := s.Validate(context.Background(), rec.Token, "kontango", "ticketarr", "prod-02")
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("expected mismatch error, got %v", err)
	}
}

// Consume marks pending → consumed; second claim fails.
func TestEnrollmentStore_ConsumeIdempotent(t *testing.T) {
	s := newStore()
	rec, _ := s.Issue(context.Background(), "kontango", "ticketarr", "prod-01", "app-host", "dillon")

	if err := s.Consume(context.Background(), rec.Token); err != nil {
		t.Fatalf("first Consume: %v", err)
	}
	got, _ := s.Get(context.Background(), rec.Token)
	if got.Status != bao.StatusConsumed {
		t.Errorf("status after Consume: %q", got.Status)
	}
	if got.ClaimedAt == nil {
		t.Error("ClaimedAt should be set after Consume")
	}
	// Second Validate returns ErrEnrollmentConsumed.
	_, err := s.Validate(context.Background(), rec.Token, "kontango", "ticketarr", "prod-01")
	if err != bao.ErrEnrollmentConsumed {
		t.Errorf("expected ErrEnrollmentConsumed, got %v", err)
	}
}

// Revoke prevents claiming.
func TestEnrollmentStore_Revoke(t *testing.T) {
	s := newStore()
	rec, _ := s.Issue(context.Background(), "kontango", "ticketarr", "prod-01", "app-host", "dillon")

	if err := s.Revoke(context.Background(), rec.Token); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	_, err := s.Validate(context.Background(), rec.Token, "kontango", "ticketarr", "prod-01")
	if err != bao.ErrEnrollmentRevoked {
		t.Errorf("expected ErrEnrollmentRevoked, got %v", err)
	}
	// Revoking already-revoked is idempotent.
	if err := s.Revoke(context.Background(), rec.Token); err != nil {
		t.Errorf("second Revoke should be idempotent, got %v", err)
	}
}

// Revoke on consumed token returns ErrEnrollmentConsumed (409 vs 204).
func TestEnrollmentStore_RevokeConsumed(t *testing.T) {
	s := newStore()
	rec, _ := s.Issue(context.Background(), "kontango", "ticketarr", "prod-01", "app-host", "dillon")
	_ = s.Consume(context.Background(), rec.Token)

	err := s.Revoke(context.Background(), rec.Token)
	if err != bao.ErrEnrollmentConsumed {
		t.Errorf("expected ErrEnrollmentConsumed on revoke-consumed, got %v", err)
	}
}

// FindByDeployment locates the pending token for a deployment.
func TestEnrollmentStore_FindByDeployment(t *testing.T) {
	s := newStore()
	_, _ = s.Issue(context.Background(), "kontango", "inventree", "prod-01", "app-host", "dillon")
	rec, _ := s.Issue(context.Background(), "kontango", "ticketarr", "prod-01", "app-host", "dillon")

	found, err := s.FindByDeployment(context.Background(), "kontango", "ticketarr", "prod-01")
	if err != nil {
		t.Fatalf("FindByDeployment: %v", err)
	}
	if found == nil {
		t.Fatal("expected to find the ticketarr token")
	}
	if found.Token != rec.Token {
		t.Errorf("wrong token: got %q want %q", found.Token, rec.Token)
	}
}

// FindByDeployment returns nil when no pending token exists.
func TestEnrollmentStore_FindByDeployment_None(t *testing.T) {
	s := newStore()
	found, err := s.FindByDeployment(context.Background(), "kontango", "ticketarr", "prod-99")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found != nil {
		t.Errorf("expected nil, got %+v", found)
	}
}

// Get on non-existent token returns ErrEnrollmentNotFound.
func TestEnrollmentStore_GetNotFound(t *testing.T) {
	s := newStore()
	_, err := s.Get(context.Background(), "enroll-does-not-exist")
	if err != bao.ErrEnrollmentNotFound {
		t.Errorf("expected ErrEnrollmentNotFound, got %v", err)
	}
}

// EnrollmentRecord JSON round-trips cleanly (for Bao persistence).
func TestEnrollmentRecord_JSONRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	rec := &bao.EnrollmentRecord{
		Token: "enroll-abc123", Tenant: "kontango", App: "ticketarr",
		Deployment: "prod-01", Flavor: "app-host", Status: bao.StatusPending,
		IssuedAt: now, ExpiresAt: now.Add(15 * time.Minute), IssuedBy: "dillon",
	}
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got bao.EnrollmentRecord
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Token != rec.Token || got.Tenant != rec.Tenant || got.Status != rec.Status {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

// Two tokens for different deployments can coexist; FindByDeployment
// returns the right one.
func TestEnrollmentStore_MultipleTokensCoexist(t *testing.T) {
	s := newStore()
	r1, _ := s.Issue(context.Background(), "kontango", "ticketarr", "prod-01", "app-host", "dillon")
	r2, _ := s.Issue(context.Background(), "kontango", "ticketarr", "prod-02", "app-host", "dillon")

	f1, _ := s.FindByDeployment(context.Background(), "kontango", "ticketarr", "prod-01")
	f2, _ := s.FindByDeployment(context.Background(), "kontango", "ticketarr", "prod-02")

	if f1 == nil || f1.Token != r1.Token {
		t.Errorf("prod-01 token mismatch: got %v", f1)
	}
	if f2 == nil || f2.Token != r2.Token {
		t.Errorf("prod-02 token mismatch: got %v", f2)
	}
}

// Consume then FindByDeployment returns nil (consumed tokens are excluded).
func TestEnrollmentStore_FindByDeployment_ExcludesConsumed(t *testing.T) {
	s := newStore()
	rec, _ := s.Issue(context.Background(), "kontango", "ticketarr", "prod-01", "app-host", "dillon")
	_ = s.Consume(context.Background(), rec.Token)

	found, _ := s.FindByDeployment(context.Background(), "kontango", "ticketarr", "prod-01")
	if found != nil {
		t.Errorf("consumed token should not be found; got %+v", found)
	}
}
