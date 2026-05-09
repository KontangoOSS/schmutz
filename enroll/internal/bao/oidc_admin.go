package bao

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"time"
)

// OIDCAdmin exposes the Bao identity-engine + auth-method admin surface used
// to provision per-app JWT auth (the bao-jwt flow in
// scripts/bao-jwt/enroll-app.sh, ported to Go).
//
// The methods are scoped narrowly to what the BaoBundleHandler needs; this is
// not a generic Bao SDK.
type OIDCAdmin interface {
	// LookupEntityByName returns the entity matching name, or ErrNotFound.
	LookupEntityByName(ctx context.Context, name string) (*Entity, error)

	// UpsertEntity creates or updates the named entity with the given metadata.
	// Returns the resolved entity (with id).
	UpsertEntity(ctx context.Context, name string, metadata map[string]string) (*Entity, error)

	// AuthAccessor returns the mount accessor for an auth method path
	// (e.g. "approle/" → "auth_approle_..."). Used to bind entity-aliases.
	AuthAccessor(ctx context.Context, path string) (string, error)

	// EnsureAuthMethod ensures an auth method is enabled at the given path.
	// path uses the auth-list shape ("approle/", "jwt/").
	EnsureAuthMethod(ctx context.Context, path, methodType string) error

	// EnsureEntityAlias ensures an alias name → canonicalEntityID exists
	// against the given mount accessor.
	EnsureEntityAlias(ctx context.Context, name, canonicalEntityID, mountAccessor string) error

	// PutPolicy upserts the named policy with the given HCL document.
	PutPolicy(ctx context.Context, name, hcl string) error

	// PutApproleRole upserts an approle with the given options.
	PutApproleRole(ctx context.Context, role string, opts ApproleRoleOptions) error

	// ReadApproleRoleID returns the role_id for an approle.
	ReadApproleRoleID(ctx context.Context, role string) (string, error)

	// WrapApproleSecretID generates a fresh secret_id for an approle and
	// returns it response-wrapped (single-use, short TTL).
	WrapApproleSecretID(ctx context.Context, role string, wrapTTL time.Duration) (token string, ttlSeconds int, err error)

	// GetApproleSecretID generates a fresh plain secret_id for an approle.
	// Use this for hub-side enrollment where the server unwraps on behalf
	// of the agent, eliminating the agent's bootstrap Bao dependency.
	GetApproleSecretID(ctx context.Context, role string) (secretID string, err error)

	// EnsureOIDCKey ensures an identity OIDC signing key exists with the given
	// algorithm + rotation policy. Adds clientID to allowed_client_ids if missing.
	EnsureOIDCKey(ctx context.Context, key, algorithm string, rotationPeriod, verificationTTL time.Duration, allowClientID string) error

	// PutOIDCRole upserts an identity OIDC role.
	PutOIDCRole(ctx context.Context, role string, opts OIDCRoleOptions) error

	// EnsureJWTAuthConfig ensures the jwt auth method is configured against
	// the given JWKS URL. boundIssuer of "" disables issuer-binding.
	EnsureJWTAuthConfig(ctx context.Context, jwksURL, boundIssuer string) error

	// PutJWTRole upserts a jwt auth role.
	PutJWTRole(ctx context.Context, role string, opts JWTRoleOptions) error
}

// Entity is the subset of Bao's identity-entity shape we care about.
type Entity struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Metadata map[string]string `json:"metadata"`
}

// ApproleRoleOptions captures the approle settings used for app auth.
// CIDR fields default to no restriction if empty.
type ApproleRoleOptions struct {
	TokenTTL            time.Duration
	TokenMaxTTL         time.Duration
	TokenPolicies       []string
	TokenBoundCIDRs     []string
	SecretIDBoundCIDRs  []string
	SecretIDNumUses     int    // 0 = unlimited
	SecretIDTTL         time.Duration
}

// OIDCRoleOptions captures the identity OIDC role settings.
type OIDCRoleOptions struct {
	Key      string
	TTL      time.Duration
	Template string // raw JSON template with {{identity.entity.metadata.X}} interpolations
	ClientID string
}

// JWTRoleOptions captures the jwt auth role settings.
type JWTRoleOptions struct {
	RoleType        string // "jwt"
	UserClaim       string
	BoundAudiences  []string
	BoundClaimsType string // "string" or "glob"
	BoundClaims     map[string]string
	TokenPolicies   []string
	TokenTTL        time.Duration
	TokenMaxTTL     time.Duration
	TokenBoundCIDRs []string
}

// rawURL returns the full Bao API URL for a path (no /v1/ prefix; caller
// supplies the full path under v1).
func (c *httpClient) rawURL(path string) string {
	return fmt.Sprintf("%s/v1/%s", c.addr, path)
}

