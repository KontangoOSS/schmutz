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

type enrollRequest struct {
	Hostname    string         `json:"hostname"`
	OS          string         `json:"os"`
	Arch        string         `json:"arch"`
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
func Register(ctx context.Context, controllerURL, hostname, osName, arch, fingerprint string, agentData map[string]any) (jwt, slug string, err error) {
	if controllerURL == "" {
		return "", "", fmt.Errorf("enroll: controller_url required")
	}
	req := enrollRequest{
		Hostname:    hostname,
		OS:          osName,
		Arch:        arch,
		Fingerprint: fingerprint,
		Slug:        hostname,
		AgentData:   agentData,
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
		if resp.StatusCode >= 500 {
			resp.Body.Close()
			return "", "", fmt.Errorf("enroll: server error %d from %s", resp.StatusCode, pollURL)
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
