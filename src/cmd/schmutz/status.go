package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/KontangoOSS/schmutz/internal/enroll"
	"github.com/KontangoOSS/schmutz/pkg/schmutz/discovery"
	"github.com/KontangoOSS/schmutz/root"
	"github.com/spf13/cobra"
)

// statusCmd is local-only diagnostics. No network calls, no controller
// dependency. Reports what schmutz knows about itself: version, identity,
// services, last-seen file timestamps, and whether the systemd unit is
// installed and active.
//
// Used at the start of every troubleshooting session — `schmutz status`
// answers "what does this device think is going on?"
func statusCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show local agent state (identity, services, systemd unit)",
		Long: "Reports the agent's view of its own state. Local data only — does not contact the controller. " +
			"Use `--json` for machine-readable output.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s := collectStatus()
			if asJSON {
				out, _ := json.MarshalIndent(s, "", "  ")
				fmt.Println(string(out))
				return nil
			}
			renderStatusText(os.Stdout, s)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

// Status is a snapshot of what the agent knows about itself locally.
type Status struct {
	Version            string   `json:"version"`
	IsControllerNode   bool     `json:"is_controller_node"`
	IsRoot             bool     `json:"is_root"`
	IdentityPath       string   `json:"identity_path"`
	IdentityExists     bool     `json:"identity_exists"`
	IdentityModTime    string   `json:"identity_modtime,omitempty"`
	ManifestPath       string   `json:"manifest_path"`
	ManifestExists     bool     `json:"manifest_exists"`
	ControllerURL      string   `json:"controller_url,omitempty"`
	Slug               string   `json:"slug,omitempty"`
	DeviceID           string   `json:"device_id,omitempty"`
	Services           []string `json:"services,omitempty"`
	SystemdUnitPath    string   `json:"systemd_unit_path"`
	SystemdUnitExists  bool     `json:"systemd_unit_exists"`
	ZitiBin            string   `json:"ziti_bin,omitempty"`
	ZitiBinExists      bool     `json:"ziti_bin_exists"`
	AgentSocketPath    string   `json:"agent_socket_path,omitempty"`
	AgentSocketAlive   bool     `json:"agent_socket_alive"`
	HardwareHash       string   `json:"hardware_hash,omitempty"`
	Class              string   `json:"class,omitempty"`
}

func collectStatus() Status {
	s := Status{
		Version:          Version,
		IsControllerNode: enroll.IsControllerNode(),
		IsRoot:           os.Geteuid() == 0,
		SystemdUnitPath:  systemdUnitPath,
	}
	s.SystemdUnitExists = fileExists(systemdUnitPath)

	if z, err := findZitiBinary(); err == nil {
		s.ZitiBin = z
		s.ZitiBinExists = true
	} else {
		s.ZitiBin = defaultZitiPath
		s.ZitiBinExists = false
	}

	r, err := root.LoadRoot(schmutzDir)
	if err == nil {
		s.IdentityPath, _ = r.IdentityPath()
		s.ManifestPath = filepath.Join(schmutzDir, "manifest.yaml")
		s.ControllerURL = r.ControllerURL()
		s.Slug = r.Slug()
		s.Services = r.Services()
	} else {
		s.IdentityPath = filepath.Join(schmutzDir, "identity.json")
		s.ManifestPath = filepath.Join(schmutzDir, "manifest.yaml")
	}

	if fi, err := os.Stat(s.IdentityPath); err == nil {
		s.IdentityExists = true
		s.IdentityModTime = fi.ModTime().UTC().Format(time.RFC3339)
	}
	s.ManifestExists = fileExists(s.ManifestPath)

	// Agent socket — if start is running, this is bound
	s.AgentSocketPath = filepath.Join(schmutzDir, "agent.sock")
	s.AgentSocketAlive = unixSocketAlive(s.AgentSocketPath)

	// Cheap discovery snapshot — just L0 so this stays fast
	disc := discovery.Probe{Layers: []discovery.Layer{discovery.LayerL0}}.Run(context.Background())
	if v, ok := disc.Get("hardware_hash"); ok {
		if hh, ok := v.(string); ok {
			s.HardwareHash = hh
		}
	}
	if v, ok := disc.Get("class"); ok {
		if c, ok := v.(string); ok {
			s.Class = c
		}
	}

	return s
}

