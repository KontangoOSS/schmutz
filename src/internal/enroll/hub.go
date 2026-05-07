package enroll

// Package enroll — hub.go adds the hub enrollment path.
//
// RegisterHub is the new primary enrollment path, replacing the legacy
// WebSocket flow (RegisterWS / schmutz-controller). It calls the hub's
// two-command flow:
//
//  1. POST /api/v1/enroll  { token, tenant, app, deployment, hardware_facts }
//     → returns { ziti_identity: {name, identity_json}, bao_bundle: {...} }
//
//  2. EnrollJWT(identity_json, identityPath)
//     → exchanges the Ziti enrollment JWT for a local identity file
//     → identical to the existing step-4 in RegisterWS
//
//  3. WriteAgentConfig(bao_bundle, agentPath)
//     → persists /etc/schmutz/agent.json for the bao-jwt refresh loop
//
// The hub itself (on the controller) handles the ziti identity creation,
// so no ziti-controller connection is needed from the agent during enrollment.
// The only required external connection is to the hub, over :443 ALPN.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/KontangoOSS/schmutz/internal/baojwt"
)

// HubEnrollConfig is everything RegisterHub needs.
type HubEnrollConfig struct {
	// ControllerURL is the hub base URL, e.g. https://join.kontango.net
	// or http://ziti-base.tango:8765. The hub must serve /api/v1/enroll.
	ControllerURL string

	// Token is the single-use enrollment token issued by the operator via
	// POST /api/v1/enrollments. Shape: enroll-<hex32>.
	Token string

	// Tenant, App, Deployment, Flavor identify what this machine is being
	// enrolled as. They must match the token record in Bao.
	Tenant     string
	App        string
	Deployment string
	Flavor     string

	// IdentityPath is where the resulting Ziti identity file will be
	// written, e.g. /etc/schmutz/<name>.json.
	IdentityPath string

	// AgentJSONPath is where the Bao bundle config is persisted,
	// e.g. /etc/schmutz/agent.json. Used by the bao-jwt refresh loop.
	AgentJSONPath string

	// Hardware is optional telemetry sent to the hub. The hub logs it
	// but doesn't require it. Nil is acceptable.
	Hardware map[string]interface{}
}

// HubEnrollResult carries the outcome of a successful enrollment.
type HubEnrollResult struct {
	ZitiIdentityName string
	IdentityPath     string
	AgentJSONPath    string
}

// hubClaimRequest is what the agent POSTs to /api/v1/enroll.
type hubClaimRequest struct {
	Token      string                 `json:"token"`
	Tenant     string                 `json:"tenant"`
	App        string                 `json:"app"`
	Deployment string                 `json:"deployment"`
	Hardware   map[string]interface{} `json:"hardware,omitempty"`
}

// hubClaimResponse is what the hub returns from /api/v1/enroll.
type hubClaimResponse struct {
	ZitiIdentity struct {
		Name         string `json:"name"`
		IdentityJSON string `json:"identity_json"` // Ziti enrollment JWT
	} `json:"ziti_identity"`
	BaoBundle struct {
		Role              string `json:"role"`
		RoleID            string `json:"role_id"`
		SecretIDWrapToken string `json:"secret_id_wrap_token"`
		WrapTTLSeconds    int    `json:"wrap_ttl_seconds"`
		OIDCRole          string `json:"oidc_role"`
		JWTRole           string `json:"jwt_role"`
		Tenant            string `json:"tenant"`
		App               string `json:"app"`
		Deployment        string `json:"deployment"`
		Flavor            string `json:"flavor"`
		ZitiIdentity      string `json:"ziti_identity"`
		EntityID          string `json:"entity_id"`
		BaoAddr           string `json:"bao_addr"`
	} `json:"bao_bundle"`
	Error string `json:"error,omitempty"`
}

