package ziti

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

type Client interface {
	CreateIdentityWithSSH(ctx context.Context, req EnrollmentRequest) (*EnrollmentResult, error)
	Health(ctx context.Context) error

	ListIdentities(ctx context.Context, f IdentityFilter) ([]Identity, error)
	GetIdentity(ctx context.Context, name string) (*Identity, error)
	UpdateIdentity(ctx context.Context, name string, req UpdateIdentityRequest) error
	DeleteIdentityWithSSH(ctx context.Context, name string) error
	CreateBareIdentity(ctx context.Context, name string, roleAttrs []string, tags map[string]string) (*EnrollmentResult, error)

	RoleAttributes(ctx context.Context, name string) ([]string, error)

	// CreateService creates a named Ziti service if it does not already exist.
	// Idempotent — returns nil if the service exists.
	CreateService(ctx context.Context, name string, roleAttrs []string) error
}

type httpClient struct {
	api          string
	user         string
	pass         string
	hc           *http.Client
	mu           sync.Mutex
	session      string
	cfgTypeCache map[string]string // name → id, populated lazily
}

func NewHTTP(api, user, pass string, skipTLS bool) Client {
	return &httpClient{
		api:          api,
		user:         user,
		pass:         pass,
		cfgTypeCache: make(map[string]string),
		hc: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: skipTLS},
			},
		},
	}
}

// authenticateLocked performs auth and stores session.
// Caller MUST hold c.mu.
func (c *httpClient) authenticateLocked(ctx context.Context) error {
	body := map[string]string{"username": c.user, "password": c.pass}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", c.api+"/edge/management/v1/authenticate?method=password", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("ziti auth: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ziti auth status %d: %s", resp.StatusCode, body)
	}
	var ar struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		return fmt.Errorf("decode auth: %w", err)
	}
	c.session = ar.Data.Token
	return nil
}

func (c *httpClient) sessionToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session != "" {
		return c.session, nil
	}
	if err := c.authenticateLocked(ctx); err != nil {
		return "", err
	}
	return c.session, nil
}

func (c *httpClient) doJSON(ctx context.Context, method, path string, in, out interface{}) error {
	for attempt := 0; attempt < 2; attempt++ {
		tok, err := c.sessionToken(ctx)
		if err != nil {
			return err
		}
		var body io.Reader
		if in != nil {
			b, _ := json.Marshal(in)
			body = bytes.NewReader(b)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.api+path, body)
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("zt-session", tok)
		resp, err := c.hc.Do(req)
		if err != nil {
			return fmt.Errorf("%s %s: %w", method, path, err)
		}

		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			resp.Body.Close()
			c.mu.Lock()
			c.session = ""
			c.mu.Unlock()
			continue
		}

		if resp.StatusCode >= 400 {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return fmt.Errorf("%s %s status %d: %s", method, path, resp.StatusCode, b)
		}

		if out != nil {
			err := json.NewDecoder(resp.Body).Decode(out)
			resp.Body.Close()
			return err
		}
		resp.Body.Close()
		return nil
	}
	return fmt.Errorf("%s %s: still 401 after retry", method, path)
}

// configTypeID looks up the ID of a config type by name. Caches results.
// Each Ziti cluster has different IDs; we discover them at runtime.
func (c *httpClient) configTypeID(ctx context.Context, name string) (string, error) {
	c.mu.Lock()
	if id, ok := c.cfgTypeCache[name]; ok {
		c.mu.Unlock()
		return id, nil
	}
	c.mu.Unlock()

	var resp struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, "GET", "/edge/management/v1/config-types?limit=200", nil, &resp); err != nil {
		return "", fmt.Errorf("list config-types: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ct := range resp.Data {
		c.cfgTypeCache[ct.Name] = ct.ID
	}
	id, ok := c.cfgTypeCache[name]
	if !ok {
		return "", fmt.Errorf("config type %q not found", name)
	}
	return id, nil
}

func (c *httpClient) Health(ctx context.Context) error {
	// Just authenticate (or use cached session): if we get here without error, healthy.
	_, err := c.sessionToken(ctx)
	return err
}

