# ZAC Feature Parity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring ziti-dash to full ZAC feature parity — typed SDK client, CRUD for all Ziti entity types, and missing pages (JWT signers, CAs, sessions, posture checks, transit routers, edge router policies, service edge router policies).

**Architecture:** Keep the Browzer framework (`pkg/browzer/`) as the backbone — `app.New()` bootstraps the server, each entity domain is a `bff.Plugin` registered via `app.Use()` in `main.go`, and `bff.Router` handles token caching. Add `Delete()` to `pkg/browzer/ziti/client.go` and wire the already-vendored `github.com/openziti/edge-api` typed SDK as an optional typed client alongside the existing raw HTTP helpers. Each new plugin in `internal/` follows the exact same pattern as `internal/dashboard/` — one file, `Plugin` struct implementing `Name()`, `Description()`, `Register(router *bff.Router)`. Frontend stays vanilla JS/HTML/CSS served by the existing SPA handler — no build step, no framework.

**Tech Stack:** Go 1.25, `github.com/openziti/edge-api v0.27.5` (already in go.mod), vanilla JS/HTML/CSS (no build step), existing Browzer BFF plugin architecture at `pkg/browzer/`.

---

## File Map

**Modified:**
- `pkg/browzer/ziti/client.go` — replace with thin SDK wrapper; expose typed `ZitiEdgeManagement` client per-request using cached OIDC token
- `pkg/browzer/bff/bff.go` — expose `MgmtClient(token) *rest_management_api_client.ZitiEdgeManagement` helper on Router
- `internal/dashboard/routes.go` — switch existing list handlers to use SDK types
- `internal/dashboard/plugin.go` — add CRUD routes for services, routers, terminators, service policies
- `frontend/index.html` — add nav items, page divs, modals, and JS for all new entity types

**Created:**
- `internal/edge_router_policy/plugin.go` — CRUD for edge router policies
- `internal/service_edge_router_policy/plugin.go` — CRUD for service edge router policies
- `internal/auth_policy/plugin.go` — CRUD for auth policies
- `internal/cert_authority/plugin.go` — CRUD for certificate authorities
- `internal/ext_jwt_signer/plugin.go` — CRUD for external JWT signers
- `internal/posture_check/plugin.go` — CRUD for posture checks
- `internal/transit_router/plugin.go` — CRUD for transit routers
- `internal/api_session/plugin.go` — list/delete for API sessions and sessions

---

## Task 1: Replace Ziti client with SDK wrapper

**Why:** The existing raw-HTTP client in `pkg/browzer/ziti/client.go` duplicates what the SDK does. The SDK gives us typed params/responses and handles TLS/pagination. We keep the OIDC refresh-token auth (it works for pre11+) but wrap it to produce an SDK client.

**Files:**
- Modify: `pkg/browzer/ziti/client.go`
- Modify: `pkg/browzer/bff/bff.go`

- [ ] **Step 1: Add SDK import to go.mod as a direct dep**

```bash
cd ~/git/kore/ziti-dash
go get github.com/openziti/edge-api@v0.27.5
```

Expected: `go.mod` updates `edge-api` from `// indirect` to a direct dep.

- [ ] **Step 2: Rewrite `pkg/browzer/ziti/client.go`**

Replace the entire file contents with:

```go
package ziti

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	httptransport "github.com/go-openapi/runtime/client"
	"github.com/openziti/edge-api/rest_management_api_client"
	"github.com/openziti/edge-api/rest_util"
)

// Client holds connection config for the Ziti management API.
// Auth uses the OIDC refresh-token flow required by pre11+ controllers.
type Client struct {
	Addr         string
	RefreshToken string
}

func (c *Client) httpClient() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
}

// Authenticate exchanges the OIDC refresh token for a fresh access token.
func (c *Client) Authenticate() (string, error) {
	if c.RefreshToken == "" {
		return "", fmt.Errorf("ZITI_REFRESH_TOKEN is required")
	}
	body := fmt.Sprintf("grant_type=refresh_token&refresh_token=%s&client_id=native", c.RefreshToken)
	req, err := http.NewRequest("POST", "https://"+c.Addr+"/oidc/oauth/token", strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("oidc refresh failed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("oidc refresh failed (status %d): %s", resp.StatusCode, string(data))
	}
	var result struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(data, &result); err != nil || result.AccessToken == "" {
		return "", fmt.Errorf("oidc returned empty access token")
	}
	return result.AccessToken, nil
}

// MgmtClient returns a typed SDK management client authenticated with the given token.
func (c *Client) MgmtClient(token string) (*rest_management_api_client.ZitiEdgeManagement, error) {
	apiURL := "https://" + c.Addr
	httpClient := c.httpClient()
	return rest_util.NewEdgeManagementClientWithToken(httpClient, apiURL, token)
}

// GetUnauthenticated does a GET without auth (for /version etc).
func (c *Client) GetUnauthenticated(path string) (json.RawMessage, error) {
	req, err := http.NewRequest("GET", "https://"+c.Addr+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// -- legacy helpers kept for existing dashboard routes -----------------------

func (c *Client) Get(token, path string) (json.RawMessage, error) {
	req, _ := http.NewRequest("GET", "https://"+c.Addr+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		trunc := string(data)
		if len(trunc) > 200 {
			trunc = trunc[:200]
		}
		return nil, fmt.Errorf("GET %s: %d %s", path, resp.StatusCode, trunc)
	}
	return data, nil
}

func (c *Client) Post(token, path string, body []byte) (json.RawMessage, error) {
	req, _ := http.NewRequest("POST", "https://"+c.Addr+path, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		trunc := string(data)
		if len(trunc) > 200 {
			trunc = trunc[:200]
		}
		return nil, fmt.Errorf("POST %s: %d %s", path, resp.StatusCode, trunc)
	}
	return data, nil
}

func (c *Client) Patch(token, path string, body []byte) (json.RawMessage, error) {
	req, _ := http.NewRequest("PATCH", "https://"+c.Addr+path, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		trunc := string(data)
		if len(trunc) > 200 {
			trunc = trunc[:200]
		}
		return nil, fmt.Errorf("PATCH %s: %d %s", path, resp.StatusCode, trunc)
	}
	return data, nil
}

func (c *Client) Delete(token, path string) error {
	req, _ := http.NewRequest("DELETE", "https://"+c.Addr+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		return fmt.Errorf("DELETE %s: %d", path, resp.StatusCode)
	}
	return nil
}

// DecodeList unwraps a Ziti {"data": [...]} envelope.
func DecodeList[T any](raw json.RawMessage) ([]T, error) {
	var envelope struct {
		Data []T `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	return envelope.Data, nil
}

