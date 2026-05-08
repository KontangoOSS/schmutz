package shared

import (
	"encoding/json"
	"strings"
	"testing"
)

// realInventreeKV is the actual data from public/secret/applications/inventree.
// Used to verify FromKV migration produces the correct grouped structure.
var realInventreeKV = map[string]string{
	"active":                          "true",
	"app_id":                          "inventree",
	"bao_secrets_path":                "kontango/secret/apps/inventree",
	"catalog_added":                   "2026-04-27",
	"catalog_maintainer":              "kontango",
	"category":                        "public/categories/operations",
	"compatible_deployment_platforms": "public/deployment-platforms/proxmox/lxc/medium,public/deployment-platforms/proxmox/lxc/large,public/deployment-platforms/proxmox/lxc/xlarge,public/deployment-platforms/proxmox/vm/medium,public/deployment-platforms/digitalocean/droplet/medium,public/deployment-platforms/digitalocean/droplet/large",
	"default_deployment_platform":     "public/deployment-platforms/proxmox/lxc/medium",
	"default_os":                      "public/os/linux/ubuntu/latest",
	"description":                     "Open-source inventory management",
	"display_name":                    "InvenTree",
	"docker_supported":                "true",
	"forgejo_url":                     "https://git.konoss.org/kore/inventree",
	"funding_url":                     "https://github.com/sponsors/inventree",
	"github_url":                      "https://github.com/inventree/InvenTree",
	"health_expect_status":            "200",
	"health_url":                      "http://localhost:8000/api/",
	"issues_url":                      "https://github.com/inventree/InvenTree/issues",
	"plugin_url":                      "https://git.konoss.org/kore/inventree-kontango-plugin",
	"schmutz_flavor_url":              "https://git.konoss.org/kore/schmutz-controller/src/branch/main/flavors/inventree/flavor.yaml",
	"secret_requirements":             "public/secret-types/composites/database_credentials:database,public/secret-types/composites/admin_user:admin,public/secret-types/composites/redis_connection:cache,public/secret-types/primitives/url:site_url,public/secret-types/primitives/symmetric_key:secret_key",
	"sizing_max":                      "public/sizing/tg-lg-1",
	"sizing_min":                      "public/sizing/tg-sm-1",
	"sizing_recommended":              "public/sizing/tg-md-1",
	"upstream_docs":                   "https://docs.inventree.org",
	"upstream_homepage":               "https://inventree.org",
	"upstream_license":                "public/licenses/mit",
	"upstream_repo":                   "https://github.com/inventree/InvenTree",
	"uuid":                            "d0afad25-f68e-4c79-b572-739e04d7d789",
}

var realActivepiecesKV = map[string]string{
	"active":               "false",
	"app_id":               "activepieces",
	"catalog_added":        "2026-04-27",
	"catalog_maintainer":   "awesome-selfhosted",
	"category":             "public/categories/automation",
	"description":          "No-code business automation tool.",
	"discovery_source":     "awesome-selfhosted",
	"display_name":         "Activepieces",
	"docker_supported":     "true",
	"forgejo_url":          "https://git.konoss.org/kore/activepieces",
	"github_url":           "https://github.com/activepieces/activepieces",
	"issues_url":           "https://github.com/activepieces/activepieces/issues",
	"plugin_url":           "https://git.konoss.org/kore/activepieces-kontango-plugin",
	"sizing_max":           "public/sizing/tg-md-1",
	"sizing_min":           "public/sizing/tg-xs-1",
	"sizing_recommended":   "public/sizing/tg-sm-1",
	"stargazers":           "21914",
	"upstream_docs":        "https://www.activepieces.com",
	"upstream_homepage":    "https://www.activepieces.com",
	"upstream_last_update": "2026-04-26",
	"upstream_latest_tag":  "0.82.1",
	"upstream_license":     "public/licenses/mit",
	"upstream_repo":        "https://github.com/activepieces/activepieces",
	"uuid":                 "cc34b191-5f3c-4073-906e-5655f95880bb",
}

