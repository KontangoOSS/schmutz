package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// resetOverride clears the package-level override after each test.
func resetOverride(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { identityOverride = "" })
}

// TestIdentityPathNotRoot verifies that when not running as root the identity
// path contains ".schmutz".
func TestIdentityPathNotRoot(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root — skipping non-root path test")
	}
	p := IdentityPath()
	if !strings.Contains(p, ".schmutz") {
		t.Errorf("expected path to contain .schmutz, got: %s", p)
	}
	if !strings.HasSuffix(p, "identity.json") {
		t.Errorf("expected path to end with identity.json, got: %s", p)
	}
}

// TestIdentityPathRoot verifies that when running as root the path is
// /opt/schmutz/identity.json. Skipped when not running as root.
func TestIdentityPathRoot(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("not running as root — skipping root path test")
	}
	want := "/opt/schmutz/identity.json"
	if got := IdentityPath(); got != want {
		t.Errorf("expected %s, got %s", want, got)
	}
}

// TestIsEnrolledFalseWhenMissing verifies IsEnrolled returns false when no
// identity file exists.
func TestIsEnrolledFalseWhenMissing(t *testing.T) {
	resetOverride(t)
	identityOverride = t.TempDir()
	if IsEnrolled() {
		t.Error("expected IsEnrolled() == false when file is absent")
	}
}

// TestSaveAndLoadRoundTrip saves bytes, loads the path, reads the file back,
// and verifies the content matches.
func TestSaveAndLoadRoundTrip(t *testing.T) {
	resetOverride(t)
	identityOverride = t.TempDir()

	want := []byte(`{"id":"test-machine","ztAPI":"https://ctrl.example"}`)

	if err := SaveIdentityJSON(want); err != nil {
		t.Fatalf("SaveIdentityJSON: %v", err)
	}

	path, err := LoadIdentityFile()
	if err != nil {
		t.Fatalf("LoadIdentityFile: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if string(got) != string(want) {
		t.Errorf("content mismatch\nwant: %s\n got: %s", want, got)
	}
}

// TestSaveCreatesDirectory verifies that SaveIdentityJSON creates the directory
// hierarchy if it doesn't already exist.
func TestSaveCreatesDirectory(t *testing.T) {
	resetOverride(t)
	// Use a subdirectory inside TempDir that does not yet exist.
	base := t.TempDir()
	identityOverride = filepath.Join(base, "nested", "schmutz")

	data := []byte(`{"id":"dir-creation-test"}`)
	if err := SaveIdentityJSON(data); err != nil {
		t.Fatalf("SaveIdentityJSON: %v", err)
	}

	if _, err := os.Stat(identityOverride); err != nil {
		t.Errorf("expected directory to exist after save: %v", err)
	}

	if _, err := os.Stat(IdentityPath()); err != nil {
		t.Errorf("expected identity file to exist after save: %v", err)
	}
}
