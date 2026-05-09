package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	BaoAddr        string
	BaoToken       string
	BaoSkipVerify  bool
	ZitiAPI        string
	ZitiUsername   string
	ZitiPassword   string
	ListenAddr     string
	BaoMount       string
	BaoTokenPrefix string

	// AgentBaoAddr is the URL embedded into bao-bundle responses so
	// agents know how to reach Bao. Must be reachable without overlay
	// access since it's used during initial enrollment.
	AgentBaoAddr string

	// Hub configuration
	ForgejoURL   string // e.g. http://10.11.30.30:3000
	ForgejoToken string // Forgejo API token for the hub service account
	ForgejoOrg   string // catalog org, defaults to "public"
	HubAdminToken string // X-Kontango-Token for operator endpoints

	// schmutz-server
	SchmutzListenAddr       string
	SchmutzZitiIdentityFile string
	SchmutzServiceName      string
	SchmutzBootstrap        bool
}

func Load() (*Config, error) {
	c := &Config{
		BaoAddr:        getenv("BAO_ADDR", ""),
		BaoToken:       getenv("BAO_TOKEN", ""),
		BaoSkipVerify:  getenv("BAO_SKIP_VERIFY", "0") == "1",
		ZitiAPI:        getenv("ZITI_API", ""),
		ZitiUsername:   getenv("ZITI_USERNAME", ""),
		ZitiPassword:   getenv("ZITI_PASSWORD", ""),
		ListenAddr:     getenv("LISTEN_ADDR", "127.0.0.1:8765"),
		BaoMount:       getenv("BAO_MOUNT", "secret"),
		BaoTokenPrefix: getenv("BAO_TOKEN_PREFIX", "enroll-tokens"),
		AgentBaoAddr:   getenv("AGENT_BAO_ADDR", "https://secrets.kontango.net"),
		ForgejoURL:     getenv("FORGEJO_URL", ""),
		ForgejoToken:   getenv("FORGEJO_TOKEN", ""),
		ForgejoOrg:     getenv("FORGEJO_ORG", "public"),
		HubAdminToken:  getenv("HUB_ADMIN_TOKEN", ""),

		SchmutzListenAddr:       getenv("SCHMUTZ_LISTEN_ADDR", "127.0.0.1:8766"),
		SchmutzZitiIdentityFile: getenv("SCHMUTZ_ZITI_IDENTITY_FILE", "/etc/schmutz/server-identity.json"),
		SchmutzServiceName:      getenv("SCHMUTZ_SERVICE_NAME", "admin.tango"),
		SchmutzBootstrap:        getenv("SCHMUTZ_BOOTSTRAP", "0") == "1",
	}
	if c.SchmutzBootstrap {
		// Bootstrap mode runs without Bao/Ziti — those clients aren't initialized.
		return c, nil
	}
	type req struct {
		name  string
		value string
	}
	required := []req{
		{"BAO_ADDR", c.BaoAddr},
		{"BAO_TOKEN", c.BaoToken},
		{"ZITI_API", c.ZitiAPI},
		{"ZITI_USERNAME", c.ZitiUsername},
		{"ZITI_PASSWORD", c.ZitiPassword},
	}
	var missing []string
	for _, r := range required {
		if r.value == "" {
			missing = append(missing, r.name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("required env vars missing: %s", strings.Join(missing, ", "))
	}
	return c, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
