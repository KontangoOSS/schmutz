// Package substrate is the agent-side reader of the per-deployment
// substrate spec the controller writes. v1 is read-only: Watcher.Run
// polls Bao every 24h, parses the spec, and logs the reconciliation plan
// without taking any action. The reconciler that actually applies the
// plan to ziti-edge-tunnel + Caddy is a separate package.
//
// Storage path it reads (mirrors what bao-app-enroll writes):
//
//	<tenant>/secret/data/apps/<app>/<deployment>/substrate
//
// Tenant isolation: the agent's bao-jwt token is scoped to its own
// tenant via the JWT auth role's bound_claims. So setting
// X-Vault-Namespace: <tenant>/ on every read is correct AND safe — Bao's
// policy layer rejects cross-tenant reads even if the namespace header
// were forged.
package substrate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is a tiny Bao HTTP client that reads exactly one thing: the
// substrate record at a known path in a known namespace. Auth is the
// scoped Bao token the bao-jwt subsystem keeps fresh at /run/bao-token.
type Client struct {
	addr string
	hc   *http.Client
}

// NewClient builds a client targeting baoAddr (e.g. http://bao.tango:8200).
// timeout applies per HTTP call; pass 0 for the default 10s.
func NewClient(baoAddr string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Client{
		addr: baoAddr,
		hc:   &http.Client{Timeout: timeout},
	}
}

// ReadSubstrateKV fetches the raw KV v2 data map for a substrate at
// <namespace>/secret/data/apps/<app>/<deployment>/substrate.
//
// On 404, returns ErrNotFound — distinct from a parse error so callers
// can distinguish "no substrate exists for this host yet" from
// "substrate exists but is malformed."
func (c *Client) ReadSubstrateKV(ctx context.Context, token, namespace, app, deployment string) (map[string]any, error) {
	if token == "" {
		return nil, fmt.Errorf("substrate: read: token required")
	}
	url := fmt.Sprintf("%s/v1/secret/data/apps/%s/%s/substrate",
		c.addr, app, deployment)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("substrate: new request: %w", err)
	}
	req.Header.Set("X-Vault-Token", token)
	if namespace != "" {
		req.Header.Set("X-Vault-Namespace", namespace)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("substrate: call %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	switch resp.StatusCode {
	case http.StatusOK:
		// proceed
	case http.StatusNotFound:
		return nil, ErrNotFound
	default:
		return nil, fmt.Errorf("substrate: read returned %d: %s",
			resp.StatusCode, bytes.TrimSpace(body))
	}
	var kv struct {
		Data struct {
			Data map[string]any `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &kv); err != nil {
		return nil, fmt.Errorf("substrate: decode kv: %w", err)
	}
	if kv.Data.Data == nil {
		return nil, ErrNotFound
	}
	return kv.Data.Data, nil
}

// ErrNotFound signals "the substrate path doesn't exist yet" — distinct
// from network or parse errors. Callers (the watcher) treat this as
// "host is not yet configured; keep polling, log at INFO."
var ErrNotFound = errSubstrateNotFound{}

type errSubstrateNotFound struct{}

func (errSubstrateNotFound) Error() string { return "substrate: not found" }
