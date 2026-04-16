// Package main is the entry point for the schmutz-agent CLI.
package main

import (
	"fmt"
	"os"

	"github.com/KontangoOSS/schmutz/internal/agent"
	"github.com/KontangoOSS/schmutz/internal/detect"
	"github.com/KontangoOSS/schmutz/internal/pipeline"
	"github.com/KontangoOSS/schmutz/internal/preflight"
	"github.com/KontangoOSS/schmutz/internal/register"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
)

var version = "dev"

// preflightStep wraps preflight.Step so it satisfies the pipeline.Step interface.
// preflight has no context dependency for Skip/Run, so the wrapper ignores ctx.
type preflightStep struct {
	inner *preflight.Step
}

func (p *preflightStep) Name() string                    { return p.inner.Name() }
func (p *preflightStep) Skip(_ *pipeline.Context) bool   { return p.inner.Skip() }
func (p *preflightStep) Run(_ *pipeline.Context) error   { return p.inner.Run() }

func main() {
	root := &cobra.Command{
		Use:     "schmutz",
		Short:   "schmutz — L4 zero-trust edge firewall + enrollment client",
		Version: version,
	}

	root.AddCommand(enrollCmd())
	root.AddCommand(agentCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// enrollCmd returns the "schmutz enroll" command.
func enrollCmd() *cobra.Command {
	var domain string
	var abuseKey string

	cmd := &cobra.Command{
		Use:   "enroll",
		Short: "Enroll this device with a schmutz controller",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Detect interactive mode: no --domain flag supplied.
			interactive := !cmd.Flags().Changed("domain")
			if interactive {
				pterm.DefaultHeader.Println("schmutz enrollment")
				result, err := pterm.DefaultInteractiveTextInput.
					WithDefaultText("ctrl.konoss.org").
					Show("Controller domain")
				if err != nil {
					return fmt.Errorf("prompt failed: %w", err)
				}
				if result != "" {
					domain = result
				}
				if domain == "" {
					domain = "ctrl.konoss.org"
				}
			}

			ctx := pipeline.NewContext()
			ctx.Domain = domain
			ctx.Interactive = interactive

			steps := []pipeline.Step{
				detect.New(),
				&preflightStep{inner: preflight.New(abuseKey)},
				register.New(),
			}

			p := pipeline.New(steps)
			if err := p.Run(ctx); err != nil {
				return err
			}

			if ctx.JWT != "" {
				if err := agent.SaveIdentityJSON([]byte(ctx.JWT)); err != nil {
					return fmt.Errorf("failed to save identity: %w", err)
				}
				pterm.Success.Printf("enrolled successfully (tier: %s)\n", ctx.Tier)
				pterm.Info.Printf("identity saved to %s\n", agent.IdentityPath())
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&domain, "domain", "ctrl.konoss.org", "controller domain")
	cmd.Flags().StringVar(&abuseKey, "abuseipdb-key", "", "AbuseIPDB API key for threat scoring")
	return cmd
}

// agentCmd returns the "schmutz agent" command group.
func agentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Manage the schmutz agent daemon",
	}

	cmd.AddCommand(agentStartCmd())
	cmd.AddCommand(agentStatusCmd())
	cmd.AddCommand(agentStopCmd())
	return cmd
}

// agentStartCmd returns the "schmutz agent start" command.
func agentStartCmd() *cobra.Command {
	var identityPath string

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the agent daemon in the foreground",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Resolve identity path: flag → default from identity package.
			if identityPath == "" {
				var err error
				identityPath, err = agent.LoadIdentityFile()
				if err != nil {
					return fmt.Errorf("no identity found — run 'schmutz enroll' first: %w", err)
				}
			}

			cfg := agent.DefaultConfig()
			cfg.IdentityPath = identityPath

			d, err := agent.NewDaemon(cfg)
			if err != nil {
				return fmt.Errorf("failed to create daemon: %w", err)
			}

			pterm.Info.Printf("starting agent (identity: %s)\n", identityPath)
			return d.Run()
		},
	}

	cmd.Flags().StringVar(&identityPath, "identity", "", "path to Ziti identity JSON (default: auto-detect)")
	return cmd
}

// agentStatusCmd returns the "schmutz agent status" command.
func agentStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show enrollment and daemon status",
		Run: func(cmd *cobra.Command, args []string) {
			if !agent.IsEnrolled() {
				pterm.Warning.Println("not enrolled — run 'schmutz enroll' to register this device")
				return
			}
			pterm.Success.Printf("enrolled\n")
			pterm.Info.Printf("identity: %s\n", agent.IdentityPath())
		},
	}
}

// agentStopCmd returns the "schmutz agent stop" command.
func agentStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the running agent daemon",
		Run: func(cmd *cobra.Command, args []string) {
			pterm.Warning.Println("not implemented yet")
		},
	}
}
