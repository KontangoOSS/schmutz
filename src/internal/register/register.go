// Package register implements the device enrollment step via the /enroll endpoint.
package register

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/KontangoOSS/schmutz/internal/agent"
	"github.com/KontangoOSS/schmutz/internal/pipeline"
)

// Step implements the register bootstrap step.
type Step struct {
	ApiBase    string // override base URL for testing
	MaxRetries int    // enrollment retries (default 3)
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
	return &Step{MaxRetries: 3}
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
// Retries on transient failures. No admin credentials — the controller
// handles authorization server-side.
func (s *Step) Run(ctx *pipeline.Context) error {
	enrollURL := s.enrollURL(ctx)
	fingerprint := machineFingerprint()

	payload := map[string]any{
		"hostname":    ctx.Hostname,
		"os":          ctx.OS,
		"arch":        ctx.Arch,
		"platform":    ctx.Platform,
		"fingerprint": fingerprint,
		"agent_data": map[string]any{
			"source":  "schmutz-agent",
			"version": agent.AgentVersion,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	retries := s.MaxRetries
	if retries < 1 {
		retries = 1
	}

	var lastErr error
	for attempt := 1; attempt <= retries; attempt++ {
		if attempt > 1 {
			wait := time.Duration(attempt*2) * time.Second
			slog.Warn("retrying enrollment", "attempt", attempt, "wait", wait)
			time.Sleep(wait)
		}

		lastErr = s.doEnroll(ctx, enrollURL, body)
		if lastErr == nil {
			return nil
		}

		slog.Error("enrollment attempt failed", "attempt", attempt, "error", lastErr)
	}

	return fmt.Errorf("enrollment failed after %d attempts: %w", retries, lastErr)
}

func (s *Step) doEnroll(ctx *pipeline.Context, enrollURL string, body []byte) error {
	slog.Info("enrolling", "url", enrollURL)

	req, err := http.NewRequest("POST", enrollURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", agent.UserAgent)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	var result EnrollResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode response (HTTP %d): %w", resp.StatusCode, err)
	}

	switch {
	case resp.StatusCode == http.StatusOK && result.Status == "approved":
		if result.JWT == "" {
			return fmt.Errorf("approved but no JWT returned")
		}
		ctx.JWT = result.JWT
		ctx.Tier = result.Tier
		ctx.Registered = true
		slog.Info("enrolled", "identity", result.IdentityID, "tier", result.Tier)
		return nil

	case resp.StatusCode == http.StatusPreconditionRequired || result.Status == "agent_required":
		return fmt.Errorf("agent install required: %s", result.Message)

	default:
		msg := result.Message
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("denied: %s", msg)
	}
}

// enrollURL returns the full URL for the /enroll endpoint.
func (s *Step) enrollURL(ctx *pipeline.Context) string {
	if s.ApiBase != "" {
		return s.ApiBase + "/enroll"
	}
	return fmt.Sprintf("https://%s/enroll", ctx.Domain)
}

// machineFingerprint returns a hex SHA256 of /etc/machine-id.
func machineFingerprint() string {
	data, err := os.ReadFile("/etc/machine-id")
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(bytes.TrimSpace(data))
	return fmt.Sprintf("%x", sum)
}
