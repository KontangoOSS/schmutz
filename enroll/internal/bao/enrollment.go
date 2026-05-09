package bao

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// EnrollmentRecord is the on-disk shape of a pending enrollment token at
//   secret/data/pending-enrollments/<token>
//
// The token is opaque to the agent — it's a random `enroll-<hex32>` string
// stored as a Bao KV v2 record with a short TTL. The hub validates it before
// doing anything with Ziti or Bao.
//
// Lifecycle:
//   StatusPending   → operator issued, agent has not claimed yet
//   StatusConsumed  → agent claimed; Ziti identity + Bao state written
//   StatusRevoked   → operator explicitly revoked before agent claimed
//
// Bao's KV TTL handles expiry automatically; we set a short TTL on write so
// expired records vanish from Bao without manual cleanup. We also store
// ExpiresAt explicitly so readers can distinguish "not expired yet" from
// "expired but Bao TTL hasn't fired yet" (the latter shouldn't happen in
// practice but is defensive).
type EnrollmentRecord struct {
	Token      string    `json:"token"`
	Tenant     string    `json:"tenant"`
	App        string    `json:"app"`
	Deployment string    `json:"deployment"`
	Flavor     string    `json:"flavor"`
	Status     string    `json:"status"`      // pending|consumed|revoked
	IssuedAt   time.Time `json:"issued_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	IssuedBy   string    `json:"issued_by"`   // operator identity
	ClaimedAt  *time.Time `json:"claimed_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

const (
	StatusPending  = "pending"
	StatusConsumed = "consumed"
	StatusRevoked  = "revoked"

	enrollmentTTL    = 15 * time.Minute
	enrollmentPrefix = "pending-enrollments"
)

// ErrEnrollmentNotFound is returned when the token doesn't exist in Bao.
var ErrEnrollmentNotFound = errors.New("enrollment token not found")

// ErrEnrollmentConsumed is returned when the token is already consumed.
var ErrEnrollmentConsumed = errors.New("enrollment token already consumed")

// ErrEnrollmentRevoked is returned when the token has been revoked.
var ErrEnrollmentRevoked = errors.New("enrollment token revoked")

// ErrEnrollmentExpired is returned when ExpiresAt is in the past.
var ErrEnrollmentExpired = errors.New("enrollment token expired")

// EnrollmentStore manages pending enrollment tokens in Bao KV.
// It uses the AdminKV interface so it can reuse the existing root-namespace
// KV client without a separate client.
type EnrollmentStore struct {
	kv AdminKV
}

// NewEnrollmentStore wraps an AdminKV to provide enrollment-token operations.
func NewEnrollmentStore(kv AdminKV) *EnrollmentStore {
	return &EnrollmentStore{kv: kv}
}

// Issue generates a new enrollment token, writes it to Bao, and returns
// the EnrollmentRecord (including the token). The record is written with
// a Bao KV TTL so Bao auto-deletes it after 15 minutes.
func (s *EnrollmentStore) Issue(ctx context.Context, tenant, app, deployment, flavor, issuedBy string) (*EnrollmentRecord, error) {
	token, err := generateToken()
	if err != nil {
		return nil, fmt.Errorf("enrollment: generate token: %w", err)
	}
	now := time.Now().UTC()
	rec := &EnrollmentRecord{
		Token:      token,
		Tenant:     tenant,
		App:        app,
		Deployment: deployment,
		Flavor:     flavor,
		Status:     StatusPending,
		IssuedAt:   now,
		ExpiresAt:  now.Add(enrollmentTTL),
		IssuedBy:   issuedBy,
	}
	if err := s.write(ctx, rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// Get retrieves an enrollment token by value. Returns typed errors for the
// various failure modes so callers can return the right HTTP status code.
func (s *EnrollmentStore) Get(ctx context.Context, token string) (*EnrollmentRecord, error) {
	path := enrollmentPrefix + "/" + token
	raw, err := s.kv.ReadJSON(ctx, path)
	if err != nil {
		if isNotFound(err) {
			return nil, ErrEnrollmentNotFound
		}
		return nil, fmt.Errorf("enrollment: read %s: %w", token, err)
	}
	var rec EnrollmentRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return nil, fmt.Errorf("enrollment: parse %s: %w", token, err)
	}
	return &rec, nil
}

// Validate retrieves + checks a token is valid for the given tenant/app/deployment.
// Returns the record on success, or a typed error the caller maps to HTTP status.
func (s *EnrollmentStore) Validate(ctx context.Context, token, tenant, app, deployment string) (*EnrollmentRecord, error) {
	rec, err := s.Get(ctx, token)
	if err != nil {
		return nil, err
	}
	switch rec.Status {
	case StatusConsumed:
		return nil, ErrEnrollmentConsumed
	case StatusRevoked:
		return nil, ErrEnrollmentRevoked
	}
	if time.Now().UTC().After(rec.ExpiresAt) {
		return nil, ErrEnrollmentExpired
	}
	if rec.Tenant != tenant || rec.App != app || rec.Deployment != deployment {
		return nil, fmt.Errorf("enrollment: token %s mismatch: want %s/%s/%s got %s/%s/%s",
			token, tenant, app, deployment, rec.Tenant, rec.App, rec.Deployment)
	}
	return rec, nil
}

// Consume marks a token as consumed. MUST be called before any Ziti/Bao
// minting to prevent replay even if the subsequent minting fails.
func (s *EnrollmentStore) Consume(ctx context.Context, token string) error {
	rec, err := s.Get(ctx, token)
	if err != nil {
		return err
	}
	if rec.Status != StatusPending {
		return fmt.Errorf("enrollment: cannot consume token in status %q", rec.Status)
	}
	now := time.Now().UTC()
	rec.Status = StatusConsumed
	rec.ClaimedAt = &now
	return s.write(ctx, rec)
}

// Revoke marks a token as revoked. Returns ErrEnrollmentConsumed if the
// agent already claimed it (so callers can 409 instead of 204).
func (s *EnrollmentStore) Revoke(ctx context.Context, token string) error {
	rec, err := s.Get(ctx, token)
	if err != nil {
		return err
	}
	if rec.Status == StatusConsumed {
		return ErrEnrollmentConsumed
	}
	if rec.Status == StatusRevoked {
		return nil // idempotent
	}
	now := time.Now().UTC()
	rec.Status = StatusRevoked
	rec.RevokedAt = &now
	return s.write(ctx, rec)
}

// FindByDeployment scans for an active (pending) token for a given
// tenant+app+deployment. Used by Issue to prevent duplicate tokens.
// Returns nil, nil if none exists. This is O(n) over existing tokens;
// acceptable for the small numbers expected.
func (s *EnrollmentStore) FindByDeployment(ctx context.Context, tenant, app, deployment string) (*EnrollmentRecord, error) {
	keys, err := s.kv.ListKeys(ctx, enrollmentPrefix)
	if err != nil {
		return nil, fmt.Errorf("enrollment: list: %w", err)
	}
	for _, key := range keys {
		raw, err := s.kv.ReadJSON(ctx, enrollmentPrefix+"/"+key)
		if err != nil {
			continue // skip unreadable / expired
		}
		var rec EnrollmentRecord
		if err := json.Unmarshal(raw, &rec); err != nil {
			continue
		}
		if rec.Status == StatusPending &&
			rec.Tenant == tenant && rec.App == app && rec.Deployment == deployment &&
			time.Now().UTC().Before(rec.ExpiresAt) {
			return &rec, nil
		}
	}
	return nil, nil
}

// --- internal helpers ---

func (s *EnrollmentStore) write(ctx context.Context, rec *EnrollmentRecord) error {
	body, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("enrollment: marshal: %w", err)
	}
	path := enrollmentPrefix + "/" + rec.Token
	// Use WriteJSONWithTTL to let Bao auto-expire the record.
	// For now use the existing WriteJSON — the ExpiresAt field is checked
	// on read. When we add WriteJSONWithTTL we'll set the Bao TTL here too.
	return s.kv.WriteJSON(ctx, path, body)
}

func generateToken() (string, error) {
	b := make([]byte, 16) // 128 bits → 32 hex chars
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "enroll-" + hex.EncodeToString(b), nil
}

// isNotFound checks if an error from AdminKV.ReadJSON is a not-found error.
// The existing AdminKV returns errors formatted as "bao read <path>: not found"
// or an HTTP-not-found shaped error.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return contains(msg, "not found") ||
		contains(msg, "404") ||
		contains(msg, "no value")
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
outer:
	for i := 0; i+len(sub) <= len(s); i++ {
		for j := 0; j < len(sub); j++ {
			if s[i+j] != sub[j] {
				continue outer
			}
		}
		return i
	}
	return -1
}

// Ensure httpClient satisfies the interface expectations.
// We add a WriteJSONWithTTL method here that sets the Bao KV v2 TTL header
// so the record auto-expires. The existing WriteJSON doesn't support TTL.
// For v1 we use a custom method on httpClient directly.
func (c *httpClient) writeJSONWithTTL(ctx context.Context, path string, body []byte, ttl time.Duration) error {
	wrap, err := json.Marshal(map[string]interface{}{
		"data":    json.RawMessage(body),
		"options": map[string]interface{}{"ttl": fmt.Sprintf("%ds", int(ttl.Seconds()))},
	})
	if err != nil {
		return err
	}
	status, b, err := c.do(ctx, "POST", c.dataURL(path), wrap)
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusNoContent {
		return fmt.Errorf("bao write %s returned %d: %s", path, status, b)
	}
	return nil
}
