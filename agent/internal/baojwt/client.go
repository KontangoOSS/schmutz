package baojwt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Client is a tiny Bao HTTP client scoped to exactly what the agent
// refresh loop needs. Unlike the controller-side bao.Client, this one
// is unauthenticated by default — each call attaches its own bearer
// token via the X-Vault-Token header.
type Client struct {
	addr string
	hc   *http.Client
}

// NewClient builds a client targeting baoAddr (e.g. http://bao.tango:8200).
// timeout applies per HTTP call; pass 0 for the package default (10s).
func NewClient(baoAddr string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Client{
		addr: baoAddr,
		hc:   &http.Client{Timeout: timeout},
	}
}

// Health probes /v1/sys/health. Returns nil on 200, 429 (standby), or 472/473
// (recovery / DR). Returns a typed error otherwise. We treat 429 as healthy
// because the agent only needs a node that can serve reads.
func (c *Client) Health(ctx context.Context) error {
	body, status, err := c.do(ctx, "GET", "/v1/sys/health", "", nil)
	if err != nil {
		return err
	}
	switch status {
	case http.StatusOK,
		http.StatusTooManyRequests, // standby with 'standbyok' default = false → 429
		472, 473:                   // recovery / DR replication
		return nil
	}
	return fmt.Errorf("bao health: status %d: %s", status, body)
}

// Unwrap consumes a response-wrapped token and returns the inner data.
// Used to redeem the install-time secret_id wrap-token.
func (c *Client) Unwrap(ctx context.Context, wrapToken string) (map[string]any, error) {
	body, status, err := c.do(ctx, "POST", "/v1/sys/wrapping/unwrap", wrapToken, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("bao unwrap: status %d: %s", status, body)
	}
	var resp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("bao unwrap: decode: %w", err)
	}
	if resp.Data == nil {
		return nil, fmt.Errorf("bao unwrap: empty data")
	}
	return resp.Data, nil
}

// ApproleLogin exchanges role_id+secret_id for an entity-bound app token.
func (c *Client) ApproleLogin(ctx context.Context, roleID, secretID string) (string, error) {
	body, status, err := c.do(ctx, "POST", "/v1/auth/approle/login", "",
		map[string]string{"role_id": roleID, "secret_id": secretID})
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("bao approle login: status %d: %s", status, body)
	}
	var resp struct {
		Auth struct {
			ClientToken string `json:"client_token"`
		} `json:"auth"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("bao approle login: decode: %w", err)
	}
	if resp.Auth.ClientToken == "" {
		return "", fmt.Errorf("bao approle login: no client_token in response")
	}
	return resp.Auth.ClientToken, nil
}

// MintOIDCToken reads the identity engine's OIDC token endpoint with the
// app token, returning the signed JWT.
func (c *Client) MintOIDCToken(ctx context.Context, appToken, oidcRole string) (string, error) {
	path := "/v1/identity/oidc/token/" + url.PathEscape(oidcRole)
	body, status, err := c.do(ctx, "GET", path, appToken, nil)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("bao oidc mint: status %d: %s", status, body)
	}
	var resp struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("bao oidc mint: decode: %w", err)
	}
	if resp.Data.Token == "" {
		return "", fmt.Errorf("bao oidc mint: no token in response")
	}
	return resp.Data.Token, nil
}

// JWTLogin exchanges the OIDC JWT for a scoped policy token via the jwt
// auth method. The returned token is what the agent writes to
// /run/bao-token.
func (c *Client) JWTLogin(ctx context.Context, jwtRole, jwt string) (string, error) {
	body, status, err := c.do(ctx, "POST", "/v1/auth/jwt/login", "",
		map[string]string{"role": jwtRole, "jwt": jwt})
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("bao jwt login: status %d: %s", status, body)
	}
	var resp struct {
		Auth struct {
			ClientToken string `json:"client_token"`
		} `json:"auth"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("bao jwt login: decode: %w", err)
	}
	if resp.Auth.ClientToken == "" {
		return "", fmt.Errorf("bao jwt login: no client_token in response")
	}
	return resp.Auth.ClientToken, nil
}

// do is the shared HTTP plumbing. token is optional; if non-empty it's
// attached as X-Vault-Token. payload, if non-nil, is JSON-encoded.
func (c *Client) do(ctx context.Context, method, path, token string, payload any) ([]byte, int, error) {
	var rdr io.Reader
	if payload != nil {
		buf, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, fmt.Errorf("encode payload: %w", err)
		}
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.addr+path, rdr)
	if err != nil {
		return nil, 0, fmt.Errorf("new request: %w", err)
	}
	if token != "" {
		req.Header.Set("X-Vault-Token", token)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		// Surface the URL so the operator can tell whether DNS, the
		// tunneler, or the controller is the failure mode.
		return nil, 0, fmt.Errorf("call %s %s: %w", method, c.addr+path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read body: %w", err)
	}
	return body, resp.StatusCode, nil
}
