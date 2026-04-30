package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/KontangoOSS/schmutz/agent"
	"github.com/KontangoOSS/schmutz/internal/discover"
	"github.com/KontangoOSS/schmutz/internal/enroll"
	"github.com/KontangoOSS/schmutz/internal/join"
	"github.com/KontangoOSS/schmutz/internal/telemetry"
	"github.com/KontangoOSS/schmutz/root"
	"github.com/spf13/cobra"
)

// Version is set at link time by GoReleaser via -ldflags "-X main.Version=..."
// Defaults to a dev string when built directly with `go build`.
var Version = "0.3.1-dev"

const (
	schmutzDir       = "/etc/schmutz"
	defaultBinPath   = "/usr/local/bin/schmutz"
	defaultZitiPath  = "/usr/local/bin/ziti"
	systemdUnitPath  = "/etc/systemd/system/schmutz.service"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "schmutz",
		Short: "Schmutz — TangoKore device agent",
		Long:  "Schmutz is a thin opinionated wrapper around the Ziti tunnel CLI plus device discovery, enrollment, and lifecycle management for TangoKore.",
	}
	rootCmd.AddCommand(
		enrollCmd(),
		startCmd(),
		tunnelCmd(),
		statusCmd(),
		fingerprintCmd(),
		installServiceCmd(),
		uninstallCmd(),
		updateCmd(),
		versionCmd(),
	)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func enrollCmd() *cobra.Command {
	var controllerURL string
	var force bool
	var profile string
	var yes bool
	var slug string
	var invitee string
	var lat float64
	var long float64
	cmd := &cobra.Command{
		Use:   "enroll",
		Short: "Register this device and enroll its Ziti identity",
		Long: `Enroll this machine into the Kontango zero-trust network.

This command must run as root — full hardware fingerprinting requires privileged
access to disk serials, SSH host keys, and DMI data. Without root the identity
fingerprint is too weak to survive re-enrollment verification.

A private key is generated ON THIS MACHINE and never transmitted. Only a
certificate signing request (CSR) is sent to the controller. The Ziti CA signs
the CSR and returns only the certificate — your private key stays here.

By enrolling you accept the Kontango Terms of Service: https://kontango.net/terms`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if enroll.IsControllerNode() {
				return fmt.Errorf("schmutz: this machine is a Ziti controller node — schmutz agent must not run here")
			}

			// T&C gate — must be accepted before any network call is made.
			// --yes bypasses the interactive prompt (for PXE/hook/scripted installs).
			termsAccepted := yes
			if !termsAccepted {
				termsAccepted = promptTerms()
			}
			if !termsAccepted {
				return fmt.Errorf("schmutz: enrollment cancelled — terms of service not accepted")
			}

			r, err := root.LoadRoot(schmutzDir)
			if err != nil {
				return err
			}
			if controllerURL != "" && r.ControllerURL() != controllerURL {
				if err := os.MkdirAll(schmutzDir, 0700); err != nil {
					return fmt.Errorf("mkdir %s: %w", schmutzDir, err)
				}
				if err := r.SetControllerURL(controllerURL); err != nil {
					return fmt.Errorf("save controller_url: %w", err)
				}
				r, err = root.LoadRoot(schmutzDir)
				if err != nil {
					return err
				}
			}
			if err := r.Validate(); err != nil {
				return err
			}
			identityPath, _ := r.IdentityPath()
			if !force && !enroll.NeedsEnrollment(identityPath) {
				log.Println("schmutz: already enrolled — use --force to re-enroll")
				return nil
			}
			if force {
				os.Remove(identityPath)
			}

			info := collectDeviceInfo()
			info.Profile = profile

			log.Printf("schmutz: enrolling with %s (fingerprint=%s platform=%s)",
				r.ControllerURL(), info.Fingerprint, info.Platform)

			// RegisterWS generates the key pair here, POSTs /api/v1/start (port 443,
			// Caddy-fronted, impossible to port-block), upgrades to wss:// for the
			// enrollment WS, then injects the local private key into the returned JSON.
			result, err := enroll.RegisterWS(cmd.Context(), enroll.WSEnrollConfig{
				ControllerURL: r.ControllerURL(),
				IdentityPath:  identityPath,
				Info:          info,
				TermsAccepted: true, // validated above
				AgentVersion:  Version,
				Profile:       profile,
				Tags:          map[string]string{"install_mode": "interactive"},
				Slug:          slug,
				Invitee:       invitee,
				Lat:           lat,
				Long:          long,
			})
			if err != nil {
				return fmt.Errorf("enroll: %w", err)
			}

			// Use the slug from the --slug flag; the WS flow doesn't return one.
			effectiveSlug := slug
			if effectiveSlug == "" {
				effectiveSlug = result.Slug
			}
			if err := r.SetSlug(effectiveSlug); err != nil {
				log.Printf("schmutz: warn: could not persist slug: %v", err)
			}
			if err := r.SetServices(result.Services); err != nil {
				log.Printf("schmutz: warn: could not persist services: %v", err)
			}
			log.Printf("schmutz: enrolled (status=%s)", result.Status)
			log.Printf("schmutz: identity written to %s", identityPath)
			if result.Status == "quarantine" {
				log.Printf("schmutz: pending operator approval — run 'schmutz start' now; access expands when approved")
			} else {
				log.Printf("schmutz: run 'schmutz start' to bind services")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&controllerURL, "controller", "",
		"TangoKore controller URL (required on first install, e.g. https://join.kontango.net)")
	cmd.Flags().BoolVar(&force, "force", false, "re-enroll even if identity already exists")
	cmd.Flags().StringVar(&profile, "profile", "server", "device profile (e.g. server, laptop, workstation)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "accept Terms of Service non-interactively (required for PXE/hook/scripted installs)")
	cmd.Flags().StringVar(&slug, "slug", "", "human-readable name for this machine (e.g. web-1, gpu-box) — stored permanently in the Ziti identity")
	cmd.Flags().StringVar(&invitee, "invitee", "", "who invited this machine — email or username of the operator vouching for it")
	cmd.Flags().Float64Var(&lat, "lat", 0, "approximate latitude at enroll time (optional, stored in identity)")
	cmd.Flags().Float64Var(&long, "long", 0, "approximate longitude at enroll time (optional, stored in identity)")
	return cmd
}

// promptTerms prints the T&C summary and returns true if the user types 'y' or 'yes'.
func promptTerms() bool {
	fmt.Println()
	fmt.Println("Kontango Terms of Service: https://kontango.net/terms")
	fmt.Println()
	fmt.Println("By enrolling this machine you agree to the Terms of Service.")
	fmt.Println("A private key will be generated on this machine and will never be transmitted.")
	fmt.Println("Diagnostic connection data (IP, TLS fingerprint) will be collected.")
	fmt.Println()
	fmt.Print("Accept and continue? [y/N]: ")

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return false
	}
	answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
	return answer == "y" || answer == "yes"
}

func startCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the Schmutz agent — connect to overlay and bind services",
		Long: `Start the Schmutz agent.

Connects to the Kontango zero-trust overlay using the enrolled Ziti identity.
The machine appears on the overlay immediately. Services (SSH, etc.) are pushed
by the controller after operator approval — no restart needed when approved.

If no identity exists yet, auto-enrolls using the controller URL from the manifest.
T&C is considered accepted at install time when running via systemd.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if enroll.IsControllerNode() {
				return fmt.Errorf("schmutz: this machine is a Ziti controller node — schmutz agent must not run here")
			}
			r, err := root.LoadRoot(schmutzDir)
			if err != nil {
				return err
			}
			if err := r.Validate(); err != nil {
				return err
			}
			identityPath, _ := r.IdentityPath()

			// Auto-enroll if identity missing.
			// Running via systemd means T&C was accepted at 'schmutz enroll' time.
			if enroll.NeedsEnrollment(identityPath) {
				info := collectDeviceInfo()
				log.Printf("schmutz: not enrolled — registering with %s", r.ControllerURL())
				result, err := enroll.RegisterWS(cmd.Context(), enroll.WSEnrollConfig{
					ControllerURL: r.ControllerURL(),
					IdentityPath:  identityPath,
					Info:          info,
					TermsAccepted: true,
					AgentVersion:  Version,
					Tags:          map[string]string{"install_mode": "auto"},
				})
				if err != nil {
					return fmt.Errorf("register: %w", err)
				}
				// Slug from auto-enroll uses the device_id already set in the manifest.
				if result.Slug != "" {
					if err := r.SetSlug(result.Slug); err != nil {
						log.Printf("schmutz: warn: could not persist slug: %v", err)
					}
				}
				log.Printf("schmutz: enrolled (status=%s)", result.Status)
				r, err = root.LoadRoot(schmutzDir)
				if err != nil {
					return err
				}
			}

			if err := enroll.CheckIdentityCA(identityPath); err != nil {
				return fmt.Errorf("identity CA check failed: %w\n  Fix: run 'schmutz enroll --force'", err)
			}

			// slug is informational — used for logging and service naming.
			// It is NOT required to start. A machine in blue-demo has no services
			// yet; they arrive via config.tango after operator approval.
			slug := r.Slug()
			if slug == "" {
				// Fall back to device_id (set by --slug at enroll time).
				slug = r.DeviceID()
			}
			if slug == "" {
				slug = "unknown"
			}

			a, err := agent.NewAgent(agent.DefaultConfig(), r)
			if err != nil {
				return err
			}

			c := make(chan os.Signal, 1)
			signal.Notify(c, os.Interrupt, syscall.SIGTERM)
			go func() {
				<-c
				a.Shutdown()
				os.Exit(0)
			}()

			log.Printf("schmutz: starting — slug=%s identity=%s", slug, identityPath)

			// Start telemetry stream over Ziti.
			tel := telemetry.NewDialer(identityPath, 30*time.Second)
			go tel.Run()
			defer tel.Stop()

			// Start service discovery in background — non-intrusive, periodic
			disc := discover.NewDiscoverer(
				filepath.Dir(identityPath), // /etc/schmutz
				os.Getenv("BAO_ADDR"),
				os.Getenv("BAO_TOKEN"),
			).WithSlug(r.Slug())
			go disc.Run()
			defer disc.Stop()

			// Bind any services already provisioned in the registry (re-start after
			// approval). New machines start with zero services — they bind from
			// config.tango pushes once the operator approves.
			services := r.Services()
			if len(services) > 0 {
				req := &agent.ServiceRequest{
					Name:     "host-" + slug,
					Services: services,
				}
					go func() {
					if err := a.StartService(req); err != nil {
						log.Printf("schmutz: bind services: %v", err)
					} else {
						log.Printf("schmutz: overlay services bound: %v", services)
					}
				}()
			} else {
				log.Printf("schmutz: no services yet — waiting for operator approval")
			}

			return a.Run()
		},
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run:   func(_ *cobra.Command, _ []string) { fmt.Println(Version) },
	}
}

func collectDeviceInfo() enroll.DeviceInfo {
	fp, err := join.Collect()
	if err != nil {
		log.Printf("schmutz: fingerprint collection failed: %v (proceeding with partial data)", err)
		hostname, _ := os.Hostname()
		return enroll.DeviceInfo{Hostname: hostname, OS: "linux", Arch: "amd64"}
	}
	return enroll.DeviceInfo{
		Hostname:    fp.Hostname,
		OS:          fp.OS,
		Arch:        fp.Arch,
		Platform:    detectPlatform(),
		MachineID:   fp.MachineID,
		Fingerprint: fp.HardwareHash,
	}
}

func detectPlatform() string {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return "docker"
	}
	if data, _ := os.ReadFile("/proc/1/environ"); len(data) > 0 {
		for _, e := range strings.Split(string(data), "\x00") {
			if e == "container=lxc" {
				return "lxc"
			}
		}
	}
	if data, _ := os.ReadFile("/sys/class/dmi/id/product_name"); len(data) > 0 {
		name := strings.ToLower(strings.TrimSpace(string(data)))
		switch {
		case strings.Contains(name, "droplet"), strings.Contains(name, "google compute"),
			strings.Contains(name, "amazon ec2"):
			return "cloud"
		case strings.Contains(name, "virtualbox"), strings.Contains(name, "vmware"),
			strings.Contains(name, "kvm"), strings.Contains(name, "qemu"):
			return "vm"
		}
	}
	return "baremetal"
}
