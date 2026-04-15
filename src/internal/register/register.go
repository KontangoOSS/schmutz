// Package register implements the ephemeral Ziti identity registration step.
package register

import (
	"crypto/tls"
	"fmt"
	"strings"

	"github.com/KontangoOSS/schmutz/internal/pipeline"
	"github.com/go-resty/resty/v2"
	"github.com/google/uuid"
)

// Step implements the register bootstrap step.
type Step struct {
	ApiBase  string // override base URL for testing
	Insecure bool   // skip TLS verify
}

// New returns an empty Step. ApiBase will be derived from ctx.Domain in Run.
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

// Run registers an ephemeral Ziti identity with the controller.
func (s *Step) Run(ctx *pipeline.Context) error {
	base := s.ApiBase
	if base == "" {
		base = fmt.Sprintf("https://%s/edge/management/v1", ctx.Domain)
	}

	client := resty.New().
		SetTLSClientConfig(&tls.Config{InsecureSkipVerify: s.Insecure}).
		SetRetryCount(3)

	// 1. Verify controller is reachable.
	versionResp, err := client.R().Get(base + "/version")
	if err != nil {
		return fmt.Errorf("controller unreachable: %w", err)
	}
	if versionResp.StatusCode() != 200 {
		return fmt.Errorf("controller /version returned HTTP %d", versionResp.StatusCode())
	}

	// 2. POST a new ephemeral identity.
	name := BuildIdentityName(ctx.Hostname)
	body := map[string]any{
		"name":           name,
		"type":           "Default",
		"isAdmin":        false,
		"roleAttributes": []string{"ephemeral", "base"},
		"enrollment":     map[string]any{"ott": true},
	}

	resp, err := client.R().
		SetHeader("Content-Type", "application/json").
		SetBody(body).
		Post(base + "/identities")
	if err != nil {
		return fmt.Errorf("identity POST failed: %w", err)
	}

	code := resp.StatusCode()
	if code != 200 && code != 201 {
		return fmt.Errorf("identity POST returned HTTP %d: %s", code, strings.TrimSpace(resp.String()))
	}

	ctx.Registered = true
	return nil
}

// BuildIdentityName returns an ephemeral identity name using the first 8
// characters of a new UUID: "eph-<hostname>-<uuid8>".
func BuildIdentityName(hostname string) string {
	id := uuid.New().String()
	short := strings.ReplaceAll(id, "-", "")[:8]
	return fmt.Sprintf("eph-%s-%s", hostname, short)
}
