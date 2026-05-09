package enroll

import (
	"bufio"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/KontangoOSS/schmutz/agent/internal/join"
	"github.com/KontangoOSS/schmutz/agent/pkg/schmutz/discovery"
	ziti "github.com/openziti/sdk-golang/ziti"
	zitiEnroll "github.com/openziti/sdk-golang/ziti/enroll"
)

// DeviceInfo is the identifying data collected before enrollment.
type DeviceInfo struct {
	Hostname    string
	OS          string
	Arch        string
	Platform    string
	MachineID   string
	Fingerprint string
	Profile     string
	AgentData   map[string]any
}

// EnrollResult carries everything returned from a successful enrollment.
// The identity JSON is fully enrolled server-side — write directly to disk.
// Ziti policy updates promote the device from quarantine to full access
// automatically when an operator approves, no restart needed.
type EnrollResult struct {
	IdentityJSON []byte   // enrolled Ziti identity — write directly to disk
	Slug         string   // nickname assigned by controller (e.g. "web-1")
	Status       string   // "approved" or "quarantine"
	Services     []string // Ziti service names provisioned (e.g. ["ssh-web-1"])
}

// NeedsEnrollment returns true if the identity file does not exist.
func NeedsEnrollment(identityPath string) bool {
	_, err := os.Stat(identityPath)
	return errors.Is(err, fs.ErrNotExist)
}

