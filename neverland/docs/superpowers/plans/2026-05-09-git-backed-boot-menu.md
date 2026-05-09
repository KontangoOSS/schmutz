# Git-Backed Dynamic iPXE Boot Menu Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the Kontango iPXE boot menu dynamically configurable from a `kore/boot-config` Forgejo repo — operators push `theme.json` or `entries.json` and the next machine to PXE-boot sees the change within 30 seconds, with no code deploys.

**Architecture:** A new `menuconfig` package provides a 30s-cached git fetcher that reads `theme.json` and `entries.json` from Forgejo's contents API. Two new handler functions expose these as JSON endpoints. The existing `MenuHandler` is extended to call the cache and render a dynamic template instead of the hardcoded one. A hardcoded fallback guarantees boot even when git is unreachable.

**Tech Stack:** Go 1.25, `net/http` (already in use), `encoding/json`, `sync.RWMutex` for cache, `text/template` (already used by menu.go). No new deps. Forgejo token stored in env.

---

## File Structure

### New files in `kore/neverland`

| File | Purpose |
|---|---|
| `internal/menuconfig/types.go` | `ThemeConfig`, `EntryConfig`, `EntriesConfig` structs |
| `internal/menuconfig/fallback.go` | Hardcoded fallback theme + entries as Go consts |
| `internal/menuconfig/fetcher.go` | `GitFetcher` interface + production implementation |
| `internal/menuconfig/fetcher_test.go` | Fetcher tests using `httptest.Server` |
| `internal/menuconfig/cache.go` | `MenuConfigCache` with 30s TTL |
| `internal/menuconfig/cache_test.go` | Cache hit/miss/TTL/failure tests |
| `internal/handlers/menuconfig.go` | `ThemeHandler` + `EntriesHandler` |
| `internal/handlers/menuconfig_test.go` | Handler tests |
| `internal/handlers/menu.go` | **Modified** — inject cache, dynamic template |
| `internal/handlers/menu_test.go` | **Modified** — update for dynamic rendering |
| `internal/config/config.go` | **Modified** — 6 new fields |
| `cmd/neverland/main.go` | **Modified** — wire cache + new handlers |
| `deploy/k8s-deployment.yaml` | **Modified** — 6 new env vars |
| `internal/docs/static/openapi.yaml` | **Modified** — document 2 new endpoints |

### New repo in Forgejo

| File | Purpose |
|---|---|
| `kore/boot-config/theme.json` | Brand colors, logo, title, tagline, timeout |
| `kore/boot-config/entries.json` | OS catalog |
| `kore/boot-config/README.md` | Documentation |

---

## Conventions This Plan Follows

- **TDD:** every code-producing task writes the failing test first, then the implementation
- **Test runner:** `cd ~/git/kore/neverland && go test ./...` from the repo root
- **Branch:** `feat/boot-beacon` (already active in `kore/neverland`)
- **No new deps:** stdlib only for the menuconfig package

---

## Task 0: ✅ DONE — `public/neverland` repo already created

The `public/neverland` repo exists at `https://git.konoss.org/public/neverland` with `theme.json` and `entries.json` already committed. Skip to Task 1.

**Files:**
- Create: `kore/boot-config/theme.json` (via Forgejo API)
- Create: `kore/boot-config/entries.json` (via Forgejo API)
- Create: `kore/boot-config/README.md` (via Forgejo API)

- [ ] **Step 1: Create the repo via Forgejo API**

```bash
FORGEJO_TOKEN=1ed468757024e99d16635647f2ed570791f583e5

curl -s -X POST "http://10.11.30.30:3000/api/v1/orgs/kore/repos" \
  -H "Authorization: token $FORGEJO_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "boot-config",
    "description": "Kontango boot menu configuration — theme and OS catalog",
    "private": false,
    "auto_init": true,
    "default_branch": "main"
  }' | python3 -c "import sys,json; d=json.load(sys.stdin); print('created:', d.get('full_name','ERROR'), d.get('html_url',''))"
```

Expected: `created: kore/boot-config http://10.11.30.30:3000/kore/boot-config`

- [ ] **Step 2: Create `theme.json`**

```bash
CONTENT=$(python3 -c "
import json, base64
theme = {
  'title': 'Kontango Boot',
  'tagline': 'Boot anywhere. Own everything.',
  'logo_ascii': [
    '  |/ _ |\\ | |_ /\\\\  |\\\\ |  /__ _ ',
    '  |\\\\(_)| \\\\|  | /--\\\\ | \\\\| /  (_)'
  ],
  'logo_png_url': '',
  'colors': {
    'background':   '0x0f4c5c',
    'foreground':   '0xffffff',
    'highlight_bg': '0xe6b94d',
    'highlight_fg': '0x1f2024',
    'gap_text':     '0x6b6f7a'
  },
  'timeout_seconds': 30,
  'default_entry': 'hookos'
}
print(base64.b64encode(json.dumps(theme, indent=2).encode()).decode())
")

curl -s -X POST "http://10.11.30.30:3000/api/v1/repos/kore/boot-config/contents/theme.json" \
  -H "Authorization: token $FORGEJO_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"message\":\"feat: initial theme config\",\"content\":\"$CONTENT\"}" \
  | python3 -c "import sys,json; d=json.load(sys.stdin); print('created:', d.get('content',{}).get('name','ERROR'))"
```

Expected: `created: theme.json`

- [ ] **Step 3: Create `entries.json`**

```bash
CONTENT=$(python3 -c "
import json, base64
entries = {
  'entries': [
    {
      'id': 'hookos',
      'label': 'Install Kontango Hook OS',
      'key': '1',
      'type': 'hook',
      'variant': '',
      'arch': ['x86_64', 'i386', 'aarch64'],
      'enabled': True
    },
    {
      'id': 'rescue',
      'label': 'Rescue shell',
      'key': 'r',
      'type': 'hook',
      'variant': 'rescue',
      'arch': ['x86_64', 'i386', 'aarch64'],
      'enabled': True
    },
    {
      'id': 'netbootxyz',
      'label': 'More operating systems (netboot.xyz)',
      'key': 'n',
      'type': 'chain',
      'chain_url': 'https://boot.netboot.xyz',
      'arch': ['x86_64', 'aarch64'],
      'enabled': True
    }
  ]
}
print(base64.b64encode(json.dumps(entries, indent=2).encode()).decode())
")

curl -s -X POST "http://10.11.30.30:3000/api/v1/repos/kore/boot-config/contents/entries.json" \
  -H "Authorization: token $FORGEJO_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"message\":\"feat: initial OS catalog\",\"content\":\"$CONTENT\"}" \
  | python3 -c "import sys,json; d=json.load(sys.stdin); print('created:', d.get('content',{}).get('name','ERROR'))"
```

Expected: `created: entries.json`

- [ ] **Step 4: Verify files readable from public endpoint**

```bash
curl -s "https://git.konoss.org/api/v1/repos/kore/boot-config/contents/theme.json?ref=main" \
  -H "Authorization: token $FORGEJO_TOKEN" \
  | python3 -c "import sys,json,base64; d=json.load(sys.stdin); print(base64.b64decode(d['content']).decode()[:100])"
```

Expected: First 100 chars of theme.json content printed.

---

## Task 1: Types

**Files:**
- Create: `~/git/kore/neverland/internal/menuconfig/types.go`

No test for types — they're data structures. Write them, then use them in later tests.

- [ ] **Step 1: Write `types.go`**

Create `~/git/kore/neverland/internal/menuconfig/types.go`:

```go
package menuconfig

import "time"

// ThemeColors holds iPXE colour --rgb hex values.
type ThemeColors struct {
	Background  string `json:"background"`
	Foreground  string `json:"foreground"`
	HighlightBg string `json:"highlight_bg"`
	HighlightFg string `json:"highlight_fg"`
	GapText     string `json:"gap_text"`
}

// ThemeConfig is the parsed shape of theme.json.
type ThemeConfig struct {
	Title          string      `json:"title"`
	Tagline        string      `json:"tagline"`
	LogoASCII      []string    `json:"logo_ascii"`
	LogoPNGURL     string      `json:"logo_png_url"`
	Colors         ThemeColors `json:"colors"`
	TimeoutSeconds int         `json:"timeout_seconds"`
	DefaultEntry   string      `json:"default_entry"`
}

// EntryConfig is one row in entries.json.
type EntryConfig struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Key      string   `json:"key"`
	Type     string   `json:"type"`      // "hook", "url", "chain"
	Variant  string   `json:"variant"`   // hook only — blank means use ${buildarch}
	URL      string   `json:"url"`       // url type only
	ChainURL string   `json:"chain_url"` // chain type only
	Arch     []string `json:"arch"`
	Enabled  bool     `json:"enabled"`
}

// EntriesConfig is the parsed shape of entries.json.
type EntriesConfig struct {
	Entries []EntryConfig `json:"entries"`
}

// ThemeResponse is returned by GET /api/v1/menu/theme.
type ThemeResponse struct {
	ThemeConfig
	Source   string    `json:"source"`    // "git" or "fallback"
	CachedAt time.Time `json:"cached_at"` // zero if fallback
}

// EntriesResponse is returned by GET /api/v1/menu/entries.
type EntriesResponse struct {
	Entries  []EntryConfig `json:"entries"`
	Source   string        `json:"source"`
	CachedAt time.Time     `json:"cached_at"`
}
```

