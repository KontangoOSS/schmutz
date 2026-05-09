package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"fmt"
	"net/http"
	"strings"
	"time"

	"git.konoss.org/kore/schmutz/enroll/internal/bao"
	"git.konoss.org/kore/schmutz/enroll/internal/forgejo"
	"git.konoss.org/kore/schmutz/shared"
	"git.konoss.org/kore/schmutz/enroll/internal/ziti"
)

// HubHandler serves the three hub endpoints:
//
//	GET  /api/v1/applications          → catalog list (public, no auth)
//	POST /api/v1/enrollments           → operator creates approval token
//	POST /api/v1/enroll                → agent claims token, gets identity + bao bundle
//
// All operator endpoints require X-Kontango-Token matching the configured
// admin token. Agent endpoints require only the enrollment token.
type HubHandler struct {
	forgejo     forgejo.CatalogClient
	enrollments *bao.EnrollmentStore
	baoAdmin    bao.OIDCAdmin
	baoKV       bao.AdminKV
	baoNS       bao.NamespacedKV
	zitiClient  ziti.Client
	adminToken  string // X-Kontango-Token value for operator endpoints
	baoAddr     string // addr embedded in bao bundles (e.g. http://bao.tango:8200)
}

// HubConfig carries the constructor parameters for HubHandler.
type HubConfig struct {
	ForgejoClient forgejo.CatalogClient
	EnrollStore   *bao.EnrollmentStore
	BaoAdmin      bao.OIDCAdmin
	BaoKV         bao.AdminKV
	BaoNS         bao.NamespacedKV
	ZitiClient    ziti.Client
	AdminToken    string
	BaoAddr       string
}

// NewHubHandler constructs a HubHandler.
func NewHubHandler(cfg HubConfig) *HubHandler {
	return &HubHandler{
		forgejo:     cfg.ForgejoClient,
		enrollments: cfg.EnrollStore,
		baoAdmin:    cfg.BaoAdmin,
		baoKV:       cfg.BaoKV,
		baoNS:       cfg.BaoNS,
		zitiClient:  cfg.ZitiClient,
		adminToken:  cfg.AdminToken,
		baoAddr:     cfg.BaoAddr,
	}
}

// ServeHTTP routes to the correct handler based on method+path.
func (h *HubHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications":
		h.listApplications(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/enrollments":
		h.createEnrollment(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/enrollments/"):
		h.getEnrollment(w, r)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/enrollments/"):
		h.revokeEnrollment(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/enroll":
		h.claimEnrollment(w, r)
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1/sync/"):
		h.syncDeployment(w, r)
	default:
		writeErr(w, http.StatusNotFound, "not found")
	}
}

// ------------------------------------------------------------------ catalog

// GET /api/v1/applications — public, no auth required.
func (h *HubHandler) listApplications(w http.ResponseWriter, r *http.Request) {
	apps, err := h.forgejo.ListApps(r.Context())
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "catalog unavailable: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"apps":  apps,
		"total": len(apps),
	})
}

// ------------------------------------------------------------------ enrollment operator endpoints

type createEnrollmentRequest struct {
	Tenant     string `json:"tenant"`
	App        string `json:"app"`
	Deployment string `json:"deployment"`
	Flavor     string `json:"flavor"`
}

