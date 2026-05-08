package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// systemdUnitTemplate is what we drop at /etc/systemd/system/schmutz.service.
// Kept minimal — we want operators to be able to read it and understand it.
// Hardening (NoNewPrivileges, ProtectSystem) is intentionally NOT here for
// the default unit because tproxy mode needs CAP_NET_ADMIN. The
// `--mode=tproxy` flag swaps to a unit with the right capabilities.
const systemdUnitTemplate = `[Unit]
Description=Schmutz Ziti agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart={{BIN}} {{COMMAND}}
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal
{{EXTRA}}
[Install]
WantedBy=multi-user.target
`

// installServiceCmd writes the systemd unit and enables the schmutz agent.
// Requires root. Idempotent — re-running installs the latest template
// content even if a unit was already present (operator's manual edits are
// overwritten; this is documented as the expected behavior).
func installServiceCmd() *cobra.Command {
	var mode string
	var force bool
	cmd := &cobra.Command{
		Use:   "install-service",
		Short: "Install and enable the schmutz systemd unit",
		Long: "Drops the systemd service unit at " + systemdUnitPath + " and enables it. " +
			"Requires root. Modes:\n" +
			"  tunnel  — `schmutz start` (in-process Ziti SDK; default)\n" +
			"  tproxy  — `schmutz tunnel tproxy` (system-wide intercept; needs CAP_NET_ADMIN)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !isRoot() {
				return errors.New("install-service requires root")
			}

			var command string
			var extra string
			switch mode {
			case "tunnel", "":
				command = "start"
			case "tproxy":
				command = "tunnel tproxy"
				extra = "AmbientCapabilities=CAP_NET_ADMIN CAP_NET_BIND_SERVICE\nCapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_BIND_SERVICE\n"
			default:
				return fmt.Errorf("unknown mode %q (use 'tunnel' or 'tproxy')", mode)
			}

			binPath, err := os.Executable()
			if err != nil {
				return fmt.Errorf("locate own binary: %w", err)
			}
			absBinPath, err := filepath.Abs(binPath)
			if err == nil {
				binPath = absBinPath
			}

			content := systemdUnitTemplate
			content = strings.ReplaceAll(content, "{{BIN}}", binPath)
			content = strings.ReplaceAll(content, "{{COMMAND}}", command)
			content = strings.ReplaceAll(content, "{{EXTRA}}", extra)

			if fileExists(systemdUnitPath) && !force {
				existing, _ := os.ReadFile(systemdUnitPath)
				if string(existing) == content {
					fmt.Println("schmutz: systemd unit already up to date")
				} else {
					fmt.Println("schmutz: systemd unit exists with different content; pass --force to overwrite")
					return nil
				}
			} else {
				if err := os.WriteFile(systemdUnitPath, []byte(content), 0644); err != nil {
					return fmt.Errorf("write %s: %w", systemdUnitPath, err)
				}
				fmt.Printf("schmutz: wrote %s\n", systemdUnitPath)
			}

			if err := runSystemctl("daemon-reload"); err != nil {
				return fmt.Errorf("daemon-reload: %w", err)
			}
			if err := runSystemctl("enable", "--now", "schmutz.service"); err != nil {
				return fmt.Errorf("enable: %w", err)
			}
			fmt.Println("schmutz: service enabled and started")
			fmt.Println("       view logs:    journalctl -u schmutz -f")
			fmt.Println("       check state:  systemctl status schmutz")
			return nil
		},
	}
	cmd.Flags().StringVar(&mode, "mode", "tunnel", "service mode: tunnel or tproxy")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing unit file")
	return cmd
}

