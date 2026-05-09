# Git-Backed Dynamic iPXE Boot Menu — Design Spec

**Date:** 2026-05-09
**Status:** Approved for implementation
**Repos:** `kore/neverland` (API + rendering), `kore/boot-config` (new, config source)

---

## Problem

The Kontango boot menu served at `boot.kontango.net/api/v1/menu.ipxe` is hardcoded in
`internal/handlers/menu.go`. Changing the logo, colors, title, or adding an OS to the
catalog requires a code change, Docker image rebuild, and pod redeployment. Operators
have no self-service path for customizing the boot experience.

---

## Goals

- Logo, colors, title, tagline, and OS catalog all live in a git repo (`kore/boot-config`)
- Push to that repo → next machine to boot sees the change (within 30 seconds)
- Two new JSON API endpoints expose the current theme and catalog as structured data
- The menu renderer calls those endpoints — iPXE machines get a dynamically composed script
- Zero code changes required to add/remove/toggle OS entries
- Forgejo now, GitHub later — migration is a single env var change
- Boot is never blocked by git unavailability — hardcoded minimal fallback always available

## Non-goals

- PNG graphical mode (deferred — requires iPXE recompile with `CONSOLE_FRAMEBUFFER`)
- Per-machine theming (one theme globally)
- Authenticated menu access (menu.ipxe is public)
- Woodpecker CI pipeline for boot-config repo (not needed — neverland fetches on demand)

---

## Architecture

```
kore/boot-config (Forgejo / future GitHub)
├── theme.json
└── entries.json
        │
        │ Forgejo contents API (HTTPS, read-only token)
        │ 30-second in-memory cache
        ▼
neverland (internal/handlers/menuconfig.go — new)
├── GET /api/v1/menu/theme     → ThemeResponse JSON
├── GET /api/v1/menu/entries   → EntriesResponse JSON
└── GET /api/v1/menu.ipxe      → rendered iPXE script (existing handler, extended)
        │
        │ boot.kontango.net (public HTTPS via Caddy + Ziti)
        ▼
iPXE client (any machine, anywhere)
```

---

## Configuration

Six new env vars added to `internal/config/config.go` and `deploy/k8s-deployment.yaml`:

| Var | Default | Meaning |
|---|---|---|
| `BOOT_CONFIG_API_BASE` | `https://git.konoss.org/api/v1` | Base URL for contents API. GitHub: `https://api.github.com` |
| `BOOT_CONFIG_REPO_OWNER` | `kore` | Repo owner / org |
| `BOOT_CONFIG_REPO_NAME` | `boot-config` | Repo name |
| `BOOT_CONFIG_REPO_REF` | `main` | Branch or tag |
| `BOOT_CONFIG_TOKEN` | `""` | Read-only API token (Forgejo personal token or GitHub PAT) |
| `BOOT_CONFIG_CACHE_TTL` | `30s` | How long to cache fetched JSON |

**GitHub migration:** change `BOOT_CONFIG_API_BASE` to `https://api.github.com`, update
`BOOT_CONFIG_REPO_OWNER` to the GitHub org, update `BOOT_CONFIG_TOKEN` to a GitHub PAT.
No other changes.

---

## `kore/boot-config` Repository

### `theme.json`

```json
{
  "title": "Kontango Boot",
  "tagline": "Boot anywhere. Own everything.",
  "logo_ascii": [
    "  |/ _ |\\ | |_ /\\  |\\ |  /__ _ ",
    "  |\\(_)| \\|  | /--\\ | \\| /  (_)"
  ],
  "logo_png_url": "",
  "colors": {
    "background": "0x0f4c5c",
    "foreground": "0xffffff",
    "highlight_bg": "0xe6b94d",
    "highlight_fg": "0x1f2024",
    "gap_text":     "0x6b6f7a"
  },
  "timeout_seconds": 30,
  "default_entry": "hookos"
}
```

**Field notes:**
- `logo_ascii`: array of strings, each rendered via `echo`. Must be CP437-safe (no Unicode box drawing). Max 6 lines, max 60 chars per line.
- `logo_png_url`: empty string means no background image. Populated when iPXE binary is compiled with `CONSOLE_FRAMEBUFFER`. Currently unused in rendering but stored and returned by the API.
- `colors.*`: hex strings matching iPXE's `colour --rgb` format.
- `timeout_seconds`: how long `choose` waits before selecting `default_entry`.
- `default_entry`: must match an `id` in `entries.json`, or `"local"` for boot-from-disk.

