package preflight

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	defaultThreshold    = 50
	connectivityURL     = "https://cloudflare.com/cdn-cgi/trace"
	publicIPURL         = "https://api.ipify.org"
	abuseIPDBCheckURL   = "https://api.abuseipdb.com/api/v2/check"
	httpTimeoutSeconds  = 5
)

// Step implements the preflight bootstrap step.
type Step struct {
	apiKey    string
	threshold int
}

// New returns a new preflight Step. Pass an empty string to skip threat scoring.
func New(abuseIPDBKey string) *Step {
	return &Step{
		apiKey:    abuseIPDBKey,
		threshold: defaultThreshold,
	}
}

// Name returns the display name of this step.
func (s *Step) Name() string {
	return "Preflight Check"
}

// Skip always returns false — the preflight step is mandatory.
func (s *Step) Skip() bool {
	return false
}

// Run executes the preflight checks.
func (s *Step) Run() error {
	// 1. Connectivity check
	if err := CheckConnectivity(connectivityURL); err != nil {
		return fmt.Errorf("connectivity check failed: %w", err)
	}

	// 2. Public IP
	ip, err := GetPublicIPFrom(publicIPURL)
	if err != nil {
		return fmt.Errorf("failed to resolve public IP: %w", err)
	}

	// 3. No API key — skip threat check
	if s.apiKey == "" {
		slog.Warn("no AbuseIPDB key configured, skipping threat score check")
		return nil
	}

	// 4. Threat score
	score, err := CheckThreatScore(s.apiKey, ip)
	if err != nil {
		return fmt.Errorf("threat score check failed: %w", err)
	}

	// 5. Abort if score meets or exceeds threshold
	if ShouldAbort(score, s.threshold) {
		return fmt.Errorf("public IP %s has AbuseIPDB confidence score %d (threshold %d) — install aborted", ip, score, s.threshold)
	}

	// 6. Warn if score is elevated but below threshold
	if score > 20 {
		slog.Warn("elevated AbuseIPDB score", "ip", ip, "score", score)
	}

	return nil
}

// CheckConnectivity performs an HTTP GET to url with a 5-second timeout.
// It is exported for testing.
func CheckConnectivity(url string) error {
	client := &http.Client{Timeout: httpTimeoutSeconds * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// GetPublicIPFrom fetches the public IP from url and trims whitespace.
// It is exported for testing.
func GetPublicIPFrom(url string) (string, error) {
	client := &http.Client{Timeout: httpTimeoutSeconds * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}

// ShouldAbort returns true when score >= threshold.
// It is exported for testing.
func ShouldAbort(score, threshold int) bool {
	return score >= threshold
}

// abuseIPDBResponse mirrors the relevant fields from the v2 API response.
type abuseIPDBResponse struct {
	Data struct {
		AbuseConfidenceScore int `json:"abuseConfidenceScore"`
	} `json:"data"`
}

// CheckThreatScore queries AbuseIPDB and returns the confidence score (0-100).
// It is exported for testing.
func CheckThreatScore(apiKey, ip string) (int, error) {
	client := &http.Client{Timeout: httpTimeoutSeconds * time.Second}

	req, err := http.NewRequest(http.MethodGet, abuseIPDBCheckURL, nil)
	if err != nil {
		return 0, err
	}
	q := req.URL.Query()
	q.Set("ipAddress", ip)
	q.Set("maxAgeInDays", "90")
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Key", apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("AbuseIPDB returned HTTP %d", resp.StatusCode)
	}

	var parsed abuseIPDBResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return 0, fmt.Errorf("failed to parse AbuseIPDB response: %w", err)
	}

	return parsed.Data.AbuseConfidenceScore, nil
}
