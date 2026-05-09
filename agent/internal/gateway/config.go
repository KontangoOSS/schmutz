package gateway

import (
	"fmt"
	"os"

	"github.com/KontangoOSS/schmutz/agent/internal/baojwt"
	"github.com/KontangoOSS/schmutz/shared"
	"gopkg.in/yaml.v3"
)

// RuntimeConfig is the resolved configuration the Gateway needs at startup.
type RuntimeConfig struct {
	Agent        baojwt.AgentConfig
	Gateway      *shared.GatewayConfig
	SchmutzDir   string
	BaoTokenPath string
	ForgejoURL   string
	ForgejoToken string
	ForgejoOrg   string
	Zone         string
}

// LoadRuntimeConfig reads agent.json and optionally the local substrate.yml
// to build config. When api is non-nil it is used directly, bypassing the
// local file — callers that already have the parsed substrate (e.g. the
// substrate watcher in main.go) should pass it to avoid a redundant read.
func LoadRuntimeConfig(schmutzDir, baoTokenPath string, api ...*shared.GatewayConfig) (*RuntimeConfig, error) {
	agentCfg, err := baojwt.LoadAgentConfig(schmutzDir + "/agent.json")
	if err != nil {
		return nil, fmt.Errorf("gateway config: load agent.json: %w", err)
	}

	var gatewayCfg *shared.GatewayConfig
	if len(api) > 0 && api[0] != nil {
		// Caller supplied the already-parsed gateway config — use it directly.
		gatewayCfg = api[0]
	} else {
		// Fall back to reading the local substrate.yml (useful in tests / legacy paths).
		substrateBytes, err := os.ReadFile(schmutzDir + "/substrate.yml")
		if err == nil {
			var s shared.Schmutz
			if err := yaml.Unmarshal(substrateBytes, &s); err == nil && s.API != nil {
				gatewayCfg = s.API
			}
		}
	}

	if baoTokenPath == "" {
		baoTokenPath = "/run/bao-token"
	}

	return &RuntimeConfig{
		Agent:        *agentCfg,
		Gateway:      gatewayCfg,
		SchmutzDir:   schmutzDir,
		BaoTokenPath: baoTokenPath,
		ForgejoURL:   os.Getenv("FORGEJO_URL"),
		ForgejoToken: os.Getenv("FORGEJO_TOKEN"),
		ForgejoOrg:   getenvOr("FORGEJO_ORG", "public"),
		Zone:         getenvOr("SCHMUTZ_ZONE", "tango"),
	}, nil
}

func getenvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