// DecodeOne unwraps a Ziti {"data": {...}} envelope.
func DecodeOne[T any](raw json.RawMessage) (*T, error) {
	var envelope struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	return &envelope.Data, nil
}
```

- [ ] **Step 3: Add `MgmtClient` helper to BFF router**

In `pkg/browzer/bff/bff.go`, add this method after the existing `Token()` method:

```go
// MgmtClient returns a typed SDK management client using the cached token.
func (rt *Router) MgmtClient() (*rest_management_api_client.ZitiEdgeManagement, error) {
	tok, err := rt.Token()
	if err != nil {
		return nil, err
	}
	return rt.Ziti.MgmtClient(tok)
}
```

Add the import at the top of the file:
```go
"github.com/openziti/edge-api/rest_management_api_client"
```

- [ ] **Step 4: Verify it compiles**

```bash
cd ~/git/kore/ziti-dash && go build ./...
```

Expected: no errors.

- [ ] **Step 5: Restart container and verify existing pages still work**

```bash
cd ~/git/kore/ziti-dash && make build && docker restart ziti-dash
sleep 3 && curl -s http://localhost:9090/api/overview | python3 -c "import json,sys; d=json.load(sys.stdin); print('ok:', list(d.keys()))"
```

Expected: `ok: ['summary', 'routersOnline', 'routersTotal']`

- [ ] **Step 6: Commit**

```bash
cd ~/git/kore/ziti-dash
git add pkg/browzer/ziti/client.go pkg/browzer/bff/bff.go go.mod go.sum
git commit -m "feat: add SDK MgmtClient wrapper to Ziti client and BFF router"
```

---

## Task 2: Add CRUD endpoints to dashboard plugin (services, routers, service policies, terminators)

**Why:** The dashboard plugin already lists these entities. We add create/patch/delete routes using the typed SDK so the frontend can do full CRUD without new plugin files.

**Files:**
- Modify: `internal/dashboard/plugin.go`
- Modify: `internal/dashboard/routes.go`

- [ ] **Step 1: Add CRUD routes to `internal/dashboard/plugin.go`**

Replace the `Register` function body:

```go
func (p *Plugin) Register(router *bff.Router) {
	z := router.Ziti

	// --- existing read routes ---
	router.Handle("GET /api/overview", overviewHandler(z))
	router.Handle("GET /api/services", listHandler[Service](z, "/edge/management/v1/services?limit=500"))
	router.Handle("GET /api/identities", listHandler[Identity](z, "/edge/management/v1/identities?limit=500"))
	router.Handle("GET /api/identities/{name}", identityDetailHandler(z))
	router.Handle("GET /api/routers", listHandler[EdgeRouter](z, "/edge/management/v1/edge-routers?limit=100"))
	router.Handle("GET /api/policies", listHandler[ServicePolicy](z, "/edge/management/v1/service-policies?limit=500"))
	router.Handle("GET /api/terminators", listHandler[Terminator](z, "/edge/management/v1/terminators?limit=500"))
	router.HandleRaw("GET /api/version", versionHandler(z))

	// --- CRUD: services ---
	router.Handle("POST /api/services", crudCreate(router, "/edge/management/v1/services"))
	router.Handle("PATCH /api/services/{id}", crudPatch(z, "/edge/management/v1/services"))
	router.Handle("DELETE /api/services/{id}", crudDelete(z, "/edge/management/v1/services"))

	// --- CRUD: edge routers ---
	router.Handle("POST /api/routers", crudCreate(router, "/edge/management/v1/edge-routers"))
	router.Handle("PATCH /api/routers/{id}", crudPatch(z, "/edge/management/v1/edge-routers"))
	router.Handle("DELETE /api/routers/{id}", crudDelete(z, "/edge/management/v1/edge-routers"))

	// --- CRUD: service policies ---
	router.Handle("POST /api/policies", crudCreate(router, "/edge/management/v1/service-policies"))
	router.Handle("PATCH /api/policies/{id}", crudPatch(z, "/edge/management/v1/service-policies"))
	router.Handle("DELETE /api/policies/{id}", crudDelete(z, "/edge/management/v1/service-policies"))

	// --- CRUD: terminators ---
	router.Handle("POST /api/terminators", crudCreate(router, "/edge/management/v1/terminators"))
	router.Handle("PATCH /api/terminators/{id}", crudPatch(z, "/edge/management/v1/terminators"))
	router.Handle("DELETE /api/terminators/{id}", crudDelete(z, "/edge/management/v1/terminators"))
}
```

- [ ] **Step 2: Add CRUD helpers to `internal/dashboard/routes.go`**

Append to the bottom of `routes.go`:

```go
import (
	"io"
	"strings"
)

// crudCreate proxies a POST body directly to the Ziti management API.
func crudCreate(router *bff.Router, basePath string) bff.Handler {
	return func(token string, w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			bff.WriteError(w, 400, "read body failed", err)
			return
		}
		z := router.Ziti
		raw, err := z.Post(token, basePath, body)
		if err != nil {
			bff.WriteError(w, 502, "create failed", err)
			return
		}
		bff.WriteRaw(w, raw)
	}
}

// crudPatch proxies a PATCH body to /basePath/{id}.
func crudPatch(z *ziti.Client, basePath string) bff.Handler {
	return func(token string, w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			bff.WriteError(w, 400, "read body failed", err)
			return
		}
		raw, err := z.Patch(token, basePath+"/"+id, body)
		if err != nil {
			bff.WriteError(w, 502, "patch failed", err)
			return
		}
		bff.WriteRaw(w, raw)
	}
}

// crudDelete calls DELETE /basePath/{id}.
func crudDelete(z *ziti.Client, basePath string) bff.Handler {
	return func(token string, w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if err := z.Delete(token, basePath+"/"+id); err != nil {
			bff.WriteError(w, 502, "delete failed", err)
			return
		}
		w.WriteHeader(204)
	}
}
```

Add `"git.kontango.io/kore/ziti-dash/pkg/browzer/ziti"` to the existing imports in `routes.go`.

- [ ] **Step 3: Build and verify**

```bash
cd ~/git/kore/ziti-dash && go build ./...
```

Expected: no errors.

- [ ] **Step 4: Smoke-test delete route responds (don't actually delete anything)**

```bash
curl -s -X DELETE http://localhost:9090/api/services/nonexistent-id | python3 -c "import json,sys; print(json.load(sys.stdin))"
```

Expected: JSON error response from Ziti (502 or similar), not a 404 from Go.

- [ ] **Step 5: Commit**

```bash
cd ~/git/kore/ziti-dash
git add internal/dashboard/
git commit -m "feat: add CRUD endpoints for services, routers, policies, terminators"
```

---

## Task 3: Add CRUD for identities (with JWT enrollment token endpoint)

**Why:** Identities need special handling — creating one can return an enrollment JWT that the UI needs to display (and optionally render as QR). The typed SDK makes the JWT extraction trivial.

**Files:**
- Modify: `internal/dashboard/routes.go`
- Modify: `internal/dashboard/plugin.go`

- [ ] **Step 1: Add identity CRUD routes to plugin.go**

In the `Register` function, after the existing identity list routes add:

```go
// --- CRUD: identities ---
router.Handle("POST /api/identities", createIdentityHandler(router))
router.Handle("PATCH /api/identities/{id}", crudPatch(z, "/edge/management/v1/identities"))
router.Handle("DELETE /api/identities/{id}", crudDelete(z, "/edge/management/v1/identities"))
router.Handle("GET /api/identities/{id}/enrollment-jwt", identityEnrollmentJWTHandler(z))
```

- [ ] **Step 2: Add `createIdentityHandler` and `identityEnrollmentJWTHandler` to routes.go**

Append to `routes.go`:

```go
func createIdentityHandler(router *bff.Router) bff.Handler {
	return func(token string, w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			bff.WriteError(w, 400, "read body failed", err)
			return
		}
		raw, err := router.Ziti.Post(token, "/edge/management/v1/identities", body)
		if err != nil {
			bff.WriteError(w, 502, "create identity failed", err)
			return
		}
		// raw is {"data":{"id":"...","enrollment":{"ott":{"jwt":"...","token":"..."}},...}}
		// pass it straight through — frontend extracts the JWT
		bff.WriteRaw(w, raw)
	}
}

func identityEnrollmentJWTHandler(z *ziti.Client) bff.Handler {
	return func(token string, w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		raw, err := z.Get(token, "/edge/management/v1/identities/"+id+"/enrollments")
		if err != nil {
			bff.WriteError(w, 502, "get enrollments failed", err)
			return
		}
		// Return raw enrollment list — frontend picks out the OTT JWT
		bff.WriteRaw(w, raw)
	}
}
```

- [ ] **Step 3: Build**

```bash
cd ~/git/kore/ziti-dash && go build ./...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
cd ~/git/kore/ziti-dash
git add internal/dashboard/
git commit -m "feat: add identity CRUD and enrollment JWT endpoint"
```

---

## Task 4: New plugin — edge router policies + service edge router policies

**Files:**
- Create: `internal/edge_router_policy/plugin.go`
- Create: `internal/service_edge_router_policy/plugin.go`
- Modify: `cmd/ziti-dash/main.go`

- [ ] **Step 1: Create `internal/edge_router_policy/plugin.go`**

```go
package edge_router_policy

import (
	"io"
	"net/http"

	"git.kontango.io/kore/ziti-dash/pkg/browzer/bff"
	"git.kontango.io/kore/ziti-dash/pkg/browzer/ziti"
)

type Plugin struct{}

func (p *Plugin) Name() string        { return "edge-router-policy" }
func (p *Plugin) Description() string { return "CRUD for Ziti edge router policies" }

func (p *Plugin) Register(router *bff.Router) {
	z := router.Ziti
	base := "/edge/management/v1/edge-router-policies"

	router.Handle("GET /api/edge-router-policies", listAllHandler(z, base))
	router.Handle("POST /api/edge-router-policies", createHandler(router, base))
	router.Handle("PATCH /api/edge-router-policies/{id}", patchHandler(z, base))
	router.Handle("DELETE /api/edge-router-policies/{id}", deleteHandler(z, base))
}

func listAllHandler(z *ziti.Client, base string) bff.Handler {
	return func(token string, w http.ResponseWriter, r *http.Request) {
		raw, err := z.Get(token, base+"?limit=500")
		if err != nil {
			bff.WriteError(w, 502, "list failed", err)
			return
		}
		bff.WriteRaw(w, raw)
	}
}

func createHandler(router *bff.Router, base string) bff.Handler {
	return func(token string, w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			bff.WriteError(w, 400, "read body failed", err)
			return
		}
		raw, err := router.Ziti.Post(token, base, body)
		if err != nil {
			bff.WriteError(w, 502, "create failed", err)
			return
		}
		bff.WriteRaw(w, raw)
	}
}

func patchHandler(z *ziti.Client, base string) bff.Handler {
	return func(token string, w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			bff.WriteError(w, 400, "read body failed", err)
			return
		}
		raw, err := z.Patch(token, base+"/"+id, body)
		if err != nil {
			bff.WriteError(w, 502, "patch failed", err)
			return
		}
		bff.WriteRaw(w, raw)
	}
}