func (c *httpClient) postJSON(ctx context.Context, path string, payload any) ([]byte, int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	status, b, err := c.do(ctx, "POST", c.rawURL(path), body)
	return b, status, err
}

func (c *httpClient) getJSON(ctx context.Context, path string) ([]byte, int, error) {
	status, b, err := c.do(ctx, "GET", c.rawURL(path), nil)
	return b, status, err
}

func (c *httpClient) LookupEntityByName(ctx context.Context, name string) (*Entity, error) {
	// Retry up to 5 times with 200ms backoff for the same Raft propagation
	// reason as ReadApproleRoleID: a write on the leader may not be visible
	// on a standby within one RTT.
	const maxAttempts = 5
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
		}
		body, status, err := c.postJSON(ctx, "identity/lookup/entity", map[string]string{"name": name})
		if err != nil {
			return nil, err
		}
		if status == http.StatusNoContent || status == http.StatusNotFound {
			continue // Raft propagation lag — retry
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("bao lookup entity %q returned %d: %s", name, status, body)
		}
		if len(body) == 0 {
			continue // empty 200 — retry
		}
		var resp struct {
			Data Entity `json:"data"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("decode entity %q: %w (body=%s)", name, err, body)
		}
		if resp.Data.ID == "" {
			continue // id not yet visible — retry
		}
		return &resp.Data, nil
	}
	return nil, fmt.Errorf("bao lookup entity %q: %w (not found after %d attempts)", name, ErrNotFound, maxAttempts)
}

func (c *httpClient) UpsertEntity(ctx context.Context, name string, metadata map[string]string) (*Entity, error) {
	payload := map[string]any{"name": name, "metadata": metadata}
	body, status, err := c.postJSON(ctx, "identity/entity", payload)
	if err != nil {
		return nil, err
	}
	switch status {
	case http.StatusOK:
		// Create path: response carries the new id+name in data. Use it
		// directly — looking up by name immediately after create can race
		// against Bao's identity-store consistency window and return 204.
		// The metadata we just wrote isn't echoed in the create response,
		// so synthesize it from the input.
		var resp struct {
			Data struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("decode create-entity response: %w (body=%s)", err, body)
		}
		if resp.Data.ID == "" {
			return nil, fmt.Errorf("bao create-entity returned no id: %s", body)
		}
		return &Entity{
			ID:       resp.Data.ID,
			Name:     resp.Data.Name,
			Metadata: copyStringMap(metadata),
		}, nil
	case http.StatusNoContent:
		// Update path: empty body. Lookup is safe here — the entity has
		// existed long enough to have settled.
		return c.LookupEntityByName(ctx, name)
	default:
		return nil, fmt.Errorf("bao upsert entity returned %d: %s", status, body)
	}
}

// copyStringMap returns a defensive copy so callers can't mutate the
// metadata stored on the returned Entity after the fact.
func copyStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (c *httpClient) AuthAccessor(ctx context.Context, path string) (string, error) {
	body, status, err := c.getJSON(ctx, "sys/auth/"+path)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("bao read sys/auth/%s returned %d: %s", path, status, body)
	}
	var resp struct {
		Data struct {
			Accessor string `json:"accessor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("decode auth accessor: %w", err)
	}
	if resp.Data.Accessor == "" {
		return "", fmt.Errorf("bao auth/%s missing accessor", path)
	}
	return resp.Data.Accessor, nil
}

func (c *httpClient) EnsureAuthMethod(ctx context.Context, path, methodType string) error {
	// Probe; if present, no-op.
	if _, err := c.AuthAccessor(ctx, path); err == nil {
		return nil
	}
	body, status, err := c.postJSON(ctx, "sys/auth/"+path, map[string]string{"type": methodType})
	if err != nil {
		return err
	}
	// 204 = enabled.
	if status != http.StatusOK && status != http.StatusNoContent {
		return fmt.Errorf("bao enable auth/%s (%s) returned %d: %s", path, methodType, status, body)
	}
	return nil
}

func (c *httpClient) EnsureEntityAlias(ctx context.Context, name, canonicalID, mountAccessor string) error {
	// Probe via the entity to see if an alias with this name + accessor already exists.
	body, status, err := c.getJSON(ctx, "identity/entity/id/"+url.PathEscape(canonicalID))
	if err != nil {
		return err
	}
	if status == http.StatusOK {
		var resp struct {
			Data struct {
				Aliases []struct {
					Name          string `json:"name"`
					MountAccessor string `json:"mount_accessor"`
				} `json:"aliases"`
			} `json:"data"`
		}
		if json.Unmarshal(body, &resp) == nil {
			for _, a := range resp.Data.Aliases {
				if a.Name == name && a.MountAccessor == mountAccessor {
					return nil
				}
			}
		}
	}
	payload := map[string]string{
		"name":           name,
		"canonical_id":   canonicalID,
		"mount_accessor": mountAccessor,
	}
	resBody, st, err := c.postJSON(ctx, "identity/entity-alias", payload)
	if err != nil {
		return err
	}
	if st != http.StatusOK && st != http.StatusNoContent {
		return fmt.Errorf("bao create entity-alias returned %d: %s", st, resBody)
	}
	return nil
}

