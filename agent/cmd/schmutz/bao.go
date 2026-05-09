package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/KontangoOSS/schmutz/agent/internal/baojwt"
	"github.com/spf13/cobra"
)

const (
	defaultAgentJSONPath = "/etc/schmutz/agent.json"
	defaultBaoTokenPath  = "/run/bao-token"
)

// baoEnrollCmd: one-shot install. Calls the controller's /api/bao-bundle
// endpoint, unwraps the secret_id, persists /etc/schmutz/agent.json, and
// runs an immediate refresh to populate /run/bao-token.
//
// Idempotent: re-running with a fresh wrap-token (new bundle) overwrites
// the persisted config.
//
// Operator runs this once after the ziti identity is in place, after which
// the systemd timer takes over.
func baoEnrollCmd() *cobra.Command {
	var controllerURL string
	var zitiIdentity string
	var agentJSON string
	var tokenPath string
	cmd := &cobra.Command{
		Use:   "bao-enroll",
		Short: "Fetch + install Bao install bundle, then refresh /run/bao-token",
		Long: `Fetch a fresh Bao install bundle from the controller, unwrap the
secret_id, persist /etc/schmutz/agent.json, and run one immediate
refresh to populate /run/bao-token.

The controller-side per-app entity must already exist (bao-app-enroll
on the controller) — if not, the controller returns 404.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if controllerURL == "" {
				return fmt.Errorf("schmutz bao-enroll: --controller required")
			}
			if zitiIdentity == "" {
				return fmt.Errorf("schmutz bao-enroll: --ziti-identity required")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			fmt.Fprintf(os.Stderr, "fetch bundle: %s\n", controllerURL)
			bundle, err := baojwt.FetchBundle(ctx, controllerURL, zitiIdentity, nil)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "  role=%s tenant=%s app=%s deployment=%s\n",
				bundle.Role, bundle.Tenant, bundle.App, bundle.Deployment)

			cfg, err := baojwt.InstallBundle(ctx, bundle, agentJSON)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (mode 0600)\n", agentJSON)

			res, err := baojwt.Refresh(ctx, cfg, tokenPath)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d bytes, mode 0640)\n", res.TokenPath, res.BytesOut)
			fmt.Fprintln(os.Stderr, "OK")
			return nil
		},
	}
	cmd.Flags().StringVar(&controllerURL, "controller", "", "controller base URL (e.g. https://join.kontango.net)")
	cmd.Flags().StringVar(&zitiIdentity, "ziti-identity", "", "this machine's ziti identity name (e.g. machine-f6f769f1)")
	cmd.Flags().StringVar(&agentJSON, "agent-json", defaultAgentJSONPath, "path to write the persisted agent config")
	cmd.Flags().StringVar(&tokenPath, "token", defaultBaoTokenPath, "path to write the scoped bao token")
	return cmd
}

// baoLoginCmd: one-shot refresh. Reads /etc/schmutz/agent.json and
// writes a fresh /run/bao-token. This is what the systemd timer fires.
func baoLoginCmd() *cobra.Command {
	var agentJSON string
	var tokenPath string
	cmd := &cobra.Command{
		Use:   "bao-login",
		Short: "Refresh /run/bao-token via approle → OIDC → JWT login",
		Long: `Read /etc/schmutz/agent.json, run one cycle of the bao-jwt flow
(approle login → OIDC mint → JWT login), and atomically write the
resulting scoped Bao token to /run/bao-token.

If anything fails the prior /run/bao-token (if any) is left in place
unchanged.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			cfg, err := baojwt.LoadAgentConfig(agentJSON)
			if err != nil {
				return err
			}
			res, err := baojwt.Refresh(ctx, cfg, tokenPath)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "schmutz bao-login: refreshed %s (role=%s, %d bytes)\n",
				res.TokenPath, res.Role, res.BytesOut)
			return nil
		},
	}
	cmd.Flags().StringVar(&agentJSON, "agent-json", defaultAgentJSONPath, "path to the persisted agent config")
	cmd.Flags().StringVar(&tokenPath, "token", defaultBaoTokenPath, "path to write the scoped bao token")
	return cmd
}
