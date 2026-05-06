package baojwt

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// DefaultTokenPath is the standard target for the scoped Bao token.
// Apps read this file to authenticate to bao for their own paths.
const DefaultTokenPath = "/run/bao-token"

// RefreshResult is what the caller logs after a successful refresh.
type RefreshResult struct {
	TokenPath string
	BytesOut  int
	Role      string
}

// Refresh runs one cycle of the bao-jwt flow against the agent's
// persisted config and writes the resulting scoped token atomically to
// tokenPath. It does NOT clobber the existing token on failure — if any
// step errors, the prior token (if present) stays in place.
//
// The method order is fixed:
//  1. approle login → entity-bound app token
//  2. mint OIDC JWT (claims templated from entity metadata)
//  3. exchange JWT for scoped policy token via the jwt auth method
//  4. atomic rename into tokenPath, mode 0640
func Refresh(ctx context.Context, cfg *AgentConfig, tokenPath string) (*RefreshResult, error) {
	if cfg == nil {
		return nil, fmt.Errorf("baojwt: refresh: nil config")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if tokenPath == "" {
		tokenPath = DefaultTokenPath
	}

	cli := NewClient(cfg.BaoAddr, 0)

	// Health check up-front so a flapping bao surfaces a clear error
	// instead of cascading approle/oidc/jwt failures.
	if err := cli.Health(ctx); err != nil {
		return nil, fmt.Errorf("baojwt: refresh: bao unreachable: %w", err)
	}

	appTok, err := cli.ApproleLogin(ctx, cfg.RoleID, cfg.SecretID)
	if err != nil {
		return nil, fmt.Errorf("baojwt: refresh: approle login: %w", err)
	}

	jwt, err := cli.MintOIDCToken(ctx, appTok, cfg.OIDCRole)
	if err != nil {
		return nil, fmt.Errorf("baojwt: refresh: oidc mint: %w", err)
	}

	scoped, err := cli.JWTLogin(ctx, cfg.JWTRole, jwt)
	if err != nil {
		return nil, fmt.Errorf("baojwt: refresh: jwt login: %w", err)
	}

	n, err := writeTokenAtomic(tokenPath, scoped)
	if err != nil {
		return nil, err
	}
	return &RefreshResult{TokenPath: tokenPath, BytesOut: n, Role: cfg.Role}, nil
}

// writeTokenAtomic writes token to path mode 0640 via a temp file +
// rename. The directory must already exist (typically /run, which the
// kernel creates).
func writeTokenAtomic(path, token string) (int, error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".bao-token.*")
	if err != nil {
		return 0, fmt.Errorf("baojwt: tempfile in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if err := tmp.Chmod(0640); err != nil {
		tmp.Close()
		cleanup()
		return 0, fmt.Errorf("baojwt: chmod tempfile: %w", err)
	}
	n, err := tmp.WriteString(token)
	if err != nil {
		tmp.Close()
		cleanup()
		return n, fmt.Errorf("baojwt: write tempfile: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return n, fmt.Errorf("baojwt: close tempfile: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return n, fmt.Errorf("baojwt: rename: %w", err)
	}
	return n, nil
}