func deleteHandler(z *ziti.Client, base string) bff.Handler {
	return func(token string, w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if err := z.Delete(token, base+"/"+id); err != nil {
			bff.WriteError(w, 502, "delete failed", err)
			return
		}
		w.WriteHeader(204)
	}
}
```

- [ ] **Step 2: Create `internal/service_edge_router_policy/plugin.go`**

```go
package service_edge_router_policy

import (
	"io"
	"net/http"

	"git.kontango.io/kore/ziti-dash/pkg/browzer/bff"
	"git.kontango.io/kore/ziti-dash/pkg/browzer/ziti"
)

type Plugin struct{}

func (p *Plugin) Name() string        { return "service-edge-router-policy" }
func (p *Plugin) Description() string { return "CRUD for Ziti service edge router policies" }

func (p *Plugin) Register(router *bff.Router) {
	z := router.Ziti
	base := "/edge/management/v1/service-edge-router-policies"

	router.Handle("GET /api/service-edge-router-policies", listAllHandler(z, base))
	router.Handle("POST /api/service-edge-router-policies", createHandler(router, base))
	router.Handle("PATCH /api/service-edge-router-policies/{id}", patchHandler(z, base))
	router.Handle("DELETE /api/service-edge-router-policies/{id}", deleteHandler(z, base))
}

func listAllHandler(z *ziti.Client, base string) bff.Handler {
	return func(token string, w http.ResponseWriter, r *http.Request) {
		raw, err := z.Get(token, base+"?limit=500")
		if err != nil {
			bff.WriteError(w, 502, "list failed", err)
			return
		}
		bff.WriteRaw(w, raw)
	}
}

func createHandler(router *bff.Router, base string) bff.Handler {
	return func(token string, w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			bff.WriteError(w, 400, "read body failed", err)
			return
		}
		raw, err := router.Ziti.Post(token, base, body)
		if err != nil {
			bff.WriteError(w, 502, "create failed", err)
			return
		}
		bff.WriteRaw(w, raw)
	}
}

func patchHandler(z *ziti.Client, base string) bff.Handler {
	return func(token string, w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			bff.WriteError(w, 400, "read body failed", err)
			return
		}
		raw, err := z.Patch(token, base+"/"+id, body)
		if err != nil {
			bff.WriteError(w, 502, "patch failed", err)
			return
		}
		bff.WriteRaw(w, raw)
	}
}

func deleteHandler(z *ziti.Client, base string) bff.Handler {
	return func(token string, w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if err := z.Delete(token, base+"/"+id); err != nil {
			bff.WriteError(w, 502, "delete failed", err)
			return
		}
		w.WriteHeader(204)
	}
}
```

- [ ] **Step 3: Register both plugins in main.go**

Open `cmd/ziti-dash/main.go`. Find where other plugins are registered with `app.Use(...)`. Add:

```go
app.Use(&edge_router_policy.Plugin{})
app.Use(&service_edge_router_policy.Plugin{})
```

Add imports:
```go
"git.kontango.io/kore/ziti-dash/internal/edge_router_policy"
"git.kontango.io/kore/ziti-dash/internal/service_edge_router_policy"
```

- [ ] **Step 4: Build and test routes exist**

```bash
cd ~/git/kore/ziti-dash && go build ./...
make build && docker restart ziti-dash
sleep 3
curl -s http://localhost:9090/api/edge-router-policies | python3 -c "import json,sys; d=json.load(sys.stdin); print('count:', len(d.get('data',[])))"
```

Expected: prints count of edge router policies (may be 0 or more).

- [ ] **Step 5: Commit**

```bash
cd ~/git/kore/ziti-dash
git add internal/edge_router_policy/ internal/service_edge_router_policy/ cmd/
git commit -m "feat: add edge router policy and service edge router policy plugins"
```

---

## Task 5: New plugins — auth policies, cert authorities, external JWT signers, posture checks, transit routers, API sessions

**Why:** These are the remaining ZAC entity types. All follow the same CRUD pattern. Group them in one task since the code is identical structure.

**Files:**
- Create: `internal/auth_policy/plugin.go`
- Create: `internal/cert_authority/plugin.go`
- Create: `internal/ext_jwt_signer/plugin.go`
- Create: `internal/posture_check/plugin.go`
- Create: `internal/transit_router/plugin.go`
- Create: `internal/api_session/plugin.go`
- Modify: `cmd/ziti-dash/main.go`

- [ ] **Step 1: Create `internal/auth_policy/plugin.go`**

```go
package auth_policy

import (
	"io"
	"net/http"

	"git.kontango.io/kore/ziti-dash/pkg/browzer/bff"
	"git.kontango.io/kore/ziti-dash/pkg/browzer/ziti"
)

type Plugin struct{}

func (p *Plugin) Name() string        { return "auth-policy" }
func (p *Plugin) Description() string { return "CRUD for Ziti auth policies" }

func (p *Plugin) Register(router *bff.Router) {
	z := router.Ziti
	base := "/edge/management/v1/auth-policies"
	router.Handle("GET /api/auth-policies", listAll(z, base))
	router.Handle("POST /api/auth-policies", create(router, base))
	router.Handle("PATCH /api/auth-policies/{id}", patch(z, base))
	router.Handle("DELETE /api/auth-policies/{id}", del(z, base))
}

func listAll(z *ziti.Client, base string) bff.Handler {
	return func(token string, w http.ResponseWriter, r *http.Request) {
		raw, err := z.Get(token, base+"?limit=500")
		if err != nil {
			bff.WriteError(w, 502, "list failed", err); return
		}
		bff.WriteRaw(w, raw)
	}
}
func create(router *bff.Router, base string) bff.Handler {
	return func(token string, w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		raw, err := router.Ziti.Post(token, base, body)
		if err != nil {
			bff.WriteError(w, 502, "create failed", err); return
		}
		bff.WriteRaw(w, raw)
	}
}
func patch(z *ziti.Client, base string) bff.Handler {
	return func(token string, w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		raw, err := z.Patch(token, base+"/"+r.PathValue("id"), body)
		if err != nil {
			bff.WriteError(w, 502, "patch failed", err); return
		}
		bff.WriteRaw(w, raw)
	}
}
func del(z *ziti.Client, base string) bff.Handler {
	return func(token string, w http.ResponseWriter, r *http.Request) {
		if err := z.Delete(token, base+"/"+r.PathValue("id")); err != nil {
			bff.WriteError(w, 502, "delete failed", err); return
		}
		w.WriteHeader(204)
	}
}
```

- [ ] **Step 2: Create the remaining five plugins using the same pattern**

`internal/cert_authority/plugin.go` — same structure, base = `/edge/management/v1/cas`, package = `cert_authority`, Name = `"cert-authority"`, Description = `"CRUD for Ziti certificate authorities"`

`internal/ext_jwt_signer/plugin.go` — base = `/edge/management/v1/external-jwt-signers`, package = `ext_jwt_signer`, Name = `"ext-jwt-signer"`, Description = `"CRUD for Ziti external JWT signers"`

`internal/posture_check/plugin.go` — base = `/edge/management/v1/posture-checks`, package = `posture_check`, Name = `"posture-check"`, Description = `"CRUD for Ziti posture checks"`

`internal/transit_router/plugin.go` — base = `/edge/management/v1/transit-routers`, package = `transit_router`, Name = `"transit-router"`, Description = `"CRUD for Ziti transit routers"`

`internal/api_session/plugin.go` — base = `/edge/management/v1/api-sessions` for API sessions, plus `/edge/management/v1/sessions` for sessions. Name = `"api-session"`, Description = `"List and delete API sessions and sessions"`. Only expose GET (list) and DELETE — no create/patch (sessions are system-managed):

```go
package api_session

import (
	"net/http"

	"git.kontango.io/kore/ziti-dash/pkg/browzer/bff"
	"git.kontango.io/kore/ziti-dash/pkg/browzer/ziti"
)

type Plugin struct{}

func (p *Plugin) Name() string        { return "api-session" }
func (p *Plugin) Description() string { return "List and delete API sessions and sessions" }

func (p *Plugin) Register(router *bff.Router) {
	z := router.Ziti
	router.Handle("GET /api/api-sessions", listAll(z, "/edge/management/v1/api-sessions"))
	router.Handle("DELETE /api/api-sessions/{id}", del(z, "/edge/management/v1/api-sessions"))
	router.Handle("GET /api/sessions", listAll(z, "/edge/management/v1/sessions"))
	router.Handle("DELETE /api/sessions/{id}", del(z, "/edge/management/v1/sessions"))
}

