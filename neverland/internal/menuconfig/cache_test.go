package menuconfig_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.konoss.org/kore/schmutz/neverland/internal/menuconfig"
)

// stubFetcher counts calls and returns preset values or errors.
type stubFetcher struct {
	themeCalls   atomic.Int64
	entriesCalls atomic.Int64
	theme        *menuconfig.ThemeConfig
	entries      *menuconfig.EntriesConfig
	themeErr     error
	entriesErr   error
}

func (s *stubFetcher) FetchTheme(_ context.Context) (*menuconfig.ThemeConfig, error) {
	s.themeCalls.Add(1)
	return s.theme, s.themeErr
}

func (s *stubFetcher) FetchEntries(_ context.Context) (*menuconfig.EntriesConfig, error) {
	s.entriesCalls.Add(1)
	return s.entries, s.entriesErr
}

func TestCache_HitAvoidsFetch(t *testing.T) {
	stub := &stubFetcher{
		theme:   &menuconfig.ThemeConfig{Title: "Cached"},
		entries: &menuconfig.EntriesConfig{},
	}
	c := menuconfig.NewMenuConfigCache(stub, 10*time.Minute)

	r1, _ := c.Theme(context.Background())
	r2, _ := c.Theme(context.Background())

	if stub.themeCalls.Load() != 1 {
		t.Fatalf("expected 1 fetch, got %d", stub.themeCalls.Load())
	}
	if r1.Title != "Cached" || r2.Title != "Cached" {
		t.Error("wrong title returned")
	}
	if r1.Source != "git" || r2.Source != "git" {
		t.Errorf("expected source=git, got %q / %q", r1.Source, r2.Source)
	}
}

func TestCache_TTLExpiry(t *testing.T) {
	stub := &stubFetcher{
		theme:   &menuconfig.ThemeConfig{Title: "Fresh"},
		entries: &menuconfig.EntriesConfig{},
	}
	c := menuconfig.NewMenuConfigCache(stub, 50*time.Millisecond)

	c.Theme(context.Background())
	time.Sleep(100 * time.Millisecond)
	c.Theme(context.Background())

	if stub.themeCalls.Load() != 2 {
		t.Fatalf("expected 2 fetches after TTL expiry, got %d", stub.themeCalls.Load())
	}
}

func TestCache_FallbackOnFetchError(t *testing.T) {
	stub := &stubFetcher{
		themeErr:   errors.New("git unreachable"),
		entriesErr: errors.New("git unreachable"),
	}
	c := menuconfig.NewMenuConfigCache(stub, 10*time.Minute)

	r, err := c.Theme(context.Background())
	if err != nil {
		t.Fatalf("expected no error from cache on git failure, got %v", err)
	}
	if r.Source != "fallback" {
		t.Errorf("expected source=fallback, got %q", r.Source)
	}
	if r.Title != "Kontango Boot" {
		t.Errorf("expected fallback title, got %q", r.Title)
	}
}

func TestCache_FallbackDoesNotUpdateTimestamp(t *testing.T) {
	stub := &stubFetcher{
		themeErr: errors.New("git unreachable"),
	}
	c := menuconfig.NewMenuConfigCache(stub, 10*time.Minute)

	c.Theme(context.Background())
	c.Theme(context.Background())

	if stub.themeCalls.Load() != 2 {
		t.Fatalf("expected 2 fetch attempts (fallback never caches), got %d", stub.themeCalls.Load())
	}
}

func TestCache_ConcurrentReadsNoPanic(t *testing.T) {
	stub := &stubFetcher{
		theme:   &menuconfig.ThemeConfig{Title: "Concurrent"},
		entries: &menuconfig.EntriesConfig{},
	}
	c := menuconfig.NewMenuConfigCache(stub, 10*time.Minute)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Theme(context.Background())
			c.Entries(context.Background())
		}()
	}
	wg.Wait()
}