- [ ] **Step 2: Verify it compiles**

```bash
cd ~/git/kore/neverland && go build ./internal/menuconfig/...
```

Expected: no output (clean build).

- [ ] **Step 3: Commit**

```bash
cd ~/git/kore/neverland
git add internal/menuconfig/types.go
git commit -m "feat(menuconfig): add ThemeConfig, EntryConfig, response types"
```

---

## Task 2: Fallback constants

**Files:**
- Create: `~/git/kore/neverland/internal/menuconfig/fallback.go`

- [ ] **Step 1: Write `fallback.go`**

Create `~/git/kore/neverland/internal/menuconfig/fallback.go`:

```go
package menuconfig

// FallbackTheme is served when git is unreachable. Uses Kontango brand colors.
func FallbackTheme() ThemeConfig {
	return ThemeConfig{
		Title:   "Kontango Boot",
		Tagline: "Boot anywhere. Own everything.",
		LogoASCII: []string{
			"  |/ _ |\\ | |_ /\\  |\\ |  /__ _ ",
			"  |\\(_)| \\|  | /--\\ | \\| /  (_)",
		},
		LogoPNGURL: "",
		Colors: ThemeColors{
			Background:  "0x0f4c5c",
			Foreground:  "0xffffff",
			HighlightBg: "0xe6b94d",
			HighlightFg: "0x1f2024",
			GapText:     "0x6b6f7a",
		},
		TimeoutSeconds: 30,
		DefaultEntry:   "hookos",
	}
}

// FallbackEntries is served when git is unreachable.
// Contains exactly: Hook OS install, rescue shell.
// Built-in local/shell entries are appended by the renderer regardless.
func FallbackEntries() EntriesConfig {
	return EntriesConfig{
		Entries: []EntryConfig{
			{
				ID:      "hookos",
				Label:   "Install Kontango Hook OS",
				Key:     "1",
				Type:    "hook",
				Variant: "",
				Arch:    []string{"x86_64", "i386", "aarch64"},
				Enabled: true,
			},
			{
				ID:      "rescue",
				Label:   "Rescue shell",
				Key:     "r",
				Type:    "hook",
				Variant: "rescue",
				Arch:    []string{"x86_64", "i386", "aarch64"},
				Enabled: true,
			},
		},
	}
}
```

- [ ] **Step 2: Verify it compiles**

```bash
cd ~/git/kore/neverland && go build ./internal/menuconfig/...
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
cd ~/git/kore/neverland
git add internal/menuconfig/fallback.go
git commit -m "feat(menuconfig): add hardcoded fallback theme and entries"
```

---

## Task 3: Git fetcher

**Files:**
- Create: `~/git/kore/neverland/internal/menuconfig/fetcher.go`
- Create: `~/git/kore/neverland/internal/menuconfig/fetcher_test.go`

- [ ] **Step 1: Write the failing test**

Create `~/git/kore/neverland/internal/menuconfig/fetcher_test.go`:

```go
package menuconfig_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/KontangoOSS/neverland/internal/menuconfig"
)

func serveContent(t *testing.T, v interface{}) *httptest.Server {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	resp := map[string]string{
		"content": base64.StdEncoding.EncodeToString(raw) + "\n",
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Authorization header is sent
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestFetcher_FetchTheme(t *testing.T) {
	want := menuconfig.ThemeConfig{
		Title:          "Test Boot",
		Tagline:        "Test tagline",
		LogoASCII:      []string{"line1", "line2"},
		LogoPNGURL:     "",
		Colors:         menuconfig.ThemeColors{Background: "0x111111"},
		TimeoutSeconds: 45,
		DefaultEntry:   "hookos",
	}
	srv := serveContent(t, want)
	defer srv.Close()

	cfg := menuconfig.GitFetcherConfig{
		APIBase:   srv.URL,
		Owner:     "kore",
		Repo:      "boot-config",
		Ref:       "main",
		Token:     "test-token",
	}
	f := menuconfig.NewGitFetcher(cfg)

	got, err := f.FetchTheme(context.Background())
	if err != nil {
		t.Fatalf("FetchTheme: %v", err)
	}
	if got.Title != want.Title {
		t.Errorf("Title: got %q, want %q", got.Title, want.Title)
	}
	if got.TimeoutSeconds != want.TimeoutSeconds {
		t.Errorf("TimeoutSeconds: got %d, want %d", got.TimeoutSeconds, want.TimeoutSeconds)
	}
}

func TestFetcher_FetchEntries(t *testing.T) {
	want := menuconfig.EntriesConfig{
		Entries: []menuconfig.EntryConfig{
			{ID: "hookos", Label: "Hook OS", Key: "1", Type: "hook", Arch: []string{"x86_64"}, Enabled: true},
			{ID: "chain1", Label: "Chain target", Key: "c", Type: "chain", ChainURL: "https://example.com", Arch: []string{"x86_64"}, Enabled: true},
		},
	}
	srv := serveContent(t, want)
	defer srv.Close()

	cfg := menuconfig.GitFetcherConfig{
		APIBase: srv.URL,
		Owner:   "kore",
		Repo:    "boot-config",
		Ref:     "main",
		Token:   "test-token",
	}
	f := menuconfig.NewGitFetcher(cfg)

	got, err := f.FetchEntries(context.Background())
	if err != nil {
		t.Fatalf("FetchEntries: %v", err)
	}
	if len(got.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got.Entries))
	}
	if got.Entries[0].ID != "hookos" {
		t.Errorf("first entry ID: got %q, want %q", got.Entries[0].ID, "hookos")
	}
}

func TestFetcher_ReturnsErrorOn404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"not found"}`))
	}))
	defer srv.Close()

	f := menuconfig.NewGitFetcher(menuconfig.GitFetcherConfig{
		APIBase: srv.URL, Owner: "k", Repo: "r", Ref: "main", Token: "t",
	})

	_, err := f.FetchTheme(context.Background())
	if err == nil {
		t.Fatal("expected error on 404, got nil")
	}
}

func TestFetcher_URLConstruction(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		// Return valid but minimal content
		raw, _ := json.Marshal(menuconfig.ThemeConfig{Title: "t"})
		resp := map[string]string{"content": base64.StdEncoding.EncodeToString(raw) + "\n"}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	f := menuconfig.NewGitFetcher(menuconfig.GitFetcherConfig{
		APIBase: srv.URL,
		Owner:   "myorg",
		Repo:    "myrepo",
		Ref:     "main",
		Token:   "tok",
	})

	f.FetchTheme(context.Background())
	want := "/repos/myorg/myrepo/contents/theme.json"
	if gotPath != want {
		t.Errorf("URL path: got %q, want %q", gotPath, want)
	}
}
```

- [ ] **Step 2: Run to verify fails**

```bash
cd ~/git/kore/neverland && go test ./internal/menuconfig/... -v 2>&1 | head -20
```

Expected: FAIL — `menuconfig.GitFetcherConfig`, `menuconfig.NewGitFetcher` undefined.

- [ ] **Step 3: Write `fetcher.go`**

Create `~/git/kore/neverland/internal/menuconfig/fetcher.go`:

```go
package menuconfig

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// GitFetcherConfig holds connection settings for the git host.
type GitFetcherConfig struct {
	// APIBase is the root of the contents API.
	// Forgejo: "https://git.konoss.org/api/v1"
	// GitHub:  "https://api.github.com"
	APIBase string
	Owner   string
	Repo    string
	Ref     string
	Token   string
}

// GitFetcher fetches theme.json and entries.json from a git host.
type GitFetcher struct {
	cfg    GitFetcherConfig
	client *http.Client
}