func listAll(z *ziti.Client, base string) bff.Handler {
	return func(token string, w http.ResponseWriter, r *http.Request) {
		raw, err := z.Get(token, base+"?limit=500")
		if err != nil {
			bff.WriteError(w, 502, "list failed", err); return
		}
		bff.WriteRaw(w, raw)
	}
}
func del(z *ziti.Client, base string) bff.Handler {
	return func(token string, w http.ResponseWriter, r *http.Request) {
		if err := z.Delete(token, base+"/"+r.PathValue("id")); err != nil {
			bff.WriteError(w, 502, "delete failed", err); return
		}
		w.WriteHeader(204)
	}
}
```

- [ ] **Step 3: Register all six in main.go**

Add to `cmd/ziti-dash/main.go`:

```go
app.Use(&auth_policy.Plugin{})
app.Use(&cert_authority.Plugin{})
app.Use(&ext_jwt_signer.Plugin{})
app.Use(&posture_check.Plugin{})
app.Use(&transit_router.Plugin{})
app.Use(&api_session.Plugin{})
```

With imports:
```go
"git.kontango.io/kore/ziti-dash/internal/auth_policy"
"git.kontango.io/kore/ziti-dash/internal/cert_authority"
"git.kontango.io/kore/ziti-dash/internal/ext_jwt_signer"
"git.kontango.io/kore/ziti-dash/internal/posture_check"
"git.kontango.io/kore/ziti-dash/internal/transit_router"
"git.kontango.io/kore/ziti-dash/internal/api_session"
```

- [ ] **Step 4: Build and spot-check two endpoints**

```bash
cd ~/git/kore/ziti-dash && go build ./...
make build && docker restart ziti-dash && sleep 3
curl -s http://localhost:9090/api/auth-policies | python3 -c "import json,sys; d=json.load(sys.stdin); print('auth-policies:', len(d.get('data',[])))"
curl -s http://localhost:9090/api/api-sessions | python3 -c "import json,sys; d=json.load(sys.stdin); print('api-sessions:', len(d.get('data',[])))"
```

Expected: both print counts (any number ≥ 0).

- [ ] **Step 5: Commit**

```bash
cd ~/git/kore/ziti-dash
git add internal/auth_policy/ internal/cert_authority/ internal/ext_jwt_signer/ internal/posture_check/ internal/transit_router/ internal/api_session/ cmd/
git commit -m "feat: add plugins for auth policies, CAs, JWT signers, posture checks, transit routers, API sessions"
```

---

## Task 6: Frontend — split JS into modules and add nav structure for new pages

**Why:** `frontend/index.html` is 2097 lines. Before adding ~600+ more lines for new entity UIs, extract the JavaScript to `frontend/app.js` and CSS to `frontend/app.css`. `index.html` becomes structural HTML only. This is a one-time refactor that makes all subsequent frontend tasks cleaner.

**Files:**
- Modify: `frontend/index.html`
- Create: `frontend/app.js`
- Create: `frontend/app.css`

- [ ] **Step 1: Extract all `<style>` content to `frontend/app.css`**

Copy everything between `<style>` and `</style>` (lines ~8–303) from `index.html` into `frontend/app.css` (without the style tags).

In `index.html`, replace the `<style>...</style>` block with:
```html
<link rel="stylesheet" href="app.css">
```

- [ ] **Step 2: Extract all `<script>` content to `frontend/app.js`**

Copy everything between `<script>` and `</script>` at the bottom of `index.html` into `frontend/app.js` (without the script tags).

In `index.html`, replace the `<script>...</script>` block with:
```html
<script src="app.js"></script>
```

- [ ] **Step 3: Add new nav items to `index.html` sidebar**

After the `<div class="nav-section">Monitor</div>` block (which contains Overview, Services, Identities, Routers, Policies, Terminators), add a new Policies sub-section and new nav items. Find the existing policy nav item and replace the entire Monitor section with:

```html
  <div class="nav-section">Monitor</div>
  <div class="nav-item active" data-page="overview">Overview <span class="badge" id="nav-services">-</span></div>
  <div class="nav-item" data-page="services">Services <span class="badge" id="nav-svc">-</span></div>
  <div class="nav-item" data-page="identities-combined">Identities <span class="badge" id="nav-identities-combined">-</span></div>
  <div class="nav-item" data-page="routers">Edge Routers <span class="badge" id="nav-routers">-</span></div>
  <div class="nav-item" data-page="transit-routers">Transit Routers <span class="badge" id="nav-transit">-</span></div>
  <div class="nav-item" data-page="terminators">Terminators</div>

  <div class="nav-section">Policies</div>
  <div class="nav-item" data-page="policies">Service Policies <span class="badge" id="nav-svc-policies">-</span></div>
  <div class="nav-item" data-page="edge-router-policies">Router Policies <span class="badge" id="nav-erp">-</span></div>
  <div class="nav-item" data-page="serp">Svc Edge Router <span class="badge" id="nav-serp">-</span></div>
  <div class="nav-item" data-page="auth-policies">Auth Policies <span class="badge" id="nav-auth">-</span></div>
  <div class="nav-item" data-page="posture-checks">Posture Checks <span class="badge" id="nav-posture">-</span></div>

  <div class="nav-section">Security</div>
  <div class="nav-item" data-page="cert-authorities">Cert Authorities <span class="badge" id="nav-ca">-</span></div>
  <div class="nav-item" data-page="ext-jwt-signers">JWT Signers <span class="badge" id="nav-jwt">-</span></div>

  <div class="nav-section">Sessions</div>
  <div class="nav-item" data-page="api-sessions">API Sessions <span class="badge" id="nav-api-sess">-</span></div>
  <div class="nav-item" data-page="sessions">Sessions <span class="badge" id="nav-sess">-</span></div>
```

- [ ] **Step 4: Add page div placeholders for each new entity in `index.html`**

After the `page-terminators` div, add:

```html
    <!-- ============== TRANSIT ROUTERS ============== -->
    <div class="page" id="page-transit-routers">
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:1rem">
        <span style="font-size:0.75rem;color:#555" id="transit-summary"></span>
        <button class="btn btn-primary" onclick="showCreateModal('transit-router')">+ new transit router</button>
      </div>
      <table><thead><tr><th>Name</th><th>Status</th><th>Roles</th><th style="width:240px">ID</th><th style="width:80px"></th></tr></thead>
      <tbody id="tb-transit-routers"></tbody></table>
    </div>

    <!-- ============== EDGE ROUTER POLICIES ============== -->
    <div class="page" id="page-edge-router-policies">
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:1rem">
        <span style="font-size:0.75rem;color:#555" id="erp-summary"></span>
        <button class="btn btn-primary" onclick="showCreateModal('edge-router-policy')">+ new policy</button>
      </div>
      <table><thead><tr><th>Name</th><th>Semantic</th><th>Identity Roles</th><th>Router Roles</th><th style="width:240px">ID</th><th style="width:80px"></th></tr></thead>
      <tbody id="tb-erp"></tbody></table>
    </div>

    <!-- ============== SERVICE EDGE ROUTER POLICIES ============== -->
    <div class="page" id="page-serp">
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:1rem">
        <span style="font-size:0.75rem;color:#555" id="serp-summary"></span>
        <button class="btn btn-primary" onclick="showCreateModal('serp')">+ new policy</button>
      </div>
      <table><thead><tr><th>Name</th><th>Semantic</th><th>Service Roles</th><th>Router Roles</th><th style="width:240px">ID</th><th style="width:80px"></th></tr></thead>
      <tbody id="tb-serp"></tbody></table>
    </div>

    <!-- ============== AUTH POLICIES ============== -->
    <div class="page" id="page-auth-policies">
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:1rem">
        <span style="font-size:0.75rem;color:#555" id="auth-summary"></span>
        <button class="btn btn-primary" onclick="showCreateModal('auth-policy')">+ new auth policy</button>
      </div>
      <table><thead><tr><th>Name</th><th>Primary</th><th>Secondary</th><th style="width:240px">ID</th><th style="width:80px"></th></tr></thead>
      <tbody id="tb-auth-policies"></tbody></table>
    </div>

    <!-- ============== POSTURE CHECKS ============== -->
    <div class="page" id="page-posture-checks">
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:1rem">
        <span style="font-size:0.75rem;color:#555" id="posture-summary"></span>
        <button class="btn btn-primary" onclick="showCreateModal('posture-check')">+ new posture check</button>
      </div>
      <table><thead><tr><th>Name</th><th>Type</th><th style="width:240px">ID</th><th style="width:80px"></th></tr></thead>
      <tbody id="tb-posture-checks"></tbody></table>
    </div>

    <!-- ============== CERT AUTHORITIES ============== -->
    <div class="page" id="page-cert-authorities">
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:1rem">
        <span style="font-size:0.75rem;color:#555" id="ca-summary"></span>
        <button class="btn btn-primary" onclick="showCreateModal('cert-authority')">+ new CA</button>
      </div>
      <table><thead><tr><th>Name</th><th>Fingerprint</th><th>Is Auto CA</th><th style="width:240px">ID</th><th style="width:80px"></th></tr></thead>
      <tbody id="tb-cas"></tbody></table>
    </div>

    <!-- ============== EXTERNAL JWT SIGNERS ============== -->
    <div class="page" id="page-ext-jwt-signers">
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:1rem">
        <span style="font-size:0.75rem;color:#555" id="jwt-summary"></span>
        <button class="btn btn-primary" onclick="showCreateModal('ext-jwt-signer')">+ new JWT signer</button>
      </div>
      <table><thead><tr><th>Name</th><th>Issuer</th><th>Audience</th><th>Enabled</th><th style="width:240px">ID</th><th style="width:80px"></th></tr></thead>
      <tbody id="tb-jwt-signers"></tbody></table>
    </div>

    <!-- ============== API SESSIONS ============== -->
    <div class="page" id="page-api-sessions">
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:1rem">
        <span style="font-size:0.75rem;color:#555" id="api-sess-summary"></span>
        <input type="text" id="api-sess-filter" placeholder="filter..." class="fb-input" style="width:200px;font-size:0.6875rem" oninput="renderApiSessions()">
      </div>
      <table><thead><tr><th>Identity</th><th>Token</th><th>Created</th><th>Last Activity</th><th style="width:80px"></th></tr></thead>
      <tbody id="tb-api-sessions"></tbody></table>
    </div>

    <!-- ============== SESSIONS ============== -->
    <div class="page" id="page-sessions">
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:1rem">
        <span style="font-size:0.75rem;color:#555" id="sess-summary"></span>
        <input type="text" id="sess-filter" placeholder="filter..." class="fb-input" style="width:200px;font-size:0.6875rem" oninput="renderSessions()">
      </div>
      <table><thead><tr><th>Service</th><th>Identity</th><th>Type</th><th>Created</th><th style="width:80px"></th></tr></thead>
      <tbody id="tb-sessions"></tbody></table>
    </div>
