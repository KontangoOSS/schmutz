// Package service implements the systemd service setup step.
package service

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/KontangoOSS/schmutz/internal/pipeline"
	"github.com/pterm/pterm"
)

// Step implements the service setup bootstrap step.
type Step struct{}

// New returns a new service Step.
func New() *Step {
	return &Step{}
}

// Name returns the display name of this step.
func (s *Step) Name() string {
	return "Setup Service"
}

// Skip returns true when the device is not registered (no point setting up
// service if not registered).
func (s *Step) Skip(ctx *pipeline.Context) bool {
	return !ctx.Registered
}

// Run sets up a systemd unit that runs schmutz in wait-role mode.
func (s *Step) Run(ctx *pipeline.Context) error {
	// 1. Get current executable path.
	binary, err := os.Executable()
	if err != nil {
		binary = "/usr/local/bin/schmutz"
	}

	// 2. Generate systemd unit content.
	unitContent := GenerateUnit(binary, ctx.Domain)

	// 3. Write to /etc/systemd/system/schmutz.service.
	if err := os.WriteFile("/etc/systemd/system/schmutz.service", []byte(unitContent), 0644); err != nil {
		return fmt.Errorf("failed to write systemd unit: %w", err)
	}

	// 4. Run systemctl daemon-reload, enable, start (ignore errors).
	_ = exec.Command("systemctl", "daemon-reload").Run()
	_ = exec.Command("systemctl", "enable", "schmutz").Run()
	_ = exec.Command("systemctl", "start", "schmutz").Run()

	// 5. Print success message.
	pterm.Success.Println("schmutz service configured and started in wait-role mode")

	return nil
}

// GenerateUnit returns a systemd unit file string with the specified binary
// and domain. It is exported for testing.
func GenerateUnit(binary, domain string) string {
	return fmt.Sprintf(`[Unit]
Description=Schmutz — waiting for role assignment
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s wait-role --domain %s
Restart=always
RestartSec=30
Environment="SCHMUTZ_MODE=wait-role"

[Install]
WantedBy=multi-user.target
`, binary, domain)
}
