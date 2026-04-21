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
