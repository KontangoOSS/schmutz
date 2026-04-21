package enroll

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"time"

	zitiEnroll "github.com/openziti/sdk-golang/ziti/enroll"
)

// DeviceInfo is the identifying data sent to POST /enroll.
// All fields match DeviceEnrollRequest on the controller side.
type DeviceInfo struct {
	Hostname    string
	OS          string
	Arch        string
	Platform    string // "lxc", "vm", "cloud", "docker", "baremetal"
	MachineID   string // contents of /etc/machine-id
	Fingerprint string // SHA256(machine_id + primary_mac + ...)
	AgentData   map[string]any
}

type enrollRequest struct {
	Hostname    string         `json:"hostname"`
	OS          string         `json:"os"`
	Arch        string         `json:"arch"`
	Platform    string         `json:"platform,omitempty"`
	MachineID   string         `json:"machine_id,omitempty"`
	Fingerprint string         `json:"fingerprint,omitempty"`
	Slug        string         `json:"slug,omitempty"`
	AgentData   map[string]any `json:"agent_data,omitempty"`
}

type enrollResponse struct {
	Status     string `json:"status"`
	JWT        string `json:"jwt,omitempty"`
	Slug       string `json:"slug,omitempty"`
	NodeURL    string `json:"node_url,omitempty"`
	RetryAfter int    `json:"retry_after,omitempty"`
	Message    string `json:"message,omitempty"`
}

// NeedsEnrollment returns true if identity file does not exist.
func NeedsEnrollment(identityPath string) bool {
	_, err := os.Stat(identityPath)
	return errors.Is(err, fs.ErrNotExist)
}

// Register posts to <controllerURL>/enroll and polls until approved.
// Pins retries to response.node_url when provided (enrollment windows are per-node).
func Register(ctx context.Context, controllerURL string, info DeviceInfo) (jwt, slug string, err error) {
	if controllerURL == "" {
		return "", "", fmt.Errorf("enroll: controller_url required")
	}
	req := enrollRequest{
		Hostname:    info.Hostname,
		OS:          info.OS,
		Arch:        info.Arch,
		Platform:    info.Platform,
		MachineID:   info.MachineID,
		Fingerprint: info.Fingerprint,
		Slug:        info.Hostname,
		AgentData:   info.AgentData,
	}
	pollURL := controllerURL + "/enroll"
	client := &http.Client{Timeout: 30 * time.Second}
	for {
		body, err := json.Marshal(req)
		if err != nil {
			return "", "", fmt.Errorf("enroll: marshal request: %w", err)
		}
		resp, err := client.Post(pollURL, "application/json", bytes.NewReader(body))
		if err != nil {
			return "", "", fmt.Errorf("enroll: POST %s: %w", pollURL, err)
		}
		if resp.StatusCode == http.StatusPreconditionRequired {
			// 428: controller says we need the schmutz agent — shouldn't happen
			// if we're sending proper device info. Abort with a clear message.
			resp.Body.Close()
			return "", "", fmt.Errorf("enroll: controller rejected request (428): ensure fingerprint and platform fields are populated")
		}
		if resp.StatusCode >= 400 {
			resp.Body.Close()
			return "", "", fmt.Errorf("enroll: HTTP %d from %s", resp.StatusCode, pollURL)
		}
		var result enrollResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			return "", "", fmt.Errorf("enroll: decode response from %s: %w", pollURL, err)
		}
		resp.Body.Close()

		switch result.Status {
		case "approved":
			return result.JWT, result.Slug, nil
		case "busy", "banned":
			return "", "", fmt.Errorf("enroll: rejected — %s", result.Message)
		default:
			wait := time.Duration(result.RetryAfter) * time.Second
			if wait < 15*time.Second {
				wait = 15 * time.Second
			}
			if result.NodeURL != "" {
				pollURL = result.NodeURL + "/enroll"
			}
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return "", "", ctx.Err()
			}
		}
	}
}

// EnrollJWT exchanges a Ziti one-time JWT for a permanent identity file.
func EnrollJWT(jwtStr, identityPath string) error {
	if jwtStr == "" {
		return fmt.Errorf("enroll: jwt required")
	}
	claims, jwtToken, err := zitiEnroll.ParseToken(jwtStr)
	if err != nil {
		return fmt.Errorf("enroll: parse JWT: %w", err)
	}
	flags := zitiEnroll.EnrollmentFlags{
		Token:     claims,
		JwtToken:  jwtToken,
		JwtString: jwtStr,
	}
	enrolled, err := zitiEnroll.Enroll(flags)
	if err != nil {
		return fmt.Errorf("enroll: ziti: %w", err)
	}
	data, err := json.Marshal(enrolled)
	if err != nil {
		return fmt.Errorf("enroll: marshal identity: %w", err)
	}
	return os.WriteFile(identityPath, data, 0600)
}
