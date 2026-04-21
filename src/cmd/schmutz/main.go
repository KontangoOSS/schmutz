package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/KontangoOSS/schmutz/agent"
	"github.com/KontangoOSS/schmutz/internal/enroll"
	"github.com/KontangoOSS/schmutz/internal/join"
	"github.com/KontangoOSS/schmutz/root"
	"github.com/openziti/sdk-golang/ziti"
	"github.com/spf13/cobra"
)

const version = "0.2.0"
const schmutzDir = "/etc/schmutz"

func main() {
	rootCmd := &cobra.Command{Use: "schmutz", Short: "Schmutz — TangoKore device agent"}
	rootCmd.AddCommand(enrollCmd(), startCmd(), serveCmd(), versionCmd())
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func enrollCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enroll",
		Short: "Register this device and enroll its Ziti identity",
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, err := root.LoadRoot(schmutzDir)
			if err != nil {
				return err
			}
			if err := r.Validate(); err != nil {
				return err
			}
			identityPath, _ := r.IdentityPath()
			if !enroll.NeedsEnrollment(identityPath) {
				log.Println("schmutz: already enrolled")
				return nil
			}
			info := collectDeviceInfo()
			log.Printf("schmutz: registering with %s (fingerprint=%s platform=%s)", r.ControllerURL(), info.Fingerprint, info.Platform)
			jwt, slug, err := enroll.Register(cmd.Context(), r.ControllerURL(), info)
			if err != nil {
				return fmt.Errorf("register: %w", err)
			}
			log.Printf("schmutz: approved as %q", slug)
			if err := enroll.EnrollJWT(jwt, identityPath); err != nil {
				return fmt.Errorf("enroll JWT: %w", err)
			}
			log.Printf("schmutz: identity written to %s", identityPath)
			return nil
		},
	}
}

func startCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the Schmutz agent",
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, err := root.LoadRoot(schmutzDir)
			if err != nil {
				return err
			}
			if err := r.Validate(); err != nil {
				return err
			}

			identityPath, _ := r.IdentityPath()
			if enroll.NeedsEnrollment(identityPath) {
				info := collectDeviceInfo()
				log.Printf("schmutz: not enrolled, registering with %s (fingerprint=%s platform=%s)", r.ControllerURL(), info.Fingerprint, info.Platform)
				jwt, slug, err := enroll.Register(cmd.Context(), r.ControllerURL(), info)
				if err != nil {
					return fmt.Errorf("register: %w", err)
				}
				log.Printf("schmutz: approved as %q, enrolling", slug)
				if err := enroll.EnrollJWT(jwt, identityPath); err != nil {
					return fmt.Errorf("enroll JWT: %w", err)
				}
			}

			a, err := agent.NewAgent(agent.DefaultConfig(), r)
			if err != nil {
				return err
			}

			deviceID := r.DeviceID()
			defaultServices := []*agent.ServiceRequest{
				{Name: "ssh." + deviceID + ".tango", LocalAddr: "localhost:22", BackendMode: "tcpTunnel"},
				{Name: "nats." + deviceID + ".tango", LocalAddr: "localhost:4222", BackendMode: "tcpTunnel"},
			}

			c := make(chan os.Signal, 1)
			signal.Notify(c, os.Interrupt, syscall.SIGTERM)
			go func() {
				<-c
				a.Shutdown()
				os.Exit(0)
			}()

			go func() {
				for _, svc := range defaultServices {
					if err := a.StartService(svc); err != nil {
						log.Printf("schmutz: start service %q: %v", svc.Name, err)
					}
				}
			}()

			return a.Run()
		},
	}
}

func serveCmd() *cobra.Command {
	var identityPath, serviceName, localAddr string
	cmd := &cobra.Command{
		Use:    "serve",
		Short:  "Bind a single service (subordinate mode, called by agent)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := ziti.NewConfigFromFile(identityPath)
			if err != nil {
				return fmt.Errorf("serve: load identity: %w", err)
			}
			ztx, err := ziti.NewContext(cfg)
			if err != nil {
				return fmt.Errorf("serve: ziti context: %w", err)
			}
			ln, err := ztx.Listen(serviceName)
			if err != nil {
				return fmt.Errorf("serve: listen %q: %w", serviceName, err)
			}
			log.Printf("serve: %s → %s", serviceName, localAddr)
			fmt.Printf("{\"msg\":\"boot\",\"service\":\"%s\"}\n", serviceName)

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()
			go func() { <-ctx.Done(); ln.Close() }()

			for {
				conn, err := ln.Accept()
				if err != nil {
					return nil
				}
				go proxyConn(conn, localAddr)
			}
		},
	}
	cmd.Flags().StringVar(&identityPath, "identity", "", "path to identity.json")
	cmd.Flags().StringVar(&serviceName, "service", "", "Ziti service name to bind")
	cmd.Flags().StringVar(&localAddr, "local-addr", "", "local address to proxy to")
	return cmd
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run:   func(_ *cobra.Command, _ []string) { fmt.Println(version) },
	}
}

// collectDeviceInfo gathers the machine fingerprint and platform for enrollment.
// Uses the existing join.Collect() which reads /etc/machine-id, MACs, etc.
func collectDeviceInfo() enroll.DeviceInfo {
	fp, err := join.Collect()
	if err != nil {
		log.Printf("schmutz: fingerprint collection failed: %v (proceeding with partial data)", err)
		hostname, _ := os.Hostname()
		return enroll.DeviceInfo{
			Hostname: hostname,
			OS:       "linux",
			Arch:     "amd64",
		}
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

// detectPlatform returns the execution environment type.
// Mirrors detect.detectPlatform but as a standalone function to avoid
// importing the pipeline-based detect package from main.
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
		case strings.Contains(name, "droplet"), strings.Contains(name, "google compute"), strings.Contains(name, "amazon ec2"):
			return "cloud"
		case strings.Contains(name, "virtualbox"), strings.Contains(name, "vmware"), strings.Contains(name, "kvm"), strings.Contains(name, "qemu"):
			return "vm"
		}
	}
	return "baremetal"
}

func proxyConn(zitiConn net.Conn, localAddr string) {
	defer zitiConn.Close()
	local, err := net.Dial("tcp", localAddr)
	if err != nil {
		log.Printf("proxy: dial %s: %v", localAddr, err)
		return
	}
	defer local.Close()
	done := make(chan struct{}, 2)
	go func() { copyAndClose(local, zitiConn, done) }()
	go func() { copyAndClose(zitiConn, local, done) }()
	<-done
	<-done // wait for both halves
}

func copyAndClose(dst, src net.Conn, done chan struct{}) {
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			dst.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	done <- struct{}{}
}
