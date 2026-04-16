// Package register implements the device enrollment step via the /enroll endpoint.
package register

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/KontangoOSS/schmutz/internal/pipeline"
)

// Step implements the register bootstrap step.
type Step struct {
	ApiBase string // override base URL for testing (e.g. mock server URL)
}

// EnrollResponse represents the JSON body returned by POST /enroll.
type EnrollResponse struct {
	Status     string `json:"status"`
	IdentityID string `json:"identity_id,omitempty"`
	JWT        string `json:"jwt,omitempty"`
	Tier       string `json:"tier,omitempty"`
	Message    string `json:"message,omitempty"`
}

// New returns a Step ready for use.
func New() *Step {
	return &Step{}
}

// Name returns the display name of this step.
func (s *Step) Name() string {
	return "Register Device"
}

// Skip returns true when the device is already registered or has an identity.
func (s *Step) Skip(ctx *pipeline.Context) bool {
	return ctx.Registered || ctx.Identity != ""
}

// Run enrolls the device by POSTing machine info to the /enroll endpoint.
// No admin credentials are used — the controller handles authorization server-side.
func (s *Step) Run(ctx *pipeline.Context) error {
	enrollURL := s.enrollURL(ctx)

	fingerprint := machineFingerprint()

	payload := map[string]any{
		"hostname":   ctx.Hostname,
		"os":         ctx.OS,
		"arch":       ctx.Arch,
		"platform":   ctx.Platform,
		"fingerprint": fingerprint,
		"agent_data": map[string]any{
			"source":  "schmutz-agent",
			"version": "0.1.0",
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal enroll payload: %w", err)
	}

	fmt.Printf("  → enrolling at %s\n", enrollURL)

	req, err := http.NewRequest("POST", enrollURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "schmutz-agent/0.1.0")

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("enroll request failed: %w", err)
	}
	defer resp.Body.Close()

	var result EnrollResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode enroll response (HTTP %d): %w", resp.StatusCode, err)
	}

	switch {
	case resp.StatusCode == http.StatusOK && result.Status == "approved":
		if result.JWT == "" {
			return fmt.Errorf("enrollment approved but no JWT returned")
		}
		ctx.JWT = result.JWT
		ctx.Tier = result.Tier
		ctx.Registered = true
		fmt.Printf("  ✓ enrolled (identity: %s, tier: %s)\n", result.IdentityID, result.Tier)
		return nil

	case resp.StatusCode == http.StatusPreconditionRequired || result.Status == "agent_required":
		return fmt.Errorf("enrollment requires agent install: %s", result.Message)

	default:
		msg := result.Message
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("enrollment denied: %s", msg)
	}
}

// enrollURL returns the full URL for the /enroll endpoint.
// If ApiBase is set it is used as-is with "/enroll" appended; otherwise the
// domain from ctx is used.
func (s *Step) enrollURL(ctx *pipeline.Context) string {
	if s.ApiBase != "" {
		return s.ApiBase + "/enroll"
	}
	return fmt.Sprintf("https://%s/enroll", ctx.Domain)
}

// machineFingerprint returns a hex SHA256 of /etc/machine-id, or an empty
// string when the file is unavailable.
func machineFingerprint() string {
	data, err := os.ReadFile("/etc/machine-id")
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(bytes.TrimSpace(data))
	return fmt.Sprintf("%x", sum)
}