// POST /api/v1/enrollments — requires operator auth.
func (h *HubHandler) createEnrollment(w http.ResponseWriter, r *http.Request) {
	if !h.requireOperatorAuth(w, r) {
		return
	}
	var req createEnrollmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.Tenant == "" || req.App == "" || req.Deployment == "" {
		writeErr(w, http.StatusBadRequest, "tenant, app, and deployment are required")
		return
	}
	if req.Flavor == "" {
		req.Flavor = "app-host"
	}

	ctx := r.Context()

	// 1. Validate app is active in Forgejo.
	bp, err := h.forgejo.GetTango(ctx, req.App)
	if errors.Is(err, forgejo.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "app "+req.App+" not found in catalog or not active")
		return
	}
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "catalog unavailable: "+err.Error())
		return
	}
	if !bp.Catalog.Active {
		writeErr(w, http.StatusNotFound, "app "+req.App+" is not active in catalog")
		return
	}

	// 2. Validate deployment.yaml exists in Forgejo with status=pending.
	dep, err := h.forgejo.GetDeployment(ctx, req.App, req.Tenant, req.Deployment)
	if errors.Is(err, forgejo.ErrNotFound) {
		writeErr(w, http.StatusConflict,
			"deployment "+req.Tenant+"/"+req.App+"/"+req.Deployment+
				" not found in Git — merge a deployment.yaml PR first")
		return
	}
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "catalog unavailable: "+err.Error())
		return
	}
	switch dep.Status {
	case "pending":
		// good
	case "active":
		writeErr(w, http.StatusGone, "deployment is already active")
		return
	case "decommissioned":
		writeErr(w, http.StatusGone, "deployment is decommissioned")
		return
	default:
		writeErr(w, http.StatusConflict, "deployment status is "+dep.Status+" — expected pending")
		return
	}

	// 3. Check for existing live token (prevent duplicate issuance).
	existing, err := h.enrollments.FindByDeployment(ctx, req.Tenant, req.App, req.Deployment)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "enrollment store error: "+err.Error())
		return
	}
	if existing != nil {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":      "active enrollment token already exists for this deployment",
			"token":      existing.Token,
			"expires_at": existing.ExpiresAt,
		})
		return
	}

	// 4. Issue token.
	issuedBy := r.Header.Get("X-Kontango-Operator")
	if issuedBy == "" {
		issuedBy = "unknown"
	}
	rec, err := h.enrollments.Issue(ctx, req.Tenant, req.App, req.Deployment, req.Flavor, issuedBy)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "issue token: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"token":      rec.Token,
		"expires_at": rec.ExpiresAt,
		"tenant":     rec.Tenant,
		"app":        rec.App,
		"deployment": rec.Deployment,
	})
}

