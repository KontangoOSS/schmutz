package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/KontangoOSS/schmutz/enroll/internal/bao"
)

// BaoBundleHandler issues install bundles for the bao-jwt agent flow.
//
// On each call:
//   1. Read the caller's ziti identity name (from the auth context — see
//      below).
//   2. Look up the Bao entity created at first-time enrollment via
//      identity/lookup/entity {name=<entity>}. The entity name encodes
//      tenant + app + deployment, set during the operator-driven
//      enroll-app step. We DO NOT provision new (tenant, app) pairs from
//      this endpoint — that's a separate admin operation.
//   3. Re-converge the supporting state (auth method enabled, OIDC key
//      allowlist, approle CIDR-bind, JWT role bound_claims). Idempotent.
//   4. Generate a fresh response-wrapped secret-id and return the
//      install bundle.
//
// Auth model (v1): trust X-Ziti-Identity header. This is intentional —
// real ziti-conn extraction lands when the controller listens behind an
// SDK-bound listener instead of the local mux. The header is acceptable
// because:
//   - the mux only accepts traffic that has already transited the ziti
//     edge router (via Caddy ALPN), so a forged header can only come
//     from a properly-authenticated identity in the first place;
//   - for now, the lookup-by-entity-name happens via a tenant+app+
//     deployment slug that the caller cannot forge into existence; if
//     no entity exists for that slug, 404.
//
// Production swap: replace ziti identity extraction here when the
// listener moves into the SDK.

// BaoBundleResponse mirrors enroll-app.sh's stdout JSON format.
type BaoBundleResponse struct {
	Role               string `json:"role"`
	RoleID             string `json:"role_id"`
	SecretID           string `json:"secret_id"`            // plain secret_id — agent writes directly, no Bao call needed at enrollment
	SecretIDWrapToken  string `json:"secret_id_wrap_token"` // legacy: kept for /api/bao-bundle (re-issue path)
	WrapTTLSeconds     int    `json:"wrap_ttl_seconds"`
	OIDCRole           string `json:"oidc_role"`
	JWTRole            string `json:"jwt_role"`
	Tenant             string `json:"tenant"`
	App                string `json:"app"`
	Deployment         string `json:"deployment"`
	Flavor             string `json:"flavor"`
	ZitiIdentity       string `json:"ziti_identity"`
	EntityID           string `json:"entity_id"`
	BaoAddr            string `json:"bao_addr"`
}

// BaoBundleHandler holds the dependencies for issuing bundles.
type BaoBundleHandler struct {
	admin   bao.OIDCAdmin
	kv      bao.AdminKV // for the ziti-identity → entity index lookup
	baoAddr string      // the addr the agent will use to reach Bao (e.g. http://bao.tango:8200)
	cfg     BaoBundleConfig
	now     func() time.Time
}

// BaoBundleConfig captures the policy knobs we want operator-tunable but
// not per-request. Defaults match scripts/bao-jwt/enroll-app.sh.
type BaoBundleConfig struct {
	// CIDR that bound-cidrs are pinned to. Default 127.0.0.0/8 since
	// overlay-routed bao requests arrive at Bao via the controller's
	// own ziti-edge-tunnel hop, sourced from localhost.
	LoginCIDR string

	// Identity OIDC signing key name + algorithm.
	OIDCKey            string
	OIDCAlgorithm      string
	OIDCRotationPeriod time.Duration
	OIDCVerificationTTL time.Duration

	// Audience the OIDC role embeds in tokens; matches the JWT auth
	// role's bound_audiences.
	JWTAudience string

	// Lifetimes for the issued bao tokens.
	AppRoleTokenTTL    time.Duration
	AppRoleTokenMaxTTL time.Duration
	JWTTokenTTL        time.Duration
	JWTTokenMaxTTL     time.Duration

	// Wrap TTL for the install secret-id.
	WrapTTL time.Duration
}

// DefaultBaoBundleConfig returns the bash-script defaults.
func DefaultBaoBundleConfig() BaoBundleConfig {
	return BaoBundleConfig{
		LoginCIDR:           "127.0.0.0/8",
		OIDCKey:             "bao-jwt-signer",
		OIDCAlgorithm:       "RS256",
		OIDCRotationPeriod:  24 * time.Hour,
		OIDCVerificationTTL: 24 * time.Hour,
		JWTAudience:         "bao-jwt",
		AppRoleTokenTTL:     10 * time.Minute,
		AppRoleTokenMaxTTL:  1 * time.Hour,
		JWTTokenTTL:         15 * time.Minute,
		JWTTokenMaxTTL:      4 * time.Hour,
		WrapTTL:             5 * time.Minute,
	}
}