```

- [ ] **Step 5: Verify pages render in browser**

```bash
curl -s http://localhost:9090/ | grep "page-edge-router-policies"
```

Expected: finds the div.

- [ ] **Step 6: Commit**

```bash
cd ~/git/kore/ziti-dash
git add frontend/
git commit -m "feat: extract CSS/JS to modules, add nav and page stubs for all new entity types"
```

---

## Task 7: Frontend JS — wire up fetch + render for all new list pages

**Why:** Each new page needs a `load*` function that calls the BFF and a `render*` function that populates the table. Follow the exact pattern already used for services/routers.

**Files:**
- Modify: `frontend/app.js`

- [ ] **Step 1: Add API fetch calls and render functions to `app.js`**

Add these functions to `app.js`. Each follows the identical pattern of the existing `renderRouters` function:

```js
// ---- Transit Routers ----
let transitRouters = [];
async function loadTransitRouters() {
  const res = await api('/api/transit-routers');
  transitRouters = res.data || [];
  document.getElementById('nav-transit').textContent = transitRouters.length;
  renderTransitRouters();
}
function renderTransitRouters() {
  const tbody = document.querySelector('#tb-transit-routers');
  tbody.innerHTML = '';
  transitRouters.forEach(r => {
    const tr = document.createElement('tr');
    tr.innerHTML = `
      <td>${r.name}</td>
      <td><span class="dot ${r.isOnline ? 'dot-green' : 'dot-red'}"></span>${r.isOnline ? 'online' : 'offline'}</td>
      <td>${(r.roleAttributes||[]).map(a=>`<span class="tag">${a}</span>`).join('')}</td>
      <td class="id-cell">${r.id}</td>
      <td><button class="btn" style="font-size:0.625rem;padding:0.125rem 0.5rem" onclick="deleteEntity('/api/transit-routers','${r.id}', loadTransitRouters)">del</button></td>`;
    tbody.appendChild(tr);
  });
}

// ---- Edge Router Policies ----
let edgeRouterPolicies = [];
async function loadEdgeRouterPolicies() {
  const res = await api('/api/edge-router-policies');
  edgeRouterPolicies = (res.data || []);
  document.getElementById('nav-erp').textContent = edgeRouterPolicies.length;
  renderEdgeRouterPolicies();
}
function renderEdgeRouterPolicies() {
  const tbody = document.querySelector('#tb-erp');
  tbody.innerHTML = '';
  edgeRouterPolicies.forEach(p => {
    const tr = document.createElement('tr');
    tr.innerHTML = `
      <td>${p.name}</td>
      <td>${p.semantic||''}</td>
      <td>${(p.identityRoles||[]).map(r=>`<span class="tag">${r}</span>`).join('')}</td>
      <td>${(p.edgeRouterRoles||[]).map(r=>`<span class="tag tag-blue">${r}</span>`).join('')}</td>
      <td class="id-cell">${p.id}</td>
      <td><button class="btn" style="font-size:0.625rem;padding:0.125rem 0.5rem" onclick="deleteEntity('/api/edge-router-policies','${p.id}', loadEdgeRouterPolicies)">del</button></td>`;
    tbody.appendChild(tr);
  });
}

// ---- Service Edge Router Policies ----
let serpolicies = [];
async function loadSERP() {
  const res = await api('/api/service-edge-router-policies');
  serpolicies = (res.data || []);
  document.getElementById('nav-serp').textContent = serpolicies.length;
  renderSERP();
}
function renderSERP() {
  const tbody = document.querySelector('#tb-serp');
  tbody.innerHTML = '';
  serpolicies.forEach(p => {
    const tr = document.createElement('tr');
    tr.innerHTML = `
      <td>${p.name}</td>
      <td>${p.semantic||''}</td>
      <td>${(p.serviceRoles||[]).map(r=>`<span class="tag">${r}</span>`).join('')}</td>
      <td>${(p.edgeRouterRoles||[]).map(r=>`<span class="tag tag-blue">${r}</span>`).join('')}</td>
      <td class="id-cell">${p.id}</td>
      <td><button class="btn" style="font-size:0.625rem;padding:0.125rem 0.5rem" onclick="deleteEntity('/api/service-edge-router-policies','${p.id}', loadSERP)">del</button></td>`;
    tbody.appendChild(tr);
  });
}

// ---- Auth Policies ----
let authPolicies = [];
async function loadAuthPolicies() {
  const res = await api('/api/auth-policies');
  authPolicies = (res.data || []);
  document.getElementById('nav-auth').textContent = authPolicies.length;
  renderAuthPolicies();
}
function renderAuthPolicies() {
  const tbody = document.querySelector('#tb-auth-policies');
  tbody.innerHTML = '';
  authPolicies.forEach(p => {
    const tr = document.createElement('tr');
    tr.innerHTML = `
      <td>${p.name}</td>
      <td>${p.primary?.cert?.allowed ? 'cert' : p.primary?.extJwt?.allowed ? 'extJwt' : p.primary?.updb?.allowed ? 'updb' : '-'}</td>
      <td>${p.secondary?.requireTotp ? 'totp' : '-'}</td>
      <td class="id-cell">${p.id}</td>
      <td><button class="btn" style="font-size:0.625rem;padding:0.125rem 0.5rem" onclick="deleteEntity('/api/auth-policies','${p.id}', loadAuthPolicies)">del</button></td>`;
    tbody.appendChild(tr);
  });
}

// ---- Posture Checks ----
let postureChecks = [];
async function loadPostureChecks() {
  const res = await api('/api/posture-checks');
  postureChecks = (res.data || []);
  document.getElementById('nav-posture').textContent = postureChecks.length;
  renderPostureChecks();
}
function renderPostureChecks() {
  const tbody = document.querySelector('#tb-posture-checks');
  tbody.innerHTML = '';
  postureChecks.forEach(p => {
    const tr = document.createElement('tr');
    tr.innerHTML = `
      <td>${p.name}</td>
      <td><span class="tag tag-purple">${p.typeId||p.type?.name||'unknown'}</span></td>
      <td class="id-cell">${p.id}</td>
      <td><button class="btn" style="font-size:0.625rem;padding:0.125rem 0.5rem" onclick="deleteEntity('/api/posture-checks','${p.id}', loadPostureChecks)">del</button></td>`;
    tbody.appendChild(tr);
  });
}

