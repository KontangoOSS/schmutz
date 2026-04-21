package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/KontangoOSS/schmutz/agent"
	"github.com/KontangoOSS/schmutz/internal/enroll"
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
			identityPath, _ := r.IdentityPath()
			if !enroll.NeedsEnrollment(identityPath) {
				log.Println("schmutz: already enrolled")
				return nil
			}
			hostname, _ := os.Hostname()
			log.Printf("schmutz: registering with %s", r.ControllerURL())
			jwt, slug, err := enroll.Register(cmd.Context(), r.ControllerURL(), hostname, runtime.GOOS, runtime.GOARCH, "unknown", nil)
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

			identityPath, _ := r.IdentityPath()
			if enroll.NeedsEnrollment(identityPath) {
				hostname, _ := os.Hostname()
				log.Printf("schmutz: not enrolled, registering with %s", r.ControllerURL())
				jwt, slug, err := enroll.Register(cmd.Context(), r.ControllerURL(), hostname, runtime.GOOS, runtime.GOARCH, "unknown", nil)
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
