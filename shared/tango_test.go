package shared

import (
	"encoding/json"
	"strings"
	"testing"
)

// JSON fixtures — real blueprint.json shapes from the Forgejo public org.
// These are the ground truth for schema validation tests.

var inventreeBlueprintJSON = []byte(`{
  "app_id": "inventree",
  "uuid": "d0afad25-f68e-4c79-b572-739e04d7d789",
  "schema_version": 2,
  "identity": {
    "display_name": "InvenTree",
    "description": "Open-source inventory management",
    "category": "operations",
    "license": "mit"
  },
  "links": {
    "github": "https://github.com/inventree/InvenTree",
    "forgejo": "https://git.konoss.org/public/inventree",
    "upstream": "https://github.com/inventree/InvenTree",
    "docs": "https://docs.inventree.org",
    "issues": "https://github.com/inventree/InvenTree/issues",
    "funding": "https://github.com/sponsors/inventree"
  },
  "sizing": {
    "min": "tg-sm-1",
    "recommended": "tg-md-1",
    "max": "tg-lg-1"
  },
  "deployment": {
    "default_os": "linux/ubuntu/latest",
    "default_platform": "proxmox/lxc/medium",
    "compatible_platforms": [
      "proxmox/lxc/medium", "proxmox/lxc/large", "proxmox/lxc/xlarge",
      "proxmox/vm/medium", "digitalocean/droplet/medium", "digitalocean/droplet/large"
    ],
    "docker_supported": true
  },
  "runtime": {
    "bao_secrets_path": "kontango/secret/apps/inventree/{deployment}",
    "secret_requirements": [
      {"ref": "composites/database_credentials", "group": "database"},
      {"ref": "composites/admin_user",            "group": "admin"},
      {"ref": "composites/redis_connection",       "group": "cache"},
      {"ref": "primitives/url",                    "group": "site_url"},
      {"ref": "primitives/symmetric_key",          "group": "secret_key"}
    ],
    "health": {"url": "http://localhost:8000/api/", "expect_status": 200}
  },
  "catalog": {
    "added": "2026-04-27",
    "maintainer": "kontango",
    "active": true
  }
}`)

// Discovery entry (active=false) — has stargazers + no health/bao path.
var activepiecesBlueprintJSON = []byte(`{
  "app_id": "activepieces",
  "uuid": "cc34b191-5f3c-4073-906e-5655f95880bb",
  "schema_version": 2,
  "identity": {
    "display_name": "Activepieces",
    "description": "No-code business automation tool.",
    "category": "automation",
    "license": "mit"
  },
  "links": {
    "github": "https://github.com/activepieces/activepieces",
    "upstream": "https://github.com/activepieces/activepieces",
    "docs": "https://www.activepieces.com",
    "issues": "https://github.com/activepieces/activepieces/issues"
  },
  "sizing": {
    "min": "tg-xs-1",
    "recommended": "tg-sm-1",
    "max": "tg-md-1"
  },
  "deployment": {"docker_supported": true},
  "runtime": {},
  "catalog": {
    "added": "2026-04-27",
    "maintainer": "awesome-selfhosted",
    "active": false,
    "discovery_source": "awesome-selfhosted",
    "stargazers": 21914,
    "latest_tag": "0.82.1",
    "last_update": "2026-04-26"
  }
}`)

// ---- tests ------------------------------------------------------------------

func TestBlueprint_ParseRealInventree(t *testing.T) {
	var b Tango
	if err := json.Unmarshal(inventreeBlueprintJSON, &b); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if b.AppID != "inventree" {
		t.Errorf("AppID: %q", b.AppID)
	}
	if !b.Catalog.Active {
		t.Error("Catalog.Active must be true")
	}
	if b.Identity.Category != "operations" {
		t.Errorf("Identity.Category: %q (expected short slug)", b.Identity.Category)
	}
	if b.Identity.License != "mit" {
		t.Errorf("Identity.License: %q (expected short slug)", b.Identity.License)
	}
	if b.Sizing.Recommended != "tg-md-1" {
		t.Errorf("Sizing.Recommended: %q", b.Sizing.Recommended)
	}
	if got, want := len(b.Deployment.CompatiblePlatforms), 6; got != want {
		t.Errorf("CompatiblePlatforms: got %d want %d", got, want)
	}
	if b.Deployment.CompatiblePlatforms[0] != "proxmox/lxc/medium" {
		t.Errorf("CompatiblePlatforms[0]: %q (expected short slug)", b.Deployment.CompatiblePlatforms[0])
	}
	if got, want := len(b.Runtime.SecretRequirements), 5; got != want {
		t.Fatalf("SecretRequirements: got %d want %d", got, want)
	}
	if b.Runtime.SecretRequirements[0].Ref != "composites/database_credentials" {
		t.Errorf("SecretRequirements[0].Ref: %q (expected short slug)", b.Runtime.SecretRequirements[0].Ref)
	}
	if b.Runtime.Health.ExpectStatus != 200 {
		t.Errorf("Health.ExpectStatus: %d", b.Runtime.Health.ExpectStatus)
	}
	if err := b.Validate(); err != nil {
		t.Errorf("Validate failed on real inventree: %v", err)
	}
}

