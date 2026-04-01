package main

import (
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

const version = "2.0.0"

func main() {
	log.SetFlags(0)

	url, roleID, secretID, session := parseArgs()

	log.Printf("schmutz-join v%s\n", version)

	// 1. Preflight
	platform, err := join.DetectPlatform()
	if err != nil {
		log.Fatalf("unsupported platform: %v", err)
	}
	if missing := platform.Preflight(); len(missing) > 0 {
		log.Println("preflight failed:")
		for _, m := range missing {
			log.Printf("  - %s", m)
		}
		os.Exit(1)
	}

	// 2. Enroll via WebSocket (v2) or REST (v1 fallback)
	var result *enrollResult

	if session != "" {
		// v2: SSE enrollment — one POST, streaming response
		log.Println("enrolling…")
		method := "new"
		if roleID != "" {
			method = "approle"
		}
		sseResult, err := join.SSEEnroll(url, method, session, roleID, secretID)
		if err != nil {
			log.Fatalf("enrollment failed: %v", err)
		}
		result = &enrollResult{
			ID:       sseResult.ID,
			Nickname: sseResult.Nickname,
			Identity: sseResult.Identity,
			Status:   sseResult.Status,
			Hosts:    sseResult.Config.Hosts,
			Tunnel:   sseResult.Config.Tunnel,
		}
	} else {
		// v1: REST enrollment — backward compatible
		log.Println("enrolling via REST (v1)…")
		result = restEnroll(url, roleID, secretID, platform)
	}

	if result == nil {
		log.Fatal("enrollment returned no result")
	}

	nick := result.Nickname
	if nick == "" {
		nick = result.ID[:8]
	}
	log.Printf("enrolled: %s (%s) [%s]", nick, result.ID, result.Status)

	// 3. Save identity + machine record
	must(platform.EnsureDir(), "create directories")

	idPath := platform.IdentityPath()
	must(os.WriteFile(idPath, result.Identity, 0600), "save identity")
	log.Printf("  identity: %s", idPath)

	record, _ := json.MarshalIndent(map[string]interface{}{
		"id": result.ID, "nickname": nick,
		"registered_at": time.Now().Unix(),
	}, "", "  ")
	os.WriteFile(filepath.Join(platform.TangoDir(), "machine.json"), record, 0600)

	// 4. Apply hosts
	applyHosts(result.Hosts)

	// 5. Install + start tunnel
	zitiVersion := "2.0.0-pre5"
	if v, ok := result.Tunnel["version"].(string); ok && v != "" {
		zitiVersion = v
	}
	log.Println("installing ziti…")
	must(platform.InstallZiti(zitiVersion), "install ziti")
	log.Println("starting tunnel…")
	must(platform.InstallService(idPath), "install service")
	must(platform.StartService(), "start service")

	// 6. Verify
	log.Println("verifying…")
	if err := platform.WaitForTunnel(60 * time.Second); err != nil {
		log.Printf("  warning: %v (may still be connecting)", err)
	} else {
		log.Println("  connected ✓")
	}

	log.Printf("\ndone.\n  nickname:  %s\n  id:        %s\n  identity:  %s\n  status:    %s\n",
		nick, result.ID, idPath, result.Status)
}

// --- Types ---

type enrollResult struct {
	ID       string
	Nickname string
	Identity []byte
	Status   string // approved, quarantine
	Hosts    []string
	Tunnel   map[string]interface{}
}

// --- Args ---

func parseArgs() (url, roleID, secretID, session string) {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "schmutz-join v%s — join the network\n\n"+
			"Usage:\n  schmutz-join <url> [--role-id=ID --secret-id=ID] [--session=TOKEN]\n\n"+
			"Example:\n  schmutz-join https://join.example.net\n  schmutz-join https://join.example.net --session=abc123\n", version)
		os.Exit(1)
	}
	url = os.Args[1]
	for _, a := range os.Args[2:] {
		if strings.HasPrefix(a, "--role-id=") {
			roleID = strings.TrimPrefix(a, "--role-id=")
		}
		if strings.HasPrefix(a, "--secret-id=") {
			secretID = strings.TrimPrefix(a, "--secret-id=")
		}
		if strings.HasPrefix(a, "--session=") {
			session = strings.TrimPrefix(a, "--session=")
		}
	}
	return
}

