package forgejo

import (
	"context"
	"sync"
	"time"

	"git.konoss.org/kore/schmutz/shared"
)

// CachedClient wraps a Client and caches ListApps results for ttl duration.
// All other Client methods pass through uncached — only ListApps is expensive
// enough to warrant caching (it makes N+1 Forgejo requests, one per repo).
//
// STOPGAP: this cache exists because the hub currently treats Forgejo as a
// live database, issuing one HTTP request per repo on every catalog query.
// The real fix is a dedicated catalog backend with its own persistence layer
// so Forgejo is only consulted on write operations. Remove this cache (and
// the CatalogClient interface) when that backend lands.
//
// Current strategy: stale-on-expiry — once the TTL expires the next caller
// blocks while re-fetching (no background refresh, no singleflight coalescing).
// Adequate for operator-driven catalog updates at current scale.
type CachedClient struct {
	inner *Client
	ttl   time.Duration

	mu        sync.Mutex
	cached    []AppSummary
	expiresAt time.Time
}

// NewCachedClient wraps inner with an in-process ListApps cache with the
// given TTL. Pass a zero or negative TTL to disable caching.
func NewCachedClient(inner *Client, ttl time.Duration) *CachedClient {
	return &CachedClient{inner: inner, ttl: ttl}
}

// ListApps returns the cached catalog if it is still fresh, otherwise
// re-fetches from Forgejo and refreshes the cache.
func (c *CachedClient) ListApps(ctx context.Context) ([]AppSummary, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ttl > 0 && time.Now().Before(c.expiresAt) {
		return c.cached, nil
	}

	apps, err := c.inner.ListApps(ctx)
	if err != nil {
		return nil, err
	}
	c.cached = apps
	c.expiresAt = time.Now().Add(c.ttl)
	return apps, nil
}

// CatalogClient is the interface the hub handler requires. Both *Client and
// *CachedClient satisfy it.
type CatalogClient interface {
	ListApps(ctx context.Context) ([]AppSummary, error)
	GetTango(ctx context.Context, app string) (*shared.Tango, error)
	GetDeployment(ctx context.Context, app, tenant, deployment string) (*DeploymentRecord, error)
	GetSchmutz(ctx context.Context, app, tenant, deployment string) (*shared.Schmutz, error)
	GetCatalogConfig(ctx context.Context) (*CatalogConfig, error)
	UpdateDeployment(ctx context.Context, app, tenant, deployment, commitMsg string, updates map[string]string) error
}

// Verify both types satisfy CatalogClient at compile time.
var _ CatalogClient = (*Client)(nil)
var _ CatalogClient = (*CachedClient)(nil)

// Pass-through methods — everything except ListApps goes directly to the inner
// client where freshness matters (write paths, point-lookups).

func (c *CachedClient) GetTango(ctx context.Context, app string) (*shared.Tango, error) {
	return c.inner.GetTango(ctx, app)
}

func (c *CachedClient) GetDeployment(ctx context.Context, app, tenant, deployment string) (*DeploymentRecord, error) {
	return c.inner.GetDeployment(ctx, app, tenant, deployment)
}

func (c *CachedClient) GetSchmutz(ctx context.Context, app, tenant, deployment string) (*shared.Schmutz, error) {
	return c.inner.GetSchmutz(ctx, app, tenant, deployment)
}

func (c *CachedClient) GetCatalogConfig(ctx context.Context) (*CatalogConfig, error) {
	return c.inner.GetCatalogConfig(ctx)
}

func (c *CachedClient) UpdateDeployment(ctx context.Context, app, tenant, deployment, commitMsg string, updates map[string]string) error {
	return c.inner.UpdateDeployment(ctx, app, tenant, deployment, commitMsg, updates)
}
