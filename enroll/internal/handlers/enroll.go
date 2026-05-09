package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/KontangoOSS/schmutz/enroll/internal/bao"
	"github.com/KontangoOSS/schmutz/enroll/internal/ziti"
)

type EnrollRequest struct {
	Token string `json:"token"`
}

type EnrollResponse struct {
	Name     string `json:"name"`
	JWT      string `json:"jwt"`
	CABundle string `json:"ca_bundle,omitempty"`
}

type EnrollErrorResponse struct {
	Error      string `json:"error"`
	ConsumedAt string `json:"consumed_at,omitempty"`
	ConsumedBy string `json:"consumed_by,omitempty"`
}

type NameGenerator func() string

type EnrollHandler struct {
	bao     bao.Client
	ziti    ziti.Client
	genName NameGenerator
}

func NewEnrollHandler(b bao.Client, z ziti.Client, gen NameGenerator) *EnrollHandler {
	return &EnrollHandler{bao: b, ziti: z, genName: gen}
}

var tokenPattern = regexp.MustCompile(`^ZE-[0-9a-f]{32}$`)

func (h *EnrollHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	var req EnrollRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if !tokenPattern.MatchString(req.Token) {
		writeErr(w, http.StatusBadRequest, "malformed token")
		return
	}

	rec, err := h.bao.GetToken(ctx, req.Token)
	if errors.Is(err, bao.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "unknown token")
		return
	}
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "bao unavailable: "+err.Error())
		return
	}
	now := time.Now().UTC()
	if rec.IsExpired(now) {
		writeErr(w, http.StatusGone, "token expired at "+rec.ExpiresAt.Format(time.RFC3339))
		return
	}
	if rec.IsConsumed() {
		writeJSON(w, http.StatusConflict, EnrollErrorResponse{
			Error:      "token already consumed",
			ConsumedAt: rec.ConsumedAt.Format(time.RFC3339),
			ConsumedBy: rec.ConsumedBy,
		})
		return
	}

	// Token-first atomic ordering: mark consumed before any Ziti calls.
	idName := h.genName()
	rec.ConsumedAt = &now
	rec.ConsumedBy = clientIP(r)
	rec.ConsumedIdentity = idName
	if err := h.bao.UpdateToken(ctx, req.Token, rec); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "bao update failed: "+err.Error())
		return
	}

	// Now call Ziti. If this fails, token is still consumed (orphan trade-off).
	zr := ziti.EnrollmentRequest{
		IdentityName: idName,
		RoleAttributes: []string{
			"quarantine",
			"machine-" + idName + "-sshhost",
		},
		Tags: map[string]string{
			"slug":         rec.Slug,
			"role-pending": joinAttrs(rec.RoleAttributes),
			"enrolled-at":  now.Format(time.RFC3339),
		},
	}
	res, err := h.ziti.CreateIdentityWithSSH(ctx, zr)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ziti create failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, EnrollResponse{
		Name: res.IdentityName,
		JWT:  res.JWT,
	})
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, EnrollErrorResponse{Error: msg})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	return r.RemoteAddr
}

func joinAttrs(a []string) string {
	out := ""
	for i, s := range a {
		if i > 0 {
			out += ","
		}
		out += s
	}
	return out
}