// RegisterHub performs the hub enrollment flow. It is the replacement for
// RegisterWS when the hub endpoint is available.
//
// On success, the agent has:
//   - A Ziti identity file at cfg.IdentityPath
//   - A Bao agent config at cfg.AgentJSONPath
//   - The bao-jwt subsystem can start refreshing /run/bao-token immediately
//
// Error handling:
//   - 401 → invalid/expired/revoked token; operator must re-issue
//   - 409 → token already consumed or deployment no longer enrollable
//   - 400 → request malformed (token/tenant/app/deployment mismatch)
//   - 5xx → hub-side failure after token consumption; request a new token
//   - network error → hub unreachable; check overlay connectivity
func RegisterHub(ctx context.Context, cfg HubEnrollConfig) (*HubEnrollResult, error) {
	if cfg.ControllerURL == "" {
		return nil, fmt.Errorf("enroll/hub: controller URL required")
	}
	if cfg.Token == "" {
		return nil, fmt.Errorf("enroll/hub: enrollment token required")
	}
	if cfg.Tenant == "" || cfg.App == "" || cfg.Deployment == "" {
		return nil, fmt.Errorf("enroll/hub: tenant, app, deployment required")
	}
	if cfg.IdentityPath == "" {
		return nil, fmt.Errorf("enroll/hub: identity path required")
	}
	if cfg.AgentJSONPath == "" {
		return nil, fmt.Errorf("enroll/hub: agent.json path required")
	}

	// 1. Call the hub's claim endpoint.
	claimReq := hubClaimRequest{
		Token:      cfg.Token,
		Tenant:     cfg.Tenant,
		App:        cfg.App,
		Deployment: cfg.Deployment,
		Hardware:   cfg.Hardware,
	}
	payload, err := json.Marshal(claimReq)
	if err != nil {
		return nil, fmt.Errorf("enroll/hub: marshal request: %w", err)
	}

	httpClient := &http.Client{Timeout: 60 * time.Second}
	url := cfg.ControllerURL + "/api/v1/enroll"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("enroll/hub: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("enroll/hub: call %s: %w (check overlay connectivity — traffic must reach the hub via :443 ALPN)", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var claimResp hubClaimResponse
	if err := json.Unmarshal(body, &claimResp); err != nil {
		return nil, fmt.Errorf("enroll/hub: decode response (status=%d): %w", resp.StatusCode, err)
	}
	if claimResp.Error != "" {
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return nil, fmt.Errorf("enroll/hub: invalid or expired token — operator must re-issue: %s", claimResp.Error)
		case http.StatusConflict:
			return nil, fmt.Errorf("enroll/hub: token already consumed or deployment not enrollable — operator must re-issue: %s", claimResp.Error)
		case http.StatusBadRequest:
			return nil, fmt.Errorf("enroll/hub: request mismatch — check tenant/app/deployment match the token: %s", claimResp.Error)
		default:
			return nil, fmt.Errorf("enroll/hub: hub error (status=%d — token may be consumed; request a new one): %s", resp.StatusCode, claimResp.Error)
		}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("enroll/hub: unexpected status %d: %s", resp.StatusCode, body)
	}
	if claimResp.ZitiIdentity.IdentityJSON == "" {
		return nil, fmt.Errorf("enroll/hub: response missing ziti identity JWT")
	}
	if claimResp.BaoBundle.RoleID == "" || claimResp.BaoBundle.BaoAddr == "" {
		return nil, fmt.Errorf("enroll/hub: response missing bao bundle fields")
	}

	// 2. Write the identity JSON the hub already enrolled server-side.
	//    The hub calls EnrollJWT on the controller node (where port 6262
	//    is reachable), so the agent receives a complete identity file and
	//    never needs to reach the Ziti enrollment port directly.
	if err := WriteIdentity([]byte(claimResp.ZitiIdentity.IdentityJSON), cfg.IdentityPath); err != nil {
		return nil, fmt.Errorf("enroll/hub: write identity: %w", err)
	}

	// 3. Persist the Bao bundle as /etc/schmutz/agent.json so the
	// bao-jwt subsystem can start refreshing /run/bao-token.
	// The bundle carries a response-wrapped secret_id; InstallBundle
	// unwraps it and writes the resolved secret_id to disk.
	b := &baojwt.Bundle{
		Role:              claimResp.BaoBundle.Role,
		RoleID:            claimResp.BaoBundle.RoleID,
		SecretIDWrapToken: claimResp.BaoBundle.SecretIDWrapToken,
		WrapTTLSeconds:    claimResp.BaoBundle.WrapTTLSeconds,
		OIDCRole:          claimResp.BaoBundle.OIDCRole,
		JWTRole:           claimResp.BaoBundle.JWTRole,
		Tenant:            claimResp.BaoBundle.Tenant,
		App:               claimResp.BaoBundle.App,
		Deployment:        claimResp.BaoBundle.Deployment,
		Flavor:            claimResp.BaoBundle.Flavor,
		ZitiIdentity:      claimResp.ZitiIdentity.Name,
		EntityID:          claimResp.BaoBundle.EntityID,
		BaoAddr:           claimResp.BaoBundle.BaoAddr,
	}
	if _, err := baojwt.InstallBundle(ctx, b, cfg.AgentJSONPath); err != nil {
		return nil, fmt.Errorf("enroll/hub: install bao bundle: %w", err)
	}

	return &HubEnrollResult{
		ZitiIdentityName: claimResp.ZitiIdentity.Name,
		IdentityPath:     cfg.IdentityPath,
		AgentJSONPath:    cfg.AgentJSONPath,
	}, nil
}
