package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// bootstrapRequest is sent to the controller's /api/server/enroll endpoint.
type bootstrapRequest struct {
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Mode     string `json:"mode"` // "server"
}

// bootstrapResponse is returned by the controller.
type bootstrapResponse struct {
	Identity []byte `json:"identity"` // enrolled Ziti identity JSON
	ID       string `json:"id"`
}

func cmdBootstrap(args []string) {
	fs := flag.NewFlagSet("bootstrap", flag.ExitOnError)
	controllerURL := fs.String("controller", "", "kontango controller URL (e.g. https://ctrl.konoss.org)")
	identityPath := fs.String("identity", "/opt/ziti/identity.json", "path to write Ziti identity")
	zitiPath := fs.String("ziti", "/usr/local/bin/ziti", "path to ziti binary")
	dryRun := fs.Bool("dry-run", false, "print steps without executing")
	fs.Parse(args)

	if *controllerURL == "" {
		fmt.Fprintln(os.Stderr, "bootstrap: --controller is required")
		fmt.Fprintln(os.Stderr, "  example: schmutz bootstrap --controller https://ctrl.konoss.org")
		os.Exit(1)
	}

	if os.Getuid() != 0 && !*dryRun {
		fmt.Fprintln(os.Stderr, "bootstrap: must run as root")
		os.Exit(1)
	}

	log.SetFlags(0)
	log.Println("=== schmutz bootstrap ===")

	hostname, _ := os.Hostname()
	log.Printf("  host:       %s", hostname)
	log.Printf("  os:         %s/%s", runtime.GOOS, runtime.GOARCH)
	log.Printf("  controller: %s", *controllerURL)
	log.Printf("  identity:   %s", *identityPath)
	log.Printf("  ziti:       %s", *zitiPath)
	log.Println()

	// Step 1: enroll with controller — get back a Ziti OTT JWT
	log.Println("step 1/4: enrolling with controller...")
	identity, id, err := bootstrapEnroll(*controllerURL, hostname, *dryRun)
	if err != nil {
		log.Fatalf("enroll failed: %v", err)
	}
	if !*dryRun {
		log.Printf("  enrolled as: %s", id)
	}

	// Step 2: write identity file
	log.Println("step 2/4: writing identity...")
	if err := bootstrapWriteIdentity(*identityPath, identity, *dryRun); err != nil {
		log.Fatalf("write identity failed: %v", err)
	}

	// Step 3: ensure ziti binary exists
	log.Println("step 3/4: checking ziti binary...")
	if err := bootstrapEnsureZiti(*zitiPath, *dryRun); err != nil {
		log.Fatalf("ziti binary: %v", err)
	}

	// Step 4: install and start systemd service
	log.Println("step 4/4: installing tunnel service...")
	if err := bootstrapInstallService(*zitiPath, *identityPath, *dryRun); err != nil {
		log.Fatalf("install service failed: %v", err)
	}

	log.Println()
	log.Println("done. tunnel is running.")
	log.Println("  services will bind automatically via lan-bind policy.")
	log.Printf("  identity: %s", *identityPath)
	log.Println("  check: systemctl status ziti-tunnel")
}

// bootstrapEnroll calls the controller's server enrollment endpoint.
// Returns the enrolled identity JSON bytes and the assigned ID.
func bootstrapEnroll(controllerURL, hostname string, dryRun bool) ([]byte, string, error) {
	if dryRun {
		log.Printf("  [dry-run] POST %s/api/server/enroll", controllerURL)
		return []byte(`{"dry-run":true}`), "dry-run-id", nil
	}

	req := bootstrapRequest{
		Hostname: hostname,
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		Mode:     "server",
	}
	body, _ := json.Marshal(req)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(
		controllerURL+"/api/server/enroll",
		"application/json",
		strings.NewReader(string(body)),
	)
	if err != nil {
		return nil, "", fmt.Errorf("POST %s/api/server/enroll: %w", controllerURL, err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, "", fmt.Errorf("controller returned %d: %s", resp.StatusCode, string(data))
	}

	var result bootstrapResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, "", fmt.Errorf("parse response: %w", err)
	}
	if len(result.Identity) == 0 {
		return nil, "", fmt.Errorf("controller returned empty identity")
	}

	return result.Identity, result.ID, nil
}