// ---- Cert Authorities ----
let certAuthorities = [];
async function loadCertAuthorities() {
  const res = await api('/api/cert-authorities');
  certAuthorities = (res.data || []);
  document.getElementById('nav-ca').textContent = certAuthorities.length;
  renderCertAuthorities();
}
function renderCertAuthorities() {
  const tbody = document.querySelector('#tb-cas');
  tbody.innerHTML = '';
  certAuthorities.forEach(ca => {
    const tr = document.createElement('tr');
    tr.innerHTML = `
      <td>${ca.name}</td>
      <td class="id-cell" style="font-size:0.6rem">${ca.fingerprint||'-'}</td>
      <td>${ca.isAutoCaEnrollmentEnabled ? '<span class="dot dot-green"></span>yes' : 'no'}</td>
      <td class="id-cell">${ca.id}</td>
      <td><button class="btn" style="font-size:0.625rem;padding:0.125rem 0.5rem" onclick="deleteEntity('/api/cert-authorities','${ca.id}', loadCertAuthorities)">del</button></td>`;
    tbody.appendChild(tr);
  });
}

// ---- External JWT Signers ----
let extJwtSigners = [];
async function loadExtJwtSigners() {
  const res = await api('/api/ext-jwt-signers');
  extJwtSigners = (res.data || []);
  document.getElementById('nav-jwt').textContent = extJwtSigners.length;
  renderExtJwtSigners();
}
function renderExtJwtSigners() {
  const tbody = document.querySelector('#tb-jwt-signers');
  tbody.innerHTML = '';
  extJwtSigners.forEach(s => {
    const tr = document.createElement('tr');
    tr.innerHTML = `
      <td>${s.name}</td>
      <td>${s.issuer||'-'}</td>
      <td>${s.audience||'-'}</td>
      <td>${s.enabled ? '<span class="dot dot-green"></span>yes' : '<span class="dot dot-red"></span>no'}</td>
      <td class="id-cell">${s.id}</td>
      <td><button class="btn" style="font-size:0.625rem;padding:0.125rem 0.5rem" onclick="deleteEntity('/api/ext-jwt-signers','${s.id}', loadExtJwtSigners)">del</button></td>`;
    tbody.appendChild(tr);
  });
}

// ---- API Sessions ----
let apiSessionsData = [];
async function loadApiSessions() {
  const res = await api('/api/api-sessions');
  apiSessionsData = (res.data || []);
  document.getElementById('nav-api-sess').textContent = apiSessionsData.length;
  renderApiSessions();
}
function renderApiSessions() {
  const filter = document.getElementById('api-sess-filter')?.value?.toLowerCase() || '';
  const tbody = document.querySelector('#tb-api-sessions');
  tbody.innerHTML = '';
  apiSessionsData.filter(s => !filter || (s.identity?.name||'').toLowerCase().includes(filter)).forEach(s => {
    const tr = document.createElement('tr');
    tr.innerHTML = `
      <td>${s.identity?.name||'-'}</td>
      <td class="id-cell">${(s.token||'').substring(0,20)}...</td>
      <td>${s.createdAt ? new Date(s.createdAt).toLocaleString() : '-'}</td>
      <td>${s.lastActivityAt ? new Date(s.lastActivityAt).toLocaleString() : '-'}</td>
      <td><button class="btn" style="font-size:0.625rem;padding:0.125rem 0.5rem;color:var(--red)" onclick="deleteEntity('/api/api-sessions','${s.id}', loadApiSessions)">kill</button></td>`;
    tbody.appendChild(tr);
  });
}

// ---- Sessions ----
let sessionsData = [];
async function loadSessions() {
  const res = await api('/api/sessions');
  sessionsData = (res.data || []);
  document.getElementById('nav-sess').textContent = sessionsData.length;
  renderSessions();
}
function renderSessions() {
  const filter = document.getElementById('sess-filter')?.value?.toLowerCase() || '';
  const tbody = document.querySelector('#tb-sessions');
  tbody.innerHTML = '';
  sessionsData.filter(s => !filter || (s.serviceName||'').toLowerCase().includes(filter)).forEach(s => {
    const tr = document.createElement('tr');
    tr.innerHTML = `
      <td>${s.serviceName||s.service?.name||'-'}</td>
      <td>${s.identity?.name||'-'}</td>
      <td><span class="tag">${s.type||'-'}</span></td>
      <td>${s.createdAt ? new Date(s.createdAt).toLocaleString() : '-'}</td>
      <td><button class="btn" style="font-size:0.625rem;padding:0.125rem 0.5rem;color:var(--red)" onclick="deleteEntity('/api/sessions','${s.id}', loadSessions)">kill</button></td>`;
    tbody.appendChild(tr);
  });
}

// ---- Generic delete helper ----
async function deleteEntity(apiPath, id, reloadFn) {
  if (!confirm('Delete this entity? This cannot be undone.')) return;
  try {
    const resp = await fetch(apiPath + '/' + id, { method: 'DELETE' });
    if (resp.status === 204 || resp.ok) {
      reloadFn();
    } else {
      const err = await resp.json().catch(() => ({error: 'unknown error'}));
      alert('Delete failed: ' + (err.error || resp.status));
    }
  } catch(e) {
    alert('Delete failed: ' + e.message);
  }
}
```

- [ ] **Step 2: Wire new loaders into the `loadAll()` function**

In `app.js`, find the `loadAll()` function (or equivalent that runs on page load and refresh). Add calls for all new loaders. The existing function likely does a `Promise.all([...])`. Add:

```js
loadTransitRouters(),
loadEdgeRouterPolicies(),
loadSERP(),
loadAuthPolicies(),
loadPostureChecks(),
loadCertAuthorities(),
loadExtJwtSigners(),
loadApiSessions(),
loadSessions(),
```

- [ ] **Step 3: Verify pages load without JS errors**

Open http://localhost:9090 in the browser. Click each new nav item. Open the browser console (F12) and confirm no errors. Each table should either show rows or be empty — not throw an error.

- [ ] **Step 4: Commit**

```bash
cd ~/git/kore/ziti-dash
git add frontend/app.js
git commit -m "feat: add fetch and render functions for all new entity list pages"
```

---

## Task 8: Frontend — CRUD modals for create/edit on core entities

**Why:** Every entity needs at minimum a "create" modal. The existing modal overlay pattern is already in `app.css`. This task wires up create forms for the most important entities (identities, services, edge routers, service policies), keeping forms minimal but functional — just the required fields.

**Files:**
- Modify: `frontend/index.html` (modal HTML)
- Modify: `frontend/app.js` (modal open/submit logic)

- [ ] **Step 1: Add create modals to `index.html` before `</body>`**

```html
<!-- ============== CREATE MODAL ============== -->
<div class="modal-overlay" id="create-modal">
  <div class="modal-content" style="width:520px">
    <div class="modal-header">
      <h2 id="create-modal-title">Create</h2>
      <button class="modal-close" onclick="closeCreateModal()">×</button>
    </div>
    <div class="modal-body" id="create-modal-body"></div>
    <div style="padding:1rem 1.25rem;border-top:1px solid var(--border);display:flex;gap:0.5rem;justify-content:flex-end">
      <button class="btn" onclick="closeCreateModal()">cancel</button>
      <button class="btn btn-primary" id="create-modal-submit" onclick="submitCreateModal()">create</button>
    </div>
  </div>
</div>

<!-- ============== JWT DISPLAY MODAL ============== -->
<div class="modal-overlay" id="jwt-modal">
  <div class="modal-content" style="width:560px">
    <div class="modal-header">
      <h2>Enrollment JWT</h2>
      <button class="modal-close" onclick="document.getElementById('jwt-modal').classList.remove('open')">×</button>
    </div>
    <div class="modal-body">
      <p style="font-size:0.75rem;color:#888;margin-bottom:1rem">Copy this JWT token and use it to enroll the identity. It expires in 24 hours.</p>
      <textarea id="jwt-display" readonly style="width:100%;height:120px;background:#080808;border:1px solid var(--border);color:var(--accent);font-family:var(--mono);font-size:0.625rem;padding:0.5rem;border-radius:4px;resize:none"></textarea>
      <button class="btn btn-primary" style="margin-top:0.75rem;width:100%" onclick="copyJWT()">copy to clipboard</button>
    </div>
  </div>
</div>
```

- [ ] **Step 2: Add modal JS to `app.js`**

```js
// ---- Create modal system ----
let currentCreateType = null;