// uninstallCmd is the inverse: stop service, disable, remove unit. By default
// preserves /etc/schmutz/ so the operator can re-install without re-enrolling.
// Pass --purge to wipe identity and bundled binaries too.
func uninstallCmd() *cobra.Command {
	var force bool
	var purge bool
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Stop and remove the schmutz systemd service",
		Long: "Stops and disables the schmutz systemd service and removes the unit file. " +
			"Identity and config under /etc/schmutz/ are preserved by default. Pass --purge to remove everything.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !isRoot() {
				return errors.New("uninstall requires root")
			}
			if !force {
				if !confirm("Stop and remove schmutz systemd unit?") {
					return errors.New("aborted")
				}
				if purge && !confirm("Also delete /etc/schmutz/ and uninstall the binary?") {
					return errors.New("aborted")
				}
			}

			// Stop the service. Tolerate "unit not loaded" — we want this idempotent.
			_ = runSystemctlBestEffort("stop", "schmutz.service")
			_ = runSystemctlBestEffort("disable", "schmutz.service")

			if fileExists(systemdUnitPath) {
				if err := os.Remove(systemdUnitPath); err != nil {
					return fmt.Errorf("remove %s: %w", systemdUnitPath, err)
				}
				fmt.Printf("schmutz: removed %s\n", systemdUnitPath)
			} else {
				fmt.Printf("schmutz: %s did not exist\n", systemdUnitPath)
			}
			_ = runSystemctlBestEffort("daemon-reload")

			if !purge {
				fmt.Println()
				fmt.Println("schmutz: identity and config preserved at /etc/schmutz/")
				fmt.Println("       Ziti identity is still registered with the controller — operator should remove it via admin UI when convenient")
				fmt.Println("       run with --purge to wipe local state and binaries")
				return nil
			}

			if err := os.RemoveAll(schmutzDir); err != nil {
				fmt.Printf("schmutz: warn: could not remove %s: %v\n", schmutzDir, err)
			} else {
				fmt.Printf("schmutz: removed %s\n", schmutzDir)
			}
			// Don't remove our own binary while running — schedule a deferred
			// unlink. Caller's current process file descriptor stays valid.
			selfPath, _ := os.Executable()
			if selfPath != "" {
				if err := os.Remove(selfPath); err != nil {
					fmt.Printf("schmutz: warn: could not remove %s: %v\n", selfPath, err)
				} else {
					fmt.Printf("schmutz: removed %s\n", selfPath)
				}
			}
			if fileExists(defaultZitiPath) {
				_ = os.Remove(defaultZitiPath)
				fmt.Printf("schmutz: removed %s\n", defaultZitiPath)
			}
			fmt.Println()
			fmt.Println("schmutz: device should be removed from the controller via admin UI")
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "skip confirmation prompts")
	cmd.Flags().BoolVar(&purge, "purge", false, "also remove /etc/schmutz/ and the bundled binaries")
	return cmd
}

// installService writes the systemd unit for the given binary path and
// enables + starts the service. Called automatically after hub enrollment.
func installService(binPath string) error {
	if binPath == "" {
		var err error
		binPath, err = os.Executable()
		if err != nil {
			return fmt.Errorf("locate binary: %w", err)
		}
	}
	content := systemdUnitTemplate
	content = strings.ReplaceAll(content, "{{BIN}}", binPath)
	content = strings.ReplaceAll(content, "{{COMMAND}}", "start")
	content = strings.ReplaceAll(content, "{{EXTRA}}", "")
	if err := os.WriteFile(systemdUnitPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write %s: %w", systemdUnitPath, err)
	}
	if err := runSystemctl("daemon-reload"); err != nil {
		return err
	}
	return runSystemctl("enable", "--now", "schmutz.service")
}

// --- helpers ---

func isRoot() bool { return os.Geteuid() == 0 }

func runSystemctl(args ...string) error {
	c := exec.Command("systemctl", args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

// runSystemctlBestEffort is like runSystemctl but swallows non-zero exits —
// used during teardown where "unit not loaded" is fine.
func runSystemctlBestEffort(args ...string) error {
	c := exec.Command("systemctl", args...)
	// suppress noisy output for best-effort cleanup
	c.Stdout = nil
	c.Stderr = nil
	_ = c.Run()
	return nil
}

func confirm(prompt string) bool {
	fmt.Printf("%s [y/N] ", prompt)
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		// Non-interactive — assume no for safety
		fmt.Println("no (non-interactive)")
		return false
	}
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	if err != nil {
		return false
	}
	switch strings.TrimSpace(strings.ToLower(line)) {
	case "y", "yes":
		return true
	}
	return false
}