### `entries.json`

```json
{
  "entries": [
    {
      "id": "hookos",
      "label": "Install Kontango Hook OS",
      "key": "1",
      "type": "hook",
      "variant": "",
      "arch": ["x86_64", "i386", "aarch64"],
      "enabled": true
    },
    {
      "id": "ubuntu2404",
      "label": "Ubuntu 24.04 LTS Server",
      "key": "2",
      "type": "url",
      "url": "https://releases.ubuntu.com/24.04/ubuntu-24.04-live-server-amd64.iso",
      "arch": ["x86_64"],
      "enabled": true
    },
    {
      "id": "netbootxyz",
      "label": "More operating systems (netboot.xyz)",
      "key": "n",
      "type": "chain",
      "chain_url": "https://boot.netboot.xyz",
      "arch": ["x86_64", "aarch64"],
      "enabled": true
    },
    {
      "id": "rescue",
      "label": "Rescue shell",
      "key": "r",
      "type": "hook",
      "variant": "rescue",
      "arch": ["x86_64", "i386", "aarch64"],
      "enabled": true
    }
  ]
}
```

**Entry types:**

| Type | Required fields | iPXE action |
|---|---|---|
| `hook` | `variant` (optional, defaults to `${buildarch}`) | `chain {{.Base}}/api/v1/hook/${buildarch}/hook.ipxe?session=...&claim=1` |
| `url` | `url` | Triggers async FetchHandler job on first request. `chain {{.Base}}/api/v1/artifacts/<name>.raw.gz`. Menu shows entry as `(preparing...)` until artifact ready. |
| `chain` | `chain_url` | `chain --autofree <chain_url> \|\| goto menu` |

**`arch` filtering:** The menu renderer compares the booting machine's resolved arch
(after alias expansion: `i386` → `x86_64`, `amd64` → `x86_64`, `arm64` → `aarch64`)
against each entry's `arch` array. Entries whose `arch` does not include the machine's
arch are omitted from the rendered menu. This runs server-side — the iPXE script only
contains entries valid for that machine's arch.

**`enabled: false`**: entry is omitted from the rendered menu but retained in the JSON
for operator visibility.

**Built-in entries (always appended, not in entries.json):**

```
l   Boot from local disk      (sanboot)
s   Drop to iPXE shell        (shell)
```

These are hardcoded in the renderer so they always exist regardless of git content.

---

## New API Endpoints

### `GET /api/v1/menu/theme`

Returns the current theme. Fetches `theme.json` from git, caches for `BOOT_CONFIG_CACHE_TTL`.
On fetch failure, returns the hardcoded fallback theme.

**Response (200):**
```json
{
  "title": "Kontango Boot",
  "tagline": "Boot anywhere. Own everything.",
  "logo_ascii": ["...", "..."],
  "logo_png_url": "",
  "colors": {
    "background":   "0x0f4c5c",
    "foreground":   "0xffffff",
    "highlight_bg": "0xe6b94d",
    "highlight_fg": "0x1f2024",
    "gap_text":     "0x6b6f7a"
  },
  "timeout_seconds": 30,
  "default_entry": "hookos",
  "source": "git",
  "cached_at": "2026-05-09T14:22:11Z"
}
```

`source` is `"git"` when the response came from Forgejo (or cache), `"fallback"` when git
was unreachable. `cached_at` is the timestamp of the last successful git fetch.

### `GET /api/v1/menu/entries`

Returns the current OS catalog. Same cache/fallback semantics as `/theme`.

**Query params:**
- `?arch=x86_64` — filters entries to only those valid for this arch (optional; if omitted, returns all entries including disabled ones for operator inspection)

**Response (200):**
```json
{
  "entries": [
    {
      "id": "hookos",
      "label": "Install Kontango Hook OS",
      "key": "1",
      "type": "hook",
      "variant": "",
      "arch": ["x86_64", "i386", "aarch64"],
      "enabled": true,
      "artifact_ready": null
    },
    {
      "id": "ubuntu2404",
      "label": "Ubuntu 24.04 LTS Server",
      "key": "2",
      "type": "url",
      "url": "https://releases.ubuntu.com/...",
      "arch": ["x86_64"],
      "enabled": true,
      "artifact_ready": false
    }
  ],
  "source": "git",
  "cached_at": "2026-05-09T14:22:11Z"
}
```

