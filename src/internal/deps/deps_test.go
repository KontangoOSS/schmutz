package deps_test

import (
	"testing"

	"github.com/KontangoOSS/schmutz/internal/deps"
	"github.com/KontangoOSS/schmutz/internal/pipeline"
)

// TestLoadManifest verifies the embedded manifest parses successfully and
// contains at least one binary entry.
func TestLoadManifest(t *testing.T) {
	m, err := deps.LoadManifest()
	if err != nil {
		t.Fatalf("loadManifest returned error: %v", err)
	}
	if len(m.Binaries) == 0 {
		t.Error("expected at least one binary in manifest, got none")
	}
}

// TestCheckBinaryExists verifies that a well-known binary ("ls") is found.
func TestCheckBinaryExists(t *testing.T) {
	found, path := deps.CheckBinary("ls")
	if !found {
		t.Error("expected 'ls' to be found on PATH")
	}
	if path == "" {
		t.Error("expected a non-empty path for 'ls'")
	}
}

// TestCheckBinaryMissing verifies that a non-existent binary is not found.
func TestCheckBinaryMissing(t *testing.T) {
	found, path := deps.CheckBinary("nonexistent-xyz-123")
	if found {
		t.Error("expected 'nonexistent-xyz-123' to be absent from PATH")
	}
	if path != "" {
		t.Errorf("expected empty path for missing binary, got %q", path)
	}
}

// TestName verifies the step returns the correct display name.
func TestName(t *testing.T) {
	s := deps.New()
	if s.Name() != "Check Dependencies" {
		t.Errorf("expected %q, got %q", "Check Dependencies", s.Name())
	}
}

// TestSkipsWhenDone verifies that the step is skipped when ctx.DepsOK is true.
func TestSkipsWhenDone(t *testing.T) {
	s := deps.New()
	ctx := pipeline.NewContext()
	ctx.DepsOK = true
	if !s.Skip(ctx) {
		t.Error("expected Skip to return true when ctx.DepsOK is true")
	}
}
