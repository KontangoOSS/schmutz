package shared

import (
	"strings"
	"testing"
)

// realInventreeKV is the actual data stored at
// public/secret/applications/inventree (read 2026-05-06). Catches
// regressions where we change the schema and forget to test against
// what's actually on disk in the production catalog.
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
	"description":                     "Open-source inventory management — parts, BOMs, stock, supplier orders, builds. Django + DRF + Mantine UI.",
	"display_name":                    "InvenTree",
	"docker_supported":                "true",
	"forgejo_url":                     "https://git.konoss.org/kore/inventree",
	"funding_url":                     "https://github.com/sponsors/inventree",
	"github_url":                      "https://github.com/inventree/InvenTree",
	"health_expect_status":            "200",
	"health_url":                      "http://localhost:8000/api/",
	"issues_url":                      "https://github.com/inventree/InvenTree/issues",
	"license_url":                     "https://github.com/inventree/InvenTree/blob/master/LICENSE",
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

// realActivepiecesKV is a discovery entry (active=false). Has different
// fields populated than an active entry — has stargazers + upstream_last_update,
// missing bao_secrets_path + secret_requirements + health_url.
var realActivepiecesKV = map[string]string{
	"active":               "false",
	"app_id":               "activepieces",
	"catalog_added":        "2026-04-27",
	"catalog_maintainer":   "awesome-selfhosted",
	"category":             "public/categories/automation",
	"description":          "No-code business automation tool like Zapier or Tray. For example, you can send a Slack notification for each new Trello card.",
	"discovery_source":     "awesome-selfhosted",
	"display_name":         "Activepieces",
	"docker_supported":     "true",
	"forgejo_url":          "https://git.konoss.org/kore/activepieces",
	"funding_url":          "",
	"github_url":           "https://github.com/activepieces/activepieces",
	"issues_url":           "https://github.com/activepieces/activepieces/issues",
	"license_url":          "https://github.com/activepieces/activepieces/blob/main/LICENSE",
	"plugin_url":           "https://git.konoss.org/kore/activepieces-kontango-plugin",
	"schmutz_flavor_url":   "https://git.konoss.org/kore/schmutz-controller/src/branch/main/flavors/activepieces/flavor.yaml",
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

func TestBlueprint_FromKV_RealInventree(t *testing.T) {
	var b Blueprint
	if err := b.FromKV(realInventreeKV); err != nil {
		t.Fatalf("FromKV: %v", err)
	}
	if !b.Active {
		t.Error("Active must be true")
	}
	if !b.DockerSupported {
		t.Error("DockerSupported must be true")
	}
	if b.HealthExpectStatus != 200 {
		t.Errorf("HealthExpectStatus: got %d want 200", b.HealthExpectStatus)
	}
	// CSV with 6 platforms
	if got, want := len(b.CompatibleDeploymentPlatforms), 6; got != want {
		t.Errorf("CompatibleDeploymentPlatforms: got %d entries want %d", got, want)
	}
	// secret_requirements parsed into 5 (database, admin, cache, site_url, secret_key)
	if got, want := len(b.SecretRequirements), 5; got != want {
		t.Fatalf("SecretRequirements: got %d entries want %d", got, want)
	}
	if b.SecretRequirements[0].Ref != "public/secret-types/composites/database_credentials" || b.SecretRequirements[0].Group != "database" {
		t.Errorf("SecretRequirements[0]: %+v", b.SecretRequirements[0])
	}
	// Validate must pass on the real record.
	if err := b.Validate(); err != nil {
		t.Errorf("real inventree fails Validate: %v", err)
	}
}

func TestBlueprint_FromKV_RealActivepieces(t *testing.T) {
	var b Blueprint
	if err := b.FromKV(realActivepiecesKV); err != nil {
		t.Fatalf("FromKV: %v", err)
	}
	if b.Active {
		t.Error("Active must be false (discovery entry)")
	}
	if !b.DockerSupported {
		t.Error("DockerSupported must be true")
	}
	if b.Stargazers != 21914 {
		t.Errorf("Stargazers: got %d want 21914", b.Stargazers)
	}
	if b.HealthExpectStatus != 0 {
		t.Errorf("HealthExpectStatus: discovery entry should be 0, got %d", b.HealthExpectStatus)
	}
	if b.HealthURL != "" {
		t.Errorf("HealthURL: discovery entry should be empty, got %q", b.HealthURL)
	}
	if b.BaoSecretsPath != "" {
		t.Errorf("BaoSecretsPath: discovery entry should be empty, got %q", b.BaoSecretsPath)
	}
	// Validate must pass — BaoSecretsPath isn't required when active=false.
	if err := b.Validate(); err != nil {
		t.Errorf("real activepieces fails Validate: %v", err)
	}
}

// Round-trip: FromKV then ToKV should preserve all the meaningful fields.
// Empty-vs-missing distinction is not preserved — that's documented behavior.
func TestBlueprint_RoundTrip(t *testing.T) {
	var b Blueprint
	if err := b.FromKV(realInventreeKV); err != nil {
		t.Fatalf("FromKV: %v", err)
	}
	out := b.ToKV()
	for _, key := range []string{"app_id", "uuid", "display_name", "category",
		"bao_secrets_path", "active", "docker_supported", "health_expect_status"} {
		if out[key] != realInventreeKV[key] {
			t.Errorf("round-trip %s: got %q want %q", key, out[key], realInventreeKV[key])
		}
	}
	// CSV order preserved (input order is platforms in their source order)
	if out["compatible_deployment_platforms"] != realInventreeKV["compatible_deployment_platforms"] {
		t.Errorf("compatible_deployment_platforms not round-tripped:\n got %s\nwant %s",
			out["compatible_deployment_platforms"], realInventreeKV["compatible_deployment_platforms"])
	}
	if out["secret_requirements"] != realInventreeKV["secret_requirements"] {
		t.Errorf("secret_requirements not round-tripped:\n got %s\nwant %s",
			out["secret_requirements"], realInventreeKV["secret_requirements"])
	}
}

// FromKV must be tolerant of forward-compat fields it doesn't know
// (catalog can add fields without breaking older readers).
func TestBlueprint_FromKV_UnknownFields(t *testing.T) {
	kv := map[string]string{}
	for k, v := range realInventreeKV {
		kv[k] = v
	}
	kv["future_field"] = "from-the-future"
	kv["another_one"] = "something else"

	var b Blueprint
	if err := b.FromKV(kv); err != nil {
		t.Fatalf("FromKV must ignore unknown keys: %v", err)
	}
	if b.AppID != "inventree" {
		t.Errorf("AppID: got %q", b.AppID)
	}
}

func TestBlueprint_FromKV_BadBool(t *testing.T) {
	kv := map[string]string{"active": "yep", "app_id": "x", "uuid": "00000000-0000-4000-8000-000000000000",
		"display_name": "X", "category": "public/categories/x"}
	var b Blueprint
	err := b.FromKV(kv)
	if err == nil || !strings.Contains(err.Error(), "active") {
		t.Errorf("expected active-parse error, got %v", err)
	}
}

func TestBlueprint_FromKV_BadInt(t *testing.T) {
	kv := map[string]string{"stargazers": "many", "app_id": "x"}
	var b Blueprint
	err := b.FromKV(kv)
	if err == nil || !strings.Contains(err.Error(), "stargazers") {
		t.Errorf("expected stargazers-parse error, got %v", err)
	}
}

func TestBlueprint_SecretRequirements_BadShape(t *testing.T) {
	cases := map[string]string{
		"missing colon": "public/secret-types/primitives/url",
		"empty ref":     ":foo",
		"empty group":   "public/secret-types/primitives/url:",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			kv := map[string]string{
				"secret_requirements": raw,
				"app_id":              "x",
			}
			var b Blueprint
			err := b.FromKV(kv)
			if err == nil || !strings.Contains(err.Error(), "secret_requirements") {
				t.Errorf("expected secret_requirements error, got %v", err)
			}
		})
	}
}

