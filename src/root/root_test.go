package root_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/KontangoOSS/schmutz/root"
)

func TestLoadRoot_notEnabled(t *testing.T) {
	dir := t.TempDir()
	r, err := root.LoadRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.IsEnabled() {
		t.Error("expected IsEnabled=false when identity missing")
	}
}

func TestLoadRoot_enabled(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "identity.json"), []byte(`{}`), 0600)
	r, err := root.LoadRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !r.IsEnabled() {
		t.Error("expected IsEnabled=true when identity exists")
	}
}

func TestRoot_paths(t *testing.T) {
	dir := t.TempDir()
	r, _ := root.LoadRoot(dir)

	sock, _ := r.AgentSocket()
	if sock == "" {
		t.Error("AgentSocket should not be empty")
	}
	reg, _ := r.AgentRegistry()
	if reg == "" {
		t.Error("AgentRegistry should not be empty")
	}
	id, _ := r.IdentityPath()
	if id == "" {
		t.Error("IdentityPath should not be empty")
	}
}

func TestLoadRoot_withManifest(t *testing.T) {
	dir := t.TempDir()
	manifest := []byte("device_id: test-node-42\ncontroller_url: https://ctrl.example.com\n")
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	r, err := root.LoadRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.DeviceID() != "test-node-42" {
		t.Errorf("DeviceID: got %q, want %q", r.DeviceID(), "test-node-42")
	}
	if r.ControllerURL() != "https://ctrl.example.com" {
		t.Errorf("ControllerURL: got %q", r.ControllerURL())
	}
	if err := r.Validate(); err != nil {
		t.Errorf("Validate() unexpected error: %v", err)
	}
}

func TestRoot_validateMissingDeviceID(t *testing.T) {
	dir := t.TempDir()
	// No manifest — zero config
	r, err := root.LoadRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Validate() == nil {
		t.Error("expected Validate() to error when device_id is empty")
	}
}

func TestSetAndGetServices(t *testing.T) {
	dir := t.TempDir()
	r, err := root.LoadRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Initially empty
	if got := r.Services(); len(got) != 0 {
		t.Errorf("expected empty services, got %v", got)
	}
	// Set and retrieve
	want := []string{"ssh-web-1", "http-web-1"}
	if err := r.SetServices(want); err != nil {
		t.Fatal(err)
	}
	// Reload from disk
	r2, err := root.LoadRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := r2.Services()
	if len(got) != len(want) {
		t.Fatalf("services: got %v, want %v", got, want)
	}
	for i, s := range want {
		if got[i] != s {
			t.Errorf("services[%d]: got %q, want %q", i, got[i], s)
		}
	}
}