func (c *httpClient) PutPolicy(ctx context.Context, name, hcl string) error {
	body, status, err := c.postJSON(ctx, "sys/policy/"+url.PathEscape(name),
		map[string]string{"policy": hcl})
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusNoContent {
		return fmt.Errorf("bao put policy %s returned %d: %s", name, status, body)
	}
	return nil
}

func durSeconds(d time.Duration) int { return int(d.Seconds()) }

func (c *httpClient) PutApproleRole(ctx context.Context, role string, opts ApproleRoleOptions) error {
	payload := map[string]any{
		"token_ttl":              durSeconds(opts.TokenTTL),
		"token_max_ttl":          durSeconds(opts.TokenMaxTTL),
		"token_policies":         opts.TokenPolicies,
		"secret_id_num_uses":     opts.SecretIDNumUses,
		"secret_id_ttl":          durSeconds(opts.SecretIDTTL),
	}
	if len(opts.TokenBoundCIDRs) > 0 {
		payload["token_bound_cidrs"] = opts.TokenBoundCIDRs
	}
	if len(opts.SecretIDBoundCIDRs) > 0 {
		payload["secret_id_bound_cidrs"] = opts.SecretIDBoundCIDRs
	}
	body, status, err := c.postJSON(ctx, "auth/approle/role/"+url.PathEscape(role), payload)
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusNoContent {
		return fmt.Errorf("bao put approle %s returned %d: %s", role, status, body)
	}
	return nil
}

func (c *httpClient) ReadApproleRoleID(ctx context.Context, role string) (string, error) {
	// Retry up to 5 times with 200ms backoff. On a 3-node Raft cluster, a
	// write on the leader propagates to standbys typically within 50-100ms.
	// If the enroll-server happens to run on a standby, a read immediately
	// after a PutApproleRole write can transiently return 404. The retry
	// is cheap and eliminates the race without requiring all servers to
	// point at the leader's address.
	const maxAttempts = 5
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
		}
		body, status, err := c.getJSON(ctx, "auth/approle/role/"+url.PathEscape(role)+"/role-id")
		if err != nil {
			return "", err
		}
		if status == http.StatusNotFound {
			continue // Raft propagation lag — retry
		}
		if status != http.StatusOK {
			return "", fmt.Errorf("bao read approle role-id returned %d: %s", status, body)
		}
		var resp struct {
			Data struct {
				RoleID string `json:"role_id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return "", fmt.Errorf("decode role_id: %w", err)
		}
		if resp.Data.RoleID == "" {
			continue // empty response — retry
		}
		return resp.Data.RoleID, nil
	}
	return "", fmt.Errorf("bao approle %s: role_id not available after %d attempts (Raft propagation timeout)", role, maxAttempts)
}

func (c *httpClient) WrapApproleSecretID(ctx context.Context, role string, wrapTTL time.Duration) (string, int, error) {
	urlPath := c.rawURL("auth/approle/role/" + url.PathEscape(role) + "/secret-id")
	// Need to inject X-Vault-Wrap-TTL header — c.do doesn't support extra
	// headers, so do this one inline.
	req, err := http.NewRequestWithContext(ctx, "POST", urlPath, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("X-Vault-Token", c.authToken)
	req.Header.Set("X-Vault-Wrap-TTL", fmt.Sprintf("%ds", durSeconds(wrapTTL)))
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("bao wrap secret-id failed: %w", err)
	}
	defer resp.Body.Close()
	body := readAll(resp)
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("bao wrap secret-id returned %d: %s", resp.StatusCode, body)
	}
	var wrap struct {
		WrapInfo struct {
			Token string `json:"token"`
			TTL   int    `json:"ttl"`
		} `json:"wrap_info"`
	}
	if err := json.Unmarshal(body, &wrap); err != nil {
		return "", 0, fmt.Errorf("decode wrap_info: %w", err)
	}
	if wrap.WrapInfo.Token == "" {
		return "", 0, fmt.Errorf("bao wrap secret-id missing wrap_info.token")
	}
	return wrap.WrapInfo.Token, wrap.WrapInfo.TTL, nil
}

func (c *httpClient) GetApproleSecretID(ctx context.Context, role string) (string, error) {
	urlPath := c.rawURL("auth/approle/role/" + url.PathEscape(role) + "/secret-id")
	req, err := http.NewRequestWithContext(ctx, "POST", urlPath, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Vault-Token", c.authToken)
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("bao get secret-id failed: %w", err)
	}
	defer resp.Body.Close()
	body := readAll(resp)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("bao get secret-id returned %d: %s", resp.StatusCode, body)
	}
	var result struct {
		Data struct {
			SecretID string `json:"secret_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode secret_id: %w", err)
	}
	if result.Data.SecretID == "" {
		return "", fmt.Errorf("bao get secret-id: missing secret_id in response")
	}
	return result.Data.SecretID, nil
}