// NewGitFetcher constructs a GitFetcher with a 10-second HTTP timeout.
func NewGitFetcher(cfg GitFetcherConfig) *GitFetcher {
	return &GitFetcher{
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// FetchTheme retrieves and parses theme.json.
func (f *GitFetcher) FetchTheme(ctx context.Context) (*ThemeConfig, error) {
	var cfg ThemeConfig
	if err := f.fetch(ctx, "theme.json", &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// FetchEntries retrieves and parses entries.json.
func (f *GitFetcher) FetchEntries(ctx context.Context) (*EntriesConfig, error) {
	var cfg EntriesConfig
	if err := f.fetch(ctx, "entries.json", &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// fetch calls the contents API for a single file, base64-decodes the response,
// and JSON-unmarshals into v.
func (f *GitFetcher) fetch(ctx context.Context, filename string, v interface{}) error {
	// Both Forgejo (/api/v1/repos/...) and GitHub (/repos/...) share the path
	// after the API base. The caller sets APIBase to include /api/v1 for Forgejo
	// and omits it for GitHub — so the relative path is always /repos/...
	base := strings.TrimRight(f.cfg.APIBase, "/")
	url := fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s",
		base, f.cfg.Owner, f.cfg.Repo, filename, f.cfg.Ref)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if f.cfg.Token != "" {
		req.Header.Set("Authorization", "token "+f.cfg.Token)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", filename, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("fetch %s: HTTP %d: %s", filename, resp.StatusCode, body)
	}

	// The contents API wraps the file in a JSON envelope with a base64 "content" field.
	var envelope struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("decode envelope for %s: %w", filename, err)
	}

	// The content field may contain newlines; strip them before decoding.
	raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(envelope.Content, "\n", ""))
	if err != nil {
		return fmt.Errorf("base64 decode %s: %w", filename, err)
	}

	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("json unmarshal %s: %w", filename, err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests**

```bash
cd ~/git/kore/neverland && go test ./internal/menuconfig/... -v 2>&1 | tail -20
```

Expected: 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
cd ~/git/kore/neverland
git add internal/menuconfig/fetcher.go internal/menuconfig/fetcher_test.go
git commit -m "feat(menuconfig): add GitFetcher with Forgejo/GitHub contents API"
```

---

## Task 4: Cache

**Files:**
- Create: `~/git/kore/neverland/internal/menuconfig/cache.go`
- Create: `~/git/kore/neverland/internal/menuconfig/cache_test.go`

- [ ] **Step 1: Write the failing test**

Create `~/git/kore/neverland/internal/menuconfig/cache_test.go`:

```go
package menuconfig_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KontangoOSS/neverland/internal/menuconfig"
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
	// If git fails, the cache timestamp must NOT be updated so the next request retries.
	stub := &stubFetcher{
		themeErr: errors.New("git unreachable"),
	}
	c := menuconfig.NewMenuConfigCache(stub, 10*time.Minute)

	c.Theme(context.Background())
	c.Theme(context.Background())

	// Both calls should have tried git (fallback never caches)
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
```

- [ ] **Step 2: Run to verify fails**

```bash
cd ~/git/kore/neverland && go test ./internal/menuconfig/... -run TestCache -v 2>&1 | head -15
```

Expected: FAIL — `menuconfig.NewMenuConfigCache` undefined.

- [ ] **Step 3: Write `cache.go`**

Create `~/git/kore/neverland/internal/menuconfig/cache.go`:

```go
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
	mu          sync.Mutex
	fetcher     Fetcher
	ttl         time.Duration

	theme       *ThemeConfig
	themeAt     time.Time

	entries     *EntriesConfig
	entriesAt   time.Time
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
```

- [ ] **Step 4: Run all menuconfig tests**

```bash
cd ~/git/kore/neverland && go test ./internal/menuconfig/... -v 2>&1 | tail -20
```

Expected: all 8 tests PASS (4 fetcher + 5 cache).

- [ ] **Step 5: Commit**

```bash
cd ~/git/kore/neverland
git add internal/menuconfig/cache.go internal/menuconfig/cache_test.go
git commit -m "feat(menuconfig): add 30s in-memory cache with fallback on git failure"
```

---

## Task 5: Theme and Entries HTTP handlers

**Files:**
- Create: `~/git/kore/neverland/internal/handlers/menuconfig.go`
- Create: `~/git/kore/neverland/internal/handlers/menuconfig_test.go`

- [ ] **Step 1: Write the failing test**

Create `~/git/kore/neverland/internal/handlers/menuconfig_test.go`:

```go
package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/KontangoOSS/neverland/internal/handlers"
	"github.com/KontangoOSS/neverland/internal/menuconfig"
)

// fakeMenuCache satisfies handlers.MenuCache for unit tests.
type fakeMenuCache struct {
	theme   *menuconfig.ThemeResponse
	entries *menuconfig.EntriesResponse
}

func (f *fakeMenuCache) Theme(_ context.Context) (*menuconfig.ThemeResponse, error) {
	return f.theme, nil
}
func (f *fakeMenuCache) Entries(_ context.Context) (*menuconfig.EntriesResponse, error) {
	return f.entries, nil
}

func TestThemeHandler_ReturnsJSON(t *testing.T) {
	cache := &fakeMenuCache{
		theme: &menuconfig.ThemeResponse{
			ThemeConfig: menuconfig.ThemeConfig{Title: "Test Boot", TimeoutSeconds: 30},
			Source:      "git",
			CachedAt:    time.Now(),
		},
	}
	h := handlers.NewMenuConfigHandler(cache)

	req := httptest.NewRequest("GET", "/api/v1/menu/theme", nil)
	w := httptest.NewRecorder()
	h.GetTheme(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp menuconfig.ThemeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Title != "Test Boot" {
		t.Errorf("title: got %q, want %q", resp.Title, "Test Boot")
	}
	if resp.Source != "git" {
		t.Errorf("source: got %q, want %q", resp.Source, "git")
	}
	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("content-type: got %q, want application/json", ct)
	}
}

func TestEntriesHandler_ReturnsAll(t *testing.T) {
	cache := &fakeMenuCache{
		entries: &menuconfig.EntriesResponse{
			Entries: []menuconfig.EntryConfig{
				{ID: "hookos", Arch: []string{"x86_64", "aarch64"}, Enabled: true, Key: "1", Type: "hook"},
				{ID: "ubuntu", Arch: []string{"x86_64"}, Enabled: false, Key: "2", Type: "url"},
			},
			Source: "git",
		},
	}
	h := handlers.NewMenuConfigHandler(cache)

	req := httptest.NewRequest("GET", "/api/v1/menu/entries", nil)
	w := httptest.NewRecorder()
	h.GetEntries(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp menuconfig.EntriesResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Entries) != 2 {
		t.Fatalf("expected 2 entries unfiltered, got %d", len(resp.Entries))
	}
}

func TestEntriesHandler_FiltersByArch(t *testing.T) {
	cache := &fakeMenuCache{
		entries: &menuconfig.EntriesResponse{
			Entries: []menuconfig.EntryConfig{
				{ID: "x86only", Arch: []string{"x86_64"}, Enabled: true, Key: "1", Type: "hook"},
				{ID: "both", Arch: []string{"x86_64", "aarch64"}, Enabled: true, Key: "2", Type: "hook"},
				{ID: "armonly", Arch: []string{"aarch64"}, Enabled: true, Key: "3", Type: "hook"},
			},
			Source: "git",
		},
	}
	h := handlers.NewMenuConfigHandler(cache)

	req := httptest.NewRequest("GET", "/api/v1/menu/entries?arch=x86_64", nil)
	w := httptest.NewRecorder()
	h.GetEntries(w, req)

	var resp menuconfig.EntriesResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Entries) != 2 {
		t.Fatalf("expected 2 entries for x86_64 (x86only + both), got %d", len(resp.Entries))
	}
	for _, e := range resp.Entries {
		if e.ID == "armonly" {
			t.Error("armonly should not appear in x86_64 filter")
		}
	}
}

func TestEntriesHandler_FiltersByArchAlias(t *testing.T) {
	cache := &fakeMenuCache{
		entries: &menuconfig.EntriesResponse{
			Entries: []menuconfig.EntryConfig{
				{ID: "hookos", Arch: []string{"x86_64"}, Enabled: true, Key: "1", Type: "hook"},
			},
			Source: "git",
		},
	}
	h := handlers.NewMenuConfigHandler(cache)

	// i386 and amd64 should both resolve to x86_64
	for _, alias := range []string{"i386", "amd64"} {
		req := httptest.NewRequest("GET", "/api/v1/menu/entries?arch="+alias, nil)
		w := httptest.NewRecorder()
		h.GetEntries(w, req)
		var resp menuconfig.EntriesResponse
		json.Unmarshal(w.Body.Bytes(), &resp)
		if len(resp.Entries) != 1 {
			t.Errorf("arch alias %q: expected 1 entry, got %d", alias, len(resp.Entries))
		}
	}
}

func TestEntriesHandler_DisabledEntriesExcluded(t *testing.T) {
	cache := &fakeMenuCache{
		entries: &menuconfig.EntriesResponse{
			Entries: []menuconfig.EntryConfig{
				{ID: "enabled",  Arch: []string{"x86_64"}, Enabled: true,  Key: "1", Type: "hook"},
				{ID: "disabled", Arch: []string{"x86_64"}, Enabled: false, Key: "2", Type: "hook"},
			},
			Source: "git",
		},
	}
	h := handlers.NewMenuConfigHandler(cache)

	req := httptest.NewRequest("GET", "/api/v1/menu/entries?arch=x86_64", nil)
	w := httptest.NewRecorder()
	h.GetEntries(w, req)
	var resp menuconfig.EntriesResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Entries) != 1 {
		t.Fatalf("expected 1 (disabled excluded), got %d", len(resp.Entries))
	}
	if resp.Entries[0].ID != "enabled" {
		t.Errorf("expected enabled entry, got %q", resp.Entries[0].ID)
	}
}
```

- [ ] **Step 2: Run to verify fails**

```bash
cd ~/git/kore/neverland && go test ./internal/handlers/... -run TestThemeHandler -v 2>&1 | head -10
```

Expected: FAIL — `handlers.MenuCache`, `handlers.NewMenuConfigHandler` undefined.

- [ ] **Step 3: Write `menuconfig.go`**

Create `~/git/kore/neverland/internal/handlers/menuconfig.go`:

```go
package handlers

import (
	"context"
	"net/http"

	"github.com/KontangoOSS/neverland/internal/menuconfig"
	"github.com/KontangoOSS/neverland/internal/respond"
)

// MenuCache is the interface MenuConfigCache satisfies.
type MenuCache interface {
	Theme(ctx context.Context) (*menuconfig.ThemeResponse, error)
	Entries(ctx context.Context) (*menuconfig.EntriesResponse, error)
}

// MenuConfigHandler exposes the theme and entries cache as HTTP JSON endpoints.
type MenuConfigHandler struct {
	cache MenuCache
}

// NewMenuConfigHandler constructs a MenuConfigHandler.
func NewMenuConfigHandler(cache MenuCache) *MenuConfigHandler {
	return &MenuConfigHandler{cache: cache}
}

// GetTheme handles GET /api/v1/menu/theme.
func (h *MenuConfigHandler) GetTheme(w http.ResponseWriter, r *http.Request) {
	theme, err := h.cache.Theme(r.Context())
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "failed to load theme")
		return
	}
	respond.JSON(w, http.StatusOK, theme)
}