func TestBlueprint_Validate_AppIDSlug(t *testing.T) {
	cases := []string{"", "Inventree", "inventree_app", "-leading"}
	for _, id := range cases {
		var b Blueprint
		_ = b.FromKV(realInventreeKV)
		b.AppID = id
		err := b.Validate()
		if err == nil || !strings.Contains(err.Error(), "app_id") {
			t.Errorf("id=%q: expected app_id error, got %v", id, err)
		}
	}
}

func TestBlueprint_Validate_UUID(t *testing.T) {
	cases := []string{
		"",
		"not-a-uuid",
		"d0afad25-f68e-3c79-b572-739e04d7d789", // version 3
		"d0afad25-f68e-4c79-c572-739e04d7d789", // bad variant
	}
	for _, u := range cases {
		var b Blueprint
		_ = b.FromKV(realInventreeKV)
		b.UUID = u
		err := b.Validate()
		if err == nil || !strings.Contains(err.Error(), "uuid") {
			t.Errorf("uuid=%q: expected uuid error, got %v", u, err)
		}
	}
}

func TestBlueprint_Validate_Refs(t *testing.T) {
	cases := []struct {
		field string
		mut   func(*Blueprint)
		want  string
	}{
		{"category not a ref", func(b *Blueprint) { b.Category = "operations" }, "category"},
		{"sizing_min not a ref", func(b *Blueprint) { b.SizingMin = "tg-sm-1" }, "sizing_min"},
		{"compatible_deployment_platforms[0] not a ref", func(b *Blueprint) {
			b.CompatibleDeploymentPlatforms = []string{"proxmox/lxc/medium"}
		}, "compatible_deployment_platforms"},
		{"upstream_license not a ref", func(b *Blueprint) { b.UpstreamLicense = "MIT" }, "upstream_license"},
	}
	for _, c := range cases {
		t.Run(c.field, func(t *testing.T) {
			var b Blueprint
			_ = b.FromKV(realInventreeKV)
			c.mut(&b)
			err := b.Validate()
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("expected %q in error, got %v", c.want, err)
			}
		})
	}
}

