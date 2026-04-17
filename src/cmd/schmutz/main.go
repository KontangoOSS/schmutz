package main

import (
	"flag"
	"fmt"
	"os"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "signup":
		cmdSignup(os.Args[2:])
	case "join":
		cmdJoin(os.Args[2:])
	case "enroll":
		cmdJoin(os.Args[2:]) // same flow — controller distinguishes by fingerprint
	case "run":
		cmdRun(os.Args[2:])
	case "agent":
		cmdAgent(os.Args[2:])
	case "bootstrap":
		cmdBootstrap(os.Args[2:])
	case "version":
		fmt.Println("schmutz", version)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`schmutz — zero-trust overlay agent

Account:
  signup       Create a platform account (Zitadel + Bao + device namespace)
               NOT YET IMPLEMENTED — endpoint does not exist on controller

Device:
  join         Enroll a new device into the overlay (first time, telemetry window)
  enroll       Re-enroll a known device (skips telemetry window, issues new JWT)

Services:
  agent        Run the machine agent (heartbeat, config listener, dark services)
  run          Run the edge gateway (SNI classifier + Ziti relay)
  bootstrap    Re-run post-approval setup (DNS, tunnel, systemd units)

Usage:
  schmutz signup --url https://ctrl.konoss.org --email user@example.com
  schmutz join   https://ctrl.konoss.org --slug my-device
  schmutz enroll https://ctrl.konoss.org --slug my-device
  schmutz agent  [--identity /opt/tango/identity.json]`)
}

func cmdRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	configPath := fs.String("config", "/opt/schmutz/config.yaml", "config file path")
	fs.Parse(args)
	startGateway(*configPath)
}
