package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"git.konoss.org/kore/schmutz/agent/internal/schmutz"
	"github.com/spf13/cobra"
)

// substrateTestCmd: one-shot watcher tick. Runs the watcher's poll once,
// prints the plan log, exits with the watcher's last-error status.
//
// Useful for operator validation ("does my agent see its substrate?")
// without starting the full daemon.
func substrateTestCmd() *cobra.Command {
	var agentJSON string
	var tokenPath string
	cmd := &cobra.Command{
		Use:   "substrate-test",
		Short: "Read this host's substrate from Bao and log the reconciliation plan (no actions)",
		Long: `Run a single poll of the substrate watcher.

Reads <agent-json>, then <token>, then fetches the substrate from Bao at
<tenant>/secret/data/apps/<app>/<deployment>/substrate, validates it,
and logs the reconciliation plan.

Takes no actions. Exits 0 on success, non-zero if the read or
validation failed.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := substrate.WatcherConfig{
				AgentJSONPath: agentJSON,
				BaoTokenPath:  tokenPath,
				// Big interval — we'll Cancel after the first tick fires.
				Interval: time.Hour,
			}
			w := substrate.NewWatcher(cfg)

			ctx, cancel := context.WithCancel(context.Background())
			doneCh := make(chan struct{})
			go func() { _ = w.Run(ctx); close(doneCh) }()

			// Wait for the first poll to actually complete. LastAttempt
			// is set at tick start; LastSpec or LastError is set at tick
			// end. We need both to be in their post-tick state before
			// cancelling, otherwise we abort the in-flight HTTP call.
			deadline := time.Now().Add(15 * time.Second)
			for time.Now().Before(deadline) {
				if w.LastAttempt() != 0 && (w.LastSpec() != nil || w.LastError() != nil) {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			cancel()
			<-doneCh

			if err := w.LastError(); err != nil {
				fmt.Fprintf(os.Stderr, "substrate-test: %v\n", err)
				return fmt.Errorf("substrate poll failed")
			}
			if w.LastSpec() == nil {
				return fmt.Errorf("substrate poll did not complete; check logs")
			}
			fmt.Fprintln(os.Stderr, "substrate-test: OK")
			return nil
		},
	}
	cmd.Flags().StringVar(&agentJSON, "agent-json", "/etc/schmutz/agent.json",
		"path to the persisted agent config")
	cmd.Flags().StringVar(&tokenPath, "token", "/run/bao-token",
		"path to the scoped bao token kept fresh by the bao-jwt subsystem")
	return cmd
}