// Register enrolls this device with the controller via POST /api/enroll/stream.
// It collects all available system data (running as root gives full access),
// POSTs it as a flat JSON body with Accept: text/event-stream, then reads the
// SSE response until the identity event arrives.
//
// The controller streams verify/decision/progress events before sending the
// final identity event containing the fully enrolled Ziti identity JSON.
// Both approved and quarantine devices get an identity and can start the tunnel;
// Ziti policy changes push automatically when an operator approves.
func Register(ctx context.Context, controllerURL string, info DeviceInfo) (*EnrollResult, error) {
	if controllerURL == "" {
		return nil, fmt.Errorf("enroll: controller_url required")
	}

	body, err := buildEnrollPayload(info)
	if err != nil {
		return nil, fmt.Errorf("enroll: build payload: %w", err)
	}

	url := strings.TrimRight(controllerURL, "/") + "/api/enroll/stream"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("enroll: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", "schmutz-agent/0.2.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("enroll: POST %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("enroll: rejected by controller (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("enroll: HTTP %d from %s", resp.StatusCode, url)
	}

	return readSSEResult(ctx, resp)
}

// buildEnrollPayload collects everything available on this machine and encodes
// it as a flat JSON KV map. The controller accepts any keys — it extracts what
// it knows about and stores the rest for audit. More data = better fingerprint
// matching and verification confidence.
//
// As of v0.3.1 the layered discovery package (pkg/schmutz/discovery) is the
// primary source. We still merge the legacy join.Collect() output so existing
// fields the controller already extracts (cpu_info, kernel, etc.) keep working.
func buildEnrollPayload(info DeviceInfo) ([]byte, error) {
	fp, _ := join.Collect()

	// Run the layered discovery probe — L0/L1 always, L2/L3 if root.
	// L4 (network egress, geolocation) is opt-in via env var so a default
	// schmutz install doesn't reveal the device's existence to third parties.
	layers := discovery.LocalLayers()
	if os.Getenv("SCHMUTZ_DISCOVER_PUBLIC") == "1" {
		layers = discovery.AllLayers()
	}
	disc := discovery.Probe{Layers: layers}.Run(context.Background())

	payload := map[string]any{
		"method":               enrollMethod(fp),
		"schmutz_layers_run":   disc.Layers,
		"schmutz_layers_skip":  disc.Skipped,
		"schmutz_privilege":    disc.Privilege,
		"schmutz_facts_count":  len(disc.Facts),
	}
	if len(disc.Errors) > 0 {
		payload["schmutz_errors"] = disc.Errors
	}
	// Merge the new discovery facts as flat KV — preserves the existing
	// stream contract the controller expects.
	for k, v := range disc.FlatKV() {
		payload[k] = v
	}

	// Basic identity — from DeviceInfo (caller-supplied) with fp fallback
	set := func(key, val string) {
		if val != "" {
			payload[key] = val
		}
	}
	set("hostname", info.Hostname)
	set("os", info.OS)
	set("arch", info.Arch)
	set("platform", info.Platform)
	set("machine_id", info.MachineID)
	set("hardware_hash", info.Fingerprint)
	set("profile", info.Profile)

	if fp != nil {
		// Fill gaps from fingerprint
		if info.Hostname == "" {
			set("hostname", fp.Hostname)
		}
		if info.OS == "" {
			set("os", fp.OS)
		}
		if info.Arch == "" {
			set("arch", fp.Arch)
		}
		if info.MachineID == "" {
			set("machine_id", fp.MachineID)
		}
		if info.Fingerprint == "" {
			set("hardware_hash", fp.HardwareHash)
		}

		set("serial", fp.SerialNumber)
		set("cpu_info", fp.CPUInfo)
		set("kernel", fp.KernelVersion)

		if len(fp.MACAddrs) > 0 {
			payload["macs"] = fp.MACAddrs
		}
	}

	// OS version string (e.g. "Ubuntu 24.04 LTS")
	set("os_version", join.DetectOSVersion())

	// Device class is now set by the layered discovery merged above.
	// Root-level extras still pulled by the legacy collector for fields the
	// controller already extracts and the new discovery hasn't moved yet.
	if runtime.GOOS == "linux" {
		collectLinuxRoot(payload)
	}

	// Agent data passthrough
	for k, v := range info.AgentData {
		payload[k] = v
	}

	return json.Marshal(payload)
}

// enrollMethod returns "scan" for returning devices (have a hardware hash),
// "new" for first-time enrollment.
func enrollMethod(fp *join.Fingerprint) string {
	if fp != nil && fp.HardwareHash != "" {
		return "scan"
	}
	return "new"
}

// collectLinuxRoot adds Linux-specific data that requires root or privileged access.
func collectLinuxRoot(payload map[string]any) {
	// SSH host keys — cryptographic machine identity, unforgeable
	var sshKeys []string
	for _, path := range []string{
		"/etc/ssh/ssh_host_ed25519_key.pub",
		"/etc/ssh/ssh_host_rsa_key.pub",
		"/etc/ssh/ssh_host_ecdsa_key.pub",
	} {
		if data, err := os.ReadFile(path); err == nil {
			key := strings.TrimSpace(string(data))
			if key != "" {
				sshKeys = append(sshKeys, key)
			}
		}
	}
	if len(sshKeys) > 0 {
		payload["ssh_host_keys"] = sshKeys
	}

	// Disk serials from /sys/block — stable hardware identifiers
	var diskSerials []string
	if entries, err := os.ReadDir("/sys/block"); err == nil {
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") {
				continue
			}
			if serial := readFileTrimmed(filepath.Join("/sys/block", name, "device/serial")); serial != "" {
				diskSerials = append(diskSerials, serial)
			}
		}
	}
	if len(diskSerials) > 0 {
		payload["disk_serials"] = diskSerials
	}

	// Uptime from /proc/uptime
	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		var up float64
		fmt.Sscanf(string(data), "%f", &up)
		payload["uptime_secs"] = int64(up)
	}

	// Boot ID — changes on every reboot, useful for detecting restarts
	if bootID := readFileTrimmed("/proc/sys/kernel/random/boot_id"); bootID != "" {
		payload["boot_id"] = bootID
	}

	// DMI product info — board/product identifiers
	for key, path := range map[string]string{
		"dmi_product_name":   "/sys/class/dmi/id/product_name",
		"dmi_board_serial":   "/sys/class/dmi/id/board_serial",
		"dmi_bios_version":   "/sys/class/dmi/id/bios_version",
		"dmi_chassis_serial": "/sys/class/dmi/id/chassis_serial",
	} {
		if val := readFileTrimmed(path); val != "" && val != "To Be Filled By O.E.M." {
			payload[key] = val
		}
	}

	// Package count — fingerprints the installed software set
	payload["package_count"] = countPackages()

	// Open listening ports (ss -tlnp)
	if ports := listeningPorts(); len(ports) > 0 {
		payload["open_ports"] = ports
	}

	// CPU core count
	payload["cpu_cores"] = runtime.NumCPU()

	// Memory from /proc/meminfo
	if mb := memoryMB(); mb > 0 {
		payload["memory_mb"] = mb
	}
}

func countPackages() int {
	// Try dpkg (Debian/Ubuntu)
	if out, err := exec.Command("dpkg", "--get-selections").Output(); err == nil {
		return strings.Count(string(out), "\n")
	}
	// Try rpm (RHEL/Fedora)
	if out, err := exec.Command("rpm", "-qa").Output(); err == nil {
		return strings.Count(strings.TrimSpace(string(out)), "\n") + 1
	}
	return 0
}

