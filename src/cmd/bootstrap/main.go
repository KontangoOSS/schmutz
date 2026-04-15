// Package main is the entry point for the schmutz bootstrap binary.
// It builds a 6-step pipeline (detect → preflight → escalate → deps →
// register → service) and drives it via a Cobra CLI.
package main

import (
	"fmt"
	"os"

	"github.com/pterm/pterm"
	"github.com/spf13/cobra"

	"github.com/KontangoOSS/schmutz/internal/deps"
	"github.com/KontangoOSS/schmutz/internal/detect"
	"github.com/KontangoOSS/schmutz/internal/escalate"
	"github.com/KontangoOSS/schmutz/internal/pipeline"
	"github.com/KontangoOSS/schmutz/internal/preflight"
	"github.com/KontangoOSS/schmutz/internal/register"
	"github.com/KontangoOSS/schmutz/internal/service"
)

var version = "dev"

// preflightAdapter wraps preflight.Step to satisfy pipeline.Step.
// preflight.Step.Skip takes no arguments; the pipeline interface requires
// Skip(*pipeline.Context).
type preflightAdapter struct {
	inner *preflight.Step
}

func (a *preflightAdapter) Name() string                       { return a.inner.Name() }
func (a *preflightAdapter) Skip(_ *pipeline.Context) bool      { return a.inner.Skip() }
func (a *preflightAdapter) Run(_ *pipeline.Context) error      { return a.inner.Run() }

func main() {
	var (
		domain      string
		force       bool
		abuseKey    string
		domainSet   bool
	)

	root := &cobra.Command{
		Use:     "schmutz",
		Short:   "Bootstrap a device into the Kontango mesh",
		Version: version,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd, domain, domainSet, force, abuseKey)
		},
	}

	// Track whether --domain was explicitly provided by the user.
	root.Flags().StringVarP(&domain, "domain", "d", "", "Controller to join (default: ctrl.konoss.org)")
	root.Flags().BoolVarP(&force, "force", "f", false, "Force re-registration")
	root.Flags().StringVar(&abuseKey, "abuseipdb-key", "", "AbuseIPDB API key (optional)")

	// Use PersistentPreRunE to detect if --domain was changed by the user.
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		domainSet = cmd.Flags().Changed("domain")
		return nil
	}

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, domain string, domainSet bool, force bool, abuseKey string) error {
	ctx := pipeline.NewContext()

	// Mode detection: if --domain was not set by the user, enter interactive mode.
	if !domainSet {
		ctx.Interactive = true
		input, err := pterm.DefaultInteractiveTextInput.
			WithDefaultValue("ctrl.konoss.org").
			Show("Enter controller domain")
		if err != nil {
			return fmt.Errorf("interactive prompt failed: %w", err)
		}
		if input == "" {
			input = "ctrl.konoss.org"
		}
		ctx.Domain = input
	} else {
		ctx.Domain = domain
		if ctx.Domain == "" {
			ctx.Domain = "ctrl.konoss.org"
		}
	}

	// --force clears registered state so all steps run fresh.
	if force {
		ctx.Identity = ""
		ctx.Registered = false
	}

	// Build the 6-step pipeline.
	steps := []pipeline.Step{
		detect.New(),
		&preflightAdapter{inner: preflight.New(abuseKey)},
		escalate.New(),
		deps.New(),
		register.New(),
		service.New(),
	}

	p := pipeline.New(steps)
	if err := p.Run(ctx); err != nil {
		return err
	}

	pterm.Success.Printf("device %q successfully bootstrapped into %s\n", ctx.Hostname, ctx.Domain)
	return nil
}