`artifact_ready`: `null` for non-`url` entries. `true` if the artifact has been fetched
and is available at `/api/v1/artifacts/`. `false` if the artifact is pending or in-flight
(triggers a background FetchHandler job if not already running).

---

## Fallback Behavior

When either `theme.json` or `entries.json` cannot be fetched from git (network error,
auth failure, repo not found, malformed JSON), the affected endpoint returns a hardcoded
Go constant. The fallback is logged at `WARN` level with the error.

**Fallback theme:** Kontango brand colors (`0x0f4c5c` background), simple ASCII title
`"Kontango Boot"`, no logo lines, 30-second timeout, default entry `"hookos"`.

**Fallback entries:**
```
1  Install Kontango Hook OS   (hook, all arch)
r  Rescue shell               (hook variant=rescue, all arch)
```

Plus the always-present `l` (local disk) and `s` (shell).

The fallback is **never** cached. On the next request (after TTL), neverland will retry git.

---

## Menu Rendering (extended `menu.go`)

The `MenuHandler.Handle` method is extended to:

1. Call `menuConfigFetcher.Theme()` — returns `ThemeConfig` (from cache or fallback)
2. Call `menuConfigFetcher.Entries(arch)` where arch is parsed from the request's
   `?arch=` query param. The iPXE script is updated to append `?arch=${buildarch}` when
   chainloading `menu.ipxe` (after beacon sets `${buildarch}`). If `?arch` is absent
   (e.g., direct curl), all entries are returned unfiltered. Returns `[]EntryConfig`
   (filtered, fallback if needed).
3. Pass both to the template

The template gains new sections:

```
{{- range .LogoLines}}
echo {{.}}
{{- end}}
echo {{.Theme.Tagline}}
echo
```

Color application:
```
colour --rgb {{.Theme.Colors.Background}} 4
colour --rgb {{.Theme.Colors.Foreground}} 7
colour --rgb {{.Theme.Colors.HighlightBg}} 2
colour --rgb {{.Theme.Colors.HighlightFg}} 3
cpair --foreground 7 --background 4 0
cpair --foreground 3 --background 2 1
```

Entry rendering per type:
```
# In :menu section
{{range .Entries}}
{{if eq .Type "url"}}{{if not .ArtifactReady}}
item --disabled {{.Key}} {{.Label}} (preparing...)
{{else}}
item {{.Key}} {{.Label}}
{{end}}{{else}}
item {{.Key}} {{.Label}}
{{end}}{{end}}
```

Label section per entry:
```
{{range .Entries}}
:{{.Key}}
{{if eq .Type "hook"}}
chain {{.Base}}/api/v1/hook/${buildarch}/hook.ipxe?session=${kontango_session_id}&claim=1
{{else if eq .Type "url"}}
chain {{.Base}}/api/v1/artifacts/{{.ArtifactName}} || goto menu
{{else if eq .Type "chain"}}
chain --autofree {{.ChainURL}} || goto menu
{{end}}
{{end}}
```

---

## Cache Implementation

A single `MenuConfigCache` struct in `internal/menuconfig/cache.go`:

```go
type MenuConfigCache struct {
    mu          sync.RWMutex
    theme       *ThemeConfig
    entries     *EntriesConfig
    themeAt     time.Time
    entriesAt   time.Time
    ttl         time.Duration
    fetcher     GitFetcher
}
```

`Theme()` and `Entries()`:
1. Read lock: if cached and within TTL, return cached value
2. Write lock: fetch from git via `GitFetcher`
3. On success: update cache, update timestamp, return
4. On failure: log warning, return hardcoded fallback (do NOT update cache timestamp — next request retries git immediately)

No background goroutine — cache is populated on-demand at request time. The 30s TTL
means at most one git fetch per 30 seconds per endpoint under normal load.

### `GitFetcher` interface

```go
type GitFetcher interface {
    FetchTheme(ctx context.Context) (*ThemeConfig, error)
    FetchEntries(ctx context.Context) (*EntriesConfig, error)
}
```

