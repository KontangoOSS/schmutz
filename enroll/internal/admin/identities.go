package admin

// Audit events are best-effort: operations are never blocked on audit write
// success or failure. A failed audit write is silently dropped.

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/KontangoOSS/schmutz/enroll/internal/audit"
	"github.com/KontangoOSS/schmutz/enroll/internal/identity"
	"github.com/KontangoOSS/schmutz/enroll/internal/ziti"
)

type IdentitiesHandler struct {
	z  ziti.Client
	au audit.Logger
}

func NewIdentitiesHandler(z ziti.Client, au audit.Logger) *IdentitiesHandler {
	return &IdentitiesHandler{z: z, au: au}
}

// --- List ---

func (h *IdentitiesHandler) List() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f := ziti.IdentityFilter{
			HasRole:      r.URL.Query().Get("role"),
			NameContains: r.URL.Query().Get("name"),
			HasTagKey:    "slug",
		}
		if slug := r.URL.Query().Get("slug"); slug != "" {
			f.HasTagValue = slug
		} else {
			f.HasTagKey = ""
		}
		if status := r.URL.Query().Get("status"); status == "quarantined" {
			f.HasRole = "quarantine"
		}
		ids, err := h.z.ListIdentities(r.Context(), f)
		if err != nil {
			writeErr(w, http.StatusServiceUnavailable, "ziti list failed: "+err.Error())
			return
		}
		if ids == nil {
			ids = []ziti.Identity{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"identities": ids})
	})
}

// --- Get ---

func (h *IdentitiesHandler) Get() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		id, err := h.z.GetIdentity(r.Context(), name)
		if err != nil {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(id)
	})
}

// --- Create (admin-only path; skips token flow) ---

type createReq struct {
	Name  string   `json:"name"`
	Roles []string `json:"roles"`
}

func (h *IdentitiesHandler) Create() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller, _ := identity.FromContext(r.Context())
		var req createReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid json body")
			return
		}
		if req.Name == "" {
			writeErr(w, http.StatusBadRequest, "name is required")
			return
		}
		if rolesContain(req.Roles, "admins") && !caller.HasRole("admins-break-glass") {
			writeErr(w, http.StatusForbidden, "creating admin-tier identity requires admins-break-glass")
			return
		}
		tags := map[string]string{
			"created-by":  caller.Name,
			"created-at":  time.Now().UTC().Format(time.RFC3339),
			"create-mode": "admin-direct",
		}
		res, err := h.z.CreateBareIdentity(r.Context(), req.Name, req.Roles, tags)
		if err != nil {
			h.recordErr(r, caller, audit.ActionIdentityCreate, req.Name, err)
			writeErr(w, http.StatusInternalServerError, "ziti create failed: "+err.Error())
			return
		}
		h.au.Record(r.Context(), audit.Event{
			Actor:    caller.Name,
			Action:   audit.ActionIdentityCreate,
			Resource: req.Name,
			Result:   audit.ResultOK,
			Metadata: map[string]string{"roles": strings.Join(req.Roles, ",")},
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"name": res.IdentityName,
			"jwt":  res.JWT,
		})
	})
}

// --- Patch ---

type patchReq struct {
	Roles []string          `json:"roles,omitempty"`
	Tags  map[string]string `json:"tags,omitempty"`
}

func (h *IdentitiesHandler) Patch() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller, _ := identity.FromContext(r.Context())
		name := r.PathValue("name")
		if err := EnsureNotBreakGlass(name); err != nil {
			writeErr(w, http.StatusForbidden, err.Error())
			return
		}
		existing, err := h.z.GetIdentity(r.Context(), name)
		if err != nil {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		var req patchReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid json body")
			return
		}
		// Patching an admin requires break-glass.
		if rolesContain(existing.RoleAttributes, "admins") && !caller.HasRole("admins-break-glass") {
			writeErr(w, http.StatusForbidden, "modifying admin-tier identity requires admins-break-glass")
			return
		}
		// Promotion to admin requires break-glass.
		if rolesContain(req.Roles, "admins") && !caller.HasRole("admins-break-glass") {
			writeErr(w, http.StatusForbidden, "promotion to admins requires admins-break-glass")
			return
		}

		old := strings.Join(existing.RoleAttributes, ",")
		newRoles := req.Roles
		if newRoles == nil {
			newRoles = existing.RoleAttributes
		}
		err = h.z.UpdateIdentity(r.Context(), name, ziti.UpdateIdentityRequest{
			RoleAttributes: newRoles,
			Tags:           req.Tags,
		})
		if err != nil {
			h.recordErr(r, caller, audit.ActionIdentityUpdate, name, err)
			writeErr(w, http.StatusInternalServerError, "ziti update failed: "+err.Error())
			return
		}
		h.au.Record(r.Context(), audit.Event{
			Actor:    caller.Name,
			Action:   audit.ActionIdentityUpdate,
			Resource: name,
			Result:   audit.ResultOK,
			Metadata: map[string]string{
				"old_roles": old,
				"new_roles": strings.Join(newRoles, ","),
			},
		})
		w.WriteHeader(http.StatusOK)
	})
}

// --- Approve ---

// approveReq accepts a single role (`role`) or a list (`roles`); both are
// additive when present. Callers should send one or the other; sending both
// merges them.
type approveReq struct {
	Role  string   `json:"role,omitempty"`
	Roles []string `json:"roles,omitempty"`
}

