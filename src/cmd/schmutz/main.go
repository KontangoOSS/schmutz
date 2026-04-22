package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/KontangoOSS/schmutz/agent"
	"github.com/KontangoOSS/schmutz/internal/enroll"
	"github.com/KontangoOSS/schmutz/internal/join"
	"github.com/KontangoOSS/schmutz/root"
	"github.com/spf13/cobra"
)

const version = "0.3.0"
const schmutzDir = "/etc/schmutz"

func main() {
	rootCmd := &cobra.Command{Use: "schmutz", Short: "Schmutz — TangoKore device agent"}
	rootCmd.AddCommand(enrollCmd(), startCmd(), versionCmd())
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func enrollCmd() *cobra.Command {
	var controllerURL string
	var force bool
	var profile string
	cmd := &cobra.Command{
		Use:   "enroll",
		Short: "Register this device and enroll its Ziti identity",
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, err := root.LoadRoot(schmutzDir)
			if err != nil {
				return err
			}
			if controllerURL != "" && r.ControllerURL() != controllerURL {
				if err := os.MkdirAll(schmutzDir, 0755); err != nil {
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
			log.Printf("schmutz: registering with %s (fingerprint=%s platform=%s)",
				r.ControllerURL(), info.Fingerprint, info.Platform)
			result, err := enroll.Register(cmd.Context(), r.ControllerURL(), info)
			if err != nil {
				return fmt.Errorf("register: %w", err)
			}
			if err := enroll.WriteIdentity(result.IdentityJSON, identityPath); err != nil {
				return fmt.Errorf("write identity: %w", err)
			}
			if err := r.SetSlug(result.Slug); err != nil {
				log.Printf("schmutz: warn: could not persist slug: %v", err)
			}
			if err := r.SetServices(result.Services); err != nil {
				log.Printf("schmutz: warn: could not persist services: %v", err)
			}
			log.Printf("schmutz: enrolled as %q (status=%s services=%v)",
				result.Slug, result.Status, result.Services)
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
		"TangoKore controller URL (required on first install, e.g. https://ctrl.konoss.org)")
	cmd.Flags().BoolVar(&force, "force", false, "re-enroll even if identity already exists")
	cmd.Flags().StringVar(&profile, "profile", "", "device profile (e.g. edge-router, application, laptop, cellphone)")
	return cmd
}

func startCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the Schmutz agent — bind overlay services",
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, err := root.LoadRoot(schmutzDir)
			if err != nil {
				return err
			}
			if err := r.Validate(); err != nil {
				return err
			}
			identityPath, _ := r.IdentityPath()

			if err := enroll.CheckIdentityCA(identityPath); err != nil {
				return fmt.Errorf("identity CA check failed: %w\n  Fix: run 'schmutz enroll --force'", err)
			}

			// Auto-enroll if identity missing.
			var enrolledServices []string
			if enroll.NeedsEnrollment(identityPath) {
				info := collectDeviceInfo()
				log.Printf("schmutz: not enrolled — registering with %s", r.ControllerURL())
				result, err := enroll.Register(cmd.Context(), r.ControllerURL(), info)
				if err != nil {
					return fmt.Errorf("register: %w", err)
				}
				if err := enroll.WriteIdentity(result.IdentityJSON, identityPath); err != nil {
					return fmt.Errorf("write identity: %w", err)
				}
				if err := r.SetSlug(result.Slug); err != nil {
					log.Printf("schmutz: warn: could not persist slug: %v", err)
				}
				if err := r.SetServices(result.Services); err != nil {
					log.Printf("schmutz: warn: could not persist services: %v", err)
				}
				enrolledServices = result.Services
				log.Printf("schmutz: enrolled as %q (status=%s)", result.Slug, result.Status)
				r, err = root.LoadRoot(schmutzDir)
				if err != nil {
					return err
				}
			}

			slug := r.Slug()
			if slug == "" {
				return fmt.Errorf("schmutz: no slug — run 'schmutz enroll' first")
			}

			// Use services from enrollment result if available, else from manifest.
			services := enrolledServices
			if len(services) == 0 {
				services = r.Services()
			}
			if len(services) == 0 {
				return fmt.Errorf("schmutz: no services to bind — re-run 'schmutz enroll' to refresh service list")
			}

			a, err := agent.NewAgent(agent.DefaultConfig(), r)
			if err != nil {
				return err
			}

			req := &agent.ServiceRequest{
				Name:     "host-" + slug,
				Services: services,
			}

			c := make(chan os.Signal, 1)
			signal.Notify(c, os.Interrupt, syscall.SIGTERM)
			go func() {
				<-c
				a.Shutdown()
				os.Exit(0)
			}()

			log.Printf("schmutz: starting — slug=%s services=%v", slug, services)
			go func() {
				if err := a.StartService(req); err != nil {
					log.Printf("schmutz: bind services: %v", err)
				} else {
					log.Printf("schmutz: overlay services bound")
				}
			}()

			return a.Run()
		},
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run:   func(_ *cobra.Command, _ []string) { fmt.Println(version) },
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