// TestFromKV_RealInventree verifies migration from old KV format produces
// correct grouped structure with short slugs.
func TestFromKV_RealInventree(t *testing.T) {
	var b Blueprint
	if err := b.FromKV(realInventreeKV); err != nil {
		t.Fatalf("FromKV: %v", err)
	}

	// Top-level
	if b.AppID != "inventree" {
		t.Errorf("AppID: %q", b.AppID)
	}

	// Identity group
	if !b.Catalog.Active {
		t.Error("Catalog.Active must be true")
	}
	if b.Identity.DisplayName != "InvenTree" {
		t.Errorf("Identity.DisplayName: %q", b.Identity.DisplayName)
	}
	if b.Identity.Category != "operations" {
		t.Errorf("Identity.Category: %q (expected short slug)", b.Identity.Category)
	}
	if b.Identity.License != "mit" {
		t.Errorf("Identity.License: %q (expected short slug)", b.Identity.License)
	}

	// Sizing — short slugs
	if b.Sizing.Min != "tg-sm-1" {
		t.Errorf("Sizing.Min: %q", b.Sizing.Min)
	}
	if b.Sizing.Recommended != "tg-md-1" {
		t.Errorf("Sizing.Recommended: %q", b.Sizing.Recommended)
	}
	if b.Sizing.Max != "tg-lg-1" {
		t.Errorf("Sizing.Max: %q", b.Sizing.Max)
	}

	// Deployment — short slugs, proper array
	if b.Deployment.DockerSupported != true {
		t.Error("Deployment.DockerSupported must be true")
	}
	if got, want := len(b.Deployment.CompatiblePlatforms), 6; got != want {
		t.Errorf("CompatiblePlatforms: got %d want %d", got, want)
	}
	if b.Deployment.CompatiblePlatforms[0] != "proxmox/lxc/medium" {
		t.Errorf("CompatiblePlatforms[0]: %q (expected short slug)", b.Deployment.CompatiblePlatforms[0])
	}

	// Runtime — secrets as typed array
	if got, want := len(b.Runtime.SecretRequirements), 5; got != want {
		t.Fatalf("SecretRequirements: got %d want %d", got, want)
	}
	if b.Runtime.SecretRequirements[0].Ref != "composites/database_credentials" {
		t.Errorf("SecretRequirements[0].Ref: %q (expected short slug)", b.Runtime.SecretRequirements[0].Ref)
	}
	if b.Runtime.SecretRequirements[0].Group != "database" {
		t.Errorf("SecretRequirements[0].Group: %q", b.Runtime.SecretRequirements[0].Group)
	}
	if b.Runtime.Health.ExpectStatus != 200 {
		t.Errorf("Health.ExpectStatus: %d", b.Runtime.Health.ExpectStatus)
	}
	if b.Runtime.BaoSecretsPath == "" {
		t.Error("Runtime.BaoSecretsPath required")
	}

	// Validate passes on real data
	if err := b.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// TestFromKV_Discovery verifies discovery entries parse correctly.
func TestFromKV_Discovery(t *testing.T) {
	var b Blueprint
	if err := b.FromKV(realActivepiecesKV); err != nil {
		t.Fatalf("FromKV: %v", err)
	}
	if b.Catalog.Active {
		t.Error("discovery entry should be active=false")
	}
	if b.Catalog.Stargazers != 21914 {
		t.Errorf("Stargazers: %d", b.Catalog.Stargazers)
	}
	if b.Runtime.Health.URL != "" {
		t.Errorf("discovery entry should have empty health url, got %q", b.Runtime.Health.URL)
	}
	if err := b.Validate(); err != nil {
		t.Errorf("discovery entry Validate: %v", err)
	}
}

// TestBlueprint_JSONRoundTrip verifies new JSON format round-trips cleanly.
func TestBlueprint_JSONRoundTrip(t *testing.T) {
	bp := Blueprint{
		AppID:  "ticketarr",
		UUID:   "b4e2c3d1-5a6f-4b8e-9c0d-1e2f3a4b5c6d",
		Schema: BlueprintSchemaVersion,
		Identity: BlueprintIdentity{
			DisplayName: "Ticketarr",
			Description: "Issue tracking",
			Category:    "operations",
			License:     "mit",
		},
		Links: BlueprintLinks{
			GitHub: "https://github.com/example/ticketarr",
		},
		Sizing: BlueprintSizing{
			Min: "tg-sm-1", Recommended: "tg-sm-1", Max: "tg-md-1",
		},
		Deployment: BlueprintDeployment{
			DefaultPlatform:     "proxmox/lxc/medium",
			CompatiblePlatforms: []string{"proxmox/lxc/medium", "proxmox/lxc/large"},
			DockerSupported:     true,
		},
		Runtime: BlueprintRuntime{
			BaoSecretsPath: "kontango/secret/apps/ticketarr/{deployment}",
			SecretRequirements: []SecretRequirement{
				{Ref: "composites/admin_user", Group: "admin"},
				{Ref: "composites/database_credentials", Group: "database"},
			},
			Health: BlueprintHealth{URL: "http://localhost:3001/health", ExpectStatus: 200},
		},
		Catalog: BlueprintCatalog{
			Added:      "2026-05-07",
			Maintainer: "kontango",
			Active:     true,
		},
	}
	data, err := bp.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}

	var got Blueprint
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.AppID != bp.AppID {
		t.Errorf("AppID: got %q want %q", got.AppID, bp.AppID)
	}
	if got.Identity.Category != "operations" {
		t.Errorf("Category: %q", got.Identity.Category)
	}
	if len(got.Runtime.SecretRequirements) != 2 {
		t.Errorf("SecretRequirements count: %d", len(got.Runtime.SecretRequirements))
	}
	if len(got.Deployment.CompatiblePlatforms) != 2 {
		t.Errorf("CompatiblePlatforms count: %d", len(got.Deployment.CompatiblePlatforms))
	}
	if err := got.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// TestBlueprint_ResolveRef verifies short ref expansion.
func TestBlueprint_ResolveRef(t *testing.T) {
	cases := []struct{ in, root, want string }{
		{"composites/database_credentials", "", "public/secret-types/composites/database_credentials"},
		{"primitives/symmetric_key", "custom/root", "custom/root/primitives/symmetric_key"},
	}
	for _, c := range cases {
		if got := ResolveRef(c.in, c.root); got != c.want {
			t.Errorf("ResolveRef(%q,%q): got %q want %q", c.in, c.root, got, c.want)
		}
	}
}

// TestBlueprint_ResolveTier verifies tier slug expansion.
func TestBlueprint_ResolveTier(t *testing.T) {
	if got := ResolveTier("tg-md-1", ""); got != "public/sizing/tg-md-1" {
		t.Errorf("ResolveTier: %q", got)
	}
}

// TestBlueprint_Validate_AppIDSlug rejects invalid app_id values.
func TestBlueprint_Validate_AppIDSlug(t *testing.T) {
	for _, id := range []string{"", "Inventree", "inv_app", "-leading"} {
		var b Blueprint
		_ = b.FromKV(realInventreeKV)
		b.AppID = id
		if err := b.Validate(); err == nil || !strings.Contains(err.Error(), "app_id") {
			t.Errorf("id=%q: expected app_id error, got %v", id, err)
		}
	}
}

// TestBlueprint_Validate_UUID rejects non-v4 UUIDs.
func TestBlueprint_Validate_UUID(t *testing.T) {
	for _, u := range []string{"", "not-a-uuid", "d0afad25-f68e-3c79-b572-739e04d7d789"} {
		var b Blueprint
		_ = b.FromKV(realInventreeKV)
		b.UUID = u
		if err := b.Validate(); err == nil || !strings.Contains(err.Error(), "uuid") {
			t.Errorf("uuid=%q: expected uuid error, got %v", u, err)
		}
	}
}

// TestBlueprint_Validate_ActiveRequiresBaoPath checks active=true enforcement.
func TestBlueprint_Validate_ActiveRequiresBaoPath(t *testing.T) {
	var b Blueprint
	_ = b.FromKV(realInventreeKV)
	b.Runtime.BaoSecretsPath = ""
	if err := b.Validate(); err == nil || !strings.Contains(err.Error(), "bao_secrets_path") {
		t.Errorf("expected bao_secrets_path error, got %v", err)
	}
}

// TestBlueprint_Validate_DiscoveryNoBaoPathOK discovery entries don't need bao path.
func TestBlueprint_Validate_DiscoveryNoBaoPathOK(t *testing.T) {
	var b Blueprint
	_ = b.FromKV(realActivepiecesKV)
	if err := b.Validate(); err != nil {
		t.Errorf("discovery without bao path should be valid: %v", err)
	}
}

// TestBlueprint_Validate_SizingTier rejects non-standard tier slugs.
func TestBlueprint_Validate_SizingTier(t *testing.T) {
	var b Blueprint
	_ = b.FromKV(realInventreeKV)
	b.Sizing.Min = "small" // not a tier slug
	if err := b.Validate(); err == nil || !strings.Contains(err.Error(), "sizing.min") {
		t.Errorf("expected sizing.min error, got %v", err)
	}
}

// TestBlueprint_Validate_HealthStatus rejects out-of-range status.
func TestBlueprint_Validate_HealthStatus(t *testing.T) {
	var b Blueprint
	_ = b.FromKV(realInventreeKV)
	b.Runtime.Health.ExpectStatus = 99
	if err := b.Validate(); err == nil || !strings.Contains(err.Error(), "expect_status") {
		t.Errorf("expected expect_status error, got %v", err)
	}
}