const createForms = {
  'identity': {
    title: 'Create Identity',
    fields: `
      <label class="fb-label">Name *</label>
      <input class="fb-input" id="cf-name" placeholder="my-identity">
      <label class="fb-label">Type</label>
      <select class="fb-input" id="cf-type">
        <option value="Default">Default</option>
        <option value="Router">Router</option>
        <option value="Host">Host</option>
        <option value="User">User</option>
      </select>
      <label class="fb-label">Role Attributes (comma-separated)</label>
      <input class="fb-input" id="cf-roles" placeholder="#role1, #role2">
      <label class="fb-label" style="margin-top:0.75rem">
        <input type="checkbox" id="cf-isAdmin"> Admin identity
      </label>`,
    build: () => ({
      name: document.getElementById('cf-name').value.trim(),
      type: { name: document.getElementById('cf-type').value },
      isAdmin: document.getElementById('cf-isAdmin').checked,
      roleAttributes: document.getElementById('cf-roles').value.split(',').map(s=>s.trim()).filter(Boolean),
      enrollment: { ott: true }
    }),
    endpoint: '/api/identities',
    onSuccess: async (data) => {
      loadAll();
      const jwt = data?.data?.enrollment?.ott?.jwt;
      if (jwt) showJWTModal(jwt);
    }
  },
  'service': {
    title: 'Create Service',
    fields: `
      <label class="fb-label">Name *</label>
      <input class="fb-input" id="cf-name" placeholder="my-service">
      <label class="fb-label">Role Attributes (comma-separated)</label>
      <input class="fb-input" id="cf-roles" placeholder="#role1, #role2">
      <label class="fb-label">Terminator Strategy</label>
      <select class="fb-input" id="cf-strategy">
        <option value="smartrouting">smartrouting</option>
        <option value="weighted">weighted</option>
        <option value="random">random</option>
        <option value="ha">ha</option>
      </select>`,
    build: () => ({
      name: document.getElementById('cf-name').value.trim(),
      roleAttributes: document.getElementById('cf-roles').value.split(',').map(s=>s.trim()).filter(Boolean),
      terminatorStrategy: document.getElementById('cf-strategy').value,
      encryptionRequired: true
    }),
    endpoint: '/api/services',
    onSuccess: () => loadAll()
  },
  'edge-router': {
    title: 'Create Edge Router',
    fields: `
      <label class="fb-label">Name *</label>
      <input class="fb-input" id="cf-name" placeholder="my-router">
      <label class="fb-label">Role Attributes (comma-separated)</label>
      <input class="fb-input" id="cf-roles" placeholder="#role1, #role2">
      <label class="fb-label" style="margin-top:0.75rem">
        <input type="checkbox" id="cf-tunneler"> Is tunneler enabled
      </label>`,
    build: () => ({
      name: document.getElementById('cf-name').value.trim(),
      roleAttributes: document.getElementById('cf-roles').value.split(',').map(s=>s.trim()).filter(Boolean),
      isTunnelerEnabled: document.getElementById('cf-tunneler').checked
    }),
    endpoint: '/api/routers',
    onSuccess: () => loadAll()
  },
  'service-policy': {
    title: 'Create Service Policy',
    fields: `
      <label class="fb-label">Name *</label>
      <input class="fb-input" id="cf-name" placeholder="my-policy">
      <label class="fb-label">Type</label>
      <select class="fb-input" id="cf-ptype">
        <option value="Dial">Dial</option>
        <option value="Bind">Bind</option>
      </select>
      <label class="fb-label">Semantic</label>
      <select class="fb-input" id="cf-semantic">
        <option value="AnyOf">AnyOf</option>
        <option value="AllOf">AllOf</option>
      </select>
      <label class="fb-label">Identity Roles (comma-separated)</label>
      <input class="fb-input" id="cf-iroles" placeholder="#role1">
      <label class="fb-label">Service Roles (comma-separated)</label>
      <input class="fb-input" id="cf-sroles" placeholder="#role1">`,
    build: () => ({
      name: document.getElementById('cf-name').value.trim(),
      type: document.getElementById('cf-ptype').value,
      semantic: document.getElementById('cf-semantic').value,
      identityRoles: document.getElementById('cf-iroles').value.split(',').map(s=>s.trim()).filter(Boolean),
      serviceRoles: document.getElementById('cf-sroles').value.split(',').map(s=>s.trim()).filter(Boolean)
    }),
    endpoint: '/api/policies',
    onSuccess: () => loadAll()
  },
  'edge-router-policy': {
    title: 'Create Edge Router Policy',
    fields: `
      <label class="fb-label">Name *</label>
      <input class="fb-input" id="cf-name" placeholder="my-erp">
      <label class="fb-label">Semantic</label>
      <select class="fb-input" id="cf-semantic">
        <option value="AnyOf">AnyOf</option>
        <option value="AllOf">AllOf</option>
      </select>
      <label class="fb-label">Identity Roles (comma-separated)</label>
      <input class="fb-input" id="cf-iroles" placeholder="#role1">
      <label class="fb-label">Edge Router Roles (comma-separated)</label>
      <input class="fb-input" id="cf-rroles" placeholder="#role1">`,
    build: () => ({
      name: document.getElementById('cf-name').value.trim(),
      semantic: document.getElementById('cf-semantic').value,
      identityRoles: document.getElementById('cf-iroles').value.split(',').map(s=>s.trim()).filter(Boolean),
      edgeRouterRoles: document.getElementById('cf-rroles').value.split(',').map(s=>s.trim()).filter(Boolean)
    }),
    endpoint: '/api/edge-router-policies',
    onSuccess: () => loadEdgeRouterPolicies()
  },
  'serp': {
    title: 'Create Service Edge Router Policy',
    fields: `
      <label class="fb-label">Name *</label>
      <input class="fb-input" id="cf-name" placeholder="my-serp">
      <label class="fb-label">Semantic</label>
      <select class="fb-input" id="cf-semantic">
        <option value="AnyOf">AnyOf</option>
        <option value="AllOf">AllOf</option>
      </select>
      <label class="fb-label">Service Roles (comma-separated)</label>
      <input class="fb-input" id="cf-sroles" placeholder="#role1">
      <label class="fb-label">Edge Router Roles (comma-separated)</label>
      <input class="fb-input" id="cf-rroles" placeholder="#role1">`,
    build: () => ({
      name: document.getElementById('cf-name').value.trim(),
      semantic: document.getElementById('cf-semantic').value,
      serviceRoles: document.getElementById('cf-sroles').value.split(',').map(s=>s.trim()).filter(Boolean),
      edgeRouterRoles: document.getElementById('cf-rroles').value.split(',').map(s=>s.trim()).filter(Boolean)
    }),
    endpoint: '/api/service-edge-router-policies',
    onSuccess: () => loadSERP()
  },
  'auth-policy': {
    title: 'Create Auth Policy',
    fields: `
      <label class="fb-label">Name *</label>
      <input class="fb-input" id="cf-name" placeholder="my-auth-policy">
      <p style="font-size:0.6875rem;color:#555;margin-top:0.5rem">Creates a basic auth policy. Edit in Ziti CLI for advanced configuration.</p>`,
    build: () => ({
      name: document.getElementById('cf-name').value.trim(),
      primary: { cert: { allowed: true, allowExpiredCerts: false }, extJwt: { allowed: false, allowedSigners: [] }, updb: { allowed: false, minPasswordLength: 5, requireSpecialChar: false, requireNumberChar: false, requireMixedCase: false, maxAttempts: 5, lockoutDurationMinutes: 0 } },
      secondary: { requireTotp: false, requireExtJwtSigner: null }
    }),
    endpoint: '/api/auth-policies',
    onSuccess: () => loadAuthPolicies()
  },
  'ext-jwt-signer': {
    title: 'Create External JWT Signer',
    fields: `
      <label class="fb-label">Name *</label>
      <input class="fb-input" id="cf-name" placeholder="my-signer">
      <label class="fb-label">Issuer *</label>
      <input class="fb-input" id="cf-issuer" placeholder="https://accounts.example.com">
      <label class="fb-label">Audience</label>
      <input class="fb-input" id="cf-audience" placeholder="my-app">
      <label class="fb-label">JWKS Endpoint *</label>
      <input class="fb-input" id="cf-jwks" placeholder="https://accounts.example.com/.well-known/jwks.json">
      <label class="fb-label">Claims Property (JWT field for identity name)</label>
      <input class="fb-input" id="cf-claims" placeholder="email">`,
    build: () => ({
      name: document.getElementById('cf-name').value.trim(),
      issuer: document.getElementById('cf-issuer').value.trim(),
      audience: document.getElementById('cf-audience').value.trim() || null,
      jwksEndpoint: document.getElementById('cf-jwks').value.trim(),
      claimsProperty: document.getElementById('cf-claims').value.trim() || 'sub',
      enabled: true,
      useExternalId: false
    }),
    endpoint: '/api/ext-jwt-signers',
    onSuccess: () => loadExtJwtSigners()
  },
  'cert-authority': {
    title: 'Create Certificate Authority',
    fields: `
      <label class="fb-label">Name *</label>
      <input class="fb-input" id="cf-name" placeholder="my-ca">
      <label class="fb-label">PEM Certificate *</label>
      <textarea class="fb-input" id="cf-pem" style="height:120px;resize:vertical" placeholder="-----BEGIN CERTIFICATE-----"></textarea>
      <label class="fb-label" style="margin-top:0.5rem">
        <input type="checkbox" id="cf-autoenroll"> Auto CA enrollment
      </label>`,
    build: () => ({
      name: document.getElementById('cf-name').value.trim(),
      certPem: document.getElementById('cf-pem').value.trim(),
      isAutoCaEnrollmentEnabled: document.getElementById('cf-autoenroll').checked,
      isOttCaEnrollmentEnabled: true,
      isAuthEnabled: true
    }),
    endpoint: '/api/cert-authorities',
    onSuccess: () => loadCertAuthorities()
  },
  'posture-check': {
    title: 'Create Posture Check',
    fields: `
      <label class="fb-label">Name *</label>
      <input class="fb-input" id="cf-name" placeholder="my-posture-check">
      <label class="fb-label">Type</label>
      <select class="fb-input" id="cf-pctype">
        <option value="OS">OS</option>
        <option value="DOMAIN">Domain</option>
        <option value="PROCESS">Process</option>
        <option value="MAC">MAC Address</option>
        <option value="MFA">MFA</option>
      </select>
      <p style="font-size:0.6875rem;color:#555;margin-top:0.5rem">Creates a basic posture check. Edit in Ziti CLI for type-specific config.</p>`,
    build: () => ({
      name: document.getElementById('cf-name').value.trim(),
      typeId: document.getElementById('cf-pctype').value
    }),
    endpoint: '/api/posture-checks',
    onSuccess: () => loadPostureChecks()
  },
  'transit-router': {
    title: 'Create Transit Router',
    fields: `
      <label class="fb-label">Name *</label>
      <input class="fb-input" id="cf-name" placeholder="my-transit-router">
      <label class="fb-label" style="margin-top:0.5rem">
        <input type="checkbox" id="cf-notraversal"> No traversal
      </label>`,
    build: () => ({
      name: document.getElementById('cf-name').value.trim(),
      noTraversal: document.getElementById('cf-notraversal').checked
    }),
    endpoint: '/api/transit-routers',
    onSuccess: () => loadTransitRouters()
  }
};