Production implementation calls the Forgejo/GitHub contents API. Test implementation
returns fixtures.

---

## `url` Entry: Artifact Lifecycle

When a `url` entry is first seen (i.e., not in the existing artifact list):

1. `GET /api/v1/menu/entries` response sets `artifact_ready: false`
2. Neverland triggers a FetchHandler job (`POST /api/v1/artifacts/fetch` internally) for the URL
3. FetchHandler downloads → converts qcow2→raw if needed → gzips → writes to artifact dir
4. Subsequent `/api/v1/menu/entries` calls check artifact dir for the expected filename:
   `<entry-id>.raw.gz` — if present, `artifact_ready: true`
5. Menu renders the entry as active (not disabled)

The artifact filename is derived from the entry `id`: `ubuntu2404.raw.gz`.

The artifact persists across pod restarts (mounted on `hostPath` volume). Adding a new
`url` entry triggers one fetch; subsequent boots serve from the cached artifact.

---

## File Structure (new files in `kore/neverland`)

```
internal/
  menuconfig/
    cache.go          ← MenuConfigCache + GitFetcher interface
    cache_test.go
    fetcher.go        ← production GitFetcher (Forgejo/GitHub contents API)
    fetcher_test.go
    fallback.go       ← hardcoded fallback theme + entries as Go consts
    types.go          ← ThemeConfig, EntryConfig, EntriesConfig structs
  handlers/
    menuconfig.go     ← ThemeHandler + EntriesHandler (new)
    menuconfig_test.go
    menu.go           ← extended to call MenuConfigCache (existing, modified)
    menu_test.go      ← extended (existing, modified)
internal/config/
  config.go           ← 6 new fields (modified)
cmd/neverland/
  main.go             ← wire MenuConfigCache, new handlers (modified)
deploy/
  k8s-deployment.yaml ← 6 new env vars (modified)
docs/
  superpowers/specs/
    2026-05-09-git-backed-boot-menu.md  ← this file
```

**New repo (separate):**
```
kore/boot-config/
  theme.json
  entries.json
  README.md
```

---

## Testing

**Unit tests (no network):**
- `cache_test.go`: cache hit/miss/TTL expiry, fallback on fetcher error, concurrent reads
- `fetcher_test.go`: correct URL construction for Forgejo vs GitHub base URLs, JSON parsing, auth header, malformed JSON returns error
- `menuconfig_test.go`: theme handler returns JSON with correct `source` field, entries handler filters by arch correctly
- `menu_test.go`: rendered iPXE contains logo lines, color commands, all enabled entries, disabled entries omitted, `url` entry with `artifact_ready: false` renders as `--disabled`

**Integration test (uses real Forgejo if BOOT_CONFIG_TOKEN set):**
- Round-trip: fetch theme → render menu → verify expected iPXE strings present

---

## Failure Modes

| Failure | Behavior |
|---|---|
| Forgejo unreachable | Fallback theme+entries served. Logged at WARN. |
| `theme.json` invalid JSON | Fallback theme. Error logged. |
| `entries.json` invalid JSON | Fallback entries. Error logged. |
| Entry has unknown type | Entry skipped with log. Other entries rendered normally. |
| `url` artifact fetch fails | Entry stays `artifact_ready: false`, shows as disabled in menu. |
| `url` artifact fetch in-progress | Entry shows `(preparing...)` as disabled. |
| `chain_url` unreachable at boot time | iPXE falls back to `:menu` via `\|\| goto menu`. |
| Token expired/invalid | Forgejo returns 401; treated as fetch failure; fallback served. |

---

## API Spec Updates

`internal/docs/static/openapi.yaml` gains two new paths:

```yaml
/api/v1/menu/theme:
  get:
    summary: Current boot menu theme
    tags: [boot]
    responses:
      "200":
        description: Theme config (from git or fallback)
        content:
          application/json:
            schema: { $ref: '#/components/schemas/ThemeResponse' }

/api/v1/menu/entries:
  get:
    summary: Current OS catalog
    tags: [boot]
    parameters:
      - in: query
        name: arch
        schema: { type: string }
        description: Filter entries by machine arch (x86_64, aarch64, i386)
    responses:
      "200":
        description: Entries (from git or fallback, filtered by arch)
        content:
          application/json:
            schema: { $ref: '#/components/schemas/EntriesResponse' }
```
