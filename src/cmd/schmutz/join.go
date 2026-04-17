package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/mattn/go-isatty"

	"github.com/KontangoOSS/schmutz/internal/join"
)

func cmdJoin(args []string) {
	fs := flag.NewFlagSet("join", flag.ExitOnError)
	roleID := fs.String("role-id", "", "Bao AppRole role ID")
	secretID := fs.String("secret-id", "", "Bao AppRole secret ID")
	session := fs.String("session", "", "one-time session token from install script")
	slug := fs.String("slug", "", "device name/identifier (e.g. homelab-proxy, office-nuc)")
	noTUI := fs.Bool("no-tui", false, "disable interactive wizard (non-interactive mode)")

	// Strip the URL positional arg first so flags can appear anywhere
	url := ""
	var filteredArgs []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") && (strings.HasPrefix(a, "http://") || strings.HasPrefix(a, "https://")) {
			url = a
		} else {
			filteredArgs = append(filteredArgs, a)
		}
	}
	fs.Parse(filteredArgs)

	// Use the TUI wizard when:
	//   - stdin is a real terminal (not a pipe / curl | sh)
	//   - --no-tui was not passed
	//   - we are not being called by the agent (agent always passes --no-tui)
	interactive := isatty.IsTerminal(os.Stdin.Fd()) && !*noTUI

	if interactive {
		// TUI path — wizard handles URL prompt, method selection, creds, confirm.
		result, err := runJoinTUI(url, *session, *roleID, *secretID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "join: %v\n", err)
			os.Exit(1)
		}
		if result == nil {
			// User quit the wizard
			os.Exit(0)
		}
		// Install services after wizard completes enrollment
		if err := installAfterEnroll(url, result); err != nil {
			fmt.Fprintf(os.Stderr, "install: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Non-interactive path (pipe, script, agent-triggered)
	if url == "" {
		fmt.Fprintf(os.Stderr, "usage: schmutz join <url> [flags]\n")
		os.Exit(1)
	}

	// Slug is required. Prompt if not provided and stdin is a TTY.
	resolvedSlug := *slug
	if resolvedSlug == "" {
		if isatty.IsTerminal(os.Stdin.Fd()) {
			resolvedSlug = promptSlug()
		}
		if resolvedSlug == "" {
			fmt.Fprintf(os.Stderr, "error: --slug is required (e.g. --slug homelab-proxy)\n")
			os.Exit(1)
		}
	}

	if err := runJoin(url, resolvedSlug, *session, *roleID, *secretID); err != nil {
		fmt.Fprintf(os.Stderr, "join: %v\n", err)
		os.Exit(1)
	}
}

// runJoin is the full non-interactive enrollment flow.
// Called by cmdJoin (non-TTY) and by the agent when it receives an enroll command.
func runJoin(url, slug, session, roleID, secretID string) error {
	log.SetFlags(0)

	platform, err := join.DetectPlatform()
	if err != nil {
		return fmt.Errorf("unsupported platform: %w", err)
	}
	if missing := platform.Preflight(); len(missing) > 0 {
		log.Println("preflight failed:")
		for _, m := range missing {
			log.Printf("  - %s", m)
		}
		return fmt.Errorf("preflight failed")
	}

	if platform.IsDirty() {
		log.Println("dirty install detected — cleaning up previous state…")
		if err := platform.Cleanup(); err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
		log.Println("  cleaned ✓")
	}

	log.Println("enrolling…")
	result, err := restJoin(url, slug, roleID, secretID)
	if err != nil {
		return fmt.Errorf("enrollment: %w", err)
	}

	nick := result.Nickname
	if nick == "" {
		nick = slug
	}
	log.Printf("enrolled: %s (%s) [%s]", nick, result.ID, result.Status)

	return installAfterEnroll(url, result)
}

// installAfterEnroll saves identity, installs ziti/tunnel/agent/caddy.
// Shared by both TUI and non-interactive paths.
func installAfterEnroll(url string, result *joinResult) error {
	log.SetFlags(0)

	platform, err := join.DetectPlatform()
	if err != nil {
		return fmt.Errorf("unsupported platform: %w", err)
	}

	nick := result.Nickname
	if nick == "" {
		nick = result.ID[:8]
	}

	if err := platform.EnsureDir(); err != nil {
		return fmt.Errorf("create directories: %w", err)
	}

	// Install ziti binary first — enrollJWT needs it.
	zitiVersion := "latest"
	if v, ok := result.Tunnel["version"].(string); ok && v != "" && v != "latest" {
		zitiVersion = v
	}
	log.Println("installing ziti…")
	if err := platform.InstallZiti(zitiVersion); err != nil {
		return fmt.Errorf("install ziti: %w", err)
	}

	idPath := platform.IdentityPath()
	if result.JWT != "" {
		// New API path: enroll the JWT to produce an x509 identity
		if err := enrollJWT(result.JWT, idPath, platform.ZitiBinaryPath()); err != nil {
			return fmt.Errorf("enroll JWT: %w", err)
		}
	} else if len(result.Identity) > 0 {
		// Legacy/SSE path: identity JSON returned directly
		if err := os.WriteFile(idPath, result.Identity, 0600); err != nil {
			return fmt.Errorf("save identity: %w", err)
		}
	} else {
		return fmt.Errorf("no identity or JWT in enrollment result")
	}
	log.Printf("  identity: %s", idPath)

	record, _ := json.MarshalIndent(map[string]interface{}{
		"id": result.ID, "nickname": nick,
		"registered_at": time.Now().Unix(),
	}, "", "  ")
	machineFile := idPath[:strings.LastIndex(idPath, "/")+1] + "machine.json"
	os.WriteFile(machineFile, record, 0600)
	log.Println("starting tunnel…")
	if err := platform.InstallService(idPath); err != nil {
		return fmt.Errorf("install tunnel service: %w", err)
	}
	if err := platform.StartService(); err != nil {
		return fmt.Errorf("start tunnel: %w", err)
	}

	log.Println("verifying tunnel…")
	if err := platform.WaitForTunnel(60 * time.Second); err != nil {
		log.Printf("  warning: %v (continuing)", err)
	} else {
		log.Println("  connected ✓")
	}

	printCompletion(nick, result)
	return nil
}

// -- Types -------------------------------------------------------------------

type joinResult struct {
	ID           string
	Nickname     string
	Identity     []byte  // raw identity JSON (SSE/TUI path)
	JWT          string  // enrollment JWT (new API path); if set, use ziti edge enroll
	Status       string
	Hosts        []string
	Tunnel       map[string]interface{}
	Services     []string
	SSHPublicKey string
	AppID        string
	ClaimURL     string
}

// -- New enrollment API (POST /enroll) ---------------------------------------

// enrollRequest is the body sent to POST /enroll.
type enrollRequest struct {
	Hostname    string         `json:"hostname"`
	OS          string         `json:"os"`
	Arch        string         `json:"arch"`
	Platform    string         `json:"platform"`
	MachineID   string         `json:"machine_id,omitempty"`
	AgentData   map[string]any `json:"agent_data,omitempty"`
	Fingerprint string         `json:"fingerprint,omitempty"`
	Slug        string         `json:"slug,omitempty"`
}

// enrollResponse is what POST /enroll returns.
type enrollResponse struct {
	Status       string   `json:"status"`
	IdentityID   string   `json:"identity_id,omitempty"`
	AppID        string   `json:"app_id,omitempty"`
	JWT          string   `json:"jwt,omitempty"`
	Tier         string   `json:"tier,omitempty"`
	Slug         string   `json:"slug,omitempty"`
	Services     []string `json:"services,omitempty"`
	SSHPublicKey string   `json:"ssh_public_key,omitempty"`
	ClaimURL     string   `json:"claim_url,omitempty"`
	RetryAfter   int      `json:"retry_after,omitempty"`
	StreamToken  string   `json:"stream_token,omitempty"`
	NodeURL      string   `json:"node_url,omitempty"`
	Message      string   `json:"message,omitempty"`
}

// restJoin enrolls via the new POST /enroll API.
// On a "pending" response it streams telemetry to POST /stream for the
// requested window duration, then retries. Returns once approved.
func restJoin(baseURL, slug, roleID, secretID string) (*joinResult, error) {
	fp, _ := join.Collect()
	h, _ := os.Hostname()
	if slug == "" {
		slug = h
	}

	fingerprint := ""
	machineID := ""
	if fp != nil {
		fingerprint = fp.HardwareHash
		machineID = fp.MachineID
	}

	req := enrollRequest{
		Hostname:    h,
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		Platform:    "baremetal",
		MachineID:   machineID,
		Fingerprint: fingerprint,
		Slug:        slug,
		AgentData: map[string]any{
			"source":  "schmutz-join",
			"version": "2.0.0",
		},
	}
	if roleID != "" && secretID != "" {
		req.AgentData["role_id"] = roleID
		req.AgentData["secret_id"] = secretID
	}

	log.Printf("enrolling %s…", h)
	return doEnroll(baseURL, req)
}

func doEnroll(baseURL string, req enrollRequest) (*joinResult, error) {
	enrollURL := baseURL + "/enroll"
	streamURL := baseURL + "/stream"

	body, _ := json.Marshal(req)

	httpClient := &http.Client{Timeout: 30 * time.Second}
	httpReq, _ := http.NewRequest("POST", enrollURL, bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("enroll request: %w", err)
	}
	defer resp.Body.Close()

	var result enrollResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response (HTTP %d): %w", resp.StatusCode, err)
	}

	switch {
	case result.Status == "approved":
		if result.JWT == "" {
			return nil, fmt.Errorf("approved but no JWT returned")
		}
		nick := result.Slug
		if nick == "" {
			nick = result.IdentityID
		}
		return &joinResult{
			ID:           result.IdentityID,
			Nickname:     nick,
			JWT:          result.JWT,
			Status:       result.Tier,
			Services:     result.Services,
			SSHPublicKey: result.SSHPublicKey,
			AppID:        result.AppID,
			ClaimURL:     result.ClaimURL,
			Tunnel:       map[string]interface{}{},
		}, nil

	case result.Status == "pending":
		waitSecs := result.RetryAfter
		if waitSecs <= 0 {
			waitSecs = 60
		}
		token := result.StreamToken

		// Pin to the node that opened the window so retry finds the same in-memory state
		pinnedBase := baseURL
		if result.NodeURL != "" {
			pinnedBase = result.NodeURL
			streamURL = pinnedBase + "/stream"
			log.Printf("  pinned to %s", result.NodeURL)
		}

		log.Printf("  pending — streaming telemetry for %ds…", waitSecs)
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(waitSecs)*time.Second)
		defer cancel()
		go streamTelemetry(ctx, streamURL, req.Fingerprint, token)
		<-ctx.Done()

		log.Printf("  window closed — retrying…")
		return doEnroll(pinnedBase, req)

	default:
		msg := result.Message
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d / status=%s", resp.StatusCode, result.Status)
		}
		return nil, fmt.Errorf("enrollment denied: %s", msg)
	}
}

