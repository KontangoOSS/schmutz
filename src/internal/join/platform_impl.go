package join

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// LinuxPlatform handles registration on Linux (x86_64, arm64, arm).
type LinuxPlatform struct{}

func (p *LinuxPlatform) Preflight() []string {
	var missing []string
	if os.Getuid() != 0 {
		missing = append(missing, "root access (run with sudo)")
	}
	if err := os.MkdirAll("/opt/tango/.preflight", 0755); err != nil {
		missing = append(missing, "write access to /opt/tango")
	} else {
		os.Remove("/opt/tango/.preflight")
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		missing = append(missing, "systemd (systemctl not found)")
	}
	if _, err := net.DialTimeout("tcp", "1.1.1.1:443", 5*time.Second); err != nil {
		missing = append(missing, "network access (cannot reach internet)")
	}
	return missing
}

func (p *LinuxPlatform) TangoDir() string { return "/opt/tango" }
func (p *LinuxPlatform) ZitiBinaryPath() string {
	return filepath.Join(p.TangoDir(), "bin", "ziti")
}
func (p *LinuxPlatform) IdentityPath() string { return filepath.Join(p.TangoDir(), "identity.json") }
func (p *LinuxPlatform) EnsureDir() error {
	return os.MkdirAll(filepath.Join(p.TangoDir(), "bin"), 0755)
}

func (p *LinuxPlatform) ZitiDownloadURL(version string) string {
	return fmt.Sprintf("https://github.com/openziti/ziti/releases/download/v%s/ziti-%s-%s-%s.tar.gz",
		version, zitiOSString(), zitiArchString(), version)
}

func (p *LinuxPlatform) InstallZiti(version string) error {
	if _, err := os.Stat(p.ZitiBinaryPath()); err == nil {
		return nil
	}
	url := p.ZitiDownloadURL(version)
	return downloadAndExtractTarGz(url, filepath.Join(p.TangoDir(), "bin"), "ziti")
}

func (p *LinuxPlatform) InstallService(identityFile string) error {
	unit := fmt.Sprintf(`[Unit]
Description=Tango Root Tunnel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s tunnel host -i %s -v
Restart=on-failure
RestartSec=5s
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
`, p.ZitiBinaryPath(), identityFile)

	if err := os.WriteFile("/etc/systemd/system/tango-tunnel.service", []byte(unit), 0644); err != nil {
		return err
	}
	exec.Command("systemctl", "daemon-reload").Run()
	return exec.Command("systemctl", "enable", "tango-tunnel.service").Run()
}

func (p *LinuxPlatform) StartService() error {
	return exec.Command("systemctl", "start", "tango-tunnel.service").Run()
}
func (p *LinuxPlatform) StopService() error {
	return exec.Command("systemctl", "stop", "tango-tunnel.service").Run()
}
func (p *LinuxPlatform) ServiceStatus() string {
	out, _ := exec.Command("systemctl", "is-active", "tango-tunnel.service").Output()
	return strings.TrimSpace(string(out))
}