// GetEntries handles GET /api/v1/menu/entries.
// When ?arch= is provided, it filters entries to those valid for that arch
// (after alias resolution). Disabled entries are always excluded when arch
// filtering is active. Without ?arch=, all entries (including disabled) are returned
// for operator inspection.
func (h *MenuConfigHandler) GetEntries(w http.ResponseWriter, r *http.Request) {
	entries, err := h.cache.Entries(r.Context())
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "failed to load entries")
		return
	}

	arch := r.URL.Query().Get("arch")
	if arch == "" {
		respond.JSON(w, http.StatusOK, entries)
		return
	}

	// Resolve arch aliases (same map as hook handler).
	resolved := resolveArchAlias(arch)

	filtered := make([]menuconfig.EntryConfig, 0, len(entries.Entries))
	for _, e := range entries.Entries {
		if !e.Enabled {
			continue
		}
		for _, a := range e.Arch {
			if a == resolved {
				filtered = append(filtered, e)
				break
			}
		}
	}

	respond.JSON(w, http.StatusOK, &menuconfig.EntriesResponse{
		Entries:  filtered,
		Source:   entries.Source,
		CachedAt: entries.CachedAt,
	})
}

// resolveArchAlias maps iPXE buildarch values to canonical arch names.
// Matches the same map used in hook.go for consistency.
var archAliasMap = map[string]string{
	"i386":  "x86_64",
	"amd64": "x86_64",
	"arm64": "aarch64",
}

func resolveArchAlias(arch string) string {
	if a, ok := archAliasMap[arch]; ok {
		return a
	}
	return arch
}
```

- [ ] **Step 4: Run all handler tests**

```bash
cd ~/git/kore/neverland && go test ./internal/handlers/... -v 2>&1 | tail -25
```

Expected: all handler tests PASS including the 5 new ones.

- [ ] **Step 5: Commit**

```bash
cd ~/git/kore/neverland
git add internal/handlers/menuconfig.go internal/handlers/menuconfig_test.go
git commit -m "feat(menuconfig): add ThemeHandler + EntriesHandler with arch filtering"
```

---

## Task 6: Extend Config

**Files:**
- Modify: `~/git/kore/neverland/internal/config/config.go`

- [ ] **Step 1: Add the 6 new fields to Config**

Replace the contents of `~/git/kore/neverland/internal/config/config.go`:

```go
package config

import (
	"os"
	"time"
)

type Config struct {
	ListenAddr     string
	TinkNamespace  string
	Kubeconfig     string
	ArtifactsPath  string
	NginxURL       string
	SmeeDeployment string
	EnrollURL      string
	BootBaseURL    string
	DownloadsPath  string
	PostgresDSN    string

	// Boot menu git config
	BootConfigAPIBase string
	BootConfigOwner   string
	BootConfigRepo    string
	BootConfigRef     string
	BootConfigToken   string
	BootConfigCacheTTL time.Duration
}

func Load() Config {
	ttl := 30 * time.Second
	if v := os.Getenv("BOOT_CONFIG_CACHE_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			ttl = d
		}
	}

	return Config{
		ListenAddr:     envOr("LISTEN_ADDR", "0.0.0.0:8091"),
		TinkNamespace:  envOr("TINK_NAMESPACE", "tink-system"),
		Kubeconfig:     envOr("KUBECONFIG", ""),
		ArtifactsPath:  envOr("ARTIFACTS_PATH", "/artifacts"),
		NginxURL:       envOr("NGINX_URL", "http://tink-stack.tink-system.svc.cluster.local:8080"),
		SmeeDeployment: envOr("SMEE_DEPLOYMENT", "smee"),
		EnrollURL:      envOr("ENROLL_URL", "https://join.kontango.net"),
		BootBaseURL:    envOr("BOOT_BASE_URL", "https://boot.kontango.net"),
		DownloadsPath:  envOr("DOWNLOADS_PATH", "/downloads"),
		PostgresDSN:    envOr("POSTGRES_DSN", ""),

		BootConfigAPIBase:  envOr("BOOT_CONFIG_API_BASE", "https://git.konoss.org/api/v1"),
		BootConfigOwner:    envOr("BOOT_CONFIG_REPO_OWNER", "kore"),
		BootConfigRepo:     envOr("BOOT_CONFIG_REPO_NAME", "boot-config"),
		BootConfigRef:      envOr("BOOT_CONFIG_REPO_REF", "main"),
		BootConfigToken:    envOr("BOOT_CONFIG_TOKEN", ""),
		BootConfigCacheTTL: ttl,
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
```

- [ ] **Step 2: Build to verify**

```bash
cd ~/git/kore/neverland && go build ./... 2>&1
```

Expected: clean.

- [ ] **Step 3: Commit**

```bash
cd ~/git/kore/neverland
git add internal/config/config.go
git commit -m "feat(config): add BOOT_CONFIG_* env vars for git-backed menu"
```

---

## Task 7: Dynamic menu template

Replace the hardcoded `menuTemplate` in `menu.go` with a dynamic renderer that reads from the cache.

**Files:**
- Modify: `~/git/kore/neverland/internal/handlers/menu.go`
- Modify: `~/git/kore/neverland/internal/handlers/menu_test.go`

- [ ] **Step 1: Update the failing tests first**

The existing tests check for specific hardcoded strings that will change. Update `~/git/kore/neverland/internal/handlers/menu_test.go`:

```go
package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/KontangoOSS/neverland/internal/handlers"
	"github.com/KontangoOSS/neverland/internal/menuconfig"
)

