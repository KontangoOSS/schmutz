package agent

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// EnrollJWT takes a raw JWT string from the /enroll endpoint and enrolls it
// using the ziti CLI to produce a real x509 identity JSON file.
func EnrollJWT(jwt string) error {
	if err := EnsureDir(); err != nil {
		return fmt.Errorf("create identity dir: %w", err)
	}

	jwtPath := filepath.Join(IdentityDir(), "enrollment.jwt")
	if err := os.WriteFile(jwtPath, []byte(jwt), 0600); err != nil {
		return fmt.Errorf("write JWT: %w", err)
	}
	defer os.Remove(jwtPath)

	identityPath := IdentityPath()

	zitiBin, err := findZitiBinary()
	if err != nil {
		return fmt.Errorf("ziti binary not found: %w", err)
	}

	// Fetch controller CA and pass --ca so enrollment works on systems that
	// haven't previously trusted the Ziti PKI (avoids "token signature is invalid").
	args := []string{"edge", "enroll", "--jwt", jwtPath, "-o", identityPath}
	if caPath, err := fetchAndSaveCA(jwt); err != nil {
		slog.Warn("could not fetch controller CA, enrolling without --ca", "err", err)
	} else {
		args = append(args, "--ca", caPath)
	}

	slog.Info("enrolling Ziti identity", "binary", zitiBin, "output", identityPath)
	cmd := exec.Command(zitiBin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ziti enroll failed: %w", err)
	}

	info, err := os.Stat(identityPath)
	if err != nil {
		return fmt.Errorf("identity file not created: %w", err)
	}
	slog.Info("identity saved", "path", identityPath, "bytes", info.Size())

	return nil
}

// fetchAndSaveCA extracts the controller URL from the JWT claims, fetches the
// CA PEM from /.well-known/est/cacerts, saves it, and returns the path.
func fetchAndSaveCA(jwt string) (string, error) {
	ctrlURL, err := controllerURLFromJWT(jwt)
	if err != nil {
		return "", fmt.Errorf("parse JWT: %w", err)
	}

	caURL := strings.TrimRight(ctrlURL, "/") + "/.well-known/est/cacerts"
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // bootstrap trust
		},
	}
	resp, err := client.Get(caURL)
	if err != nil {
		return "", fmt.Errorf("fetch CA: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch CA: HTTP %d", resp.StatusCode)
	}

	pem, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read CA: %w", err)
	}

	caPath := filepath.Join(IdentityDir(), "ca.pem")
	if err := os.WriteFile(caPath, pem, 0644); err != nil {
		return "", fmt.Errorf("save CA: %w", err)
	}
	return caPath, nil
}

// controllerURLFromJWT decodes the JWT claims (without verification) and
// returns the first controller URL from the "ctrls" claim.
func controllerURLFromJWT(jwt string) (string, error) {
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid JWT format")
	}
	// JWT uses base64url without padding
	payload := parts[1]
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}
	data, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return "", fmt.Errorf("decode claims: %w", err)
	}

	var claims struct {
		Ctrls []struct {
			Name string `json:"name"`
		} `json:"ctrls"`
		// fallback: some JWTs use a flat "iss" URL
		Issuer string `json:"iss"`
	}
	if err := json.Unmarshal(data, &claims); err != nil {
		return "", fmt.Errorf("parse claims: %w", err)
	}
	if len(claims.Ctrls) > 0 && claims.Ctrls[0].Name != "" {
		return claims.Ctrls[0].Name, nil
	}
	if claims.Issuer != "" {
		return claims.Issuer, nil
	}
	return "", fmt.Errorf("no controller URL in JWT claims")
}

// findZitiBinary locates the ziti binary on the system.
func findZitiBinary() (string, error) {
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

	p, err := exec.LookPath("ziti")
	if err == nil {
		return p, nil
	}

	return "", fmt.Errorf("ziti not found in PATH or common locations")
}
