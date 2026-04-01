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
	fmt.Println(`schmutz — zero-trust edge firewall

Commands:
  run          Start the edge gateway (SNI classifier + Ziti relay)
  version      Print version

Usage:
  schmutz run --config /opt/schmutz/config.yaml`)
}

func cmdRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	configPath := fs.String("config", "/opt/schmutz/config.yaml", "config file path")
	fs.Parse(args)
	startGateway(*configPath)
}
