package main

import (
	"bytes"
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

const version = "1.0.0"

func main() {
	log.SetFlags(0)

	url, roleID, secretID := parseArgs()

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

	// 2. Scan
	req := scan()
	if roleID != "" && secretID != "" {
		req.RoleID = roleID
		req.SecretID = secretID
	}

	// 3. Register
	log.Printf("\nRegistering %s …", req.Hostname)
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

	// 4. Save machine record
	must(platform.EnsureDir(), "create directories")
	record, _ := json.MarshalIndent(map[string]interface{}{
		"id": reg.ID, "nickname": nick, "hostname": req.Hostname,
		"registered_at": time.Now().Unix(),
	}, "", "  ")
	os.WriteFile(filepath.Join(platform.TangoDir(), "machine.json"), record, 0600)

	// 5. Fetch config — controller tells us everything
	log.Println("fetching config…")
	cfg := fetchConfig(url, reg.ID, nick)
	applyHosts(cfg)

	// 6. Enroll — controller proxies to the dark port
	log.Println("enrolling…")
	identity := post(url+"/api/enroll", map[string]string{
		"jwt": reg.ZitiJWT, "nickname": nick,
	})
	idPath := platform.IdentityPath()
	must(os.WriteFile(idPath, identity, 0600), "save identity")
	log.Printf("  identity: %s", idPath)

	// 7. Install + start tunnel
	zitiVersion := configStr(cfg, "tunnel", "version")
	if zitiVersion == "" {
		zitiVersion = "2.0.0-pre5"
	}
	log.Println("installing ziti…")
	must(platform.InstallZiti(zitiVersion), "install ziti")
	log.Println("starting tunnel…")
	must(platform.InstallService(idPath), "install service")
	must(platform.StartService(), "start service")

	// 8. Verify
	log.Println("verifying…")
	if err := platform.WaitForTunnel(60 * time.Second); err != nil {
		log.Printf("  warning: %v (may still be connecting)", err)
	} else {
		log.Println("  connected ✓")
	}

	// 9. Done
	status := "quarantine"
	if roleID != "" {
		status = "trusted"
	}
	log.Printf("\ndone.\n  nickname:  %s\n  id:        %s\n  identity:  %s\n  status:    %s\n",
		nick, reg.ID, idPath, status)
}

// --- Args ---

func parseArgs() (url, roleID, secretID string) {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "schmutz-join v%s — join the network\n\n"+
			"Usage:\n  schmutz-join <url> [--role-id=ID --secret-id=ID]\n\n"+
			"Example:\n  schmutz-join https://join.example.net\n  schmutz-join https://join.example.net --role-id=xxx --secret-id=yyy\n", version)
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
	}
	return
}

// --- Scan ---

func scan() join.Request {
	h, _ := os.Hostname()
	fp, _ := join.Collect()

	tz := ""
	if loc := time.Now().Location(); loc != nil {
		tz = loc.String()
	}
	req := join.Request{
		Hostname:  h,
		OS:        runtime.GOOS,
		OSVersion: join.DetectOSVersion(),
		Arch:      runtime.GOARCH,
		Timezone:  tz,
	}
	if fp != nil {
		req.HardwareHash = fp.HardwareHash
		req.MACAddrs = fp.MACAddrs
		req.CPUInfo = fp.CPUInfo
		req.MachineID = fp.MachineID
		req.SerialNumber = fp.SerialNumber
		req.KernelVersion = fp.KernelVersion
	}

	log.Println("  hostname:     ", req.Hostname)
	log.Println("  os:           ", req.OSVersion)
	log.Println("  arch:         ", req.Arch)
	if fp != nil && fp.CPUInfo != "" {
		log.Println("  cpu:          ", fp.CPUInfo)
	}
	if fp != nil && fp.HardwareHash != "" {
		log.Println("  fingerprint:  ", fp.HardwareHash)
	}

	return req
}

// --- HTTP helpers ---

var client = &http.Client{Timeout: 60 * time.Second}

func post(url string, payload interface{}) []byte {
	body, _ := json.Marshal(payload)
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
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

// --- Config application ---

func applyHosts(cfg map[string]interface{}) {
	if cfg == nil {
		return
	}
	// Hosts can be []interface{} (list of hostnames) or map[string]interface{} (hostname→ip)
	switch v := cfg["hosts"].(type) {
	case []interface{}:
		// Controller returns a list of hostnames — nothing to write
	case map[string]interface{}:
		existing, _ := os.ReadFile("/etc/hosts")
		f, err := os.OpenFile("/etc/hosts", os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return
		}
		defer f.Close()
		for hostname, ip := range v {
			if ipStr, ok := ip.(string); ok && ipStr != "" && !bytes.Contains(existing, []byte(hostname)) {
				f.WriteString(ipStr + " " + hostname + "\n")
				log.Printf("  host: %s → %s", hostname, ipStr)
			}
		}
	}
}

func configStr(cfg map[string]interface{}, keys ...string) string {
	if cfg == nil {
		return ""
	}
	var cur interface{} = cfg
	for _, k := range keys {
		m, ok := cur.(map[string]interface{})
		if !ok {
			return ""
		}
		cur = m[k]
	}
	s, _ := cur.(string)
	return s
}

func must(err error, context string) {
	if err != nil {
		log.Fatalf("%s: %v", context, err)
	}
}
