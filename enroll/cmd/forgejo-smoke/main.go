// forgejo-smoke is a one-shot connectivity check for the Forgejo client
// against a real Forgejo instance.
//
// Run:
//
//	FORGEJO_URL=http://10.11.30.30:3000 \
//	FORGEJO_TOKEN=<token> \
//	FORGEJO_ORG=public \
//	./forgejo-smoke
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/KontangoOSS/schmutz/enroll/internal/forgejo"
)

func main() {
	base := os.Getenv("FORGEJO_URL")
	token := os.Getenv("FORGEJO_TOKEN")
	org := os.Getenv("FORGEJO_ORG")
	if base == "" || token == "" || org == "" {
		fmt.Fprintln(os.Stderr, "FORGEJO_URL + FORGEJO_TOKEN + FORGEJO_ORG required")
		os.Exit(2)
	}
	cli := forgejo.NewClient(base, org, token, 0)
	ctx := context.Background()

	apps, err := cli.ListApps(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ListApps: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("OK ListApps: %d active apps\n", len(apps))
	for _, a := range apps {
		fmt.Printf("  - %s (%s)\n", a.AppID, a.DisplayName)
	}

	if len(apps) == 0 {
		fmt.Println("no active apps found — seed blueprint.yaml files first")
		os.Exit(0)
	}

	firstApp := apps[0].AppID
	bp, err := cli.GetTango(ctx, firstApp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "GetBlueprint(%s): %v\n", firstApp, err)
		os.Exit(1)
	}
	fmt.Printf("OK GetBlueprint: %s — %s\n", bp.AppID, bp.Identity.DisplayName)
	if err := bp.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "blueprint Validate: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("OK blueprint.Validate: passed")
	fmt.Println("ALL CHECKS PASSED")
}
