package profiles_test

import (
	"os"
	"path/filepath"
	"testing"

	"git.konoss.org/kore/schmutz/controller/internal/controller/profiles"
)

func writeYAML(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadProfiles_basic(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "edge-router.yaml", `
name: edge-router
description: "Edge router"
attributes:
  - "#type-edge-router"
extra_services: []
`)
	reg, err := profiles.LoadProfiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := reg.Get("edge-router")
	if p == nil {
		t.Fatal("expected edge-router profile, got nil")
	}
	if p.Name != "edge-router" {
		t.Errorf("name: got %q want %q", p.Name, "edge-router")
	}
	if len(p.Attributes) != 1 || p.Attributes[0] != "#type-edge-router" {
		t.Errorf("attributes: got %v", p.Attributes)
	}
}

func TestLoadProfiles_fallback(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "base.yaml", `
name: base
description: "Default"
attributes: []
extra_services: []
`)
	reg, err := profiles.LoadProfiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Unknown profile name falls back to base
	p := reg.Get("nonexistent")
	if p == nil {
		t.Fatal("expected fallback to base, got nil")
	}
	if p.Name != "base" {
		t.Errorf("fallback: got %q want %q", p.Name, "base")
	}
}

func TestLoadProfiles_emptyDir(t *testing.T) {
	dir := t.TempDir()
	reg, err := profiles.LoadProfiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	// No profiles loaded — Get returns nil (no base to fall back to)
	p := reg.Get("anything")
	if p != nil {
		t.Errorf("expected nil for empty registry, got %+v", p)
	}
}

func TestLoadProfiles_missingDir(t *testing.T) {
	// Missing directory logs a warning and returns an empty registry, not an error
	reg, err := profiles.LoadProfiles("/tmp/does-not-exist-schmutz-profiles-xyz")
	if err != nil {
		t.Errorf("expected nil error for missing dir, got: %v", err)
	}
	if reg == nil {
		t.Fatal("expected non-nil registry")
	}
}

func TestLoadProfiles_extraServices(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "metrics.yaml", `
name: metrics
description: "Metrics node"
attributes:
  - "#type-metrics"
extra_services:
  - name: prometheus
    port: 9090
`)
	reg, _ := profiles.LoadProfiles(dir)
	p := reg.Get("metrics")
	if p == nil {
		t.Fatal("expected metrics profile")
	}
	if len(p.ExtraServices) != 1 {
		t.Fatalf("extra_services: got %d want 1", len(p.ExtraServices))
	}
	if p.ExtraServices[0].Name != "prometheus" || p.ExtraServices[0].Port != 9090 {
		t.Errorf("extra service: got %+v", p.ExtraServices[0])
	}
}