func listeningPorts() []int {
	out, err := exec.Command("ss", "-tlnp").Output()
	if err != nil {
		return nil
	}
	var ports []int
	seen := map[int]bool{}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		// ss output: State Recv-Q Send-Q Local-Address:Port ...
		if len(fields) < 4 || fields[0] == "State" {
			continue
		}
		addr := fields[3]
		if i := strings.LastIndex(addr, ":"); i >= 0 {
			if p, err := strconv.Atoi(addr[i+1:]); err == nil && !seen[p] {
				ports = append(ports, p)
				seen[p] = true
			}
		}
	}
	return ports
}

func memoryMB() int64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, _ := strconv.ParseInt(fields[1], 10, 64)
				return kb / 1024
			}
		}
	}
	return 0
}

func readFileTrimmed(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// readSSEResult reads the SSE stream from the controller until the identity
// event arrives. It logs verify/decision/progress events to stderr so the
// operator can see what's happening in real time.
func readSSEResult(ctx context.Context, resp *http.Response) (*EnrollResult, error) {
	scanner := bufio.NewScanner(resp.Body)

	var eventType string
	var dataLines []string

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		line := scanner.Text()

		switch {
		case strings.HasPrefix(line, "event:"):
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		case line == "":
			// Blank line = end of event, dispatch it
			if eventType != "" && len(dataLines) > 0 {
				data := strings.Join(dataLines, "")
				result, err := handleSSEEvent(eventType, data)
				if result != nil || err != nil {
					return result, err
				}
			}
			eventType = ""
			dataLines = nil
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("enroll: read stream: %w", err)
	}
	return nil, fmt.Errorf("enroll: stream ended without identity event")
}

// handleSSEEvent processes one SSE event. Returns non-nil result on identity
// event, non-nil error on rejection or controller error.
func handleSSEEvent(event, data string) (*EnrollResult, error) {
	var msg map[string]any
	if err := json.Unmarshal([]byte(data), &msg); err != nil {
		return nil, nil // malformed event, skip
	}

	switch event {
	case "verify":
		check, _ := msg["check"].(string)
		passed, _ := msg["passed"].(bool)
		reason, _ := msg["reason"].(string)
		if passed {
			fmt.Fprintf(os.Stderr, "  ✓ %s\n", check)
		} else {
			fmt.Fprintf(os.Stderr, "  ✗ %s — %s\n", check, reason)
		}

	case "decision":
		status, _ := msg["status"].(string)
		reason, _ := msg["reason"].(string)
		if status == "rejected" {
			return nil, fmt.Errorf("enroll: rejected — %s", reason)
		}
		fmt.Fprintf(os.Stderr, "  → %s (%s)\n", status, reason)

	case "progress":
		step, _ := msg["step"].(string)
		fmt.Fprintf(os.Stderr, "  … %s\n", step)

	case "identity":
		status, _ := msg["status"].(string)
		if status == "rejected" {
			reason, _ := msg["reason"].(string)
			return nil, fmt.Errorf("enroll: rejected — %s", reason)
		}

		// Identity is sent as a JSON string (the controller marshals it)
		var identityBytes []byte
		switch v := msg["identity"].(type) {
		case string:
			identityBytes = []byte(v)
		default:
			b, err := json.Marshal(v)
			if err != nil {
				return nil, fmt.Errorf("enroll: marshal identity: %w", err)
			}
			identityBytes = b
		}

		slug, _ := msg["nickname"].(string)
		if slug == "" {
			slug, _ = msg["id"].(string)
		}

		var services []string
		if svcs, ok := msg["services"].([]any); ok {
			for _, s := range svcs {
				if name, ok := s.(string); ok {
					services = append(services, name)
				}
			}
		}
		if status == "" {
			status = "quarantine"
		}

		return &EnrollResult{
			IdentityJSON: identityBytes,
			Slug:         slug,
			Status:       status,
			Services:     services,
		}, nil

	case "error":
		reason, _ := msg["reason"].(string)
		return nil, fmt.Errorf("enroll: controller error — %s", reason)
	}

	return nil, nil
}

// WriteIdentity writes the enrolled identity JSON to disk with restrictive permissions
// and validates the embedded CA is the Ziti controller CA.
func WriteIdentity(identityJSON []byte, identityPath string) error {
	if err := os.MkdirAll(filepath.Dir(identityPath), 0755); err != nil {
		return fmt.Errorf("enroll: mkdir: %w", err)
	}
	if err := os.WriteFile(identityPath, identityJSON, 0600); err != nil {
		return fmt.Errorf("enroll: write identity: %w", err)
	}
	return validateIdentityCA(identityPath)
}

// EnrollJWT exchanges a Ziti one-time JWT for an identity file using the Go SDK.
// Does not require the ziti CLI binary on PATH.
// Use only when the controller cannot enroll server-side (out-of-band JWT flow).
// The primary enrollment path is Register() → WriteIdentity().
func EnrollJWT(jwtStr, identityPath string) error {
	if jwtStr == "" {
		return fmt.Errorf("enroll: jwt required")
	}

	claims, jwtToken, err := zitiEnroll.ParseToken(jwtStr)
	if err != nil {
		return fmt.Errorf("enroll: parse jwt: %w", err)
	}

	var keyAlg ziti.KeyAlgVar
	_ = keyAlg.Set("EC")

	flags := zitiEnroll.EnrollmentFlags{
		Token:     claims,
		JwtToken:  jwtToken,
		JwtString: jwtStr,
		KeyAlg:    keyAlg,
	}

	cfg, err := zitiEnroll.Enroll(flags)
	if err != nil {
		// The Ziti SDK enroll posts to the controller's edge API on a non-443
		// port (typically 1280 today). When that port is blocked — captive
		// portals, locked-down hotel WiFi, restrictive corp egress — this is
		// the failure mode. Surface a hint so the user doesn't think the
		// enrollment endpoint is broken when their network is the problem.
		return fmt.Errorf("enroll: ziti sdk enroll: %w (your network may block port 1280; try tethering or another network — see docs/TECH-DEBT.md)", err)
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("enroll: marshal identity: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(identityPath), 0755); err != nil {
		return fmt.Errorf("enroll: mkdir: %w", err)
	}
	if err := os.WriteFile(identityPath, data, 0600); err != nil {
		return fmt.Errorf("enroll: write identity: %w", err)
	}

	return validateIdentityCA(identityPath)
}

// identityFile is the minimal structure of an enrolled Ziti identity.json.
type identityFile struct {
	ID struct {
		Cert string `json:"cert"`
		Key  string `json:"key"`
		CA   string `json:"ca"`
	} `json:"id"`
	Cert string `json:"cert"`
	Key  string `json:"key"`
	CA   string `json:"ca"`
}

func validateIdentityCA(identityPath string) error {
	raw, err := os.ReadFile(identityPath)
	if err != nil {
		return fmt.Errorf("validate identity: read %s: %w", identityPath, err)
	}
	var id identityFile
	if err := json.Unmarshal(raw, &id); err != nil {
		return fmt.Errorf("validate identity: parse: %w", err)
	}

	caPEM := id.ID.CA
	if caPEM == "" {
		caPEM = id.CA
	}
	certPEM := id.ID.Cert
	if certPEM == "" {
		certPEM = id.Cert
	}
	if caPEM == "" {
		return fmt.Errorf("validate identity: no CA in identity file — re-enroll with 'schmutz enroll --force'")
	}
	if certPEM == "" {
		return fmt.Errorf("validate identity: no cert in identity file — re-enroll with 'schmutz enroll --force'")
	}

	caPEM = strings.TrimPrefix(caPEM, "pem:")
	certPEM = strings.TrimPrefix(certPEM, "pem:")

	caPool := x509.NewCertPool()
	var caCerts []*x509.Certificate
	rest := []byte(caPEM)
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		ca, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		cn := strings.ToLower(ca.Subject.CommonName)
		if strings.Contains(cn, "kontango") || strings.Contains(cn, "bao") {
			return fmt.Errorf("validate identity: CA %q is the Kontango/Bao system CA, not the Ziti controller CA — "+
				"run 'schmutz enroll --force' to re-enroll with the correct CA", ca.Subject.CommonName)
		}
		caPool.AddCert(ca)
		caCerts = append(caCerts, ca)
	}
	if len(caCerts) == 0 {
		return fmt.Errorf("validate identity: no parseable CA certs in identity — re-enroll with 'schmutz enroll --force'")
	}

	var deviceCerts []*x509.Certificate
	rest = []byte(certPEM)
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		deviceCerts = append(deviceCerts, c)
	}
	if len(deviceCerts) == 0 {
		return fmt.Errorf("validate identity: no parseable device cert in identity — re-enroll with 'schmutz enroll --force'")
	}

	intermediates := x509.NewCertPool()
	for _, c := range deviceCerts[1:] {
		intermediates.AddCert(c)
	}
	if _, err := deviceCerts[0].Verify(x509.VerifyOptions{
		Roots:         caPool,
		Intermediates: intermediates,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}); err != nil {
		return fmt.Errorf("validate identity: device cert does not chain to embedded CA — "+
			"wrong CA captured during enrollment; run 'schmutz enroll --force': %w", err)
	}
	return nil
}

// CheckIdentityCA reads an existing identity.json and returns an error if it
// contains the wrong CA. No-op if the file doesn't exist.
func CheckIdentityCA(identityPath string) error {
	if _, err := os.Stat(identityPath); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return validateIdentityCA(identityPath)
}