func (c *httpClient) EnsureOIDCKey(ctx context.Context, key, algorithm string, rotation, verificationTTL time.Duration, allowClientID string) error {
	// Read existing key.
	body, status, err := c.getJSON(ctx, "identity/oidc/key/"+url.PathEscape(key))
	if err != nil {
		return err
	}
	existingClients := []string{}
	if status == http.StatusOK {
		var resp struct {
			Data struct {
				AllowedClientIDs []string `json:"allowed_client_ids"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &resp); err == nil {
			existingClients = resp.Data.AllowedClientIDs
		}
	} else if status != http.StatusNotFound {
		return fmt.Errorf("bao read oidc key %s returned %d: %s", key, status, body)
	}
	// Merge with desired client_id.
	want := mergeUnique(existingClients, allowClientID)
	payload := map[string]any{
		"algorithm":          algorithm,
		"rotation_period":    fmt.Sprintf("%ds", durSeconds(rotation)),
		"verification_ttl":   fmt.Sprintf("%ds", durSeconds(verificationTTL)),
		"allowed_client_ids": want,
	}
	resBody, st, err := c.postJSON(ctx, "identity/oidc/key/"+url.PathEscape(key), payload)
	if err != nil {
		return err
	}
	if st != http.StatusOK && st != http.StatusNoContent {
		return fmt.Errorf("bao upsert oidc key %s returned %d: %s", key, st, resBody)
	}
	return nil
}

func (c *httpClient) PutOIDCRole(ctx context.Context, role string, opts OIDCRoleOptions) error {
	payload := map[string]any{
		"key":       opts.Key,
		"ttl":       fmt.Sprintf("%ds", durSeconds(opts.TTL)),
		"template":  opts.Template,
		"client_id": opts.ClientID,
	}
	body, status, err := c.postJSON(ctx, "identity/oidc/role/"+url.PathEscape(role), payload)
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusNoContent {
		return fmt.Errorf("bao put oidc role %s returned %d: %s", role, status, body)
	}
	return nil
}

func (c *httpClient) EnsureJWTAuthConfig(ctx context.Context, jwksURL, boundIssuer string) error {
	payload := map[string]any{
		"jwks_url":      jwksURL,
		"bound_issuer":  boundIssuer,
		"default_role":  "",
	}
	body, status, err := c.postJSON(ctx, "auth/jwt/config", payload)
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusNoContent {
		return fmt.Errorf("bao set jwt config returned %d: %s", status, body)
	}
	return nil
}

func (c *httpClient) PutJWTRole(ctx context.Context, role string, opts JWTRoleOptions) error {
	payload := map[string]any{
		"role_type":         opts.RoleType,
		"user_claim":        opts.UserClaim,
		"bound_audiences":   opts.BoundAudiences,
		"bound_claims_type": opts.BoundClaimsType,
		"bound_claims":      opts.BoundClaims,
		"token_policies":    opts.TokenPolicies,
		"token_ttl":         durSeconds(opts.TokenTTL),
		"token_max_ttl":     durSeconds(opts.TokenMaxTTL),
	}
	if len(opts.TokenBoundCIDRs) > 0 {
		payload["token_bound_cidrs"] = opts.TokenBoundCIDRs
	}
	body, status, err := c.postJSON(ctx, "auth/jwt/role/"+url.PathEscape(role), payload)
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusNoContent {
		return fmt.Errorf("bao put jwt role %s returned %d: %s", role, status, body)
	}
	return nil
}

func mergeUnique(existing []string, add string) []string {
	have := map[string]struct{}{}
	for _, s := range existing {
		have[s] = struct{}{}
	}
	have[add] = struct{}{}
	out := make([]string, 0, len(have))
	for s := range have {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func readAll(resp *http.Response) []byte {
	if resp == nil || resp.Body == nil {
		return nil
	}
	const max = 1 << 20
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for len(buf) < max {
		n, err := resp.Body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	return buf
}

// Compile-time check.
var _ OIDCAdmin = (*httpClient)(nil)
