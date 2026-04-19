package agent

import (
	"fmt"
	"os"
	"path/filepath"
)

// identityOverride is set by tests to redirect identity storage to a temp dir.
var identityOverride string

// IdentityDir returns the directory for storing identity files.
// If running as root (uid 0): /opt/schmutz/
// Otherwise: ~/.schmutz/
// If identityOverride is set (tests only), it returns that instead.
func IdentityDir() string {
	if identityOverride != "" {
		return identityOverride
	}
	if os.Getuid() == 0 {
		return "/opt/schmutz"
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}
	return filepath.Join(home, ".schmutz")
}

// IdentityPath returns the full path to the identity JSON file.
func IdentityPath() string {
	return filepath.Join(IdentityDir(), "identity.json")
}

// IsEnrolled checks if an identity file exists at IdentityPath().
func IsEnrolled() bool {
	_, err := os.Stat(IdentityPath())
	return err == nil
}

// SaveIdentityJSON writes raw identity JSON bytes to IdentityPath().
// Uses an atomic write (temp file + rename) to avoid partial writes on power loss.
func SaveIdentityJSON(data []byte) error {
	if err := EnsureDir(); err != nil {
		return err
	}
	tmp := IdentityPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, IdentityPath())
}

// LoadIdentityFile reads the identity JSON file and returns the path.
// Returns an error if the file doesn't exist.
func LoadIdentityFile() (string, error) {
	p := IdentityPath()
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("identity file not found at %s: %w", p, err)
	}
	return p, nil
}

// EnsureDir creates the identity directory if it doesn't exist.
func EnsureDir() error {
	return os.MkdirAll(IdentityDir(), 0700)
}
