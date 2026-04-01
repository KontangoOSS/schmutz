package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/KontangoOSS/schmutz/internal/join"
)

const version = "0.3.0"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, `schmutz-join v%s — join the network

Usage:
  schmutz-join <controller-url> [options]

Options:
  --scan              Auto-detect everything, confirm, register (default)
  --manual            Answer questions, we verify against detected values
  --role-id=ID        Bao AppRole role-id (trusted enrollment — skips quarantine)
  --secret-id=ID      Bao AppRole secret-id (required with --role-id)

Example:
  schmutz-join https://join.example.net
  schmutz-join https://join.example.net --role-id=xxx --secret-id=yyy
`, version)
		os.Exit(1)
	}

	controllerURL := os.Args[1]
	mode := "scan"
	roleID := ""
	secretID := ""
	for _, a := range os.Args[2:] {
		if a == "--manual" {
			mode = "manual"
		}
		if strings.HasPrefix(a, "--role-id=") {
			roleID = strings.TrimPrefix(a, "--role-id=")
		}
		if strings.HasPrefix(a, "--secret-id=") {
			secretID = strings.TrimPrefix(a, "--secret-id=")
		}
	}

	log.SetFlags(0)
	log.Printf("schmutz-join v%s", version)
	log.Println()

	platform, err := join.DetectPlatform()
	if err != nil {
		log.Fatalf("unsupported platform: %v", err)
	}

	// Preflight — check all dependencies before doing anything
	if missing := platform.Preflight(); len(missing) > 0 {
		log.Println("preflight check failed:")
		for _, m := range missing {
			log.Printf("  - %s", m)
		}
		os.Exit(1)
	}

	// Detect everything
	det := detect()
	fp, _ := join.Collect()

	var req join.Request

	if mode == "scan" {
		req = scanFlow(det, fp)
	} else {
		req = manualFlow(det, fp)
	}

	// Attach Bao credentials if provided (trusted enrollment)
	if roleID != "" && secretID != "" {
		req.RoleID = roleID
		req.SecretID = secretID
		log.Println("  trusted enrollment (Bao AppRole)")
	}

	// Register
	log.Println()
	log.Printf("Registering %s with %s …", req.Hostname, controllerURL)
	resp, err := doRegister(controllerURL, req)
	if err != nil {
		log.Fatalf("registration failed: %v", err)
	}
	if resp.ZitiJWT == "" {
		log.Printf("already registered (id: %s)", resp.ID)
		os.Exit(0)
	}

	nick := resp.Nickname
	if nick == "" {
		nick = resp.ID[:8]
	}
	log.Printf("registered: %s (%s)", nick, resp.ID)

	// Install
	tangoDir := platform.TangoDir()
	if err := platform.EnsureDir(); err != nil {
		log.Fatalf("create directories: %v", err)
	}

	machineIDPath := filepath.Join(tangoDir, "machine.json")
	record, _ := json.MarshalIndent(map[string]interface{}{
		"id": resp.ID, "nickname": nick, "hostname": req.Hostname,
		"registered_at": time.Now().Unix(),
	}, "", "  ")
	os.WriteFile(machineIDPath, record, 0600)

	// Fetch config from the controller — uses machine ID + nickname as auth
	// Returns /etc/hosts, tunnel binary, services — everything the machine needs
	log.Println("fetching config…")
	cfg := fetchConfig(controllerURL, resp.ID, nick)
	if cfg != nil {
		if hosts, ok := cfg["hosts"].(map[string]interface{}); ok {
			existing, _ := os.ReadFile("/etc/hosts")
			if f, err := os.OpenFile("/etc/hosts", os.O_APPEND|os.O_WRONLY, 0644); err == nil {
				for hostname, ip := range hosts {
					if ipStr, ok := ip.(string); ok && ipStr != "" && !bytes.Contains(existing, []byte(hostname)) {
						f.WriteString(ipStr + " " + hostname + "\n")
						log.Printf("  host: %s → %s", hostname, ipStr)
					}
				}
				f.Close()
			}
		}
	} else {
		// Fallback: add controller hosts from legacy endpoint
		addControllerHosts(controllerURL)
	}

	// Enroll via join service — nickname proves we received the registration
	log.Println("enrolling…")
	identity, err := proxyEnroll(controllerURL, resp.ZitiJWT, nick)
	if err != nil {
		log.Fatalf("enrollment failed: %v", err)
	}

	zitiIDPath := platform.IdentityPath()
	if err := os.WriteFile(zitiIDPath, identity, 0600); err != nil {
		log.Fatalf("save identity: %v", err)
	}
	log.Printf("  identity saved to %s", zitiIDPath)

	// Install ziti for the tunnel
	log.Println("installing ziti…")
	if err := platform.InstallZiti("2.0.0-pre5"); err != nil {
		log.Fatalf("install ziti: %v", err)
	}

	// Start tunnel
	log.Println("starting tunnel…")
	if err := platform.InstallService(zitiIDPath); err != nil {
		log.Fatalf("install service: %v", err)
	}
	if err := platform.StartService(); err != nil {
		log.Fatalf("start service: %v", err)
	}

	// Verify tunnel is connected and healthy via IPC agent
	log.Println("verifying tunnel connectivity…")
	if err := platform.WaitForTunnel(60 * time.Second); err != nil {
		log.Printf("  warning: %v", err)
		log.Println("  tunnel may still be connecting — verify manually with:")
		log.Printf("    %s agent stats --app-type tunnel", platform.ZitiBinaryPath())
	} else {
		log.Println("  tunnel connected ✓")
		log.Printf("  ssh reachable at: %s.tango", nick)
	}

	log.Println()
	log.Println("done.")
	log.Printf("  nickname:  %s", nick)
	log.Printf("  id:        %s", resp.ID)
	log.Printf("  identity:  %s", zitiIDPath)
	log.Println("  status:    quarantine (awaiting profile assignment)")
}

