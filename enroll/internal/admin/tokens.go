package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/KontangoOSS/schmutz/enroll/internal/audit"
	"github.com/KontangoOSS/schmutz/enroll/internal/bao"
	"github.com/KontangoOSS/schmutz/enroll/internal/identity"
)

type TokensHandler struct {
	b  bao.Client
	au audit.Logger
}

func NewTokensHandler(b bao.Client, au audit.Logger) *TokensHandler {
	return &TokensHandler{b: b, au: au}
}

type issueReq struct {
	Slug    string `json:"slug"`
	Role    string `json:"role"`
	Expires string `json:"expires"`
}

type issueResp struct {
	Token     string    `json:"token"`
	Slug      string    `json:"slug"`
	Role      string    `json:"role"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (h *TokensHandler) Issue() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller, _ := identity.FromContext(r.Context())
		var req issueReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid json body")
			return
		}
		if req.Slug == "" || req.Role == "" {
			writeErr(w, http.StatusBadRequest, "slug and role are required")
			return
		}
		if req.Role == "admins" && !caller.HasRole("admins-break-glass") {
			writeErr(w, http.StatusForbidden, "issuing token for admins role requires admins-break-glass")
			return
		}
		dur, err := time.ParseDuration(req.Expires)
		if err != nil || dur <= 0 {
			writeErr(w, http.StatusBadRequest, "expires must be a Go duration like 1h, 24h")
			return
		}
		const maxDuration = 30 * 24 * time.Hour
		if dur > maxDuration {
			writeErr(w, http.StatusBadRequest, "expires must not exceed 30 days (720h)")
			return
		}
		token, err := generateToken()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "rng failure: "+err.Error())
			return
		}
		expiresAt := time.Now().UTC().Add(dur)
		rec := &bao.TokenRecord{
			Slug:           req.Slug,
			RoleAttributes: []string{req.Role},
			ExpiresAt:      expiresAt,
			IssuedBy:       caller.Name,
		}
		if err := h.b.UpdateToken(r.Context(), token, rec); err != nil {
			h.recordErr(r.Context(), caller, audit.ActionTokenIssue, redact(token), err)
			writeErr(w, http.StatusServiceUnavailable, "bao write failed: "+err.Error())
			return
		}
		h.au.Record(r.Context(), audit.Event{
			Actor:    caller.Name,
			Action:   audit.ActionTokenIssue,
			Resource: redact(token),
			Result:   audit.ResultOK,
			Metadata: map[string]string{
				"slug":       req.Slug,
				"role":       req.Role,
				"expires_at": expiresAt.Format(time.RFC3339),
			},
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(issueResp{
			Token:     token,
			Slug:      req.Slug,
			Role:      req.Role,
			ExpiresAt: expiresAt,
		})
	})
}

func (h *TokensHandler) List() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller, _ := identity.FromContext(r.Context())
		all, err := h.b.ListTokens(r.Context(), true)
		if err != nil {
			writeErr(w, http.StatusServiceUnavailable, "bao list failed: "+err.Error())
			return
		}
		now := time.Now().UTC()
		var active []bao.TokenListEntry
		for _, e := range all {
			if e.Record.IsConsumed() {
				continue
			}
			if e.Record.IsExpired(now) {
				h.au.Record(r.Context(), audit.Event{
					Actor:    caller.Name,
					Action:   audit.ActionTokenExpired,
					Resource: redact(e.Token),
					Result:   audit.ResultOK,
					Metadata: map[string]string{"slug": e.Record.Slug},
				})
				// Sweep: delete the expired record from Bao so we don't re-audit it
				// on every subsequent List call. Best-effort — if delete fails, the
				// next List call will retry; the audit log records the attempt.
				_ = h.b.DeleteToken(r.Context(), e.Token)
				continue
			}
			active = append(active, e)
		}
		if active == nil {
			active = []bao.TokenListEntry{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"tokens": active})
	})
}

func (h *TokensHandler) Get() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.PathValue("token")
		rec, err := h.b.GetToken(r.Context(), token)
		if err != nil {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"token":  redact(token),
			"record": rec,
		})
	})
}

func (h *TokensHandler) Revoke() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller, _ := identity.FromContext(r.Context())
		token := r.PathValue("token")
		rec, err := h.b.GetToken(r.Context(), token)
		if err != nil {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		if rec.IsConsumed() {
			writeErr(w, http.StatusConflict, "cannot revoke consumed token; use DELETE on the resulting identity instead")
			return
		}
		if err := h.b.DeleteToken(r.Context(), token); err != nil {
			h.recordErr(r.Context(), caller, audit.ActionTokenRevoke, redact(token), err)
			writeErr(w, http.StatusServiceUnavailable, "bao delete failed: "+err.Error())
			return
		}
		h.au.Record(r.Context(), audit.Event{
			Actor:    caller.Name,
			Action:   audit.ActionTokenRevoke,
			Resource: redact(token),
			Result:   audit.ResultOK,
			Metadata: map[string]string{
				"slug":      rec.Slug,
				"issued_by": rec.IssuedBy,
			},
		})
		w.WriteHeader(http.StatusOK)
	})
}

func (h *TokensHandler) recordErr(ctx context.Context, caller identity.Caller, action, resource string, err error) {
	h.au.Record(ctx, audit.Event{
		Actor:    caller.Name,
		Action:   action,
		Resource: resource,
		Result:   audit.ResultError,
		Metadata: map[string]string{"error": err.Error()},
	})
}

// generateToken returns a 32-hex-char token prefixed with ZE-.
func generateToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "ZE-" + hex.EncodeToString(b), nil
}

// redact returns a short prefix of the token suitable for logging:
// "ZE-a5e9..." rather than the full secret. The 8-char cutoff matches
// the "ZE-" prefix length plus 5 hex chars (~20 bits) — enough for log
// correlation, not enough to reconstruct the secret.
func redact(token string) string {
	if len(token) < 8 {
		return token
	}
	return token[:8] + "..."
}