// GET /api/v1/enrollments/{token} — requires operator auth.
func (h *HubHandler) getEnrollment(w http.ResponseWriter, r *http.Request) {
	if !h.requireOperatorAuth(w, r) {
		return
	}
	token := strings.TrimPrefix(r.URL.Path, "/api/v1/enrollments/")
	rec, err := h.enrollments.Get(r.Context(), token)
	if errors.Is(err, bao.ErrEnrollmentNotFound) {
		writeErr(w, http.StatusNotFound, "token not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "enrollment store: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// DELETE /api/v1/enrollments/{token} — requires operator auth.
func (h *HubHandler) revokeEnrollment(w http.ResponseWriter, r *http.Request) {
	if !h.requireOperatorAuth(w, r) {
		return
	}
	token := strings.TrimPrefix(r.URL.Path, "/api/v1/enrollments/")
	err := h.enrollments.Revoke(r.Context(), token)
	if errors.Is(err, bao.ErrEnrollmentNotFound) {
		writeErr(w, http.StatusNotFound, "token not found")
		return
	}
	if errors.Is(err, bao.ErrEnrollmentConsumed) {
		writeErr(w, http.StatusConflict, "token already consumed by agent")
		return
	}
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "revoke: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ------------------------------------------------------------------ agent claim

type claimRequest struct {
	Token      string                 `json:"token"`
	Tenant     string                 `json:"tenant"`
	App        string                 `json:"app"`
	Deployment string                 `json:"deployment"`
	Hardware   map[string]interface{} `json:"hardware"`
}

type claimResponse struct {
	ZitiIdentity zitiIdentityPayload `json:"ziti_identity"`
	BaoBundle    baoBundlePayload    `json:"bao_bundle"`
}

type zitiIdentityPayload struct {
	Name         string `json:"name"`
	IdentityJSON string `json:"identity_json"`
}

type baoBundlePayload struct {
	Role         string `json:"role"`
	RoleID       string `json:"role_id"`
	SecretID     string `json:"secret_id"`
	OIDCRole     string `json:"oidc_role"`
	JWTRole      string `json:"jwt_role"`
	Tenant       string `json:"tenant"`
	App          string `json:"app"`
	Deployment   string `json:"deployment"`
	Flavor       string `json:"flavor"`
	ZitiIdentity string `json:"ziti_identity"`
	EntityID     string `json:"entity_id"`
	BaoAddr      string `json:"bao_addr"`
}

// POST /api/v1/enroll — no auth; the token is the credential.
func (h *HubHandler) claimEnrollment(w http.ResponseWriter, r *http.Request) {
	var req claimRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.Token == "" {
		writeErr(w, http.StatusBadRequest, "token is required")
		return
	}

	ctx := r.Context()

	// 1. Look up token — tenant/app/deployment are optional in the request;
	// if omitted we fill them from the token record so the agent only needs
	// to pass --token.
	rec, err := h.enrollments.Get(ctx, req.Token)
	if err != nil {
		switch {
		case errors.Is(err, bao.ErrEnrollmentNotFound), errors.Is(err, bao.ErrEnrollmentExpired):
			writeErr(w, http.StatusUnauthorized, "invalid or expired enrollment token")
		default:
			writeErr(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	switch rec.Status {
	case bao.StatusConsumed:
		writeErr(w, http.StatusConflict, "enrollment token already consumed")
		return
	case bao.StatusRevoked:
		writeErr(w, http.StatusUnauthorized, "enrollment token has been revoked")
		return
	}
	if time.Now().UTC().After(rec.ExpiresAt) {
		writeErr(w, http.StatusUnauthorized, "enrollment token expired")
		return
	}
	if req.Tenant == "" {
		req.Tenant = rec.Tenant
	}
	if req.App == "" {
		req.App = rec.App
	}
	if req.Deployment == "" {
		req.Deployment = rec.Deployment
	}
	// If the agent did supply values, verify they match the token.
	if req.Tenant != rec.Tenant || req.App != rec.App || req.Deployment != rec.Deployment {
		writeErr(w, http.StatusBadRequest,
			fmt.Sprintf("token mismatch: token is for %s/%s/%s", rec.Tenant, rec.App, rec.Deployment))
		return
	}

	// 2. Re-validate app still active in Forgejo.
	bp, err := h.forgejo.GetTango(ctx, req.App)
	if err != nil || !bp.Catalog.Active {
		writeErr(w, http.StatusConflict, "app "+req.App+" is no longer active in catalog")
		return
	}

	// 3. Re-validate deployment still pending in Forgejo.
	dep, err := h.forgejo.GetDeployment(ctx, req.App, req.Tenant, req.Deployment)
	if err != nil || dep.Status != "pending" {
		writeErr(w, http.StatusConflict, "deployment is no longer in pending state")
		return
	}

	// 4. Consume token FIRST — before any minting. If minting fails after
	//    this, the agent must request a new token. This prevents replay.
	if err := h.enrollments.Consume(ctx, req.Token); err != nil {
		writeErr(w, http.StatusConflict, "could not consume token: "+err.Error())
		return
	}

	// From here on: token is consumed. Any error returns 500 (minting failed
	// after consumption). The agent must request a new token from the operator.
	mintErr := func(msg string) {
		writeErr(w, http.StatusInternalServerError,
			msg+" — enrollment token has been consumed; request a new token from the operator")
	}

	// 5. Mint Ziti identity server-side and immediately enroll it.
	//
	// CreateBareIdentity returns an enrollment JWT that embeds the Ziti
	// controller's enrollment endpoint (tls://<ip>:6262). On the controller
	// node, that endpoint is reachable at localhost:6262. We call
	// EnrollJWTToJSON server-side so the agent receives a complete identity
	// JSON and never needs to reach port 6262 directly — all traffic flows
	// through :443 ALPN as required.
	identityName := generateMachineNameHex()
	roleAttrs := flavorRoleAttrs(rec.Flavor, req.App)

	// 5b. Ensure schmutz-api.{zone} service exists. Non-fatal if it fails —
	// the agent can still enroll without the service existing yet.
	zone := "tango"
	if cfg, err := h.forgejo.GetCatalogConfig(ctx); err == nil {
		zone = cfg.Defaults.ZoneOrDefault()
	}
	schmutzAPIService := "schmutz-api." + zone
	if err := h.zitiClient.CreateService(ctx, schmutzAPIService,
		[]string{"schmutz-api-hosts"}); err != nil {
		// Log but don't fail enrollment — service creation is best-effort.
		_ = err
	}
	// Add schmutz-api-hosts so the identity can bind schmutz-api.{zone}.
	alreadyHasRole := false
	for _, r := range roleAttrs {
		if r == "schmutz-api-hosts" {
			alreadyHasRole = true
			break
		}
	}
	if !alreadyHasRole {
		roleAttrs = append(roleAttrs, "schmutz-api-hosts")
	}

	tags := map[string]string{
		"tenant": req.Tenant, "app": req.App,
		"deployment": req.Deployment, "flavor": rec.Flavor,
	}
	zitiResult, err := h.zitiClient.CreateBareIdentity(ctx, identityName, roleAttrs, tags)
	if err != nil {
		mintErr("ziti identity creation failed: " + err.Error())
		return
	}

	// Exchange the enrollment JWT for a fully-enrolled identity JSON.
	identityJSON, err := ziti.EnrollJWTToJSON(zitiResult.JWT)
	if err != nil {
		mintErr("ziti enrollment failed: " + err.Error())
		return
	}

	// 6. Write Bao state (entity + index + OIDC/JWT/AppRole setup).
	//    This reuses the existing BaoBundleHandler logic via OIDCAdmin.
	bundleResult, err := h.provisionBaoBundle(ctx, req.Tenant, req.App, req.Deployment, rec.Flavor, identityName)
	if err != nil {
		mintErr("bao provisioning failed: " + err.Error())
		return
	}

	// 7. Write substrate to Bao tenant namespace.
	if err := h.writeSchmutz(ctx, req.App, req.Tenant, req.Deployment, identityName); err != nil {
		// Non-fatal: substrate can be written later via bao-app-enroll.
		// Log but don't fail the enrollment.
		_ = err // TODO: log this properly once we have structured logging
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// 8a. Write runtime details to Bao — host info, identity, entity_id stay
	//     out of git. Path: <tenant>/secret/deployments/<app>/<deployment>.
	runtime := forgejo.DeploymentRuntime{
		Tenant:       req.Tenant,
		App:          req.App,
		Deployment:   req.Deployment,
		ZitiIdentity: identityName,
		EntityID:     bundleResult.EntityID,
		ClaimedAt:    now,
	}
	if rtBody, err := json.Marshal(runtime); err == nil {
		_ = h.baoNS.WriteJSON(ctx, req.Tenant+"/", "secret",
			"deployments/"+req.App+"/"+req.Deployment, rtBody)
	}

	// 8b. Update only status in git — no host details, no identity info.
	_ = h.forgejo.UpdateDeployment(ctx, req.App, req.Tenant, req.Deployment,
		"chore: mark "+req.Deployment+" as active",
		map[string]string{"status": "active"},
	)

	// 9. Return bundle. identity_json is now a complete enrolled identity
	//    that the agent writes directly to disk — not a JWT. The agent
	//    never needs to reach port 6262 or 1280.
	writeJSON(w, http.StatusOK, claimResponse{
		ZitiIdentity: zitiIdentityPayload{
			Name:         identityName,
			IdentityJSON: string(identityJSON), // fully-enrolled identity JSON
		},
		BaoBundle: baoBundlePayload{
			Role:         bundleResult.Role,
			RoleID:       bundleResult.RoleID,
			SecretID:     bundleResult.SecretID,
			OIDCRole:     bundleResult.OIDCRole,
			JWTRole:      bundleResult.JWTRole,
			Tenant:       req.Tenant,
			App:          req.App,
			Deployment:   req.Deployment,
			Flavor:       rec.Flavor,
			ZitiIdentity: identityName,
			EntityID:     bundleResult.EntityID,
			BaoAddr:      h.baoAddr,
		},
	})
}

// ------------------------------------------------------------------ internal helpers

// provisionBaoBundle runs the same steps as BaoBundleHandler's issue()
// but for a fresh identity (no prior entity exists). It creates the entity,
// sets up the OIDC/JWT/AppRole machinery, and returns the wrap token.
func (h *HubHandler) provisionBaoBundle(ctx context.Context, tenant, app, deployment, flavor, zitiIdentity string) (*BaoBundleResponse, error) {
	// Reuse the BaoBundleHandler logic via a synthetic HubBundle request.
	// The BaoBundleHandler.issue() method is package-private; replicate the
	// key steps here so the hub can drive them directly.
	//
	// In a future refactor, extract the shared provisioning logic into
	// an internal/provision package that both handlers import. For now,
	// the duplication is small enough to be acceptable.
	bh := &BaoBundleHandler{
		admin:   h.baoAdmin,
		kv:      h.baoKV,
		baoAddr: h.baoAddr,
		cfg:     DefaultBaoBundleConfig(),
		now:     time.Now,
	}

	// Write the ziti-index so BaoBundleHandler can find this deployment.
	idxBody, err := json.Marshal(map[string]string{
		"entity": tenant + "-" + app + "-" + deployment,
	})
	if err != nil {
		return nil, err
	}
	if err := h.baoKV.WriteJSON(ctx, "schmutz/ziti-index/"+zitiIdentity, idxBody); err != nil {
		return nil, err
	}

	// Upsert the entity with full metadata.
	_, err = h.baoAdmin.UpsertEntity(ctx, tenant+"-"+app+"-"+deployment, map[string]string{
		"tenant": tenant, "app": app, "deployment": deployment,
		"flavor": flavor, "ziti_identity": zitiIdentity,
	})
	if err != nil {
		return nil, err
	}

	// issue() reads the entity back via the index we just wrote.
	return bh.issue(ctx, zitiIdentity)
}

// POST /api/v1/sync/{tenant}/{app}/{deployment} — operator endpoint.
//
// Re-reads schmutz.yml from Forgejo and overwrites the Bao substrate for the
// named deployment. The agent's substrate watcher will pick it up on its next
// poll (default 24h) or immediately if the agent is sent SIGHUP.
//
// Use this after updating schmutz.yml in git — no re-enrollment needed.
func (h *HubHandler) syncDeployment(w http.ResponseWriter, r *http.Request) {
	if !h.requireOperatorAuth(w, r) {
		return
	}
	// Parse /api/v1/sync/{tenant}/{app}/{deployment}
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/api/v1/sync/"), "/", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		writeErr(w, http.StatusBadRequest, "path must be /api/v1/sync/{tenant}/{app}/{deployment}")
		return
	}
	tenant, app, deployment := parts[0], parts[1], parts[2]
	ctx := r.Context()

	// Look up the ziti_identity from the Bao runtime record.
	raw, err := h.baoNS.ReadJSON(ctx, tenant+"/", "secret", "deployments/"+app+"/"+deployment)
	if err != nil {
		writeErr(w, http.StatusNotFound,
			fmt.Sprintf("no runtime record for %s/%s/%s — is it enrolled?", tenant, app, deployment))
		return
	}
	var rt map[string]any
	if err := json.Unmarshal(raw, &rt); err != nil {
		writeErr(w, http.StatusInternalServerError, "parse runtime record: "+err.Error())
		return
	}
	zitiIdentity, _ := rt["ziti_identity"].(string)
	if zitiIdentity == "" {
		writeErr(w, http.StatusConflict, "runtime record missing ziti_identity — re-enroll the deployment")
		return
	}

	// Re-read schmutz.yml from Forgejo and write to Bao.
	if err := h.writeSchmutz(ctx, app, tenant, deployment, zitiIdentity); err != nil {
		writeErr(w, http.StatusInternalServerError, "sync schmutz: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"synced":        true,
		"tenant":        tenant,
		"app":           app,
		"deployment":    deployment,
		"ziti_identity": zitiIdentity,
		"message":       "substrate written to Bao — agent will pick up on next poll or SIGHUP",
	})
}

// writeSchmutz reads the schmutz.yml template from Forgejo, interpolates
// all template variables, sets identity fields, and writes to Bao.
//
// Template variables resolved at enrollment time so the agent always
// sees concrete names — never placeholders:
//
//   {tenant}     → the tenant namespace (e.g. "kontango")
//   {app}        → the app slug (e.g. "ticketarr")
//   {deployment} → the deployment slug (e.g. "prod-01")
//   {zone}       → the overlay TLD (default "tango" from _catalog/config.json)
//
// This means a schmutz.yml containing "ssh-{deployment}.{zone}" becomes
// "ssh-prod-01.tango" — or "ssh-prod-01.acme" for a different overlay zone.
func (h *HubHandler) writeSchmutz(ctx context.Context, app, tenant, deployment, zitiIdentity string) error {
	spec, err := h.forgejo.GetSchmutz(ctx, app, tenant, deployment)
	if errors.Is(err, forgejo.ErrNotFound) {
		return nil // no schmutz.yml defined; watcher will log "not found" on first poll
	}
	if err != nil {
		return err
	}

	// Resolve the overlay zone from _catalog/config.json defaults.
	// Falls back to "tango" if the catalog config is unavailable.
	zone := "tango"
	if cfg, err := h.forgejo.GetCatalogConfig(ctx); err == nil {
		zone = cfg.Defaults.ZoneOrDefault()
	}

	// Set identity fields.
	spec.Tenant = tenant
	spec.App = app
	spec.Deployment = deployment
	spec.ZitiIdentity = zitiIdentity

	// interpolate resolves all four template variables in a string.
	interpolate := func(s string) string {
		s = strings.ReplaceAll(s, "{tenant}", tenant)
		s = strings.ReplaceAll(s, "{app}", app)
		s = strings.ReplaceAll(s, "{deployment}", deployment)
		s = strings.ReplaceAll(s, "{zone}", zone)
		return s
	}

	for i, b := range spec.Binds {
		spec.Binds[i].Service = interpolate(b.Service)
	}
	for i, r := range spec.Routes {
		spec.Routes[i].Host = interpolate(r.Host)
	}

	// Auto-inject api: block if not already declared.
	// Derives the service list from the declared binds so operators never
	// need to manually add the api: section — it's always in sync with binds.
	if spec.API == nil {
		spec.API = autoGatewayConfig(spec, tenant, app, deployment, zone)
	}

	body, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return err
	}
	return h.baoNS.WriteJSON(ctx, tenant+"/", "secret",
		"apps/"+app+"/"+deployment+"/substrate", body)
}

// autoGatewayConfig builds a GatewayConfig from the app's declared binds.
// It finds the first HTTP bind and uses its port as the primary service.
// The oidc_role follows the standard naming: {tenant}-{app}-{dep}-token.
// Returns nil if no HTTP services are found (e.g. SSH-only or DB-only apps).
func autoGatewayConfig(spec *shared.Schmutz, tenant, app, deployment, zone string) *shared.GatewayConfig {
	var services []shared.GatewayService
	for _, b := range spec.Binds {
		// Skip the auto-injected SSH bind — it's operational, not an API
		if strings.HasPrefix(b.Service, "ssh-") {
			continue
		}
		// Parse port from local_addr (e.g. "127.0.0.1:9090" → 9090)
		_, portStr, err := splitHostPort(b.LocalAddr)
		if err != nil || portStr == 0 {
			continue
		}
		// Use the service name (without .zone suffix) as the service name
		svcName := strings.TrimSuffix(b.Service, "."+zone)
		svcName = strings.TrimSuffix(svcName, ".tango") // belt+braces
		services = append(services, shared.GatewayService{
			Name: svcName,
			Port: portStr,
		})
	}
	if len(services) == 0 {
		return nil
	}
	return &shared.GatewayConfig{
		Enabled:  true,
		Port:     7070,
		OIDCRole: tenant + "-" + app + "-" + deployment + "-token",
		Services: services,
	}
}

// splitHostPort parses "host:port" and returns the port as uint16.
func splitHostPort(addr string) (string, uint16, error) {
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, err
	}
	var port uint16
	if _, err := fmt.Sscanf(p, "%d", &port); err != nil {
		return h, 0, err
	}
	return h, port, nil
}

// requireOperatorAuth checks X-Kontango-Token. Returns false + writes 401
// on failure so callers can return immediately.
func (h *HubHandler) requireOperatorAuth(w http.ResponseWriter, r *http.Request) bool {
	if h.adminToken == "" {
		return true // no token configured = open (dev mode)
	}
	tok := r.Header.Get("X-Kontango-Token")
	if tok == "" {
		writeErr(w, http.StatusUnauthorized, "X-Kontango-Token required")
		return false
	}
	if tok != h.adminToken {
		writeErr(w, http.StatusUnauthorized, "invalid operator token")
		return false
	}
	return true
}

// flavorRoleAttrs returns the Ziti role attributes for a given enrollment flavor.
//
//	app-host   — standard app host: can dial bao + bind its app services.
//	             Starts in quarantine; operator approves via Ziti policy.
//	ctrl-node  — controller infrastructure: admins + ctrl-node roles.
//	             Full overlay access; no quarantine.
//	edge-router — Ziti edge router: gets edge-router role only.
//	db-only    — database-only host: bao-clients + host-{app}-db.
//	             No general host-{app} dial access.
func flavorRoleAttrs(flavor, app string) []string {
	switch flavor {
	case "ctrl-node":
		return []string{"admins", "ctrl-node", "bao-clients"}
	case "edge-router":
		return []string{"edge-routers"}
	case "db-only":
		return []string{"bao-clients", "host-" + app + "-db", "quarantine"}
	default: // app-host
		return []string{"bao-clients", "host-" + app, "quarantine"}
	}
}

// generateMachineNameHex mirrors the convention in enroll-server main.go:
// "machine-" + 8 lowercase hex chars (4 random bytes).
func generateMachineNameHex() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return "machine-" + hex.EncodeToString(b)
}
