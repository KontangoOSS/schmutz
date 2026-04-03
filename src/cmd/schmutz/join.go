package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
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
	noTUI := fs.Bool("no-tui", false, "disable interactive wizard (non-interactive mode)")
	fs.Parse(args)

	// URL is optional in TUI mode — the wizard will prompt for it.
	url := ""
	if fs.NArg() >= 1 {
		url = fs.Arg(0)
	}

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
	if err := runJoin(url, *session, *roleID, *secretID); err != nil {
		fmt.Fprintf(os.Stderr, "join: %v\n", err)
		os.Exit(1)
	}
}

// runJoin is the full non-interactive enrollment flow.
// Called by cmdJoin (non-TTY) and by the agent when it receives an enroll command.
func runJoin(url, session, roleID, secretID string) error {
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

	var result *joinResult

	if session != "" {
		method := "new"
		if roleID != "" {
			method = "approle"
		} else if session != "" {
			// non-empty session with no role_id = invite token
			method = "invite"
		}
		log.Println("enrolling…")
		sseResult, err := join.SSEEnroll(url, method, session, roleID, secretID)
		if err != nil {
			return fmt.Errorf("enrollment: %w", err)
		}
		result = &joinResult{
			ID:       sseResult.ID,
			Nickname: sseResult.Nickname,
			Identity: sseResult.Identity,
			Status:   sseResult.Status,
			Hosts:    sseResult.Config.Hosts,
			Tunnel:   sseResult.Config.Tunnel,
		}
	} else {
		log.Println("enrolling…")
		result, err = restJoin(url, roleID, secretID)
		if err != nil {
			return err
		}
	}

	nick := result.Nickname
	if nick == "" {
		nick = result.ID[:8]
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

	idPath := platform.IdentityPath()
	if err := os.WriteFile(idPath, result.Identity, 0600); err != nil {
		return fmt.Errorf("save identity: %w", err)
	}
	log.Printf("  identity: %s", idPath)

	record, _ := json.MarshalIndent(map[string]interface{}{
		"id": result.ID, "nickname": nick,
		"registered_at": time.Now().Unix(),
	}, "", "  ")
	machineFile := idPath[:strings.LastIndex(idPath, "/")+1] + "machine.json"
	os.WriteFile(machineFile, record, 0600)

	zitiVersion := "latest"
	if v, ok := result.Tunnel["version"].(string); ok && v != "" && v != "latest" {
		zitiVersion = v
	}
	log.Println("installing ziti…")
	if err := platform.InstallZiti(zitiVersion); err != nil {
		return fmt.Errorf("install ziti: %w", err)
	}
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

	log.Println("installing agent…")
	if err := platform.InstallAgent(url, idPath); err != nil {
		log.Printf("  warning: agent install failed: %v", err)
	} else {
		log.Println("  agent started ✓")
	}

	log.Println("installing caddy…")
	if err := platform.InstallCaddy(url, idPath); err != nil {
		log.Printf("  warning: caddy install failed: %v", err)
	} else {
		log.Println("  caddy started ✓")
	}

	log.Printf("\ndone.\n  nickname: %s\n  id:       %s\n  status:   %s\n", nick, result.ID, result.Status)
	return nil
}

// -- Types -------------------------------------------------------------------

type joinResult struct {
	ID       string
	Nickname string
	Identity []byte
	Status   string
	Hosts    []string
	Tunnel   map[string]interface{}
}

// -- v1 REST fallback --------------------------------------------------------

func restJoin(baseURL, roleID, secretID string) (*joinResult, error) {
	req := collectJoinRequest()
	if roleID != "" && secretID != "" {
		req.RoleID = roleID
		req.SecretID = secretID
	}

	log.Printf("registering %s…", req.Hostname)
	resp, err := joinPost(baseURL+"/api/register", req)
	if err != nil {
		return nil, err
	}

	var reg join.Response
	if err := json.Unmarshal(resp, &reg); err != nil {
		return nil, fmt.Errorf("parse registration: %w", err)
	}
	if reg.ZitiJWT == "" {
		return nil, fmt.Errorf("already registered: %s", reg.ID)
	}

	nick := reg.Nickname
	if nick == "" {
		nick = reg.ID[:8]
	}

	cfg := joinFetchConfig(baseURL, reg.ID, nick)
	identity, err := joinPost(baseURL+"/api/enroll", map[string]string{
		"jwt": reg.ZitiJWT, "nickname": nick,
	})
	if err != nil {
		return nil, fmt.Errorf("enroll: %w", err)
	}

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
	if tunnel == nil {
		tunnel = map[string]interface{}{}
	}

	status := "quarantine"
	if roleID != "" {
		status = "trusted"
	}

	return &joinResult{
		ID: reg.ID, Nickname: nick, Identity: identity,
		Status: status, Hosts: hosts, Tunnel: tunnel,
	}, nil
}

// -- Helpers -----------------------------------------------------------------

func collectJoinRequest() join.Request {
	h, _ := os.Hostname()
	fp, _ := join.Collect()
	tz := ""
	if loc := time.Now().Location(); loc != nil {
		tz = loc.String()
	}
	req := join.Request{
		Hostname: h, OS: runtime.GOOS, OSVersion: join.DetectOSVersion(),
		Arch: runtime.GOARCH, Timezone: tz,
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

func joinPost(url string, payload interface{}) ([]byte, error) {
	body, _ := json.Marshal(payload)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Post(url, "application/json", strings.NewReader(string(body)))
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

func joinFetchConfig(baseURL, machineID, nickname string) map[string]interface{} {
	req, _ := http.NewRequest("GET", baseURL+"/api/config", nil)
	req.Header.Set("Authorization", "Bearer "+machineID)
	req.Header.Set("X-Nickname", nickname)
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil || resp.StatusCode != 200 {
		return nil
	}
	defer resp.Body.Close()
	var cfg map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&cfg)
	return cfg
}
