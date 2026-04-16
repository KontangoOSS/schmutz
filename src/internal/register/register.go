// Package register implements the device enrollment step via the /enroll endpoint.
package register

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/KontangoOSS/schmutz/internal/agent"
	"github.com/KontangoOSS/schmutz/internal/pipeline"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
)

// Step implements the register bootstrap step.
type Step struct {
	ApiBase    string // override base URL for testing
	MaxRetries int    // enrollment retries (default 3)
}

// EnrollResponse represents the JSON body returned by POST /enroll.
type EnrollResponse struct {
	Status      string `json:"status"`
	IdentityID  string `json:"identity_id,omitempty"`
	JWT         string `json:"jwt,omitempty"`
	Tier        string `json:"tier,omitempty"`
	RetryAfter  int    `json:"retry_after,omitempty"`
	StreamToken string `json:"stream_token,omitempty"`
	NodeURL     string `json:"node_url,omitempty"` // pin to this node for window duration
	Message     string `json:"message,omitempty"`
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
// On a "pending" response, streams telemetry via POST /stream for retry_after
// seconds using the issued stream_token, then retries /enroll.
func (s *Step) Run(ctx *pipeline.Context) error {
	enrollURL := s.apiURL(ctx, "/enroll")
	streamURL := s.apiURL(ctx, "/stream")
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

		lastErr = s.doEnroll(ctx, enrollURL, streamURL, fingerprint, body)
		if lastErr == nil {
			return nil
		}

		slog.Error("enrollment attempt failed", "attempt", attempt, "error", lastErr)
	}

	return fmt.Errorf("enrollment failed after %d attempts: %w", retries, lastErr)
}

func (s *Step) doEnroll(ctx *pipeline.Context, enrollURL, streamURL, fingerprint string, body []byte) error {
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

	case resp.StatusCode == http.StatusOK && result.Status == "pending":
		waitSecs := result.RetryAfter
		if waitSecs <= 0 {
			waitSecs = 60
		}
		token := result.StreamToken

		// Pin to the specific node that opened our window.
		// Enrollment windows are in-memory per-node — if we retry on a different
		// node it won't find our window. node_url overrides the LB for this session.
		pinnedEnrollURL := enrollURL
		pinnedStreamURL := streamURL
		if result.NodeURL != "" {
			pinnedEnrollURL = result.NodeURL + "/enroll"
			pinnedStreamURL = result.NodeURL + "/stream"
			slog.Info("pinned to node", "node", result.NodeURL)
		}

		slog.Info("enrollment pending — streaming telemetry", "url", pinnedStreamURL, "wait_secs", waitSecs)

		streamCtx, cancel := context.WithTimeout(context.Background(), time.Duration(waitSecs)*time.Second)
		defer cancel()
		go streamMetrics(streamCtx, pinnedStreamURL, fingerprint, token)

		<-streamCtx.Done()

		slog.Info("telemetry window closed — retrying enrollment")
		return s.doEnroll(ctx, pinnedEnrollURL, pinnedStreamURL, fingerprint, body)

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

// streamMetrics posts system metrics to POST /stream every 5 seconds until ctx
// is cancelled. Uses the stream_token issued by the controller to authenticate.
// Best-effort — errors are logged but do not abort the enrollment.
func streamMetrics(ctx context.Context, streamURL, fingerprint, token string) {
	hostname, _ := os.Hostname()
	client := &http.Client{Timeout: 10 * time.Second}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	send := func() {
		cpuPct, _ := cpu.Percent(0, false)
		vmStat, _ := mem.VirtualMemory()
		ldStat, _ := load.Avg()

		cpuPctVal := 0.0
		if len(cpuPct) > 0 {
			cpuPctVal = cpuPct[0]
		}
		var load1, load5, load15 float64
		if ldStat != nil {
			load1, load5, load15 = ldStat.Load1, ldStat.Load5, ldStat.Load15
		}

		metric := map[string]any{
			"fingerprint":  fingerprint,
			"stream_token": token,
			"hostname":     hostname,
			"cpu_percent":  cpuPctVal,
			"num_cpu":      runtime.NumCPU(),
			"load_avg_1":   load1,
			"load_avg_5":   load5,
			"load_avg_15":  load15,
		}
		if vmStat != nil {
			metric["mem_total"] = vmStat.Total
			metric["mem_used"] = vmStat.Used
			metric["mem_percent"] = vmStat.UsedPercent
		}

		b, err := json.Marshal(metric)
		if err != nil {
			return
		}

		req, err := http.NewRequestWithContext(ctx, "POST", streamURL, bytes.NewReader(b))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			slog.Warn("stream: post failed", "error", err)
			return
		}
		resp.Body.Close()
	}

	// Send immediately, then on every tick
	send()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			send()
		}
	}
}

// apiURL returns the full URL for the given path on the controller.
func (s *Step) apiURL(ctx *pipeline.Context, path string) string {
	if s.ApiBase != "" {
		return s.ApiBase + path
	}
	return fmt.Sprintf("https://%s%s", ctx.Domain, path)
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
