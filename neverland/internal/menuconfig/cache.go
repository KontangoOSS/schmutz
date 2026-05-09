package menuconfig

import (
	"context"
	"log"
	"sync"
	"time"
)

// Fetcher is the interface both the real GitFetcher and test stubs satisfy.
type Fetcher interface {
	FetchTheme(ctx context.Context) (*ThemeConfig, error)
	FetchEntries(ctx context.Context) (*EntriesConfig, error)
}

// MenuConfigCache is a short-lived in-memory cache over a Fetcher.
// Both Theme() and Entries() are safe for concurrent use.
type MenuConfigCache struct {
	mu        sync.Mutex
	fetcher   Fetcher
	ttl       time.Duration
	theme     *ThemeConfig
	themeAt   time.Time
	entries   *EntriesConfig
	entriesAt time.Time
}

// NewMenuConfigCache wraps fetcher with a TTL cache.
func NewMenuConfigCache(f Fetcher, ttl time.Duration) *MenuConfigCache {
	return &MenuConfigCache{fetcher: f, ttl: ttl}
}

// Theme returns the cached theme, refreshing from git if the TTL has expired.
// On fetch failure, returns the hardcoded fallback without updating the cache
// (so the next call retries git immediately).
func (c *MenuConfigCache) Theme(ctx context.Context) (*ThemeResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.theme != nil && time.Since(c.themeAt) < c.ttl {
		return &ThemeResponse{ThemeConfig: *c.theme, Source: "git", CachedAt: c.themeAt}, nil
	}

	t, err := c.fetcher.FetchTheme(ctx)
	if err != nil {
		log.Printf("[menuconfig] theme fetch failed (serving fallback): %v", err)
		fb := FallbackTheme()
		return &ThemeResponse{ThemeConfig: fb, Source: "fallback"}, nil
	}

	c.theme = t
	c.themeAt = time.Now()
	return &ThemeResponse{ThemeConfig: *t, Source: "git", CachedAt: c.themeAt}, nil
}

// Entries returns the cached OS catalog, refreshing from git if the TTL has expired.
// On fetch failure, returns the hardcoded fallback without updating the cache.
func (c *MenuConfigCache) Entries(ctx context.Context) (*EntriesResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.entries != nil && time.Since(c.entriesAt) < c.ttl {
		return &EntriesResponse{Entries: c.entries.Entries, Source: "git", CachedAt: c.entriesAt}, nil
	}

	e, err := c.fetcher.FetchEntries(ctx)
	if err != nil {
		log.Printf("[menuconfig] entries fetch failed (serving fallback): %v", err)
		fb := FallbackEntries()
		return &EntriesResponse{Entries: fb.Entries, Source: "fallback"}, nil
	}

	c.entries = e
	c.entriesAt = time.Now()
	return &EntriesResponse{Entries: e.Entries, Source: "git", CachedAt: c.entriesAt}, nil
}