// --- v1 REST fallback ---

func restEnroll(url, roleID, secretID string, platform join.Platform) *enrollResult {
	req := scan()
	if roleID != "" && secretID != "" {
		req.RoleID = roleID
		req.SecretID = secretID
	}

	log.Printf("registering %s …", req.Hostname)
	resp := post(url+"/api/register", req)
	var reg join.Response
	must(json.Unmarshal(resp, &reg), "parse registration")
	if reg.ZitiJWT == "" {
		log.Printf("already registered: %s", reg.ID)
		os.Exit(0)
	}

	nick := reg.Nickname
	if nick == "" {
		nick = reg.ID[:8]
	}
	log.Printf("registered: %s (%s)", nick, reg.ID)

	cfg := fetchConfig(url, reg.ID, nick)
	identity := post(url+"/api/enroll", map[string]string{
		"jwt": reg.ZitiJWT, "nickname": nick,
	})

	var hosts []string
	var tunnel map[string]interface{}
	if cfg != nil {
		if h, ok := cfg["hosts"].([]interface{}); ok {
			for _, v := range h {
				if s, ok := v.(string); ok {
					hosts = append(hosts, s)
				}
			}
		}
		if t, ok := cfg["tunnel"].(map[string]interface{}); ok {
			tunnel = t
		}
	}

	status := "quarantine"
	if roleID != "" {
		status = "trusted"
	}

	return &enrollResult{
		ID: reg.ID, Nickname: nick, Identity: identity,
		Status: status, Hosts: hosts, Tunnel: tunnel,
	}
}

// --- Helpers ---

func scan() join.Request {
	h, _ := os.Hostname()
	fp, _ := join.Collect()
	tz := ""
	if loc := time.Now().Location(); loc != nil {
		tz = loc.String()
	}
	req := join.Request{
		Hostname: h, OS: osName(), OSVersion: join.DetectOSVersion(),
		Arch: archName(), Timezone: tz,
	}
	if fp != nil {
		req.HardwareHash = fp.HardwareHash
		req.MACAddrs = fp.MACAddrs
		req.CPUInfo = fp.CPUInfo
		req.MachineID = fp.MachineID
		req.SerialNumber = fp.SerialNumber
		req.KernelVersion = fp.KernelVersion
	}
	log.Println("  hostname:    ", req.Hostname)
	log.Println("  os:          ", req.OSVersion)
	log.Println("  arch:        ", req.Arch)
	if fp != nil && fp.HardwareHash != "" {
		log.Println("  fingerprint: ", fp.HardwareHash)
	}
	return req
}

func applyHosts(hosts []string) {
	// Hosts from config — nothing to write to /etc/hosts for bare hostnames
	// The Ziti tunnel handles DNS resolution
}

func osName() string   { return runtime.GOOS }
func archName() string { return runtime.GOARCH }

func post(url string, payload interface{}) []byte {
	body, _ := json.Marshal(payload)
	resp, err := httpClient.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		log.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		log.Fatalf("HTTP %d: %s", resp.StatusCode, string(data))
	}
	return data
}

func fetchConfig(baseURL, machineID, nickname string) map[string]interface{} {
	req, _ := http.NewRequest("GET", baseURL+"/api/config", nil)
	req.Header.Set("Authorization", "Bearer "+machineID)
	req.Header.Set("X-Nickname", nickname)
	resp, err := httpClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return nil
	}
	defer resp.Body.Close()
	var cfg map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&cfg)
	return cfg
}

var httpClient = &http.Client{Timeout: 60 * time.Second}

func must(err error, context string) {
	if err != nil {
		log.Fatalf("%s: %v", context, err)
	}
}