// bootstrapWriteIdentity writes the identity JSON to disk.
func bootstrapWriteIdentity(path string, identity []byte, dryRun bool) error {
	if dryRun {
		log.Printf("  [dry-run] write %s (0600)", path)
		return nil
	}

	dir := path[:strings.LastIndex(path, "/")]
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	if err := os.WriteFile(path, identity, 0600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	log.Printf("  written: %s", path)
	return nil
}

// bootstrapEnsureZiti checks if the ziti binary exists. If not, tries to copy
// from /opt/kontango/bin/ziti or /usr/local/bin/ziti as fallback.
func bootstrapEnsureZiti(zitiPath string, dryRun bool) error {
	if dryRun {
		log.Printf("  [dry-run] check %s", zitiPath)
		return nil
	}

	if _, err := os.Stat(zitiPath); err == nil {
		log.Printf("  found: %s", zitiPath)
		return nil
	}

	// Try to copy from known locations
	candidates := []string{
		"/opt/kontango/bin/ziti",
		"/usr/bin/ziti",
	}
	for _, src := range candidates {
		if _, err := os.Stat(src); err == nil {
			log.Printf("  copying %s → %s", src, zitiPath)
			return bootstrapCopyFile(src, zitiPath)
		}
	}

	return fmt.Errorf("ziti binary not found at %s or any fallback path; install it first", zitiPath)
}

func bootstrapCopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	dir := dst[:strings.LastIndex(dst, "/")]
	os.MkdirAll(dir, 0755)

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// bootstrapInstallService writes the ziti-tunnel systemd unit and starts it.
func bootstrapInstallService(zitiPath, identityPath string, dryRun bool) error {
	const serviceName = "ziti-tunnel"
	const serviceFile = "/etc/systemd/system/ziti-tunnel.service"

	hostname, _ := os.Hostname()
	unit := fmt.Sprintf(`[Unit]
Description=Ziti Tunnel (%s)
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=0

[Service]
Type=simple
ExecStart=%s tunnel run -i %s --dnsSvcIpRange 100.64.0.1/10 --dnsUpstream udp://1.1.1.1:53
Restart=always
RestartSec=5
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
`, hostname, zitiPath, identityPath)

	if dryRun {
		log.Printf("  [dry-run] write %s", serviceFile)
		log.Printf("  [dry-run] systemctl daemon-reload")
		log.Printf("  [dry-run] systemctl enable --now %s", serviceName)
		fmt.Println()
		fmt.Println("--- service file ---")
		fmt.Print(unit)
		fmt.Println("---")
		return nil
	}

	if err := os.WriteFile(serviceFile, []byte(unit), 0644); err != nil {
		return fmt.Errorf("write %s: %w", serviceFile, err)
	}
	log.Printf("  wrote: %s", serviceFile)

	if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("daemon-reload: %w: %s", err, out)
	}

	if out, err := exec.Command("systemctl", "enable", "--now", serviceName).CombinedOutput(); err != nil {
		return fmt.Errorf("enable %s: %w: %s", serviceName, err, out)
	}
	log.Printf("  started: %s", serviceName)

	// Wait briefly and check status
	time.Sleep(2 * time.Second)
	out, _ := exec.Command("systemctl", "is-active", serviceName).Output()
	status := strings.TrimSpace(string(out))
	if status != "active" {
		log.Printf("  warning: service status is '%s' — check: journalctl -u %s -n 20", status, serviceName)
	} else {
		log.Printf("  status: %s ✓", status)
	}

	return nil
}