func TestBlueprint_ParseDiscoveryEntry(t *testing.T) {
	var b Tango
	if err := json.Unmarshal(activepiecesBlueprintJSON, &b); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if b.Catalog.Active {
		t.Error("discovery entry must be active=false")
	}
	if b.Catalog.Stargazers != 21914 {
		t.Errorf("Stargazers: %d", b.Catalog.Stargazers)
	}
	if b.Runtime.Health.URL != "" {
		t.Errorf("discovery entry should have empty health url, got %q", b.Runtime.Health.URL)
	}
	if b.Runtime.BaoSecretsPath != "" {
		t.Errorf("discovery entry should have empty bao_secrets_path, got %q", b.Runtime.BaoSecretsPath)
	}
	if err := b.Validate(); err != nil {
		t.Errorf("discovery entry Validate: %v", err)
	}
}

func TestBlueprint_JSONRoundTrip(t *testing.T) {
	var original Tango
	if err := json.Unmarshal(inventreeBlueprintJSON, &original); err != nil {
		t.Fatalf("parse: %v", err)
	}
	data, err := original.JSON()
	if err != nil {
		t.Fatalf("JSON(): %v", err)
	}
	var roundTripped Tango
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if roundTripped.AppID != original.AppID {
		t.Errorf("AppID: %q vs %q", roundTripped.AppID, original.AppID)
	}
	if len(roundTripped.Runtime.SecretRequirements) != len(original.Runtime.SecretRequirements) {
		t.Errorf("SecretRequirements count mismatch")
	}
	if len(roundTripped.Deployment.CompatiblePlatforms) != len(original.Deployment.CompatiblePlatforms) {
		t.Errorf("CompatiblePlatforms count mismatch")
	}
}

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

func TestBlueprint_ResolveTier(t *testing.T) {
	if got, want := ResolveTier("tg-md-1", ""), "public/sizing/tg-md-1"; got != want {
		t.Errorf("ResolveTier: got %q want %q", got, want)
	}
}

func TestBlueprint_Validate_AppIDSlug(t *testing.T) {
	for _, id := range []string{"", "Inventree", "inv_app", "-leading"} {
		var b Tango
		_ = json.Unmarshal(inventreeBlueprintJSON, &b)
		b.AppID = id
		if err := b.Validate(); err == nil || !strings.Contains(err.Error(), "app_id") {
			t.Errorf("id=%q: expected app_id error, got %v", id, err)
		}
	}
}

func TestBlueprint_Validate_UUID(t *testing.T) {
	for _, u := range []string{"", "not-a-uuid", "d0afad25-f68e-3c79-b572-739e04d7d789"} {
		var b Tango
		_ = json.Unmarshal(inventreeBlueprintJSON, &b)
		b.UUID = u
		if err := b.Validate(); err == nil || !strings.Contains(err.Error(), "uuid") {
			t.Errorf("uuid=%q: expected uuid error, got %v", u, err)
		}
	}
}

func TestBlueprint_Validate_ActiveRequiresBaoPath(t *testing.T) {
	var b Tango
	_ = json.Unmarshal(inventreeBlueprintJSON, &b)
	b.Runtime.BaoSecretsPath = ""
	if err := b.Validate(); err == nil || !strings.Contains(err.Error(), "bao_secrets_path") {
		t.Errorf("expected bao_secrets_path error, got %v", err)
	}
}

func TestBlueprint_Validate_DiscoveryNoBaoPathOK(t *testing.T) {
	var b Tango
	_ = json.Unmarshal(activepiecesBlueprintJSON, &b)
	if err := b.Validate(); err != nil {
		t.Errorf("discovery without bao path should be valid: %v", err)
	}
}

func TestBlueprint_Validate_SizingTier(t *testing.T) {
	var b Tango
	_ = json.Unmarshal(inventreeBlueprintJSON, &b)
	b.Sizing.Min = "small" // not a tier slug
	if err := b.Validate(); err == nil || !strings.Contains(err.Error(), "sizing.min") {
		t.Errorf("expected sizing.min error, got %v", err)
	}
}

func TestBlueprint_Validate_HealthStatus(t *testing.T) {
	var b Tango
	_ = json.Unmarshal(inventreeBlueprintJSON, &b)
	b.Runtime.Health.ExpectStatus = 99
	if err := b.Validate(); err == nil || !strings.Contains(err.Error(), "expect_status") {
		t.Errorf("expected expect_status error, got %v", err)
	}
}