// ============================================================
// Scan flow — detect, show, confirm hostname, register
// ============================================================
func scanFlow(det detected, fp *join.Fingerprint) join.Request {
	log.Println("Scanning…")
	log.Println()
	log.Println("  Your machine:")
	log.Printf("    %-14s %s", "Hostname:", det.hostname)
	log.Printf("    %-14s %s", "OS:", det.osVersion)
	log.Printf("    %-14s %s", "Arch:", det.arch)
	log.Printf("    %-14s %s", "Timezone:", det.timezone)
	log.Printf("    %-14s %d", "Cores:", det.cores)

	if fp != nil {
		if fp.CPUInfo != "" {
			log.Printf("    %-14s %s", "CPU:", fp.CPUInfo)
		}
		if fp.MachineID != "" {
			log.Printf("    %-14s %s…", "Machine ID:", fp.MachineID[:min(12, len(fp.MachineID))])
		}
		if len(fp.MACAddrs) > 0 {
			log.Printf("    %-14s %s", "MACs:", strings.Join(fp.MACAddrs, ", "))
		}
		if fp.HardwareHash != "" {
			log.Printf("    %-14s %s", "Fingerprint:", fp.HardwareHash)
		}
	}

	log.Println()
	req := join.Request{
		Hostname:  det.hostname,
		OS:        det.os,
		OSVersion: det.osVersion,
		Arch:      det.arch,
		Timezone:  det.timezone,
	}
	if fp != nil {
		req.HardwareHash = fp.HardwareHash
		req.MACAddrs = fp.MACAddrs
		req.CPUInfo = fp.CPUInfo
		req.MachineID = fp.MachineID
		req.SerialNumber = fp.SerialNumber
		req.KernelVersion = fp.KernelVersion
	}
	return req
}