// streamTelemetry POSTs basic system metrics to POST /stream every 5s.
func streamTelemetry(ctx context.Context, streamURL, fingerprint, token string) {
	h, _ := os.Hostname()
	client := &http.Client{Timeout: 10 * time.Second}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	send := func() {
		metric := map[string]any{
			"fingerprint":  fingerprint,
			"stream_token": token,
			"hostname":     h,
			"os":           runtime.GOOS,
			"arch":         runtime.GOARCH,
		}
		b, _ := json.Marshal(metric)
		req, err := http.NewRequestWithContext(ctx, "POST", streamURL, bytes.NewReader(b))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return
		}
		resp.Body.Close()
	}

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

// enrollJWT saves the JWT to a temp file and runs `ziti edge enroll` to produce
// the x509 identity JSON file at identityPath.
func enrollJWT(jwt, identityPath, zitiBin string) error {
	// Find ziti binary if not provided
	if zitiBin == "" || func() bool { _, err := os.Stat(zitiBin); return err != nil }() {
		for _, p := range []string{"/usr/local/bin/ziti", "/usr/bin/ziti", "/opt/tango/bin/ziti"} {
			if _, err := os.Stat(p); err == nil {
				zitiBin = p
				break
			}
		}
	}
	if zitiBin == "" {
		return fmt.Errorf("ziti binary not found")
	}

	jwtFile, err := os.CreateTemp("", "schmutz-enroll-*.jwt")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(jwtFile.Name())
	if _, err := jwtFile.WriteString(jwt); err != nil {
		return fmt.Errorf("write JWT: %w", err)
	}
	jwtFile.Close()

	log.Printf("  enrolling identity via ziti…")
	cmd := exec.Command(zitiBin, "edge", "enroll", jwtFile.Name(), "-o", identityPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ziti edge enroll: %w", err)
	}
	return nil
}

