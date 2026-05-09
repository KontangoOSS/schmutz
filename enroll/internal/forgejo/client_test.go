package forgejo_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"git.konoss.org/kore/schmutz/enroll/internal/forgejo"
)

// fakeForgejo is a minimal httptest server that emulates the Forgejo API
// endpoints the client uses. Tests use it to verify request shapes and
// error handling without needing a real Forgejo instance.
type fakeForgejo struct {
	t      *testing.T
	server *httptest.Server

	// repos is the list of repos in the org.
	repos []fakeRepo
}

type fakeRepo struct {
	name     string
	files    map[string]string // path -> yaml content
	archived bool
}

func newFakeForgejo(t *testing.T, org string, repos []fakeRepo) *fakeForgejo {
	f := &fakeForgejo{t: t, repos: repos}
	mux := http.NewServeMux()

	// GET /api/v1/repos/search — list repos
	mux.HandleFunc("/api/v1/repos/search", func(w http.ResponseWriter, r *http.Request) {
		owner := r.URL.Query().Get("owner")
		type repoItem struct {
			Name        string      `json:"name"`
			Description string      `json:"description"`
			HTMLURL     string      `json:"html_url"`
			Archived    bool        `json:"archived"`
			Owner       interface{} `json:"owner"`
		}
		var items []repoItem
		for _, repo := range f.repos {
			if !repo.archived {
				items = append(items, repoItem{
					Name:     repo.name,
					HTMLURL:  "http://git.test/" + owner + "/" + repo.name,
					Archived: false,
					Owner:    map[string]string{"login": owner},
				})
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": items, "ok": true})
	})

	// GET /api/v1/repos/{org}/{repo}/contents/{path} — read file
	mux.HandleFunc("/api/v1/repos/", func(w http.ResponseWriter, r *http.Request) {
		// Parse: /api/v1/repos/<org>/<repo>/contents/<path>
		parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/api/v1/repos/"), "/", 4)
		if len(parts) < 4 || parts[2] != "contents" {
			http.NotFound(w, r)
			return
		}
		repoName := parts[1]
		filePath := parts[3]

		var found *fakeRepo
		for i := range f.repos {
			if f.repos[i].name == repoName {
				found = &f.repos[i]
				break
			}
		}
		if found == nil {
			http.NotFound(w, r)
			return
		}
		content, ok := found.files[filePath]
		if !ok {
			http.NotFound(w, r)
			return
		}

		if r.Method == "PUT" {
			// Write: validate body has sha+content
			var req struct {
				SHA     string `json:"sha"`
				Content string `json:"content"`
				Message string `json:"message"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.SHA == "" {
				w.WriteHeader(http.StatusUnprocessableEntity)
				return
			}
			decoded, _ := base64.StdEncoding.DecodeString(strings.ReplaceAll(req.Content, "\n", ""))
			found.files[filePath] = string(decoded)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"content": req.Content})
			return
		}

		// GET: return file
		encoded := base64.StdEncoding.EncodeToString([]byte(content))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content":  encoded,
			"encoding": "base64",
			"sha":      "fake-sha-" + repoName + "-" + strings.ReplaceAll(filePath, "/", "_"),
		})
	})

	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeForgejo) client(org string) *forgejo.Client {
	return forgejo.NewClient(f.server.URL, org, "test-token", 0)
}

// ---- tests ----

const inventreeBlueprint = `{
  "app_id": "inventree",
  "uuid": "d0afad25-f68e-4c79-b572-739e04d7d789",
  "schema_version": 2,
  "identity": {"display_name": "InvenTree", "description": "Open-source inventory management",
               "category": "operations", "license": "mit"},
  "links": {"github": "https://github.com/inventree/InvenTree"},
  "sizing": {"min": "tg-sm-1", "recommended": "tg-md-1", "max": "tg-lg-1"},
  "deployment": {
    "compatible_platforms": ["proxmox/lxc/medium", "proxmox/lxc/large"],
    "docker_supported": true
  },
  "runtime": {
    "bao_secrets_path": "kontango/secret/apps/inventree/{deployment}",
    "secret_requirements": [
      {"ref": "composites/database_credentials", "group": "database"},
      {"ref": "composites/admin_user", "group": "admin"}
    ],
    "health": {"url": "http://localhost:8000/api/", "expect_status": 200}
  },
  "catalog": {"added": "2026-04-27", "maintainer": "kontango", "active": true}
}`

const inventreeDeployment = `{
  "tenant": "kontango",
  "app": "inventree",
  "deployment": "prod-01",
  "flavor": "app-host",
  "status": "pending",
  "ziti_identity": "",
  "entity_id": "",
  "approved_by": "dillon",
  "approved_at": "2026-05-06T00:00:00Z",
  "claimed_at": ""
}`

func TestListApps_ReturnsActiveApps(t *testing.T) {
	f := newFakeForgejo(t, "public", []fakeRepo{
		{
			name: "inventree",
			files: map[string]string{
				"tango.json": inventreeBlueprint,
			},
		},
		{
			name: "no-blueprint", // repo without blueprint.yaml
		},
		{
			name:     "archived-app",
			archived: true,
			files:    map[string]string{"tango.json": inventreeBlueprint},
		},
	})
	cli := f.client("public")
	apps, err := cli.ListApps(context.Background())
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	if len(apps) != 1 {
		t.Fatalf("expected 1 app, got %d: %v", len(apps), apps)
	}
	if apps[0].AppID != "inventree" {
		t.Errorf("AppID: got %q want %q", apps[0].AppID, "inventree")
	}
	if apps[0].DisplayName != "InvenTree" {
		t.Errorf("DisplayName: got %q", apps[0].DisplayName)
	}
}

func TestGetBlueprint_ParsesRealShape(t *testing.T) {
	f := newFakeForgejo(t, "public", []fakeRepo{
		{name: "inventree", files: map[string]string{"tango.json": inventreeBlueprint}},
	})
	bp, err := f.client("public").GetTango(context.Background(), "inventree")
	if err != nil {
		t.Fatalf("GetTango: %v", err)
	}
	if !bp.Catalog.Active {
		t.Error("Active must be true")
	}
	if bp.AppID != "inventree" {
		t.Errorf("AppID: %q", bp.AppID)
	}
	if len(bp.Runtime.SecretRequirements) != 2 {
		t.Errorf("SecretRequirements: got %d", len(bp.Runtime.SecretRequirements))
	}
	if err := bp.Validate(); err != nil {
		t.Errorf("blueprint failed Validate: %v", err)
	}
}

func TestGetBlueprint_NotFound(t *testing.T) {
	f := newFakeForgejo(t, "public", []fakeRepo{{name: "empty"}})
	_, err := f.client("public").GetTango(context.Background(), "empty")
	if err != forgejo.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestGetDeployment_PendingStatus(t *testing.T) {
	f := newFakeForgejo(t, "public", []fakeRepo{
		{name: "inventree", files: map[string]string{
			"deployments/kontango/prod-01/deployment.json": inventreeDeployment,
		}},
	})
	rec, err := f.client("public").GetDeployment(context.Background(), "inventree", "kontango", "prod-01")
	if err != nil {
		t.Fatalf("GetDeployment: %v", err)
	}
	if rec.Status != "pending" {
		t.Errorf("Status: got %q want pending", rec.Status)
	}
	if rec.Tenant != "kontango" || rec.App != "inventree" || rec.Deployment != "prod-01" {
		t.Errorf("identity fields: %+v", rec)
	}
	// ZitiIdentity and Host are no longer in DeploymentRecord — they live in Bao.
	_ = rec
}

func TestGetDeployment_NotFound(t *testing.T) {
	f := newFakeForgejo(t, "public", []fakeRepo{{name: "inventree"}})
	_, err := f.client("public").GetDeployment(context.Background(), "inventree", "kontango", "prod-99")
	if err != forgejo.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestGetSubstrate_FallsBackToRoot(t *testing.T) {
	rootSubstrate := `
version: 1
tenant: ""
app: inventree
deployment: ""
ziti_identity: ""
binds:
  - service: inventree.tango
    local_addr: 127.0.0.1:8000
    proto: tcp
`
	f := newFakeForgejo(t, "public", []fakeRepo{
		{name: "inventree", files: map[string]string{
			"schmutz.yml": rootSubstrate,
			// no deployments/<tenant>/<dep>/schmutz.yml
		}},
	})
	spec, err := f.client("public").GetSchmutz(context.Background(), "inventree", "kontango", "prod-01")
	if err != nil {
		t.Fatalf("GetSchmutz: %v", err)
	}
	if len(spec.Binds) != 1 || spec.Binds[0].Service != "inventree.tango" {
		t.Errorf("binds: %+v", spec.Binds)
	}
}

// UpdateDeployment only writes status to git — runtime fields go to Bao.
func TestUpdateDeployment_WritesClaimed(t *testing.T) {
	f := newFakeForgejo(t, "public", []fakeRepo{
		{name: "inventree", files: map[string]string{
			"deployments/kontango/prod-01/deployment.json": inventreeDeployment,
		}},
	})
	err := f.client("public").UpdateDeployment(
		context.Background(), "inventree", "kontango", "prod-01",
		"chore: mark prod-01 as active",
		map[string]string{"status": "active"},
	)
	if err != nil {
		t.Fatalf("UpdateDeployment: %v", err)
	}
	updated := f.repos[0].files["deployments/kontango/prod-01/deployment.json"]
	if !strings.Contains(updated, "active") {
		t.Errorf("status=active not in updated file: %s", updated)
	}
	// ziti_identity must NOT appear in git — it lives in Bao.
	if strings.Contains(updated, "ziti_identity") {
		t.Errorf("ziti_identity must not be written to git: %s", updated)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Error handling + regression tests
// ──────────────────────────────────────────────────────────────────────────────

// Forgejo unreachable → error wraps the network failure clearly.
func TestClient_ForgejoDown(t *testing.T) {
	cli := forgejo.NewClient("http://127.0.0.1:1", "public", "tok", 0)
	_, err := cli.GetTango(context.Background(), "inventree")
	if err == nil || !strings.Contains(err.Error(), "forgejo: call") {
		t.Errorf("expected wrapped network error, got %v", err)
	}
}

// Missing tango.json returns ErrNotFound (not a parse error).
func TestGetBlueprint_MissingFile(t *testing.T) {
	f := newFakeForgejo(t, "public", []fakeRepo{{name: "inventree"}})
	_, err := f.client("public").GetTango(context.Background(), "inventree")
	if err != forgejo.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// Malformed tango.json returns a parse error, not ErrNotFound.
func TestGetBlueprint_MalformedJSON(t *testing.T) {
	f := newFakeForgejo(t, "public", []fakeRepo{
		{name: "inventree", files: map[string]string{
			"tango.json": `{"app_id": "inventree", INVALID JSON`,
		}},
	})
	_, err := f.client("public").GetTango(context.Background(), "inventree")
	if err == nil {
		t.Error("expected parse error for malformed JSON")
	}
	if err == forgejo.ErrNotFound {
		t.Error("parse error must not be ErrNotFound")
	}
}

// Deployment.json only carries approval-gate fields — runtime details are in Bao.
func TestGetDeployment_FullTypedRecord(t *testing.T) {
	// Old format with host/ziti_identity included — unknown fields are ignored.
	full := `{
		"tenant": "kontango", "app": "ticketarr", "deployment": "prod-01",
		"flavor": "app-host", "status": "active", "version": "latest",
		"platform": "proxmox/lxc/medium",
		"approved_by": "dillon", "approved_at": "2026-05-07T21:30:00Z"
	}`
	f := newFakeForgejo(t, "public", []fakeRepo{
		{name: "ticketarr", files: map[string]string{
			"deployments/kontango/prod-01/deployment.json": full,
		}},
	})
	rec, err := f.client("public").GetDeployment(context.Background(), "ticketarr", "kontango", "prod-01")
	if err != nil {
		t.Fatalf("GetDeployment: %v", err)
	}
	if rec.Version != "latest" {
		t.Errorf("Version: %q", rec.Version)
	}
	if rec.Platform != "proxmox/lxc/medium" {
		t.Errorf("Platform: %q", rec.Platform)
	}
	if rec.ApprovedBy != "dillon" {
		t.Errorf("ApprovedBy: %q", rec.ApprovedBy)
	}
}

// Schmutz cascade: L3 override takes priority over L2 default.
func TestGetSubstrate_L3OverridesL2(t *testing.T) {
	l2 := `
version: 1
app: inventree
binds:
  - service: inventree.tango
    local_addr: 127.0.0.1:8000
`
	l3 := `
version: 1
app: inventree
deployment: prod-01
binds:
  - service: inventree.tango
    local_addr: 127.0.0.1:9000
`
	f := newFakeForgejo(t, "public", []fakeRepo{
		{name: "inventree", files: map[string]string{
			"schmutz.yml": l2,
			"deployments/kontango/prod-01/schmutz.yml": l3,
		}},
	})
	spec, err := f.client("public").GetSchmutz(context.Background(), "inventree", "kontango", "prod-01")
	if err != nil {
		t.Fatalf("GetSchmutz: %v", err)
	}
	if len(spec.Binds) == 0 || spec.Binds[0].LocalAddr != "127.0.0.1:9000" {
		t.Errorf("L3 override should win: got %q", spec.Binds[0].LocalAddr)
	}
}

// ErrNotFound when both L2 and L3 substrate are absent.
func TestGetSubstrate_BothAbsent(t *testing.T) {
	f := newFakeForgejo(t, "public", []fakeRepo{{name: "inventree"}})
	_, err := f.client("public").GetSchmutz(context.Background(), "inventree", "kontango", "prod-01")
	if err != forgejo.ErrNotFound {
		t.Errorf("expected ErrNotFound when no substrate at all, got %v", err)
	}
}

// GetComposeFile reads from the deployment folder.
func TestGetComposeFile(t *testing.T) {
	compose := "services:\n  app:\n    image: ticketarr:latest\n"
	f := newFakeForgejo(t, "public", []fakeRepo{
		{name: "ticketarr", files: map[string]string{
			"deployments/kontango/prod-01/docker-compose.yml": compose,
		}},
	})
	raw, err := f.client("public").GetComposeFile(context.Background(), "ticketarr", "kontango", "prod-01")
	if err != nil {
		t.Fatalf("GetComposeFile: %v", err)
	}
	if string(raw) != compose {
		t.Errorf("compose mismatch: got %q", string(raw))
	}
}

// GetComposeFile returns ErrNotFound when no compose file exists.
func TestGetComposeFile_NotFound(t *testing.T) {
	f := newFakeForgejo(t, "public", []fakeRepo{{name: "ticketarr"}})
	_, err := f.client("public").GetComposeFile(context.Background(), "ticketarr", "kontango", "prod-01")
	if err != forgejo.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// GetEnvTemplate reads the .env.template from the deployment folder.
func TestGetEnvTemplate(t *testing.T) {
	tmpl := "DB_PASSWORD=\nADMIN_PASSWORD=\n"
	f := newFakeForgejo(t, "public", []fakeRepo{
		{name: "ticketarr", files: map[string]string{
			"deployments/kontango/prod-01/.env.template": tmpl,
		}},
	})
	raw, err := f.client("public").GetEnvTemplate(context.Background(), "ticketarr", "kontango", "prod-01")
	if err != nil {
		t.Fatalf("GetEnvTemplate: %v", err)
	}
	if !strings.Contains(string(raw), "DB_PASSWORD=") {
		t.Errorf("env template content wrong: %q", string(raw))
	}
}

// GetEnvTemplate returns ErrNotFound when no template exists.
func TestGetEnvTemplate_NotFound(t *testing.T) {
	f := newFakeForgejo(t, "public", []fakeRepo{{name: "ticketarr"}})
	_, err := f.client("public").GetEnvTemplate(context.Background(), "ticketarr", "kontango", "prod-01")
	if err != forgejo.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// UpdateDeployment only touches git-safe fields (status, approved_by/at, version).
// Runtime fields (host, ziti_identity, entity_id) live in Bao, not git.
func TestUpdateDeployment_PreservesTypedFields(t *testing.T) {
	full := `{
		"tenant": "kontango", "app": "ticketarr", "deployment": "prod-01",
		"flavor": "app-host", "status": "pending", "version": "1.0.0",
		"platform": "proxmox/lxc/medium",
		"approved_by": "dillon", "approved_at": "2026-05-07T21:30:00Z"
	}`
	f := newFakeForgejo(t, "public", []fakeRepo{
		{name: "ticketarr", files: map[string]string{
			"deployments/kontango/prod-01/deployment.json": full,
		}},
	})
	err := f.client("public").UpdateDeployment(
		context.Background(), "ticketarr", "kontango", "prod-01",
		"chore: mark active",
		map[string]string{"status": "active"},
	)
	if err != nil {
		t.Fatalf("UpdateDeployment: %v", err)
	}
	updated := f.repos[0].files["deployments/kontango/prod-01/deployment.json"]
	var rec forgejo.DeploymentRecord
	if err := json.Unmarshal([]byte(updated), &rec); err != nil {
		t.Fatalf("re-parse updated JSON: %v", err)
	}
	if rec.Status != "active" {
		t.Errorf("Status not updated: %q", rec.Status)
	}
	// Fields not in the update must be preserved.
	if rec.Version != "1.0.0" {
		t.Errorf("Version was clobbered: %q", rec.Version)
	}
	if rec.Flavor != "app-host" {
		t.Errorf("Flavor was clobbered: %q", rec.Flavor)
	}
	if rec.ApprovedBy != "dillon" {
		t.Errorf("ApprovedBy was clobbered: %q", rec.ApprovedBy)
	}
}

// ListApps skips repos with missing tango.json — not ErrNotFound, just absent.
func TestListApps_SkipsMissingBlueprint(t *testing.T) {
	f := newFakeForgejo(t, "public", []fakeRepo{
		{name: "inventree", files: map[string]string{"tango.json": inventreeBlueprint}},
		{name: "no-blueprint"}, // no tango.json
	})
	apps, err := f.client("public").ListApps(context.Background())
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	if len(apps) != 1 || apps[0].AppID != "inventree" {
		t.Errorf("expected only inventree, got %v", apps)
	}
}

// ListApps skips apps with active=false.
func TestListApps_SkipsInactiveApps(t *testing.T) {
	inactive := `{"app_id":"draft","uuid":"00000000-0000-4000-8000-000000000001",
		"schema_version":2,"identity":{"display_name":"Draft","category":"operations"},
		"sizing":{},"deployment":{},"runtime":{},
		"catalog":{"added":"2026-05-07","maintainer":"kontango","active":false}}`
	f := newFakeForgejo(t, "public", []fakeRepo{
		{name: "inventree", files: map[string]string{"tango.json": inventreeBlueprint}},
		{name: "draft",     files: map[string]string{"tango.json": inactive}},
	})
	apps, err := f.client("public").ListApps(context.Background())
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	for _, a := range apps {
		if a.AppID == "draft" {
			t.Error("inactive app should not appear in ListApps")
		}
	}
}

// Regression: Validate() on real ticketarr tango.json shape.
func TestBlueprint_Validate_RealTicketarr(t *testing.T) {
	ticketarrJSON := `{
		"app_id": "ticketarr",
		"uuid": "b4e2c3d1-5a6f-4b8e-9c0d-1e2f3a4b5c6d",
		"schema_version": 2,
		"identity": {"display_name": "Ticketarr", "description": "Issue tracking",
		             "category": "operations", "license": "mit"},
		"links": {"github": "https://git.konoss.org/kore/ticketarr"},
		"sizing": {"min": "tg-sm-1", "recommended": "tg-sm-1", "max": "tg-md-1"},
		"deployment": {
			"compatible_platforms": ["proxmox/lxc/medium"],
			"docker_supported": true
		},
		"runtime": {
			"bao_secrets_path": "kontango/secret/apps/ticketarr/{deployment}",
			"secret_requirements": [
				{"ref": "composites/admin_user", "group": "admin"},
				{"ref": "composites/database_credentials", "group": "database"}
			],
			"health": {"url": "http://localhost:80/health", "expect_status": 200}
		},
		"catalog": {"added": "2026-05-07", "maintainer": "kontango", "active": true}
	}`
	f := newFakeForgejo(t, "public", []fakeRepo{
		{name: "ticketarr", files: map[string]string{"tango.json": ticketarrJSON}},
	})
	bp, err := f.client("public").GetTango(context.Background(), "ticketarr")
	if err != nil {
		t.Fatalf("GetTango ticketarr: %v", err)
	}
	if err := bp.Validate(); err != nil {
		t.Errorf("ticketarr tango.json fails Validate: %v", err)
	}
	if bp.Identity.DisplayName != "Ticketarr" {
		t.Errorf("DisplayName: %q", bp.Identity.DisplayName)
	}
	if len(bp.Runtime.SecretRequirements) != 2 {
		t.Errorf("SecretRequirements: %d", len(bp.Runtime.SecretRequirements))
	}
}

// ── catalog resolution tests ──────────────────────────────────────────────────

func TestGetCatalogConfig(t *testing.T) {
	cfg := `{
		"paths":{"sizing":"public/sizing","platforms":"public/deployment-platforms"},
		"defaults":{"zone":"tango","overlay_tld":"tango","bao_secrets_path":"{tenant}/secret/apps/{app}/{deployment}"},
		"schema_version":1
	}`
	f := newFakeForgejo(t, "public", []fakeRepo{
		{name: "_catalog", files: map[string]string{"config.json": cfg}},
	})
	c, err := f.client("public").GetCatalogConfig(context.Background())
	if err != nil {
		t.Fatalf("GetCatalogConfig: %v", err)
	}
	if c.Paths["sizing"] != "public/sizing" {
		t.Errorf("sizing path: %q", c.Paths["sizing"])
	}
	if c.SchemaVersion != 1 {
		t.Errorf("schema_version: %d", c.SchemaVersion)
	}
	// Defaults
	if c.Defaults.Zone != "tango" {
		t.Errorf("defaults.zone: %q", c.Defaults.Zone)
	}
	if c.Defaults.ZoneOrDefault() != "tango" {
		t.Errorf("ZoneOrDefault: %q", c.Defaults.ZoneOrDefault())
	}
	if c.Defaults.BaoSecretsPath != "{tenant}/secret/apps/{app}/{deployment}" {
		t.Errorf("defaults.bao_secrets_path: %q", c.Defaults.BaoSecretsPath)
	}
}

// ZoneOrDefault falls back to "tango" when not configured.
func TestCatalogDefaults_ZoneOrDefault_Fallback(t *testing.T) {
	cfg := `{"paths":{},"schema_version":1}`
	f := newFakeForgejo(t, "public", []fakeRepo{
		{name: "_catalog", files: map[string]string{"config.json": cfg}},
	})
	c, err := f.client("public").GetCatalogConfig(context.Background())
	if err != nil {
		t.Fatalf("GetCatalogConfig: %v", err)
	}
	if c.Defaults.ZoneOrDefault() != "tango" {
		t.Errorf("expected 'tango' fallback, got %q", c.Defaults.ZoneOrDefault())
	}
}

func TestGetSizingTier(t *testing.T) {
	tier := `{"id":"tg-md-1","cpu":2,"memory_gb":4,"disk_gb":32,"description":"Medium"}`
	f := newFakeForgejo(t, "public", []fakeRepo{
		{name: "_catalog", files: map[string]string{"sizing/tg-md-1.json": tier}},
	})
	m, err := f.client("public").GetSizingTier(context.Background(), "tg-md-1")
	if err != nil {
		t.Fatalf("GetSizingTier: %v", err)
	}
	if m["id"] != "tg-md-1" {
		t.Errorf("id: %v", m["id"])
	}
	// cpu is float64 after JSON unmarshal
	if m["cpu"].(float64) != 2 {
		t.Errorf("cpu: %v", m["cpu"])
	}
	if m["memory_gb"].(float64) != 4 {
		t.Errorf("memory_gb: %v", m["memory_gb"])
	}
}

func TestGetSizingTier_NotFound(t *testing.T) {
	f := newFakeForgejo(t, "public", []fakeRepo{{name: "_catalog"}})
	_, err := f.client("public").GetSizingTier(context.Background(), "tg-xx-1")
	if err != forgejo.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestGetPlatformTier(t *testing.T) {
	platform := `{"id":"proxmox/lxc/medium","provider":"proxmox","form_factor":"lxc",
		"size":"medium","implements":"tg-md-1","cpu":4,"memory_mb":4096,"disk_gb":32,
		"features":["nesting","keyctl"],"unprivileged":true,
		"terraform":{"provider":"bpg/proxmox","resource":"proxmox_virtual_environment_container"},
		"notes":"Standard app LXC."}`
	f := newFakeForgejo(t, "public", []fakeRepo{
		{name: "_catalog", files: map[string]string{
			"platforms/proxmox/lxc/medium.json": platform,
		}},
	})
	m, err := f.client("public").GetPlatformTier(context.Background(), "proxmox/lxc/medium")
	if err != nil {
		t.Fatalf("GetPlatformTier: %v", err)
	}
	if m["implements"] != "tg-md-1" {
		t.Errorf("implements: %v", m["implements"])
	}
	if m["memory_mb"].(float64) != 4096 {
		t.Errorf("memory_mb: %v", m["memory_mb"])
	}
	tf, ok := m["terraform"].(map[string]any)
	if !ok || tf["provider"] != "bpg/proxmox" {
		t.Errorf("terraform.provider: %v", m["terraform"])
	}
}

func TestGetPlatformTier_NotFound(t *testing.T) {
	f := newFakeForgejo(t, "public", []fakeRepo{{name: "_catalog"}})
	_, err := f.client("public").GetPlatformTier(context.Background(), "proxmox/lxc/nonexistent")
	if err != forgejo.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestGetSecretType(t *testing.T) {
	st := `{"id":"database_credentials","kind":"composite",
		"primitives":["username","password","hostname","url"],
		"description":"DB connection"}`
	f := newFakeForgejo(t, "public", []fakeRepo{
		{name: "_catalog", files: map[string]string{
			"secret-types/composites/database_credentials.json": st,
		}},
	})
	m, err := f.client("public").GetSecretType(context.Background(), "composites/database_credentials")
	if err != nil {
		t.Fatalf("GetSecretType: %v", err)
	}
	if m["kind"] != "composite" {
		t.Errorf("kind: %v", m["kind"])
	}
	prims, ok := m["primitives"].([]any)
	if !ok || len(prims) != 4 {
		t.Errorf("primitives: %v", m["primitives"])
	}
}

// Tango sizing refs resolve to real catalog entries.
// This is the key integration: blueprint says "tg-md-1", catalog
// has sizing/tg-md-1.json with the concrete spec.
func TestCatalog_BlueprintSizingResolvesToCatalogEntry(t *testing.T) {
	tier := `{"id":"tg-md-1","cpu":2,"memory_gb":4,"disk_gb":32,"description":"Medium"}`
	f := newFakeForgejo(t, "public", []fakeRepo{
		{name: "inventree",  files: map[string]string{"tango.json": inventreeBlueprint}},
		{name: "_catalog",   files: map[string]string{"sizing/tg-md-1.json": tier}},
	})
	cli := f.client("public")

	bp, err := cli.GetTango(context.Background(), "inventree")
	if err != nil {
		t.Fatalf("GetTango: %v", err)
	}

	// Tango declares tg-md-1
	if bp.Sizing.Recommended != "tg-md-1" {
		t.Fatalf("blueprint sizing.recommended: %q", bp.Sizing.Recommended)
	}

	// Agent resolves to concrete spec
	spec, err := cli.GetSizingTier(context.Background(), bp.Sizing.Recommended)
	if err != nil {
		t.Fatalf("GetSizingTier(%s): %v", bp.Sizing.Recommended, err)
	}
	if spec["memory_gb"].(float64) != 4 {
		t.Errorf("resolved memory_gb: %v", spec["memory_gb"])
	}
	if spec["disk_gb"].(float64) != 32 {
		t.Errorf("resolved disk_gb: %v", spec["disk_gb"])
	}
}

// Compile-time check: *forgejo.Client satisfies catalogReader interface.
var _ = bytes.Buffer{} // avoid unused import warning

// ──────────────────────────────────────────────────────────────────────────────
// CachedClient — ListApps cache tests
// ──────────────────────────────────────────────────────────────────────────────

// countingFakeForgejo wraps fakeForgejo and counts total HTTP requests.
type countingFakeForgejo struct {
	*fakeForgejo
	requests int
	server   *httptest.Server
}

func newCountingFakeForgejo(t *testing.T, org string, repos []fakeRepo) *countingFakeForgejo {
	t.Helper()
	inner := newFakeForgejo(t, org, repos)
	// The inner server is already running; wrap it with a counting proxy.
	c := &countingFakeForgejo{fakeForgejo: inner}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		c.requests++
		// Proxy to the inner fakeForgejo server.
		proxyReq, _ := http.NewRequestWithContext(r.Context(), r.Method, inner.server.URL+r.URL.RequestURI(), r.Body)
		for k, vs := range r.Header {
			proxyReq.Header[k] = vs
		}
		resp, err := http.DefaultTransport.RoundTrip(proxyReq)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for k, vs := range resp.Header {
			w.Header()[k] = vs
		}
		w.WriteHeader(resp.StatusCode)
		body, _ := io.ReadAll(resp.Body)
		w.Write(body)
	})
	c.server = httptest.NewServer(mux)
	t.Cleanup(c.server.Close)
	return c
}

func (c *countingFakeForgejo) client(org string) *forgejo.Client {
	return forgejo.NewClient(c.server.URL, org, "test-token", 0)
}

// TestCachedClient_ServesFromCache verifies that repeated ListApps calls within
// the TTL window hit Forgejo only once.
func TestCachedClient_ServesFromCache(t *testing.T) {
	f := newCountingFakeForgejo(t, "public", []fakeRepo{
		{name: "inventree", files: map[string]string{"tango.json": inventreeBlueprint}},
	})
	cli := f.client("public")
	cached := forgejo.NewCachedClient(cli, time.Minute)

	ctx := context.Background()

	// First call — must hit Forgejo.
	apps1, err := cached.ListApps(ctx)
	if err != nil {
		t.Fatalf("first ListApps: %v", err)
	}
	if len(apps1) != 1 {
		t.Fatalf("expected 1 app, got %d", len(apps1))
	}
	firstCount := f.requests

	// Second call within TTL — must NOT hit Forgejo again.
	apps2, err := cached.ListApps(ctx)
	if err != nil {
		t.Fatalf("second ListApps: %v", err)
	}
	if len(apps2) != 1 {
		t.Fatalf("cache returned wrong count: got %d", len(apps2))
	}
	if f.requests != firstCount {
		t.Errorf("second ListApps made %d additional Forgejo requests, want 0",
			f.requests-firstCount)
	}
}

// TestCachedClient_RefreshesAfterTTL verifies that ListApps re-fetches from
// Forgejo once the TTL has elapsed.
func TestCachedClient_RefreshesAfterTTL(t *testing.T) {
	f := newCountingFakeForgejo(t, "public", []fakeRepo{
		{name: "inventree", files: map[string]string{"tango.json": inventreeBlueprint}},
	})
	cli := f.client("public")
	cached := forgejo.NewCachedClient(cli, 50*time.Millisecond) // short TTL

	ctx := context.Background()

	if _, err := cached.ListApps(ctx); err != nil {
		t.Fatalf("first ListApps: %v", err)
	}
	firstCount := f.requests

	// Wait for TTL to expire.
	time.Sleep(100 * time.Millisecond)

	if _, err := cached.ListApps(ctx); err != nil {
		t.Fatalf("post-TTL ListApps: %v", err)
	}
	if f.requests == firstCount {
		t.Error("expected Forgejo to be re-fetched after TTL, but request count did not increase")
	}
}