func (h *IdentitiesHandler) Approve() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller, _ := identity.FromContext(r.Context())
		name := r.PathValue("name")
		if err := EnsureNotBreakGlass(name); err != nil {
			writeErr(w, http.StatusForbidden, err.Error())
			return
		}
		// Body decoded before GetIdentity so RBAC on the requested role can fail
		// with 400/403 before we touch Ziti.
		var req approveReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid json body")
			return
		}
		newRoles := req.Roles
		if req.Role != "" {
			newRoles = append(newRoles, req.Role)
		}
		if rolesContain(newRoles, "admins") && !caller.HasRole("admins-break-glass") {
			writeErr(w, http.StatusForbidden, "approving with admins role requires admins-break-glass")
			return
		}
		existing, err := h.z.GetIdentity(r.Context(), name)
		if err != nil {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		if !rolesContain(existing.RoleAttributes, "quarantine") {
			writeErr(w, http.StatusConflict, "identity is not in quarantine")
			return
		}
		// Build merged roles: drop quarantine, retain ssh-host, add new roles.
		merged := []string{}
		for _, r := range existing.RoleAttributes {
			if r == "quarantine" {
				continue
			}
			merged = append(merged, r)
		}
		merged = append(merged, newRoles...)

		err = h.z.UpdateIdentity(r.Context(), name, ziti.UpdateIdentityRequest{
			RoleAttributes: merged,
		})
		if err != nil {
			h.recordErr(r, caller, audit.ActionIdentityApprove, name, err)
			writeErr(w, http.StatusInternalServerError, "ziti update failed: "+err.Error())
			return
		}
		h.au.Record(r.Context(), audit.Event{
			Actor:    caller.Name,
			Action:   audit.ActionIdentityApprove,
			Resource: name,
			Result:   audit.ResultOK,
			Metadata: map[string]string{
				"old_roles": strings.Join(existing.RoleAttributes, ","),
				"new_roles": strings.Join(merged, ","),
			},
		})
		w.WriteHeader(http.StatusOK)
	})
}

// --- Deny ---

type denyReq struct {
	Reason string `json:"reason"`
}

func (h *IdentitiesHandler) Deny() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller, _ := identity.FromContext(r.Context())
		name := r.PathValue("name")
		if err := EnsureNotBreakGlass(name); err != nil {
			writeErr(w, http.StatusForbidden, err.Error())
			return
		}
		existing, err := h.z.GetIdentity(r.Context(), name)
		if err != nil {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		if !rolesContain(existing.RoleAttributes, "quarantine") {
			writeErr(w, http.StatusConflict, "deny is only valid on quarantined identities; use DELETE for offboarding")
			return
		}
		var req denyReq
		json.NewDecoder(r.Body).Decode(&req)

		err = h.z.DeleteIdentityWithSSH(r.Context(), name)
		if err != nil {
			h.recordErr(r, caller, audit.ActionIdentityDeny, name, err)
			writeErr(w, http.StatusInternalServerError, "ziti delete failed: "+err.Error())
			return
		}
		h.au.Record(r.Context(), audit.Event{
			Actor:    caller.Name,
			Action:   audit.ActionIdentityDeny,
			Resource: name,
			Result:   audit.ResultOK,
			Metadata: map[string]string{
				"reason":    req.Reason,
				"old_roles": strings.Join(existing.RoleAttributes, ","),
			},
		})
		w.WriteHeader(http.StatusOK)
	})
}

// --- Delete (offboarding) ---

func (h *IdentitiesHandler) Delete() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller, _ := identity.FromContext(r.Context())
		name := r.PathValue("name")
		if err := EnsureNotBreakGlass(name); err != nil {
			writeErr(w, http.StatusForbidden, err.Error())
			return
		}
		existing, err := h.z.GetIdentity(r.Context(), name)
		if err != nil {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		if rolesContain(existing.RoleAttributes, "admins") && !caller.HasRole("admins-break-glass") {
			writeErr(w, http.StatusForbidden, "deleting admin-tier identity requires admins-break-glass")
			return
		}
		err = h.z.DeleteIdentityWithSSH(r.Context(), name)
		if err != nil {
			h.recordErr(r, caller, audit.ActionIdentityDelete, name, err)
			writeErr(w, http.StatusInternalServerError, "ziti delete failed: "+err.Error())
			return
		}
		h.au.Record(r.Context(), audit.Event{
			Actor:    caller.Name,
			Action:   audit.ActionIdentityDelete,
			Resource: name,
			Result:   audit.ResultOK,
			Metadata: map[string]string{
				"old_roles": strings.Join(existing.RoleAttributes, ","),
			},
		})
		w.WriteHeader(http.StatusOK)
	})
}

// --- helpers ---

func (h *IdentitiesHandler) recordErr(r *http.Request, caller identity.Caller, action, resource string, err error) {
	h.au.Record(r.Context(), audit.Event{
		Actor:    caller.Name,
		Action:   action,
		Resource: resource,
		Result:   audit.ResultError,
		Metadata: map[string]string{"error": err.Error()},
	})
}

func rolesContain(roles []string, want string) bool {
	for _, r := range roles {
		if r == want {
			return true
		}
	}
	return false
}

