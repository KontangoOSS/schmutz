package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFindZitiBinary_EnvVarTakesPrecedence(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "ziti-fake")
	if err := os.WriteFile(fake, []byte("fake"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SCHMUTZ_ZITI_BIN", fake)

	got, err := findZitiBinary()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != fake {
		t.Errorf("got %q, want %q", got, fake)
	}
}

func TestFindZitiBinary_EnvVarMissingFile(t *testing.T) {
	t.Setenv("SCHMUTZ_ZITI_BIN", "/nope/does/not/exist")
	_, err := findZitiBinary()
	if err == nil {
		t.Error("expected error for missing SCHMUTZ_ZITI_BIN target, got nil")
	}
}

func TestFindZitiBinary_FallsBackToPath(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("PATH layout test is linux-specific")
	}
	// Clear the env var so PATH lookup runs
	t.Setenv("SCHMUTZ_ZITI_BIN", "")

	// On this dev workstation /usr/local/bin/ziti exists per the build setup;
	// if not, this test should self-skip rather than fail spuriously.
	if _, err := os.Stat(defaultZitiPath); err != nil {
		t.Skip("no ziti at default path; cannot exercise fallback")
	}
	got, err := findZitiBinary()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != defaultZitiPath {
		t.Errorf("got %q, want %q (default)", got, defaultZitiPath)
	}
}