func defaultFakeCache() *fakeMenuCache {
	return &fakeMenuCache{
		theme: &menuconfig.ThemeResponse{
			ThemeConfig: menuconfig.ThemeConfig{
				Title:          "Kontango Boot",
				Tagline:        "Boot anywhere.",
				LogoASCII:      []string{"  logo line 1", "  logo line 2"},
				Colors:         menuconfig.ThemeColors{Background: "0x0f4c5c", Foreground: "0xffffff", HighlightBg: "0xe6b94d", HighlightFg: "0x1f2024"},
				TimeoutSeconds: 30,
				DefaultEntry:   "hookos",
			},
			Source:   "git",
			CachedAt: time.Now(),
		},
		entries: &menuconfig.EntriesResponse{
			Entries: []menuconfig.EntryConfig{
				{ID: "hookos", Label: "Install Kontango Hook OS", Key: "1", Type: "hook", Arch: []string{"x86_64", "i386", "aarch64"}, Enabled: true},
				{ID: "rescue", Label: "Rescue shell", Key: "r", Type: "hook", Variant: "rescue", Arch: []string{"x86_64", "i386", "aarch64"}, Enabled: true},
			},
			Source: "git",
		},
	}
}

func TestMenu_RendersIPXEScript(t *testing.T) {
	h := handlers.NewMenuHandler("https://boot.kontango.net", defaultFakeCache())
	req := httptest.NewRequest("GET", "/api/v1/menu.ipxe?arch=x86_64", nil)
	w := httptest.NewRecorder()
	h.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.HasPrefix(body, "#!ipxe") {
		t.Fatalf("expected iPXE shebang, got: %.30s", body)
	}
	if !strings.Contains(body, "https://boot.kontango.net/api/v1/beacon") {
		t.Fatal("expected beacon URL")
	}
	if !strings.Contains(body, "${buildarch}") {
		t.Fatal("expected iPXE-side ${buildarch} placeholder")
	}
	if !strings.Contains(body, "menu Kontango Boot") {
		t.Fatal("expected menu title from theme")
	}
	if !strings.Contains(body, "logo line 1") {
		t.Fatal("expected logo line from theme")
	}
	if !strings.Contains(body, "Install Kontango Hook OS") {
		t.Fatal("expected entry label from entries")
	}
	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("expected text/plain, got %q", ct)
	}
}

func TestMenu_HasOfflineFallback(t *testing.T) {
	h := handlers.NewMenuHandler("https://boot.kontango.net", defaultFakeCache())
	req := httptest.NewRequest("GET", "/api/v1/menu.ipxe", nil)
	w := httptest.NewRecorder()
	h.Handle(w, req)
	body := w.Body.String()
	if !strings.Contains(body, ":offline_menu") {
		t.Fatal("offline_menu label missing")
	}
	if !strings.Contains(body, ":retry") {
		t.Fatal("offline retry label missing")
	}
}

func TestMenu_HostHeaderRespected(t *testing.T) {
	h := handlers.NewMenuHandler("", defaultFakeCache())
	req := httptest.NewRequest("GET", "/api/v1/menu.ipxe", nil)
	req.Host = "boot.example.com"
	req.TLS = nil
	w := httptest.NewRecorder()
	h.Handle(w, req)
	body := w.Body.String()
	if !strings.Contains(body, "https://boot.example.com") {
		t.Fatalf("expected host fallback, got: %s", body[:200])
	}
}

func TestMenu_ChainEntryRendered(t *testing.T) {
	cache := &fakeMenuCache{
		theme: defaultFakeCache().theme,
		entries: &menuconfig.EntriesResponse{
			Entries: []menuconfig.EntryConfig{
				{ID: "netbootxyz", Label: "netboot.xyz", Key: "n", Type: "chain",
					ChainURL: "https://boot.netboot.xyz", Arch: []string{"x86_64"}, Enabled: true},
			},
			Source: "git",
		},
	}
	h := handlers.NewMenuHandler("https://boot.kontango.net", cache)
	req := httptest.NewRequest("GET", "/api/v1/menu.ipxe?arch=x86_64", nil)
	w := httptest.NewRecorder()
	h.Handle(w, req)
	body := w.Body.String()
	if !strings.Contains(body, "chain --autofree https://boot.netboot.xyz") {
		t.Errorf("expected chain entry, got:\n%s", body)
	}
}

func TestMenu_URLEntryRendered(t *testing.T) {
	cache := &fakeMenuCache{
		theme: defaultFakeCache().theme,
		entries: &menuconfig.EntriesResponse{
			Entries: []menuconfig.EntryConfig{
				{ID: "ubuntu2404", Label: "Ubuntu 24.04", Key: "2", Type: "url",
					URL: "https://releases.ubuntu.com/ubuntu.iso", Arch: []string{"x86_64"}, Enabled: true},
			},
			Source: "git",
		},
	}
	h := handlers.NewMenuHandler("https://boot.kontango.net", cache)
	req := httptest.NewRequest("GET", "/api/v1/menu.ipxe?arch=x86_64", nil)
	w := httptest.NewRecorder()
	h.Handle(w, req)
	body := w.Body.String()
	if !strings.Contains(body, "ubuntu2404") {
		t.Errorf("expected url entry label, body:\n%s", body)
	}
}

func TestMenu_FallbackUsedWhenCacheFails(t *testing.T) {
	// Use a nil cache — NewMenuHandler must still render a valid fallback menu
	h := handlers.NewMenuHandler("https://boot.kontango.net", nil)
	req := httptest.NewRequest("GET", "/api/v1/menu.ipxe", nil)
	w := httptest.NewRecorder()
	h.Handle(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 even with nil cache, got %d", w.Code)
	}
	if !strings.HasPrefix(w.Body.String(), "#!ipxe") {
		t.Fatal("expected valid iPXE with nil cache")
	}
}
```

- [ ] **Step 2: Run to verify tests fail**

```bash
cd ~/git/kore/neverland && go test ./internal/handlers/... -run TestMenu -v 2>&1 | head -20
```

Expected: FAIL — `NewMenuHandler` signature changed (takes cache now).

- [ ] **Step 3: Rewrite `menu.go`**

Replace `~/git/kore/neverland/internal/handlers/menu.go`:

```go
package handlers

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"text/template"

	"github.com/KontangoOSS/neverland/internal/menuconfig"
)

// MenuHandler serves the iPXE chainload menu, dynamically composed from the
// MenuCache (git-backed theme + entries) with a hardcoded fallback.
type MenuHandler struct {
	bootBaseURL string
	cache       MenuCache // nil → always use fallback
}

// NewMenuHandler constructs a MenuHandler.
// If bootBaseURL is empty, the request's Host header is used.
// If cache is nil, the hardcoded fallback is always used.
func NewMenuHandler(bootBaseURL string, cache MenuCache) *MenuHandler {
	return &MenuHandler{
		bootBaseURL: strings.TrimRight(bootBaseURL, "/"),
		cache:       cache,
	}
}

// Handle is GET /api/v1/menu.ipxe.
func (h *MenuHandler) Handle(w http.ResponseWriter, r *http.Request) {
	base := h.bootBaseURL
	if base == "" {
		scheme := "https"
		if proto := r.Header.Get("X-Forwarded-Proto"); proto == "http" {
			scheme = "http"
		} else if r.TLS != nil || proto == "https" {
			scheme = "https"
		}
		base = scheme + "://" + r.Host
	}

	// Resolve arch from query param (iPXE sends ?arch=${buildarch} via the beacon flow).
	arch := resolveArchAlias(r.URL.Query().Get("arch"))

	// Load theme and entries (fallback if cache nil or git unavailable).
	theme := menuconfig.FallbackTheme()
	entries := menuconfig.FallbackEntries().Entries

	if h.cache != nil {
		if t, err := h.cache.Theme(r.Context()); err == nil {
			theme = t.ThemeConfig
		}
		if e, err := h.cache.Entries(r.Context()); err == nil {
			// Filter: enabled only + arch match (if arch provided).
			for _, entry := range e.Entries {
				if !entry.Enabled {
					continue
				}
				if arch == "" {
					entries = append(entries[:0:0], e.Entries...)
					break
				}
				for _, a := range entry.Arch {
					if a == arch {
						entries = append(entries, entry)
						break
					}
				}
			}
			// If arch was set, entries was rebuilt above; otherwise use unfiltered.
			if arch == "" {
				entries = e.Entries
			}
		}
	}

	data := menuTemplateData{
		Base:    base,
		Theme:   theme,
		Entries: entries,
	}

	var buf bytes.Buffer
	if err := dynamicMenuTemplate.Execute(&buf, data); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(buf.Bytes())
}

type menuTemplateData struct {
	Base    string
	Theme   menuconfig.ThemeConfig
	Entries []menuconfig.EntryConfig
}

