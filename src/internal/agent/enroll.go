package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// EnrollJWT takes a raw JWT string from the /enroll endpoint and enrolls it
// using the ziti CLI to produce a real x509 identity JSON file.
//
// The flow:
//  1. Save JWT to a temp file
//  2. Run `ziti edge enroll <jwt-file> -o <identity-path>`
//  3. Verify the identity file was created
//  4. Clean up the temp JWT file
func EnrollJWT(jwt string) error {
	if err := EnsureDir(); err != nil {
		return fmt.Errorf("create identity dir: %w", err)
	}

	// Save JWT to temp file
	jwtPath := filepath.Join(IdentityDir(), "enrollment.jwt")
	if err := os.WriteFile(jwtPath, []byte(jwt), 0600); err != nil {
		return fmt.Errorf("write JWT: %w", err)
	}
	defer os.Remove(jwtPath)

	identityPath := IdentityPath()

	// Find ziti binary
	zitiBin, err := findZitiBinary()
	if err != nil {
		return fmt.Errorf("ziti binary not found: %w", err)
	}

	// Run enrollment
	fmt.Printf("  → enrolling with %s\n", zitiBin)
	cmd := exec.Command(zitiBin, "edge", "enroll", jwtPath, "-o", identityPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ziti enroll failed: %w", err)
	}

	// Verify identity was created
	info, err := os.Stat(identityPath)
	if err != nil {
		return fmt.Errorf("identity file not created: %w", err)
	}
	fmt.Printf("  ✓ identity saved (%d bytes)\n", info.Size())

	return nil
}

// findZitiBinary locates the ziti binary on the system.
func findZitiBinary() (string, error) {
	// Check common locations
	paths := []string{
		"/usr/local/bin/ziti",
		"/usr/bin/ziti",
		"/opt/kontango/bin/ziti",
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	// Check PATH
	p, err := exec.LookPath("ziti")
	if err == nil {
		return p, nil
	}

	return "", fmt.Errorf("ziti not found in PATH or common locations")
}