function showCreateModal(type) {
  const form = createForms[type];
  if (!form) return;
  currentCreateType = type;
  document.getElementById('create-modal-title').textContent = form.title;
  document.getElementById('create-modal-body').innerHTML = form.fields;
  document.getElementById('create-modal').classList.add('open');
}

function closeCreateModal() {
  document.getElementById('create-modal').classList.remove('open');
  currentCreateType = null;
}

async function submitCreateModal() {
  const form = createForms[currentCreateType];
  if (!form) return;
  const btn = document.getElementById('create-modal-submit');
  btn.disabled = true;
  btn.textContent = 'creating...';
  try {
    const payload = form.build();
    const resp = await fetch(form.endpoint, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    const data = await resp.json().catch(() => ({}));
    if (!resp.ok) {
      alert('Create failed: ' + (data.error || JSON.stringify(data).substring(0, 200)));
      return;
    }
    closeCreateModal();
    if (form.onSuccess) await form.onSuccess(data);
  } catch(e) {
    alert('Create failed: ' + e.message);
  } finally {
    btn.disabled = false;
    btn.textContent = 'create';
  }
}

function showJWTModal(jwt) {
  document.getElementById('jwt-display').value = jwt;
  document.getElementById('jwt-modal').classList.add('open');
}

function copyJWT() {
  const ta = document.getElementById('jwt-display');
  navigator.clipboard.writeText(ta.value).then(() => {
    const btn = ta.nextElementSibling;
    btn.textContent = 'copied!';
    setTimeout(() => btn.textContent = 'copy to clipboard', 2000);
  });
}
```

Also update the existing services/routers/policies tables to include a "create" button in their headers (find their existing page divs and add `<button class="btn btn-primary" onclick="showCreateModal('service')">+ new service</button>` etc. to the header bar).

- [ ] **Step 3: Wire up existing entity create buttons**

In `index.html`, find `page-services`, `page-routers`, `page-policies` divs and ensure each has a create button calling `showCreateModal` with the right type. If the div currently has no header bar, add:

For services:
```html
<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:1rem">
  <span style="font-size:0.75rem;color:#555"></span>
  <button class="btn btn-primary" onclick="showCreateModal('service')">+ new service</button>
</div>
```

For routers:
```html
<button class="btn btn-primary" onclick="showCreateModal('edge-router')">+ new edge router</button>
```

For policies:
```html
<button class="btn btn-primary" onclick="showCreateModal('service-policy')">+ new service policy</button>
```

For identities — the combined identities page already has a header. Add the create button next to the search input:
```html
<button class="btn btn-primary" onclick="showCreateModal('identity')">+ new identity</button>
```

- [ ] **Step 4: Test create flow end-to-end**

In the browser at http://localhost:9090:
1. Click "Services" → click "+ new service" → fill in a test name → click "create"
2. Verify the service appears in the table immediately after creation
3. Click "Identities" → click "+ new identity" → fill in name → click "create"
4. Verify the JWT modal appears with a non-empty token
5. Click "copy to clipboard" — verify it says "copied!"

- [ ] **Step 5: Commit**

```bash
cd ~/git/kore/ziti-dash
git add frontend/
git commit -m "feat: add create modals and JWT display for all entity types"
```

---

## Task 9: Add delete buttons to existing entity tables (services, routers, policies, terminators, identities)

**Why:** The existing tables render name/ID/roles but have no action column. This task adds a "del" button to each existing table row using the `deleteEntity` helper added in Task 7.

**Files:**
- Modify: `frontend/app.js` (update existing render functions)

- [ ] **Step 1: Update existing render functions to include delete buttons**

In `app.js`, find each of the existing render functions and add a delete column. The pattern is identical for each:

For `renderServices` (find the existing tbody population code):
```js
// In the row innerHTML, add after existing columns:
`<td><button class="btn" style="font-size:0.625rem;padding:0.125rem 0.5rem" onclick="deleteEntity('/api/services','${svc.id}', loadAll)">del</button></td>`
```
Also add `<th style="width:80px"></th>` to the services table `<thead>` in `index.html`.

Apply the same pattern for:
- Services → `deleteEntity('/api/services', id, loadAll)`
- Edge Routers → `deleteEntity('/api/routers', id, loadAll)`  
- Service Policies → `deleteEntity('/api/policies', id, loadAll)`
- Terminators → `deleteEntity('/api/terminators', id, loadAll)`
- Identities combined grid — add a small "×" delete button to each identity card

- [ ] **Step 2: Test deletion**

In the browser:
1. Create a test service via the create modal
2. Click "del" on that service row
3. Confirm the dialog → verify row disappears

- [ ] **Step 3: Commit**

```bash
cd ~/git/kore/ziti-dash
git add frontend/
git commit -m "feat: add delete buttons to all existing entity tables"
```

---

## Task 10: Rebuild container and verify full feature set

- [ ] **Step 1: Rebuild and restart**

```bash
cd ~/git/kore/ziti-dash
make build && docker restart ziti-dash && sleep 3
```

- [ ] **Step 2: Verify all API endpoints**

```bash
for ep in overview services identities routers policies terminators edge-router-policies service-edge-router-policies auth-policies cert-authorities ext-jwt-signers posture-checks transit-routers api-sessions sessions; do
  status=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:9090/api/$ep)
  echo "$ep: $status"
done
```

Expected: all return `200`.

- [ ] **Step 3: Smoke-test UI — verify nav and page loads**

Open http://localhost:9090 in a browser. Manually navigate to every nav section and confirm:
- No blank pages or JS errors in console (F12)
- Tables render (empty is fine if no data exists)
- Create buttons open modals
- Modals close on cancel

- [ ] **Step 4: Final commit**

```bash
cd ~/git/kore/ziti-dash
git add -A
git commit -m "feat: ZAC feature parity complete — full CRUD for all Ziti entity types"
```

---

## Self-Review Notes

**Spec coverage check:**
- ✅ Identities CRUD + JWT token download
- ✅ Services CRUD
- ✅ Edge Routers CRUD
- ✅ Service Policies CRUD
- ✅ Edge Router Policies (new plugin)
- ✅ Service Edge Router Policies (new plugin)
- ✅ Auth Policies CRUD
- ✅ Certificate Authorities CRUD
- ✅ External JWT Signers CRUD
- ✅ Posture Checks CRUD
- ✅ Transit Routers CRUD
- ✅ API Sessions list + kill
- ✅ Sessions list + kill
- ✅ Terminators CRUD (via dashboard plugin)
- ✅ SDK client swap (Task 1)
- ✅ Frontend modularization (Task 6)
- ❌ Fabric circuit/link map (ZAC v4.2.0 feature) — excluded intentionally, requires separate Fabric API integration; add as follow-up

**Type consistency:** `deleteEntity(path, id, reloadFn)` is defined in Task 7 and called identically in Tasks 8 and 9. `showCreateModal(type)` defined in Task 8, called from button `onclick` attributes added in Tasks 6 and 8. `loadAll()` referenced in Task 7 — this is the existing function in `app.js`, matches existing codebase.

**Placeholder check:** No TBDs. All forms show complete field lists and `build()` functions. All API paths are concrete strings.