// ============================================================
// Manual flow — ask everything, compare, flag mismatches
// ============================================================
func manualFlow(det detected, fp *join.Fingerprint) join.Request {
	log.Println("Tell us about this machine. We'll verify what we can.")
	log.Println()

	reader := bufio.NewReader(os.Stdin)
	ask := func(prompt, def string) string {
		fmt.Printf("  %s [%s]: ", prompt, def)
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			return def
		}
		return line
	}

	hostname := ask("Hostname", det.hostname)
	claimedOS := ask("OS", det.os)
	claimedArch := ask("Architecture", det.arch)
	claimedTZ := ask("Timezone", det.timezone)
	claimedCores := ask("CPU cores", fmt.Sprintf("%d", det.cores))

	// Compare
	log.Println()
	log.Println("  Verification:")
	mm := 0
	check := func(field, claimed, actual string) {
		if strings.EqualFold(strings.TrimSpace(claimed), strings.TrimSpace(actual)) {
			log.Printf("    %-14s ✓ %s", field, actual)
		} else {
			log.Printf("    %-14s ✗ %s → %s", field, claimed, actual)
			mm++
		}
	}
	check("Hostname", hostname, det.hostname)
	check("OS", claimedOS, det.os)
	check("Arch", claimedArch, det.arch)
	check("Timezone", claimedTZ, det.timezone)
	check("Cores", claimedCores, fmt.Sprintf("%d", det.cores))

	if mm >= 2 {
		log.Println()
		log.Fatalf("  %d mismatches — registration denied. Use --scan for auto-detection.", mm)
	}
	if mm > 0 {
		log.Printf("\n  %d mismatch — noted.", mm)
	} else {
		log.Println("\n  All verified.")
	}

	req := join.Request{
		Hostname:  hostname,
		OS:        det.os,
		OSVersion: det.osVersion,
		Arch:      det.arch,
		Timezone:  det.timezone,
	}
	if fp != nil {
		req.HardwareHash = fp.HardwareHash
		req.MACAddrs = fp.MACAddrs
		req.CPUInfo = fp.CPUInfo
		req.MachineID = fp.MachineID
		req.SerialNumber = fp.SerialNumber
		req.KernelVersion = fp.KernelVersion
	}
	return req
}

// ============================================================
// Detection
// ============================================================
type detected struct {
	hostname, os, osVersion, arch, timezone string
	cores                                   int
}

func detect() detected {
	h, _ := os.Hostname()
	tz := ""
	if loc := time.Now().Location(); loc != nil {
		tz = loc.String()
	}
	return detected{
		hostname: h, os: runtime.GOOS, osVersion: join.DetectOSVersion(),
		arch: runtime.GOARCH, timezone: tz, cores: runtime.NumCPU(),
	}
}

// addControllerHosts fetches controller IPs and adds /etc/hosts entries
// so the ziti tunnel SDK can reach controllers after enrollment.
func addControllerHosts(controllerURL string) {
	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}},
	}
	resp, err := client.Get(controllerURL + "/api/controllers")
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return
	}
	var controllers []struct {
		IP    string   `json:"ip"`
		Hosts []string `json:"hosts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&controllers); err != nil {
		return
	}
	existing, _ := os.ReadFile("/etc/hosts")
	f, err := os.OpenFile("/etc/hosts", os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	for _, c := range controllers {
		if c.IP == "" {
			continue
		}
		for _, h := range c.Hosts {
			if !bytes.Contains(existing, []byte(h)) {
				f.WriteString(c.IP + " " + h + "\n")
				log.Printf("  host: %s → %s", h, c.IP)
			}
		}
	}
}

// proxyEnroll sends the JWT to the join service's /api/enroll endpoint.
// The server enrolls via localhost:1280 (port 1280 is dark to the public)
// and returns the identity JSON.
func proxyEnroll(controllerURL, jwt, nickname string) ([]byte, error) {
	body, _ := json.Marshal(map[string]string{"jwt": jwt, "nickname": nickname})
	client := &http.Client{
		Timeout:   60 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}},
	}
	resp, err := client.Post(controllerURL+"/api/enroll", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(data))
	}
	return data, nil
}

// fetchConfig gets the machine's configuration from the controller.
// Authenticated by machine ID (Bearer) + nickname (X-Nickname header).
func fetchConfig(controllerURL, machineID, nickname string) map[string]interface{} {
	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}},
	}
	req, _ := http.NewRequest("GET", controllerURL+"/api/config", nil)
	req.Header.Set("Authorization", "Bearer "+machineID)
	req.Header.Set("X-Nickname", nickname)
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}
	var cfg map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&cfg)
	return cfg
}

func doRegister(url string, req join.Request) (*join.Response, error) {
	body, _ := json.Marshal(req)
	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}},
	}
	resp, err := client.Post(url+"/api/register", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(data))
	}
	var r join.Response
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("invalid response: %w", err)
	}
	return &r, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