// -- Helpers -----------------------------------------------------------------

// promptSlug reads a slug from stdin interactively.
func promptSlug() string {
	fmt.Printf("  device name (slug): ")
	var s string
	fmt.Fscan(os.Stdin, &s)
	return strings.TrimSpace(s)
}

// printCompletion prints the Ziti Basic enrollment completion screen.
func printCompletion(nick string, result *joinResult) {
	statusLabel := "quarantine"
	statusNote := "limited access — awaiting approval"
	if result.Status == "approved" {
		statusLabel = "approved"
		statusNote = "full access"
	}

	fmt.Printf("\n")
	fmt.Printf("  ╔══════════════════════════════════════════════════════╗\n")
	fmt.Printf("  ║              schmutz — enrolled successfully         ║\n")
	fmt.Printf("  ╚══════════════════════════════════════════════════════╝\n")
	fmt.Printf("\n")
	fmt.Printf("  slug:      %s\n", nick)
	fmt.Printf("  id:        %s\n", result.ID)
	if result.AppID != "" && result.AppID != result.ID {
		fmt.Printf("  app_id:    %s\n", result.AppID)
	}
	fmt.Printf("  status:    %s  (%s)\n", statusLabel, statusNote)
	fmt.Printf("\n")
	fmt.Printf("  Ziti Basic services:\n")

	services := result.Services
	if len(services) == 0 {
		// Fallback if server is older and doesn't return service names
		services = []string{
			"ssh-" + nick,
			"http-" + nick,
			"https-" + nick,
			"nats-" + nick,
		}
	}
	ports := map[string]string{
		"ssh":   "22",
		"http":  "80",
		"https": "443",
		"nats":  "4222",
	}
	for _, svc := range services {
		prefix := svc
		if idx := len(svc) - len(nick) - 1; idx > 0 {
			prefix = svc[:idx]
		}
		port := ports[prefix]
		if port == "" {
			port = "?"
		}
		fmt.Printf("    %-28s  %s.tango:%s\n", svc, nick, port)
	}

	fmt.Printf("\n")
	if result.SSHPublicKey != "" {
		fmt.Printf("  SSH public key:\n")
		fmt.Printf("    %s\n", result.SSHPublicKey)
	}

	claimURL := result.ClaimURL
	if claimURL == "" {
		claimURL = "https://auth.konoss.org/ui/login?slug=" + nick
	}
	fmt.Printf("  Claim this device:\n")
	fmt.Printf("    %s\n", claimURL)
	fmt.Printf("\n")
}

