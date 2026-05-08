package baojwt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// AgentConfig is the on-disk per-agent state at /etc/schmutz/agent.json.
// It carries everything the refresh loop needs: where Bao lives, which
// approle this agent uses, and which OIDC + JWT roles to drive.
//
// Created by InstallBundle once the agent has unwrapped its install bundle.
// Re-readable on every refresh; never edited by the refresh loop itself.
type AgentConfig struct {
	Role         string `json:"role"`
	RoleID       string `json:"role_id"`
	SecretID     string `json:"secret_id"`
	OIDCRole     string `json:"oidc_role"`
	JWTRole      string `json:"jwt_role"`
	BaoAddr      string `json:"bao_addr"`
	Tenant       string `json:"tenant,omitempty"`
	App          string `json:"app,omitempty"`
	Deployment   string `json:"deployment,omitempty"`
	Flavor       string `json:"flavor,omitempty"`
	ZitiIdentity string `json:"ziti_identity,omitempty"`
}

// Validate fails fast on a config that's missing fields the refresh loop
// would crash on. Cheap to call before every refresh.
func (a *AgentConfig) Validate() error {
	switch {
	case a.Role == "":
		return fmt.Errorf("agent.json: role missing")
	case a.RoleID == "":
		return fmt.Errorf("agent.json: role_id missing")
	case a.SecretID == "":
		return fmt.Errorf("agent.json: secret_id missing")
	case a.OIDCRole == "":
		return fmt.Errorf("agent.json: oidc_role missing")
	case a.JWTRole == "":
		return fmt.Errorf("agent.json: jwt_role missing")
	case a.BaoAddr == "":
		return fmt.Errorf("agent.json: bao_addr missing")
	}
	return nil
}

// Bundle mirrors the controller's BaoBundleResponse — what /api/bao-bundle
// and /api/v1/enroll return. Only the fields we consume are listed.
//
// Hub enrollment (POST /api/v1/enroll) populates SecretID directly so the
// agent never needs to call Bao during bootstrap. Legacy /api/bao-bundle
// still populates SecretIDWrapToken; InstallBundle handles both.
type Bundle struct {
	Role              string `json:"role"`
	RoleID            string `json:"role_id"`
	SecretID          string `json:"secret_id,omitempty"`            // hub path: plain secret_id
	SecretIDWrapToken string `json:"secret_id_wrap_token,omitempty"` // legacy path: wrap token
	WrapTTLSeconds    int    `json:"wrap_ttl_seconds,omitempty"`
	OIDCRole          string `json:"oidc_role"`
	JWTRole           string `json:"jwt_role"`
	Tenant            string `json:"tenant"`
	App               string `json:"app"`
	Deployment        string `json:"deployment"`
	Flavor            string `json:"flavor"`
	ZitiIdentity      string `json:"ziti_identity"`
	EntityID          string `json:"entity_id"`
	BaoAddr           string `json:"bao_addr"`
}

// FetchBundle calls the controller's /api/bao-bundle endpoint and returns
// the parsed bundle. The header X-Ziti-Identity is sent so the controller
// can identify the caller; in production this gets replaced by SDK-bound
// connection identity, but the wire format stays the same.
func FetchBundle(ctx context.Context, controllerURL, zitiIdentity string, hc *http.Client) (*Bundle, error) {
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, "POST", controllerURL+"/api/bao-bundle", nil)
	if err != nil {
		return nil, fmt.Errorf("baojwt: new request: %w", err)
	}
	if zitiIdentity != "" {
		req.Header.Set("X-Ziti-Identity", zitiIdentity)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("baojwt: call %s: %w", controllerURL, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("baojwt: controller returned %d: %s", resp.StatusCode, string(bytes.TrimSpace(body)))
	}
	var b Bundle
	if err := json.Unmarshal(body, &b); err != nil {
		return nil, fmt.Errorf("baojwt: decode bundle: %w", err)
	}
	if (b.SecretID == "" && b.SecretIDWrapToken == "") || b.RoleID == "" || b.BaoAddr == "" {
		return nil, fmt.Errorf("baojwt: bundle missing required fields (need secret_id or secret_id_wrap_token)")
	}
	return &b, nil
}

// InstallBundle persists a bundle to the on-disk agent config.
//
// Hub path (SecretID set): writes directly — no Bao call needed at enrollment.
// Legacy path (SecretIDWrapToken set): unwraps via Bao first.
// Either way validates credentials with an approle login, then atomically
// writes /etc/schmutz/agent.json with mode 0600.
//
// Returns the AgentConfig that was written.
func InstallBundle(ctx context.Context, b *Bundle, agentJSONPath string) (*AgentConfig, error) {
	if b == nil {
		return nil, fmt.Errorf("baojwt: install: bundle nil")
	}
	if b.RoleID == "" || b.BaoAddr == "" {
		return nil, fmt.Errorf("baojwt: install: bundle missing role_id or bao_addr")
	}

	var secretID string
	cli := NewClient(b.BaoAddr, 0)

	if b.SecretID != "" {
		// Hub path: secret_id already resolved server-side.
		secretID = b.SecretID
	} else if b.SecretIDWrapToken != "" {
		// Legacy path: unwrap the response-wrapped secret_id.
		data, err := cli.Unwrap(ctx, b.SecretIDWrapToken)
		if err != nil {
			return nil, fmt.Errorf("baojwt: install: unwrap: %w", err)
		}
		secretID, _ = data["secret_id"].(string)
		if secretID == "" {
			return nil, fmt.Errorf("baojwt: install: unwrap returned no secret_id")
		}
	} else {
		return nil, fmt.Errorf("baojwt: install: bundle missing both secret_id and secret_id_wrap_token")
	}

	// Validate by login.
	if _, err := cli.ApproleLogin(ctx, b.RoleID, secretID); err != nil {
		return nil, fmt.Errorf("baojwt: install: approle login validate: %w", err)
	}

	// 3. Write atomically.
	cfg := &AgentConfig{
		Role:         b.Role,
		RoleID:       b.RoleID,
		SecretID:     secretID,
		OIDCRole:     b.OIDCRole,
		JWTRole:      b.JWTRole,
		BaoAddr:      b.BaoAddr,
		Tenant:       b.Tenant,
		App:          b.App,
		Deployment:   b.Deployment,
		Flavor:       b.Flavor,
		ZitiIdentity: b.ZitiIdentity,
	}
	if err := writeAgentConfig(agentJSONPath, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// LoadAgentConfig reads and validates the persisted agent config.
func LoadAgentConfig(path string) (*AgentConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("baojwt: load %s: %w", path, err)
	}
	var c AgentConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("baojwt: parse %s: %w", path, err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func writeAgentConfig(path string, cfg *AgentConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return fmt.Errorf("baojwt: mkdir %s: %w", filepath.Dir(path), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".agent.json.*")
	if err != nil {
		return fmt.Errorf("baojwt: tempfile: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("baojwt: chmod tempfile: %w", err)
	}
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cfg); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("baojwt: encode: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("baojwt: close tempfile: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("baojwt: rename: %w", err)
	}
	return nil
}