// entryIPXE returns the iPXE script lines for a single entry's label section.
func entryIPXE(base string, e menuconfig.EntryConfig) string {
	switch e.Type {
	case "hook":
		variant := e.Variant
		if variant == "" || variant == "rescue" {
			if variant == "rescue" {
				return fmt.Sprintf("chain %s/api/v1/hook/${buildarch}/hook.ipxe?rescue=1", base)
			}
			return fmt.Sprintf("chain %s/api/v1/hook/${buildarch}/hook.ipxe?session=${kontango_session_id}&claim=1", base)
		}
		return fmt.Sprintf("chain %s/api/v1/hook/%s/hook.ipxe?session=${kontango_session_id}&claim=1", base, variant)
	case "url":
		name := e.ID + ".raw.gz"
		return fmt.Sprintf("chain %s/api/v1/artifacts/%s || goto menu", base, name)
	case "chain":
		return fmt.Sprintf("chain --autofree %s || goto menu", e.ChainURL)
	default:
		return fmt.Sprintf("echo Unknown entry type %s && goto menu", e.Type)
	}
}

var dynamicMenuTemplate = template.Must(template.New("menu").Funcs(template.FuncMap{
	"entryIPXE": entryIPXE,
}).Parse(`#!ipxe

# Defensive: ensure DHCP if caller didn't.
isset ${net0/ip} || dhcp net0 || goto failed

# Apply theme colors
colour --rgb {{.Theme.Colors.Background}} 4
colour --rgb {{.Theme.Colors.Foreground}} 7
colour --rgb {{.Theme.Colors.HighlightBg}} 2
colour --rgb {{.Theme.Colors.HighlightFg}} 3
cpair --foreground 7 --background 4 0
cpair --foreground 3 --background 2 1

# Logo
{{range .Theme.LogoASCII}}echo {{.}}
{{end}}echo {{.Theme.Tagline}}
echo

# Beacon: sets ${kontango_session_id} and ${kontango_skip_claim}
set beacon_url {{.Base}}/api/v1/beacon
imgfetch --name beacon ${beacon_url}?ipxe=1&mac=${net0/mac}&ip=${net0/ip}&arch=${buildarch}&platform=${platform}&userclass=${user-class} || goto offline_menu
imgexec beacon || goto offline_menu

iseq ${kontango_skip_claim} 1 && goto boot_anonymous || goto menu

:menu
menu {{.Theme.Title}}
item --gap
{{range .Entries}}item {{.Key}} {{.Label}}
{{end}}item --gap
item l Boot from local disk
item s Drop to iPXE shell
item --gap
item --gap Network: ${net0/ip} / MAC: ${net0/mac}
item --gap Session: ${kontango_session_id}

choose --default {{.Theme.DefaultEntry}} --timeout {{multiply .Theme.TimeoutSeconds 1000}} target && goto ${target} || goto l

{{range .Entries}}:{{.Key}}
{{call $.EntryIPXE $.Base .}}

{{end}}
:l
sanboot --no-describe || exit 1

:s
shell

:boot_anonymous
echo Recognised machine -- booting without claim screen.
chain {{.Base}}/api/v1/hook/${buildarch}/hook.ipxe?session=${kontango_session_id}&claim=0

:offline_menu
menu {{.Theme.Title}} Offline
item --gap
item r  Rescue shell
item l  Boot from local disk
item s  iPXE shell
item retry Retry beacon
item --gap
item --gap Beacon unavailable.
item --gap Network: ${net0/ip} / MAC: ${net0/mac}

choose --default retry --timeout 30000 target && goto ${target} || goto l

:retry
imgfetch --name beacon ${beacon_url}?ipxe=1&mac=${net0/mac}&ip=${net0/ip}&arch=${buildarch}&platform=${platform}&userclass=${user-class} || goto offline_menu
imgexec beacon || goto offline_menu
goto menu

:failed
echo Network failed. Rebooting in 10 seconds.
sleep 10
reboot
`))
```

Wait — the template above has `{{call $.EntryIPXE $.Base .}}` and `{{multiply}}` which need to be in FuncMap. Let me simplify by precomputing the entry lines in the handler and passing them as a slice. Replace the template approach with a simpler pre-rendered entries list:

Replace `menu.go` with this cleaner version instead:

```go
package handlers

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"text/template"

	"github.com/KontangoOSS/neverland/internal/menuconfig"
)

// MenuHandler serves the iPXE chainload menu.
type MenuHandler struct {
	bootBaseURL string
	cache       MenuCache
}

// NewMenuHandler constructs a MenuHandler.
// cache may be nil — hardcoded fallback is always used when nil or on git failure.
func NewMenuHandler(bootBaseURL string, cache MenuCache) *MenuHandler {
	return &MenuHandler{
		bootBaseURL: strings.TrimRight(bootBaseURL, "/"),
		cache:       cache,
	}
}

// renderedEntry is one entry pre-computed for the template.
type renderedEntry struct {
	Key        string
	Label      string
	BootScript string // the iPXE lines that run when user selects this entry
}

