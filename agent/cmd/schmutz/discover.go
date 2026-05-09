package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"git.konoss.org/kore/schmutz/agent/internal/discover"
	"git.konoss.org/kore/schmutz/agent/root"
	"github.com/spf13/cobra"
)

func discoverCmd() *cobra.Command {
	var jsonOut bool
	var ports []int
	var sourceDir string

	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Scan localhost and discover running services",
		Long: `Scans common localhost ports, fingerprints each service,
probes for existing OpenAPI specs, and generates a schema document.

Results are printed to stdout (--json for machine-readable).
Also writes /etc/schmutz/schema.json and publishes to Bao if configured.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := root.LoadRoot("/etc/schmutz")
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			scanPorts := discover.CommonPorts
			if len(ports) > 0 {
				scanPorts = make([]uint16, len(ports))
				for i, p := range ports {
					scanPorts[i] = uint16(p)
				}
			}

			fmt.Fprintf(os.Stderr, "Scanning %d ports on localhost...\n", len(scanPorts))
			start := time.Now()

			targets, err := discover.ScanLocalhost(scanPorts)
			if err != nil {
				return fmt.Errorf("scan: %w", err)
			}

			fmt.Fprintf(os.Stderr, "Found %d open services in %s\n",
				len(targets), time.Since(start).Round(time.Millisecond))

			result := discover.BuildResult(targets, r.Slug())

			if sourceDir != "" {
				fmt.Fprintf(os.Stderr, "Analyzing source at %s...\n", sourceDir)
				routes, err := discover.ExtractGoRoutes(sourceDir)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: source analysis error: %v\n", err)
				} else if len(routes) > 0 {
					fmt.Fprintf(os.Stderr, "Found %d route hints from source\n", len(routes))
				}
			}

			pub := discover.NewPublisher(
				"/etc/schmutz",
				os.Getenv("BAO_ADDR"),
				os.Getenv("BAO_TOKEN"),
			)
			if err := pub.Publish(result); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: publish error: %v\n", err)
			}

			if jsonOut {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			fmt.Printf("\nDiscovered services on %s:\n\n", r.Slug())
			if len(result.Services) == 0 {
				fmt.Println("  (none found)")
				return nil
			}
			for _, svc := range result.Services {
				source := "stub"
				if svc.Source == "openapi" {
					source = "OpenAPI spec found ✓"
				}
				fmt.Printf("  :%d  %-12s  [%s]\n", svc.Port, svc.Protocol, source)
			}
			fmt.Printf("\nSchema written to /etc/schmutz/schema.json\n")
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output JSON")
	cmd.Flags().IntSliceVar(&ports, "ports", nil, "Ports to scan (default: common ports)")
	cmd.Flags().StringVar(&sourceDir, "source", "", "Source directory to analyze with tree-sitter (optional enrichment)")
	return cmd
}