// NewBaoBundleHandler builds a handler. baoAddr is the URL the agent will
// use to dial Bao (e.g. http://bao.tango:8200), embedded into the bundle
// so the agent doesn't have to be told separately.
func NewBaoBundleHandler(admin bao.OIDCAdmin, kv bao.AdminKV, baoAddr string, cfg BaoBundleConfig) *BaoBundleHandler {
	return &BaoBundleHandler{
		admin:   admin,
		kv:      kv,
		baoAddr: baoAddr,
		cfg:     cfg,
		now:     time.Now,
	}
}

// zitiIdentityPattern matches well-formed ziti machine identity names.
// e.g. machine-f6f769f1, machine-1a695510. Mirrors the generator in
// enroll-server's main.go: "machine-" + 8 lowercase hex chars.
var zitiIdentityPattern = regexp.MustCompile(`^machine-[0-9a-f]{8}$`)

// errEntityNotFound is the typed signal we use to reach the 404 branch.
var errEntityNotFound = errors.New("no entity for ziti identity")

func (h *BaoBundleHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	zitiID := r.Header.Get("X-Ziti-Identity")
	if !zitiIdentityPattern.MatchString(zitiID) {
		writeErr(w, http.StatusUnauthorized, "missing or malformed X-Ziti-Identity")
		return
	}

	// The entity name is what we created at admin-driven enroll time.
	// We don't know the tenant/app/deployment for this caller until we
	// look up an existing entity by ziti_identity metadata. Bao's
	// identity/lookup/entity supports `name=` but not arbitrary metadata
	// queries; instead we list entities and filter. For now we rely on
	// the convention that admins enrolled with a predictable name and
	// stored ziti_identity in the entity's metadata, then walk by
	// matching metadata.ziti_identity == zitiID.
	//
	// In v1 this lookup is O(n) over all entities. That's fine for our
	// fleet size. If/when it isn't, switch to a Bao alias lookup keyed
	// on ziti_identity (custom metadata on the alias).
	bundle, err := h.issue(r.Context(), zitiID)
	if err != nil {
		if errors.Is(err, errEntityNotFound) {
			writeErr(w, http.StatusNotFound,
				fmt.Sprintf("no enrollment for %s — operator must enroll first", zitiID))
			return
		}
		writeErr(w, http.StatusServiceUnavailable, "bao error: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, bundle)
}

// issue does the lookup-and-converge + wrap. Pure logic; ServeHTTP wraps
// it with HTTP concerns.
func (h *BaoBundleHandler) issue(ctx context.Context, zitiID string) (*BaoBundleResponse, error) {
	ent, err := h.findEntityByZitiIdentity(ctx, zitiID)
	if err != nil {
		return nil, err
	}

	tenant := ent.Metadata["tenant"]
	app := ent.Metadata["app"]
	deployment := ent.Metadata["deployment"]
	flavor := ent.Metadata["flavor"]
	if tenant == "" || app == "" || deployment == "" {
		return nil, fmt.Errorf("entity %s missing required metadata (tenant/app/deployment)", ent.Name)
	}

	roleName := fmt.Sprintf("%s-%s-%s", tenant, app, deployment)
	oidcRole := fmt.Sprintf("%s-%s-%s-token", tenant, app, deployment)
	// Per-deployment JWT role and policy — avoids the templating issue where
	// {{identity.entity.metadata.deployment}} doesn't evaluate correctly for
	// JWT auth method tokens. Each deployment gets its own scoped policy.
	jwtRole := fmt.Sprintf("%s-%s-%s-jwt-app", tenant, app, deployment)
	appPolicy := fmt.Sprintf("%s-%s-%s-app", tenant, app, deployment)
	approlePolicy := fmt.Sprintf("%s-%s-%s-oidc-issue", tenant, app, deployment)

	// --- Re-converge supporting state. Each step is idempotent. ---
	if err := h.admin.EnsureAuthMethod(ctx, "approle/", "approle"); err != nil {
		return nil, err
	}
	if err := h.admin.EnsureAuthMethod(ctx, "jwt/", "jwt"); err != nil {
		return nil, err
	}
	approleAcc, err := h.admin.AuthAccessor(ctx, "approle/")
	if err != nil {
		return nil, err
	}
	jwksURL := fmt.Sprintf("%s/v1/identity/oidc/.well-known/keys", "http://127.0.0.1:8200")
	if err := h.admin.EnsureJWTAuthConfig(ctx, jwksURL, ""); err != nil {
		return nil, err
	}
	if err := h.admin.EnsureOIDCKey(ctx, h.cfg.OIDCKey, h.cfg.OIDCAlgorithm,
		h.cfg.OIDCRotationPeriod, h.cfg.OIDCVerificationTTL, h.cfg.JWTAudience); err != nil {
		return nil, err
	}
	// OIDC issuance policy for the approle (needed for the refresh loop)
	if err := h.admin.PutPolicy(ctx, approlePolicy, oidcIssuePolicyHCL(oidcRole)); err != nil {
		return nil, err
	}
	// Per-deployment app policy — three layers:
	//   Layer 1: base paths every agent needs (SSH, token self-management)
	//   Layer 2: app-level public catalog access
	//   Layer 3: deployment-specific secret path (scoped to this exact deployment)
	if err := h.admin.PutPolicy(ctx, appPolicy, deploymentPolicyHCL(tenant, app, deployment, flavor)); err != nil {
		return nil, err
	}
	if err := h.admin.PutApproleRole(ctx, roleName, bao.ApproleRoleOptions{
		TokenTTL:           h.cfg.AppRoleTokenTTL,
		TokenMaxTTL:        h.cfg.AppRoleTokenMaxTTL,
		TokenPolicies:      []string{"default", approlePolicy},
		TokenBoundCIDRs:    []string{h.cfg.LoginCIDR},
		SecretIDBoundCIDRs: []string{h.cfg.LoginCIDR},
		SecretIDNumUses:    0,
		SecretIDTTL:        720 * time.Hour,
	}); err != nil {
		return nil, err
	}
	roleID, err := h.admin.ReadApproleRoleID(ctx, roleName)
	if err != nil {
		return nil, err
	}
	if err := h.admin.EnsureEntityAlias(ctx, roleID, ent.ID, approleAcc); err != nil {
		return nil, err
	}
	if err := h.admin.PutOIDCRole(ctx, oidcRole, bao.OIDCRoleOptions{
		Key:      h.cfg.OIDCKey,
		TTL:      h.cfg.JWTTokenTTL,
		Template: oidcRoleTemplate(),
		ClientID: h.cfg.JWTAudience,
	}); err != nil {
		return nil, err
	}
	if err := h.admin.PutJWTRole(ctx, jwtRole, bao.JWTRoleOptions{
		RoleType:        "jwt",
		UserClaim:       "ziti_identity",
		BoundAudiences:  []string{h.cfg.JWTAudience},
		BoundClaimsType: "string",
		// All three dimensions pinned: tenant + app + deployment.
		// A token for ticketarr/prod-01 cannot claim ticketarr/prod-02.
		BoundClaims:     map[string]string{"tenant": tenant, "app": app, "deployment": deployment},
		TokenPolicies:   []string{appPolicy},
		TokenTTL:        h.cfg.JWTTokenTTL,
		TokenMaxTTL:     h.cfg.JWTTokenMaxTTL,
		TokenBoundCIDRs: []string{h.cfg.LoginCIDR},
	}); err != nil {
		return nil, err
	}

	// --- Generate secret_id server-side (no wrap needed for hub flow). ---
	secretID, err := h.admin.GetApproleSecretID(ctx, roleName)
	if err != nil {
		return nil, err
	}

	return &BaoBundleResponse{
		Role:         roleName,
		RoleID:       roleID,
		SecretID:     secretID,
		OIDCRole:     oidcRole,
		JWTRole:      jwtRole,
		Tenant:       tenant,
		App:          app,
		Deployment:   deployment,
		Flavor:       flavor,
		ZitiIdentity: zitiID,
		EntityID:     ent.ID,
		BaoAddr:      h.baoAddr,
	}, nil
}

// findEntityByZitiIdentity returns the Bao entity whose
// metadata.ziti_identity matches zitiID. v1 implementation: name is
// expected to be `${tenant}-${app}-${deployment}` and the operator-side
// enroll-app sets metadata.ziti_identity. Since we don't index by
// metadata, we look up by predicted name patterns the operator-side
// script stamped on the entity.
//
// For now, since the operator-side enroll-app uses a deterministic naming
// convention but we don't know the tenant/app/deployment from the
// agent's perspective, we fall back to a list-and-filter walk. v2
// improvement: use entity-alias custom_metadata indexed by ziti_identity.
func (h *BaoBundleHandler) findEntityByZitiIdentity(ctx context.Context, zitiID string) (*bao.Entity, error) {
	// The naive approach: there's no list endpoint for entities in
	// OIDCAdmin yet. For v1 we lean on a convention: there exists a
	// "ziti-identity → entity-name" pointer in Bao at
	//   secret/data/schmutz/ziti-index/<ziti_identity>
	// containing {"entity":"<entity-name>"}. The operator-side
	// enroll-app populates it; we read it to resolve.
	//
	// This sidesteps having to add a list-entities call to OIDCAdmin
	// and keeps the handler O(1).
	//
	// If the index is missing, fall back to ErrNotFound.
	indexPath := fmt.Sprintf("schmutz/ziti-index/%s", zitiID)
	raw, err := h.kv.ReadJSON(ctx, indexPath)
	if err != nil {
		// "not found" comes back as an error here — distinguish if needed.
		// For v1, any read failure ⇒ no enrollment yet.
		return nil, errEntityNotFound
	}
	var idx struct {
		Entity string `json:"entity"`
	}
	if err := json.Unmarshal(raw, &idx); err != nil || idx.Entity == "" {
		return nil, errEntityNotFound
	}
	ent, err := h.admin.LookupEntityByName(ctx, idx.Entity)
	if err == bao.ErrNotFound {
		return nil, errEntityNotFound
	}
	if err != nil {
		return nil, err
	}
	return ent, nil
}

// deploymentPolicyHCL generates the three-layer Bao policy for a deployment:
//
//   Layer 1 — base (every machine):
//     Token self-management. SSH access credentials live in the deployment
//     secrets so the agent can read its own SSH host key etc.
//
//   Layer 2 — app catalog:
//     Read the app's public blueprint and its subpaths so the agent and
//     operators can read generic app metadata (sizing, health URL, etc.)
//
//   Layer 3 — deployment secrets:
//     Read the exact secret path for this (tenant, app, deployment) triple.
//     Nothing else — not other deployments, not other apps, not other tenants.
// deploymentPolicyHCL generates a scoped Bao policy for one deployment.
// The flavor parameter extends the base policy for specialized host types:
//
//	app-host  (default) — reads its own deployment secrets only
//	ctrl-node           — additionally reads infra secrets (ziti-admin, bao-admin)
//	db-only             — reads only the database subpath
//	edge-router         — no secret access needed; minimal token self-management only
func deploymentPolicyHCL(tenant, app, deployment, flavor string) string {
	base := fmt.Sprintf(`
# Layer 1 — token self-management (every flavor)
path "auth/token/lookup-self"  { capabilities = ["read"] }
path "auth/token/renew-self"   { capabilities = ["update"] }
path "auth/token/revoke-self"  { capabilities = ["update"] }

# Layer 2 — app catalog: public blueprint (read-only)
path "public/secret/data/applications/%s"    { capabilities = ["read"] }
path "public/secret/data/applications/%s/*"  { capabilities = ["read"] }
path "public/secret/metadata/applications/%s"   { capabilities = ["read","list"] }
path "public/secret/metadata/applications/%s/*" { capabilities = ["read","list"] }

# Layer 2b — gateway publishes its OpenAPI spec to a single well-known
# public path so other apps can discover it. Narrowly scoped: only the
# api-spec path is writable, blueprint subtree stays read-only above.
path "public/secret/data/applications/%s/api-spec"   { capabilities = ["create","update"] }
path "public/secret/metadata/applications/%s/api-spec" { capabilities = ["read","update","delete"] }

# Layer 3 — deployment secrets + runtime record
# write allowed so the gateway can persist discovered api-spec to Bao
path "%s/secret/data/apps/%s/%s/*"          { capabilities = ["read","create","update"] }
path "%s/secret/metadata/apps/%s/%s/*"      { capabilities = ["read","list"] }
path "%s/secret/data/deployments/%s/%s"     { capabilities = ["read"] }
path "%s/secret/metadata/deployments/%s/%s" { capabilities = ["read"] }
`, app, app, app, app,
		app, app,
		tenant, app, deployment, tenant, app, deployment,
		tenant, app, deployment, tenant, app, deployment)

	switch flavor {
	case "ctrl-node":
		// Controller nodes also read infra secrets (Ziti admin, Bao admin creds).
		base += fmt.Sprintf(`
# ctrl-node extras — infra secrets
path "%s/secret/data/infra/*"          { capabilities = ["read"] }
path "%s/secret/metadata/infra/*"      { capabilities = ["read","list"] }
`, tenant, tenant)
	case "db-only":
		// Database hosts only need the database subpath; strip app/* access.
		base = fmt.Sprintf(`
path "auth/token/lookup-self"  { capabilities = ["read"] }
path "auth/token/renew-self"   { capabilities = ["update"] }
path "auth/token/revoke-self"  { capabilities = ["update"] }
path "%s/secret/data/apps/%s/%s/database"     { capabilities = ["read"] }
path "%s/secret/metadata/apps/%s/%s/database" { capabilities = ["read"] }
path "%s/secret/data/deployments/%s/%s"       { capabilities = ["read"] }
`, tenant, app, deployment, tenant, app, deployment, tenant, app, deployment)
	}
	return base
}

func oidcRoleTemplate() string {
	// Templated claims interpolated by Bao's identity engine at mint time.
	return `{"tenant":{{identity.entity.metadata.tenant}},"app":{{identity.entity.metadata.app}},"deployment":{{identity.entity.metadata.deployment}},"flavor":{{identity.entity.metadata.flavor}},"ziti_identity":{{identity.entity.metadata.ziti_identity}}}`
}

func oidcIssuePolicyHCL(oidcRole string) string {
	return fmt.Sprintf(`path "identity/oidc/token/%s" { capabilities = ["read"] }`, oidcRole)
}
