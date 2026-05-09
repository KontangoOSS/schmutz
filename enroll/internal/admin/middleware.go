package admin

import (
	"encoding/json"
	"net/http"

	"github.com/KontangoOSS/schmutz/enroll/internal/identity"
)

type errResp struct {
	Error string `json:"error"`
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(errResp{Error: msg})
}

// RequireAdmin gates the handler on the caller having the `admins` role.
// Missing caller is treated as an internal invariant violation (500),
// not a 403 — the SDK listener should always populate caller before
// reaching here.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, ok := identity.FromContext(r.Context())
		if !ok {
			writeErr(w, http.StatusInternalServerError, "caller identity missing from context")
			return
		}
		if !c.HasRole("admins") {
			writeErr(w, http.StatusForbidden, "caller lacks admins role")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireBreakGlass gates the handler on the caller having the
// `admins-break-glass` role. Used for endpoints that touch admin-tier
// identities (creating new admins, modifying or deleting them).
func RequireBreakGlass(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, ok := identity.FromContext(r.Context())
		if !ok {
			writeErr(w, http.StatusInternalServerError, "caller identity missing from context")
			return
		}
		if !c.HasRole("admins-break-glass") {
			writeErr(w, http.StatusForbidden, "operation requires admins-break-glass role")
			return
		}
		next.ServeHTTP(w, r)
	})
}
