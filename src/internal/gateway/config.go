package gateway

import (
	"fmt"
	"os"

	"github.com/KontangoOSS/schmutz/internal/baojwt"
	"github.com/KontangoOSS/schmutz/internal/shared"
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

// LoadRuntimeConfig reads agent.json and the substrate file to build config.
func LoadRuntimeConfig(schmutzDir, baoTokenPath string) (*RuntimeConfig, error) {
	agentCfg, err := baojwt.LoadAgentConfig(schmutzDir + "/agent.json")
	if err != nil {
		return nil, fmt.Errorf("gateway config: load agent.json: %w", err)
	}

	var gatewayCfg *shared.GatewayConfig
	substrateBytes, err := os.ReadFile(schmutzDir + "/substrate.yml")
	if err == nil {
		var s shared.Schmutz
		if err := yaml.Unmarshal(substrateBytes, &s); err == nil && s.API != nil {
			gatewayCfg = s.API
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