func (p *LinuxPlatform) WaitForTunnel(timeout time.Duration) error {
	zitiBin := p.ZitiBinaryPath()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if p.ServiceStatus() != "active" {
			time.Sleep(2 * time.Second)
			continue
		}
		// Use the v2 ziti agent IPC to check tunnel health
		out, err := exec.Command(zitiBin, "agent", "stats",
			"--app-type", "tunnel", "--timeout", "3s").Output()
		if err == nil && len(out) > 0 {
			var stats map[string]interface{}
			if json.Unmarshal(out, &stats) == nil {
				return nil // agent responded — tunnel is connected
			}
			// Non-JSON response is also fine — it means the agent is alive
			if len(out) > 10 {
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("tunnel did not become healthy within %s", timeout)
}

// DarwinPlatform handles registration on macOS (Intel + Apple Silicon).
type DarwinPlatform struct{}

func (p *DarwinPlatform) Preflight() []string {
	var missing []string
	if os.Getuid() != 0 {
		missing = append(missing, "root access (run with sudo)")
	}
	if err := os.MkdirAll("/usr/local/tango/.preflight", 0755); err != nil {
		missing = append(missing, "write access to /usr/local/tango")
	} else {
		os.Remove("/usr/local/tango/.preflight")
	}
	if _, err := exec.LookPath("launchctl"); err != nil {
		missing = append(missing, "launchctl (not found)")
	}
	if _, err := net.DialTimeout("tcp", "1.1.1.1:443", 5*time.Second); err != nil {
		missing = append(missing, "network access (cannot reach internet)")
	}
	return missing
}

func (p *DarwinPlatform) TangoDir() string       { return "/usr/local/tango" }
func (p *DarwinPlatform) ZitiBinaryPath() string { return filepath.Join(p.TangoDir(), "bin", "ziti") }
func (p *DarwinPlatform) IdentityPath() string   { return filepath.Join(p.TangoDir(), "identity.json") }
func (p *DarwinPlatform) EnsureDir() error {
	return os.MkdirAll(filepath.Join(p.TangoDir(), "bin"), 0755)
}

func (p *DarwinPlatform) ZitiDownloadURL(version string) string {
	return fmt.Sprintf("https://github.com/openziti/ziti/releases/download/v%s/ziti-%s-%s-%s.tar.gz",
		version, zitiOSString(), zitiArchString(), version)
}

func (p *DarwinPlatform) InstallZiti(version string) error {
	if _, err := os.Stat(p.ZitiBinaryPath()); err == nil {
		return nil
	}
	url := p.ZitiDownloadURL(version)
	return downloadAndExtractTarGz(url, filepath.Join(p.TangoDir(), "bin"), "ziti")
}

const darwinPlistPath = "/Library/LaunchDaemons/io.schmutz.tango-tunnel.plist"

func (p *DarwinPlatform) InstallService(identityFile string) error {
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key><string>io.schmutz.tango-tunnel</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string><string>tunnel</string><string>host</string>
        <string>-i</string><string>%s</string><string>-v</string>
    </array>
    <key>RunAtLoad</key><true/>
    <key>KeepAlive</key><true/>
    <key>StandardOutPath</key><string>%s/tango-tunnel.log</string>
    <key>StandardErrorPath</key><string>%s/tango-tunnel.log</string>
</dict>
</plist>`, p.ZitiBinaryPath(), identityFile, p.TangoDir(), p.TangoDir())
	return os.WriteFile(darwinPlistPath, []byte(plist), 0644)
}

func (p *DarwinPlatform) StartService() error {
	return exec.Command("launchctl", "load", darwinPlistPath).Run()
}
func (p *DarwinPlatform) StopService() error {
	return exec.Command("launchctl", "unload", darwinPlistPath).Run()
}
func (p *DarwinPlatform) ServiceStatus() string {
	if err := exec.Command("launchctl", "list", "io.schmutz.tango-tunnel").Run(); err != nil {
		return "not running"
	}
	return "running"
}

func (p *DarwinPlatform) WaitForTunnel(timeout time.Duration) error {
	zitiBin := p.ZitiBinaryPath()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if p.ServiceStatus() != "running" {
			time.Sleep(2 * time.Second)
			continue
		}
		out, err := exec.Command(zitiBin, "agent", "stats",
			"--app-type", "tunnel", "--timeout", "3s").Output()
		if err == nil && len(out) > 10 {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("tunnel did not become healthy within %s", timeout)
}

// WindowsPlatform handles registration on Windows.
type WindowsPlatform struct{}

func (p *WindowsPlatform) Preflight() []string {
	var missing []string
	tangoDir := p.TangoDir()
	if err := os.MkdirAll(filepath.Join(tangoDir, ".preflight"), 0755); err != nil {
		missing = append(missing, "admin access (cannot write to "+tangoDir+")")
	} else {
		os.Remove(filepath.Join(tangoDir, ".preflight"))
	}
	if _, err := exec.LookPath("sc.exe"); err != nil {
		missing = append(missing, "sc.exe (not found)")
	}
	if _, err := net.DialTimeout("tcp", "1.1.1.1:443", 5*time.Second); err != nil {
		missing = append(missing, "network access (cannot reach internet)")
	}
	return missing
}

func (p *WindowsPlatform) TangoDir() string {
	pd := os.Getenv("ProgramData")
	if pd == "" {
		pd = `C:\ProgramData`
	}
	return filepath.Join(pd, "tango")
}
func (p *WindowsPlatform) ZitiBinaryPath() string {
	return filepath.Join(p.TangoDir(), "bin", "ziti.exe")
}
func (p *WindowsPlatform) IdentityPath() string { return filepath.Join(p.TangoDir(), "identity.json") }
func (p *WindowsPlatform) EnsureDir() error {
	return os.MkdirAll(filepath.Join(p.TangoDir(), "bin"), 0755)
}

func (p *WindowsPlatform) ZitiDownloadURL(version string) string {
	return fmt.Sprintf("https://github.com/openziti/ziti/releases/download/v%s/ziti-windows-%s-%s.zip",
		version, zitiArchString(), version)
}

func (p *WindowsPlatform) InstallZiti(version string) error {
	if _, err := os.Stat(p.ZitiBinaryPath()); err == nil {
		return nil
	}
	url := p.ZitiDownloadURL(version)
	return downloadAndExtractZip(url, filepath.Join(p.TangoDir(), "bin"), "ziti.exe")
}

func (p *WindowsPlatform) InstallService(identityFile string) error {
	binPath := fmt.Sprintf(`"%s" tunnel host -i "%s" -v`, p.ZitiBinaryPath(), identityFile)
	return exec.Command("sc.exe", "create", "TangoTunnel",
		fmt.Sprintf("binPath=%s", binPath), "start=auto", "DisplayName=Tango Root Tunnel").Run()
}

func (p *WindowsPlatform) StartService() error {
	return exec.Command("sc.exe", "start", "TangoTunnel").Run()
}
func (p *WindowsPlatform) StopService() error {
	return exec.Command("sc.exe", "stop", "TangoTunnel").Run()
}
func (p *WindowsPlatform) ServiceStatus() string {
	out, err := exec.Command("sc.exe", "query", "TangoTunnel").Output()
	if err != nil {
		return "not installed"
	}
	if bytes.Contains(out, []byte("RUNNING")) {
		return "running"
	}
	return "stopped"
}

func (p *WindowsPlatform) WaitForTunnel(timeout time.Duration) error {
	zitiBin := p.ZitiBinaryPath()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if p.ServiceStatus() != "running" {
			time.Sleep(2 * time.Second)
			continue
		}
		out, err := exec.Command(zitiBin, "agent", "stats",
			"--app-type", "tunnel", "--timeout", "3s").Output()
		if err == nil && len(out) > 10 {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("tunnel did not become healthy within %s", timeout)
}

// ensure runtime import is used
var _ = runtime.GOOS