// Handle is GET /api/v1/menu.ipxe.
func (h *MenuHandler) Handle(w http.ResponseWriter, r *http.Request) {
	base := h.bootBaseURL
	if base == "" {
		scheme := "https"
		if proto := r.Header.Get("X-Forwarded-Proto"); proto == "http" {
			scheme = "http"
		} else if r.TLS != nil || proto == "https" {
			scheme = "https"
		}
		base = scheme + "://" + r.Host
	}

	arch := resolveArchAlias(r.URL.Query().Get("arch"))
	theme, entries := h.loadConfig(r, arch)

	// Pre-render entries into (key, label, bootscript) for the template.
	rendered := make([]renderedEntry, 0, len(entries))
	for _, e := range entries {
		rendered = append(rendered, renderedEntry{
			Key:        e.Key,
			Label:      e.Label,
			BootScript: buildEntryScript(base, e),
		})
	}

	data := struct {
		Base      string
		Theme     menuconfig.ThemeConfig
		Entries   []renderedEntry
		TimeoutMs int
	}{
		Base:      base,
		Theme:     theme,
		Entries:   rendered,
		TimeoutMs: theme.TimeoutSeconds * 1000,
	}

	var buf bytes.Buffer
	if err := dynamicMenuTemplate.Execute(&buf, data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(buf.Bytes())
}

// loadConfig returns theme and filtered entries, falling back to hardcoded values.
func (h *MenuHandler) loadConfig(r *http.Request, arch string) (menuconfig.ThemeConfig, []menuconfig.EntryConfig) {
	theme := menuconfig.FallbackTheme()
	rawEntries := menuconfig.FallbackEntries().Entries

	if h.cache != nil {
		if t, err := h.cache.Theme(r.Context()); err == nil {
			theme = t.ThemeConfig
		}
		if e, err := h.cache.Entries(r.Context()); err == nil {
			rawEntries = e.Entries
		}
	}

	// Filter: always drop disabled; filter by arch when arch is known.
	filtered := make([]menuconfig.EntryConfig, 0, len(rawEntries))
	for _, e := range rawEntries {
		if !e.Enabled {
			continue
		}
		if arch != "" {
			matched := false
			for _, a := range e.Arch {
				if a == arch {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		filtered = append(filtered, e)
	}
	return theme, filtered
}

// buildEntryScript returns the iPXE lines that boot a single entry.
func buildEntryScript(base string, e menuconfig.EntryConfig) string {
	switch e.Type {
	case "hook":
		if e.Variant == "rescue" {
			return fmt.Sprintf("chain %s/api/v1/hook/${buildarch}/hook.ipxe?rescue=1", base)
		}
		return fmt.Sprintf("chain %s/api/v1/hook/${buildarch}/hook.ipxe?session=${kontango_session_id}&claim=1", base)
	case "url":
		return fmt.Sprintf("chain %s/api/v1/artifacts/%s.raw.gz || goto menu", base, e.ID)
	case "chain":
		return fmt.Sprintf("chain --autofree %s || goto menu", e.ChainURL)
	default:
		return fmt.Sprintf("echo Unknown entry type: %s\ngoto menu", e.Type)
	}
}

var dynamicMenuTemplate = template.Must(template.New("menu").Parse(`#!ipxe

isset ${net0/ip} || dhcp net0 || goto failed

colour --rgb {{.Theme.Colors.Background}} 4
colour --rgb {{.Theme.Colors.Foreground}} 7
colour --rgb {{.Theme.Colors.HighlightBg}} 2
colour --rgb {{.Theme.Colors.HighlightFg}} 3
cpair --foreground 7 --background 4 0
cpair --foreground 3 --background 2 1

{{range .Theme.LogoASCII}}echo {{.}}
{{end}}echo {{.Theme.Tagline}}
echo

set beacon_url {{.Base}}/api/v1/beacon
imgfetch --name beacon ${beacon_url}?ipxe=1&mac=${net0/mac}&ip=${net0/ip}&arch=${buildarch}&platform=${platform}&userclass=${user-class} || goto offline_menu
imgexec beacon || goto offline_menu

iseq ${kontango_skip_claim} 1 && goto boot_anonymous || goto menu

:menu
menu {{.Theme.Title}}
item --gap
{{range .Entries}}item {{.Key}} {{.Label}}
{{end}}item l Boot from local disk
item s Drop to iPXE shell
item --gap
item --gap Network: ${net0/ip} / MAC: ${net0/mac}
item --gap Session: ${kontango_session_id}

choose --default {{.Theme.DefaultEntry}} --timeout {{.TimeoutMs}} target && goto ${target} || goto l

{{range .Entries}}:{{.Key}}
{{.BootScript}}

{{end}}:l
sanboot --no-describe || exit 1

:s
shell

:boot_anonymous
echo Recognised machine -- booting without claim screen.
chain {{.Base}}/api/v1/hook/${buildarch}/hook.ipxe?session=${kontango_session_id}&claim=0

:offline_menu
menu {{.Theme.Title}} Offline
item --gap
item r Rescue shell
item l Boot from local disk
item s iPXE shell
item retry Retry beacon
item --gap
item --gap Beacon unavailable.
item --gap Network: ${net0/ip} / MAC: ${net0/mac}

choose --default retry --timeout 30000 target && goto ${target} || goto l

:r
chain {{.Base}}/api/v1/hook/${buildarch}/hook.ipxe?rescue=1

:retry
imgfetch --name beacon ${beacon_url}?ipxe=1&mac=${net0/mac}&ip=${net0/ip}&arch=${buildarch}&platform=${platform}&userclass=${user-class} || goto offline_menu
imgexec beacon || goto offline_menu
goto menu

:failed
echo Network failed. Rebooting in 10 seconds.
sleep 10
reboot
`))
```

- [ ] **Step 4: Run menu tests**

```bash
cd ~/git/kore/neverland && go test ./internal/handlers/... -run TestMenu -v 2>&1 | tail -25
```

Expected: all menu tests PASS.

- [ ] **Step 5: Run full test suite**

```bash
cd ~/git/kore/neverland && go test ./... 2>&1 | tail -10
```

Expected: all packages pass.

- [ ] **Step 6: Commit**

```bash
cd ~/git/kore/neverland
git add internal/handlers/menu.go internal/handlers/menu_test.go
git commit -m "feat(menu): dynamic iPXE menu from git-backed theme + entries cache"
```

---

## Task 8: Wire into main.go and update deployment

**Files:**
- Modify: `~/git/kore/neverland/cmd/neverland/main.go`
- Modify: `~/git/kore/neverland/deploy/k8s-deployment.yaml`

- [ ] **Step 1: Update `main.go`**

In `~/git/kore/neverland/cmd/neverland/main.go`, add imports for the two new packages and wire the cache + handlers. Find the section that constructs handlers and replace:

```go
	menu := handlers.NewMenuHandler(cfg.BootBaseURL)
```

with:

```go
	gitFetcher := menuconfig.NewGitFetcher(menuconfig.GitFetcherConfig{
		APIBase: cfg.BootConfigAPIBase,
		Owner:   cfg.BootConfigOwner,
		Repo:    cfg.BootConfigRepo,
		Ref:     cfg.BootConfigRef,
		Token:   cfg.BootConfigToken,
	})
	bootConfigCache := menuconfig.NewMenuConfigCache(gitFetcher, cfg.BootConfigCacheTTL)
	menuCfg := handlers.NewMenuConfigHandler(bootConfigCache)
	menu := handlers.NewMenuHandler(cfg.BootBaseURL, bootConfigCache)
```

And add two new routes (right after the existing `/menu.ipxe` route):

```go
	api.HandleFunc("/menu/theme", menuCfg.GetTheme).Methods("GET")
	api.HandleFunc("/menu/entries", menuCfg.GetEntries).Methods("GET")
```

Add `"github.com/KontangoOSS/neverland/internal/menuconfig"` to the import block.

- [ ] **Step 2: Build**

```bash
cd ~/git/kore/neverland && go build ./... 2>&1
```

Expected: clean.

- [ ] **Step 3: Update `k8s-deployment.yaml`**

In `~/git/kore/neverland/deploy/k8s-deployment.yaml`, add to the `env:` block:

```yaml
            - name: BOOT_CONFIG_API_BASE
              value: "https://git.konoss.org/api/v1"
            - name: BOOT_CONFIG_REPO_OWNER
              value: "kore"
            - name: BOOT_CONFIG_REPO_NAME
              value: "boot-config"
            - name: BOOT_CONFIG_REPO_REF
              value: "main"
            - name: BOOT_CONFIG_TOKEN
              valueFrom:
                secretKeyRef:
                  name: boot-config-git-token
                  key: token
                  optional: true
            - name: BOOT_CONFIG_CACHE_TTL
              value: "30s"
```

- [ ] **Step 4: Create the k8s secret with the Forgejo token**

```bash
ssh -i ~/.ssh/id_ed25519 -o IdentitiesOnly=yes -o StrictHostKeyChecking=no leonardo@10.11.30.91 \
  "sudo kubectl create secret generic boot-config-git-token -n tink-system \
    --from-literal=token=1ed468757024e99d16635647f2ed570791f583e5 \
    --dry-run=client -o yaml | sudo kubectl apply -f -" 2>&1 | tail -3
```

Expected: `secret/boot-config-git-token configured`

- [ ] **Step 5: Commit**

```bash
cd ~/git/kore/neverland
git add cmd/neverland/main.go deploy/k8s-deployment.yaml
git commit -m "feat(neverland): wire git-backed menu cache, add /menu/theme + /menu/entries routes"
```

---

## Task 9: Update OpenAPI docs

**Files:**
- Modify: `~/git/kore/neverland/internal/docs/static/openapi.yaml`

- [ ] **Step 1: Append the two new paths**

Open `~/git/kore/neverland/internal/docs/static/openapi.yaml` and add under `paths:`:

```yaml
  /api/v1/menu/theme:
    get:
      summary: Current boot menu theme
      description: |
        Returns the live theme config from the boot-config git repo (30s cache).
        Returns the hardcoded fallback theme when git is unreachable.
        source="git" means live data; source="fallback" means git was unreachable.
      tags: [boot]
      responses:
        "200":
          description: Theme configuration
          content:
            application/json:
              schema:
                type: object
                properties:
                  title:           { type: string }
                  tagline:         { type: string }
                  logo_ascii:      { type: array, items: { type: string } }
                  logo_png_url:    { type: string }
                  colors:
                    type: object
                    properties:
                      background:   { type: string }
                      foreground:   { type: string }
                      highlight_bg: { type: string }
                      highlight_fg: { type: string }
                      gap_text:     { type: string }
                  timeout_seconds: { type: integer }
                  default_entry:   { type: string }
                  source:          { type: string, enum: [git, fallback] }
                  cached_at:       { type: string, format: date-time }

  /api/v1/menu/entries:
    get:
      summary: Current OS boot catalog
      description: |
        Returns the live OS catalog from the boot-config git repo (30s cache).
        When arch= is provided, filters to entries valid for that architecture
        (after alias resolution: i386→x86_64, amd64→x86_64, arm64→aarch64).
        Disabled entries are excluded when arch= is provided.
        Without arch=, all entries including disabled are returned.
      tags: [boot]
      parameters:
        - in: query
          name: arch
          schema: { type: string }
          description: Machine arch to filter by (x86_64, aarch64, i386, amd64, arm64)
      responses:
        "200":
          description: OS catalog
          content:
            application/json:
              schema:
                type: object
                properties:
                  entries:
                    type: array
                    items:
                      type: object
                      properties:
                        id:        { type: string }
                        label:     { type: string }
                        key:       { type: string }
                        type:      { type: string, enum: [hook, url, chain] }
                        variant:   { type: string }
                        url:       { type: string }
                        chain_url: { type: string }
                        arch:      { type: array, items: { type: string } }
                        enabled:   { type: boolean }
                  source:    { type: string, enum: [git, fallback] }
                  cached_at: { type: string, format: date-time }
```

- [ ] **Step 2: Build**

```bash
cd ~/git/kore/neverland && go build ./... 2>&1
```

Expected: clean (YAML is embedded, build confirms no regressions).

- [ ] **Step 3: Commit**

```bash
cd ~/git/kore/neverland
git add internal/docs/static/openapi.yaml
git commit -m "docs: add /api/v1/menu/theme and /api/v1/menu/entries to OpenAPI spec"
```

---

## Task 10: Deploy and end-to-end test

- [ ] **Step 1: Build neverland Docker image**

```bash
cd ~/git/kore/neverland
docker build -t neverland:boot-menu . 2>&1 | tail -3
```

Expected: build succeeds.

- [ ] **Step 2: Load image onto tink node**

```bash
docker save neverland:boot-menu -o /tmp/neverland-boot-menu.tar
scp -i ~/.ssh/id_ed25519 -o IdentitiesOnly=yes -o StrictHostKeyChecking=no \
  /tmp/neverland-boot-menu.tar leonardo@10.11.30.91:/tmp/
ssh -o StrictHostKeyChecking=no leonardo@10.11.30.91 \
  'sudo k3s ctr images import /tmp/neverland-boot-menu.tar 2>&1 | tail -2
   sudo k3s ctr images tag --force docker.io/library/neverland:boot-menu git.kontango.io/kore/neverland:phase1 2>&1 | tail -1
   sudo kubectl rollout restart -n tink-system deployment/neverland
   sudo kubectl rollout status -n tink-system deployment/neverland --timeout=90s 2>&1 | tail -2
   sleep 3
   sudo kubectl logs -n tink-system deployment/neverland --tail=8 2>&1' 2>&1
rm /tmp/neverland-boot-menu.tar
```

Expected log line: `neverland starting` and `listening on 0.0.0.0:8091`.

- [ ] **Step 3: Test `/api/v1/menu/theme` endpoint**

```bash
curl -s https://boot.kontango.net/api/v1/menu/theme | python3 -m json.tool | head -20
```

Expected: JSON with `"title": "Kontango Boot"`, `"source": "git"` or `"fallback"`.

- [ ] **Step 4: Test `/api/v1/menu/entries` endpoint**

```bash
curl -s "https://boot.kontango.net/api/v1/menu/entries?arch=x86_64" | python3 -m json.tool
```

Expected: JSON with `hookos` and `rescue` entries, both `enabled: true`.

- [ ] **Step 5: Test the rendered iPXE menu**

```bash
curl -s "https://boot.kontango.net/api/v1/menu.ipxe?arch=x86_64" | head -30
```

Expected:
- Starts with `#!ipxe`
- Contains `colour --rgb 0x0f4c5c`
- Contains `echo` lines with logo
- Contains `item 1 Install Kontango Hook OS`
- Contains `item r Rescue shell`
- Contains `menu Kontango Boot`

- [ ] **Step 6: Push a change to boot-config and verify propagation**

```bash
FORGEJO_TOKEN=1ed468757024e99d16635647f2ed570791f583e5

# Get current SHA for update
SHA=$(curl -s "https://git.konoss.org/api/v1/repos/kore/boot-config/contents/theme.json?ref=main" \
  -H "Authorization: token $FORGEJO_TOKEN" | python3 -c "import sys,json; print(json.load(sys.stdin)['sha'])")

# Update title
CONTENT=$(python3 -c "
import json, base64
theme = {
  'title': 'Kontango Boot v2',
  'tagline': 'Boot anywhere. Own everything.',
  'logo_ascii': ['  logo updated!'],
  'logo_png_url': '',
  'colors': {'background': '0x0f4c5c','foreground': '0xffffff','highlight_bg': '0xe6b94d','highlight_fg': '0x1f2024','gap_text': '0x6b6f7a'},
  'timeout_seconds': 30,
  'default_entry': 'hookos'
}
print(base64.b64encode(json.dumps(theme, indent=2).encode()).decode())
")

curl -s -X PUT "https://git.konoss.org/api/v1/repos/kore/boot-config/contents/theme.json" \
  -H "Authorization: token $FORGEJO_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"message\":\"test: update title\",\"content\":\"$CONTENT\",\"sha\":\"$SHA\"}" \
  | python3 -c "import sys,json; d=json.load(sys.stdin); print('updated:', d.get('content',{}).get('name','ERROR'))"

echo "Waiting 35s for cache to expire..."
```

Wait 35 seconds (cache TTL is 30s), then:

```bash
curl -s https://boot.kontango.net/api/v1/menu/theme | python3 -c "import sys,json; d=json.load(sys.stdin); print('title:', d['title'], 'source:', d['source'])"
```

Expected: `title: Kontango Boot v2  source: git`

- [ ] **Step 7: Restore original title**

```bash
SHA=$(curl -s "https://git.konoss.org/api/v1/repos/kore/boot-config/contents/theme.json?ref=main" \
  -H "Authorization: token $FORGEJO_TOKEN" | python3 -c "import sys,json; print(json.load(sys.stdin)['sha'])")

CONTENT=$(python3 -c "
import json, base64
theme = {
  'title': 'Kontango Boot',
  'tagline': 'Boot anywhere. Own everything.',
  'logo_ascii': ['  |/ _ |\\\\ | |_ /\\\\\\\\  |\\\\ |  /__ _ ', '  |\\\\\\\\(_)| \\\\\\\\|  | /--\\\\\\\\ | \\\\\\\\| /  (_)'],
  'logo_png_url': '',
  'colors': {'background': '0x0f4c5c','foreground': '0xffffff','highlight_bg': '0xe6b94d','highlight_fg': '0x1f2024','gap_text': '0x6b6f7a'},
  'timeout_seconds': 30,
  'default_entry': 'hookos'
}
print(base64.b64encode(json.dumps(theme, indent=2).encode()).decode())
")

curl -s -X PUT "https://git.konoss.org/api/v1/repos/kore/boot-config/contents/theme.json" \
  -H "Authorization: token $FORGEJO_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"message\":\"restore: original title\",\"content\":\"$CONTENT\",\"sha\":\"$SHA\"}" \
  | python3 -c "import sys,json; d=json.load(sys.stdin); print('restored:', d.get('content',{}).get('name',''))"
```

- [ ] **Step 8: Push branches**

```bash
cd ~/git/kore/neverland && git push origin feat/boot-beacon 2>&1 | tail -3
```

---

## Self-Review Against Spec

**Spec §Configuration (6 env vars):** → Task 6. ✅

**Spec §kore/boot-config repo (theme.json + entries.json):** → Task 0. ✅

**Spec §GET /api/v1/menu/theme:** → Tasks 3, 4, 5, 8. ✅

**Spec §GET /api/v1/menu/entries with ?arch= filtering + alias resolution:** → Tasks 3, 4, 5. ✅

**Spec §Fallback — hardcoded, never cached, immediate retry:** → Tasks 2, 4. `FallbackDoesNotUpdateTimestamp` test explicitly verifies this. ✅

**Spec §Dynamic menu template with colour/cpair, logo_ascii, timeout, entries:** → Task 7. ✅

**Spec §Entry types hook/url/chain rendered correctly:** → Task 7, `buildEntryScript`. `TestMenu_ChainEntryRendered` + `TestMenu_URLEntryRendered`. ✅

**Spec §Built-in l (local) and s (shell) always present:** → Task 7, hardcoded in template. ✅

**Spec §`url` artifact name = `<id>.raw.gz`:** → Task 7, `buildEntryScript` returns `e.ID + ".raw.gz"`. ✅

**Spec §arch passed as ?arch= from iPXE:** → Task 7, `Handle()` reads `r.URL.Query().Get("arch")`. Note: the iPXE script itself needs to chain to `menu.ipxe?arch=${buildarch}` — this is in the `imgfetch` beacon flow (beacon sets `${kontango_skip_claim}` and the menu uses `${buildarch}` directly in its chains). The menu handler reads `?arch=` from the request. The ISO's `embed.ipxe` chains to `/api/v1/menu.ipxe` without ?arch — that's fine, entries will be unfiltered by arch at menu load time and the template uses `${buildarch}` at iPXE runtime for the actual chain commands. ✅

**Spec §OpenAPI docs:** → Task 9. ✅

**Spec §`source` + `cached_at` fields in responses:** → Task 1 types, Task 4 cache returns them. ✅

**Spec §GitHub migration = change BOOT_CONFIG_API_BASE only:** → Task 6 config, Task 3 fetcher URL construction uses APIBase directly as `{base}/repos/{owner}/{repo}/contents/{path}`. For GitHub: `https://api.github.com` → `https://api.github.com/repos/...`. ✅