func TestBlueprint_Validate_ActiveRequiresBaoPath(t *testing.T) {
	var b Blueprint
	_ = b.FromKV(realInventreeKV)
	b.BaoSecretsPath = ""
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "bao_secrets_path") {
		t.Errorf("expected bao_secrets_path error, got %v", err)
	}
}

// Discovery entries are exempt from the "active=true requires bao_secrets_path" rule.
func TestBlueprint_Validate_DiscoveryNoBaoPathOK(t *testing.T) {
	var b Blueprint
	if err := b.FromKV(realActivepiecesKV); err != nil {
		t.Fatalf("FromKV: %v", err)
	}
	if b.Active {
		t.Fatal("activepieces fixture is supposed to be active=false")
	}
	if b.BaoSecretsPath != "" {
		t.Fatal("activepieces fixture should have empty bao_secrets_path")
	}
	if err := b.Validate(); err != nil {
		t.Errorf("discovery entry without bao_secrets_path should validate: %v", err)
	}
}

func TestBlueprint_Validate_HealthStatusRange(t *testing.T) {
	var b Blueprint
	_ = b.FromKV(realInventreeKV)
	b.HealthExpectStatus = 99
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "health_expect_status") {
		t.Errorf("expected health_expect_status error, got %v", err)
	}
	b.HealthExpectStatus = 600
	err = b.Validate()
	if err == nil || !strings.Contains(err.Error(), "health_expect_status") {
		t.Errorf("expected health_expect_status range error, got %v", err)
	}
}

func TestBlueprint_Validate_CatalogAddedFormat(t *testing.T) {
	var b Blueprint
	_ = b.FromKV(realInventreeKV)
	b.CatalogAdded = "April 27 2026"
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "catalog_added") {
		t.Errorf("expected catalog_added error, got %v", err)
	}
}
