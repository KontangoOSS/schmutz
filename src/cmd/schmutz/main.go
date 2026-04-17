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
	case "run":
		cmdRun(os.Args[2:])
	case "agent":
		cmdAgent(os.Args[2:])
	case "join":
		cmdJoin(os.Args[2:])
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
	fmt.Println(`schmutz — zero-trust edge firewall + universal machine agent

Commands:
  run          Start the edge gateway (SNI classifier + Ziti relay)
  agent        Start the universal machine agent (heartbeat + config listener)
  version      Print version

Usage:
  schmutz run --config /opt/schmutz/config.yaml
  schmutz join https://your-controller.example [--session=TOKEN] [--role-id=ID --secret-id=ID]
  schmutz agent [--identity /opt/tango/identity.json] [--fallback https://your-controller.example]
  schmutz bootstrap --controller https://ctrl.konoss.org [--identity /opt/ziti/identity.json] [--ziti /usr/local/bin/ziti] [--dry-run]`)
}

func cmdRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	configPath := fs.String("config", "/opt/schmutz/config.yaml", "config file path")
	fs.Parse(args)
	startGateway(*configPath)
}