// CreateIdentityWithSSH creates the identity, the per-machine SSH service,
// the bind policy, and the dial policy. Returns the JWT for the
// auto-generated OTT enrollment.
//
// The caller MUST include "machine-<IdentityName>-sshhost" in
// req.RoleAttributes so the bind policy matches. Otherwise the SSH service
// will be unbindable.
func (c *httpClient) CreateIdentityWithSSH(ctx context.Context, req EnrollmentRequest) (*EnrollmentResult, error) {
	// rollback closures, executed in reverse order on any failure.
	var rollbacks []func()
	cleanupCtx := context.WithoutCancel(ctx)
	doRollback := func() {
		for i := len(rollbacks) - 1; i >= 0; i-- {
			rollbacks[i]()
		}
	}

	// Step 1: create identity
	idBody := map[string]interface{}{
		"name":             req.IdentityName,
		"type":             "Default",
		"isAdmin":          false,
		"enrollment":       map[string]bool{"ott": true},
		"roleAttributes":   req.RoleAttributes,
		"tags":             req.Tags,
	}
	var idResp struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, "POST", "/edge/management/v1/identities", idBody, &idResp); err != nil {
		return nil, fmt.Errorf("create identity: %w", err)
	}
	idID := idResp.Data.ID
	rollbacks = append(rollbacks, func() {
		_ = c.doJSON(cleanupCtx, "DELETE", "/edge/management/v1/identities/"+idID, nil, nil)
	})

	// Step 2: read identity back to get the auto-generated OTT JWT
	var idDetail struct {
		Data struct {
			Enrollment struct {
				OTT struct {
					JWT string `json:"jwt"`
				} `json:"ott"`
			} `json:"enrollment"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, "GET", "/edge/management/v1/identities/"+idID, nil, &idDetail); err != nil {
		doRollback()
		return nil, fmt.Errorf("read identity OTT: %w", err)
	}
	jwt := idDetail.Data.Enrollment.OTT.JWT
	if jwt == "" {
		doRollback()
		return nil, fmt.Errorf("identity %s missing OTT JWT", idID)
	}

	// Look up config type IDs (Ziti requires the ID, not the name)
	hostV1ID, err := c.configTypeID(ctx, "host.v1")
	if err != nil {
		doRollback()
		return nil, fmt.Errorf("lookup host.v1 config type: %w", err)
	}
	interceptV1ID, err := c.configTypeID(ctx, "intercept.v1")
	if err != nil {
		doRollback()
		return nil, fmt.Errorf("lookup intercept.v1 config type: %w", err)
	}

	// Step 3: create host config (host.v1)
	hostCfgBody := map[string]interface{}{
		"name":         "ssh-" + req.IdentityName + "-host",
		"configTypeId": hostV1ID,
		"data": map[string]interface{}{
			"protocol": "tcp",
			"address":  "127.0.0.1",
			"port":     22,
		},
	}
	var hostCfgResp struct {
		Data struct{ ID string `json:"id"` } `json:"data"`
	}
	if err := c.doJSON(ctx, "POST", "/edge/management/v1/configs", hostCfgBody, &hostCfgResp); err != nil {
		doRollback()
		return nil, fmt.Errorf("create host config: %w", err)
	}
	hostCfgID := hostCfgResp.Data.ID
	rollbacks = append(rollbacks, func() {
		_ = c.doJSON(cleanupCtx, "DELETE", "/edge/management/v1/configs/"+hostCfgID, nil, nil)
	})

	// Step 4: create intercept config (intercept.v1)
	interceptName := "ssh-" + req.IdentityName + ".tango"
	interceptCfgBody := map[string]interface{}{
		"name":         "ssh-" + req.IdentityName + "-intercept",
		"configTypeId": interceptV1ID,
		"data": map[string]interface{}{
			"protocols":  []string{"tcp"},
			"addresses":  []string{interceptName},
			"portRanges": []map[string]int{{"low": 22, "high": 22}},
		},
	}
	var interceptCfgResp struct {
		Data struct{ ID string `json:"id"` } `json:"data"`
	}
	if err := c.doJSON(ctx, "POST", "/edge/management/v1/configs", interceptCfgBody, &interceptCfgResp); err != nil {
		doRollback()
		return nil, fmt.Errorf("create intercept config: %w", err)
	}
	interceptCfgID := interceptCfgResp.Data.ID
	rollbacks = append(rollbacks, func() {
		_ = c.doJSON(cleanupCtx, "DELETE", "/edge/management/v1/configs/"+interceptCfgID, nil, nil)
	})

	// Step 5: create service
	svcBody := map[string]interface{}{
		"name":               interceptName,
		"configs":            []string{hostCfgID, interceptCfgID},
		"roleAttributes":     []string{"admin-only"},
		"encryptionRequired": true,
	}
	var svcResp struct {
		Data struct{ ID string `json:"id"` } `json:"data"`
	}
	if err := c.doJSON(ctx, "POST", "/edge/management/v1/services", svcBody, &svcResp); err != nil {
		doRollback()
		return nil, fmt.Errorf("create service: %w", err)
	}
	svcID := svcResp.Data.ID
	rollbacks = append(rollbacks, func() {
		_ = c.doJSON(cleanupCtx, "DELETE", "/edge/management/v1/services/"+svcID, nil, nil)
	})

	// Step 6: create bind policy (only this identity's #machine-<name>-sshhost can bind)
	bindBody := map[string]interface{}{
		"name":          "bind-ssh-" + req.IdentityName,
		"type":          "Bind",
		"identityRoles": []string{"#machine-" + req.IdentityName + "-sshhost"},
		"serviceRoles":  []string{"@" + svcID},
		"semantic":      "AllOf",
	}
	var bindResp struct {
		Data struct{ ID string `json:"id"` } `json:"data"`
	}
	if err := c.doJSON(ctx, "POST", "/edge/management/v1/service-policies", bindBody, &bindResp); err != nil {
		doRollback()
		return nil, fmt.Errorf("create bind policy: %w", err)
	}
	bindPolID := bindResp.Data.ID
	rollbacks = append(rollbacks, func() {
		_ = c.doJSON(cleanupCtx, "DELETE", "/edge/management/v1/service-policies/"+bindPolID, nil, nil)
	})

	// Step 7: create dial policy (#admins can dial)
	dialBody := map[string]interface{}{
		"name":          "dial-ssh-" + req.IdentityName,
		"type":          "Dial",
		"identityRoles": []string{"#admins"},
		"serviceRoles":  []string{"@" + svcID},
		"semantic":      "AllOf",
	}
	var dialResp struct {
		Data struct{ ID string `json:"id"` } `json:"data"`
	}
	if err := c.doJSON(ctx, "POST", "/edge/management/v1/service-policies", dialBody, &dialResp); err != nil {
		doRollback()
		return nil, fmt.Errorf("create dial policy: %w", err)
	}
	dialPolID := dialResp.Data.ID
	rollbacks = append(rollbacks, func() {
		_ = c.doJSON(cleanupCtx, "DELETE", "/edge/management/v1/service-policies/"+dialPolID, nil, nil)
	})

	// Step 8: create service-edge-router-policy — grants the service access
	// to the cluster's edge routers (#ref-routers). Without this, the service
	// has no routers and is unreachable even if dial/bind policies are correct.
	serpBody := map[string]interface{}{
		"name":            "router-ssh-" + req.IdentityName,
		"serviceRoles":    []string{"@" + svcID},
		"edgeRouterRoles": []string{"#ref-routers"},
		"semantic":        "AllOf",
	}
	var serpResp struct {
		Data struct{ ID string `json:"id"` } `json:"data"`
	}
	if err := c.doJSON(ctx, "POST", "/edge/management/v1/service-edge-router-policies", serpBody, &serpResp); err != nil {
		doRollback()
		return nil, fmt.Errorf("create service-edge-router-policy: %w", err)
	}
	serpID := serpResp.Data.ID
	rollbacks = append(rollbacks, func() {
		_ = c.doJSON(cleanupCtx, "DELETE", "/edge/management/v1/service-edge-router-policies/"+serpID, nil, nil)
	})

	// Step 9: create edge-router-policy for the bind identity — the
	// #machine-<name>-sshhost role needs router access to bind the service.
	erpBody := map[string]interface{}{
		"name":            "routers-machine-" + req.IdentityName + "-sshhost",
		"identityRoles":   []string{"#machine-" + req.IdentityName + "-sshhost"},
		"edgeRouterRoles": []string{"#ref-routers"},
		"semantic":        "AllOf",
	}
	if err := c.doJSON(ctx, "POST", "/edge/management/v1/edge-router-policies", erpBody, nil); err != nil {
		doRollback()
		return nil, fmt.Errorf("create edge-router-policy: %w", err)
	}
	// step 9 not tracked — last operation, no further steps to fail.

	return &EnrollmentResult{
		IdentityID:   idID,
		IdentityName: req.IdentityName,
		JWT:          jwt,
		CABundle:     "",
	}, nil
}

// CreateService creates a named Ziti service if it does not already exist.
// Idempotent — returns nil if the service already exists.
func (c *httpClient) CreateService(ctx context.Context, name string, roleAttrs []string) error {
	// Check if service already exists by querying with a name filter.
	var listResp struct {
		Data []struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	filterPath := fmt.Sprintf("/edge/management/v1/services?filter=name%%3D%%22%s%%22", name)
	if err := c.doJSON(ctx, "GET", filterPath, nil, &listResp); err != nil {
		return fmt.Errorf("ziti: list services: %w", err)
	}
	for _, s := range listResp.Data {
		if s.Name == name {
			return nil // already exists
		}
	}
	body := map[string]any{
		"name":               name,
		"roleAttributes":     roleAttrs,
		"terminatorStrategy": "smartrouting",
		"encryptionRequired": true,
	}
	var createResp struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, "POST", "/edge/management/v1/services", body, &createResp); err != nil {
		return fmt.Errorf("ziti: create service %s: %w", name, err)
	}
	return nil
}

// Compile-time check: httpClient implements the full Client interface
// (admin methods are defined in admin_client.go).
var _ Client = (*httpClient)(nil)
