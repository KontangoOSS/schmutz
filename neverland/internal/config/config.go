package config

import (
	"os"
	"time"
)

type Config struct {
	ListenAddr     string
	TinkNamespace  string
	Kubeconfig     string
	ArtifactsPath  string
	NginxURL       string
	SmeeDeployment string
	EnrollURL      string
	BootBaseURL    string
	DownloadsPath  string
	PostgresDSN    string

	// Boot menu git config
	BootConfigAPIBase  string
	BootConfigOwner    string
	BootConfigRepo     string
	BootConfigRef      string
	BootConfigToken    string
	BootConfigCacheTTL time.Duration
}

func Load() Config {
	ttl := 30 * time.Second
	if v := os.Getenv("BOOT_CONFIG_CACHE_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			ttl = d
		}
	}

	return Config{
		ListenAddr:     envOr("LISTEN_ADDR", "0.0.0.0:8091"),
		TinkNamespace:  envOr("TINK_NAMESPACE", "tink-system"),
		Kubeconfig:     envOr("KUBECONFIG", ""),
		ArtifactsPath:  envOr("ARTIFACTS_PATH", "/artifacts"),
		NginxURL:       envOr("NGINX_URL", "http://tink-stack.tink-system.svc.cluster.local:8080"),
		SmeeDeployment: envOr("SMEE_DEPLOYMENT", "smee"),
		EnrollURL:      envOr("ENROLL_URL", "https://join.kontango.net"),
		BootBaseURL:    envOr("BOOT_BASE_URL", "https://boot.kontango.net"),
		DownloadsPath:  envOr("DOWNLOADS_PATH", "/downloads"),
		PostgresDSN:    envOr("POSTGRES_DSN", ""),

		BootConfigAPIBase:  envOr("BOOT_CONFIG_API_BASE", "https://git.konoss.org/api/v1"),
		BootConfigOwner:    envOr("BOOT_CONFIG_REPO_OWNER", "public"),
		BootConfigRepo:     envOr("BOOT_CONFIG_REPO_NAME", "neverland"),
		BootConfigRef:      envOr("BOOT_CONFIG_REPO_REF", "main"),
		BootConfigToken:    envOr("BOOT_CONFIG_TOKEN", ""),
		BootConfigCacheTTL: ttl,
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