func renderStatusText(w *os.File, s Status) {
	yn := func(b bool) string {
		if b {
			return "yes"
		}
		return "no"
	}
	fmt.Fprintf(w, "schmutz %s\n", s.Version)
	fmt.Fprintf(w, "  controller node: %s\n", yn(s.IsControllerNode))
	if s.IsControllerNode {
		fmt.Fprintln(w, "  WARNING: schmutz must not run on a controller node")
	}
	fmt.Fprintf(w, "  running as root: %s\n", yn(s.IsRoot))
	fmt.Fprintf(w, "  hardware hash: %s\n", strOrDash(s.HardwareHash))
	fmt.Fprintf(w, "  device class: %s\n", strOrDash(s.Class))
	fmt.Fprintln(w)
	fmt.Fprintln(w, "enrollment")
	fmt.Fprintf(w, "  controller url: %s\n", strOrDash(s.ControllerURL))
	fmt.Fprintf(w, "  slug: %s\n", strOrDash(s.Slug))
	fmt.Fprintf(w, "  identity: %s (%s)\n", s.IdentityPath, yn(s.IdentityExists))
	if s.IdentityExists {
		fmt.Fprintf(w, "    written: %s\n", s.IdentityModTime)
	}
	fmt.Fprintf(w, "  manifest: %s (%s)\n", s.ManifestPath, yn(s.ManifestExists))
	if len(s.Services) > 0 {
		fmt.Fprintf(w, "  services: %s\n", strings.Join(s.Services, ", "))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "runtime")
	fmt.Fprintf(w, "  systemd unit: %s (%s)\n", s.SystemdUnitPath, yn(s.SystemdUnitExists))
	fmt.Fprintf(w, "  agent socket: %s (alive=%s)\n", s.AgentSocketPath, yn(s.AgentSocketAlive))
	fmt.Fprintf(w, "  ziti binary: %s (exists=%s)\n", s.ZitiBin, yn(s.ZitiBinExists))
}

func strOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// fingerprintCmd dumps the discovery output as JSON. Used to debug what the
// agent will send to the controller during enrollment.
func fingerprintCmd() *cobra.Command {
	var withL4 bool
	cmd := &cobra.Command{
		Use:   "fingerprint",
		Short: "Print the device discovery payload as JSON",
		Long: "Runs the schmutz discovery probe and prints the result. By default network probes (L4) " +
			"are skipped — pass --l4 to include public IP and geolocation.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			layers := discovery.LocalLayers()
			if withL4 {
				layers = discovery.AllLayers()
			}
			res := discovery.Probe{Layers: layers}.Run(context.Background())
			out, err := json.MarshalIndent(res, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(out))
			return nil
		},
	}
	cmd.Flags().BoolVar(&withL4, "l4", false, "include outbound network probes (public IP, geolocation)")
	return cmd
}

// fileExists is duplicated locally because importing pkg/schmutz/discovery's
// internal helpers would couple the cmd layer to package internals.
func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// unixSocketAlive reports whether the agent's local IPC socket has a live
// listener. Useful to tell "is `schmutz start` actually running right now."
func unixSocketAlive(path string) bool {
	if !fileExists(path) {
		return false
	}
	c, err := net.DialTimeout("unix", path, 200*time.Millisecond)
	if err != nil {
		// "no such file" is filtered above; "connection refused" means a
		// stale socket file with no listener.
		if errors.Is(err, syscallECONNREFUSED) {
			return false
		}
		return false
	}
	_ = c.Close()
	return true
}

// Tiny shim so we don't import syscall just for one constant; net.Dial sets
// these as syscall errors but errors.Is on a non-nil net.OpError works
// against the unwrapped syscall error.
//
// We accept the small false-negative risk: if it's not "connection refused"
// it's reported as not alive anyway, which matches operator expectations.
var syscallECONNREFUSED = errFromConnRefused()

func errFromConnRefused() error {
	c, err := net.DialTimeout("unix", "/nonexistent/sock-for-comparison", 1*time.Millisecond)
	if c != nil {
		_ = c.Close()
	}
	return err
}
