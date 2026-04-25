package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/KontangoOSS/schmutz/internal/enroll"
	"github.com/KontangoOSS/schmutz/root"
	"github.com/spf13/cobra"
)

// tunnelCmd is the umbrella for `schmutz tunnel <subcmd>`. The subcommands
// shell out to the bundled `ziti` binary — schmutz is a thin opinionated
// wrapper over the upstream Ziti CLI.
//
// Why shell out vs use the Go SDK in-process?  The default `schmutz start`
// path uses the SDK directly. The `tunnel` subcommands exist for operators
// who need the full upstream feature surface (tproxy, dial-only proxy mode,
// per-service host bindings) without us having to re-implement everything
// in Go. Anything requiring CAP_NET_ADMIN (tproxy) MUST shell out.
func tunnelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tunnel",
		Short: "Run Ziti tunnel commands (wraps the bundled ziti binary)",
		Long: "Schmutz ships with the OpenZiti `ziti` binary alongside its own. " +
			"`schmutz tunnel <subcommand>` invokes the corresponding `ziti tunnel <subcommand>` " +
			"using the device's enrolled identity at /etc/schmutz/identity.json.",
	}
	cmd.AddCommand(tunnelRunCmd(), tunnelHostCmd(), tunnelProxyCmd(), tunnelTproxyCmd())
	return cmd
}

func tunnelRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run the in-process Ziti tunnel (alias for `schmutz start`)",
		Long:  "Equivalent to `schmutz start` — uses the embedded Go SDK to bind services without spawning the ziti binary.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return startCmd().RunE(cmd, nil)
		},
	}
}

func tunnelHostCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                "host [extra args...]",
		Short:              "Bind one or more overlay services explicitly (wraps `ziti tunnel host`)",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return execZitiTunnel("host", args)
		},
	}
	return cmd
}

func tunnelProxyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                "proxy [extra args...]",
		Short:              "Run a local TCP proxy for an overlay service (wraps `ziti tunnel proxy`)",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return execZitiTunnel("proxy", args)
		},
	}
	return cmd
}

func tunnelTproxyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                "tproxy [extra args...]",
		Short:              "System-wide tproxy interception (wraps `ziti tunnel tproxy`, requires CAP_NET_ADMIN)",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return execZitiTunnel("tproxy", args)
		},
	}
	return cmd
}

// execZitiTunnel finds the bundled `ziti` binary and execs it with the given
// subcommand plus our identity. Stdin/stdout/stderr are inherited; signals
// are forwarded so that systemd's SIGTERM cleanly stops the child.
//
// We don't want to syscall.Exec here — keeping the schmutz process in the
// process tree means systemd can supervise it, telemetry hooks see exits,
// and we report the exit code with consistent error wrapping.
func execZitiTunnel(sub string, extraArgs []string) error {
	if enroll.IsControllerNode() {
		return errors.New("schmutz: this machine is a Ziti controller node — schmutz tunnel must not run here")
	}

	zitiBin, err := findZitiBinary()
	if err != nil {
		return err
	}

	r, err := root.LoadRoot(schmutzDir)
	if err != nil {
		return fmt.Errorf("load root: %w", err)
	}
	identityPath, _ := r.IdentityPath()
	if _, err := os.Stat(identityPath); err != nil {
		return fmt.Errorf("identity file missing at %s — run `schmutz enroll` first", identityPath)
	}

	args := []string{"tunnel", sub, "--identity", identityPath}
	args = append(args, extraArgs...)

	c := exec.Command(zitiBin, args...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	if err := c.Start(); err != nil {
		return fmt.Errorf("start ziti tunnel %s: %w", sub, err)
	}

	// Forward SIGTERM/SIGINT to the child so systemd-stop and Ctrl-C work.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case sig := <-sigCh:
				if c.Process != nil {
					_ = c.Process.Signal(sig)
				}
			case <-done:
				return
			}
		}
	}()

	err = c.Wait()
	close(done)
	signal.Stop(sigCh)

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		return fmt.Errorf("ziti tunnel %s: %w", sub, err)
	}
	return nil
}

// findZitiBinary locates the bundled ziti binary.
//   - $SCHMUTZ_ZITI_BIN if set (escape hatch for development)
//   - /usr/local/bin/ziti (default install location)
//   - the same directory as the running schmutz binary, named "ziti"
//   - $PATH lookup as a last resort
func findZitiBinary() (string, error) {
	if env := os.Getenv("SCHMUTZ_ZITI_BIN"); env != "" {
		if _, err := os.Stat(env); err == nil {
			return env, nil
		}
		return "", fmt.Errorf("SCHMUTZ_ZITI_BIN=%s does not exist", env)
	}
	if _, err := os.Stat(defaultZitiPath); err == nil {
		return defaultZitiPath, nil
	}
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "ziti")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	if found, err := exec.LookPath("ziti"); err == nil {
		return found, nil
	}
	return "", fmt.Errorf("ziti binary not found (checked %s, alongside schmutz, and $PATH); set SCHMUTZ_ZITI_BIN to override", defaultZitiPath)
}
