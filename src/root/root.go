package root

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	identityFile  = "identity.json"
	agentSockFile = "agent.sock"
	registryFile  = "registry.json"
)

// Root carries device identity paths and config.
// Mirrors ZROK's env_core.Root interface.
type Root interface {
	IsEnabled() bool
	IdentityPath() (string, error)
	AgentSocket() (string, error)
	AgentRegistry() (string, error)
	DeviceID() string
	ControllerURL() string
	Slug() string

	// Zitadel machine account credentials — for TangoKore API (not for Ziti)
	ZitadelIssuer() string
	ZitadelClientID() string
	ZitadelClientSecret() string

	// Validate checks required fields; callers invoke when they actually need device_id
	Validate() error

	// SetSlug persists the enrollment slug to manifest.yaml.
	SetSlug(slug string) error
}

type schmutzRoot struct {
	dir  string
	cfg  config
}

// LoadRoot loads the root from a config directory (default: /etc/schmutz).
// If manifest.yaml is missing, returns a zero-config root (not an error).
func LoadRoot(dir string) (Root, error) {
	cfg, err := loadConfig(dir)
	if err != nil {
		return nil, err
	}
	return &schmutzRoot{
		dir: dir,
		cfg: cfg,
	}, nil
}

func (r *schmutzRoot) IsEnabled() bool {
	_, err := os.Stat(filepath.Join(r.dir, identityFile))
	return err == nil
}

func (r *schmutzRoot) IdentityPath() (string, error) {
	return filepath.Join(r.dir, identityFile), nil
}

func (r *schmutzRoot) AgentSocket() (string, error) {
	return filepath.Join(r.dir, agentSockFile), nil
}

func (r *schmutzRoot) AgentRegistry() (string, error) {
	return filepath.Join(r.dir, registryFile), nil
}

func (r *schmutzRoot) DeviceID() string            { return r.cfg.DeviceID }
func (r *schmutzRoot) ControllerURL() string       { return r.cfg.ControllerURL }
func (r *schmutzRoot) Slug() string                { return r.cfg.Slug }
func (r *schmutzRoot) ZitadelIssuer() string       { return r.cfg.ZitadelIssuer }
func (r *schmutzRoot) ZitadelClientID() string     { return r.cfg.ZitadelClientID }
func (r *schmutzRoot) ZitadelClientSecret() string { return r.cfg.ZitadelClientSecret }

func (r *schmutzRoot) SetSlug(slug string) error {
	r.cfg.Slug = slug
	return r.writeManifest()
}

func (r *schmutzRoot) writeManifest() error {
	data, err := yaml.Marshal(r.cfg)
	if err != nil {
		return fmt.Errorf("root: marshal manifest: %w", err)
	}
	return os.WriteFile(filepath.Join(r.dir, "manifest.yaml"), data, 0644)
}

func (r *schmutzRoot) Validate() error {
	if r.cfg.DeviceID == "" {
		return fmt.Errorf("root: device_id required in manifest.yaml")
	}
	return nil
}

// config is the minimal subset of manifest.yaml that Root needs.
type config struct {
	DeviceID      string `yaml:"device_id"`
	ControllerURL string `yaml:"controller_url"`
	Slug          string `yaml:"slug,omitempty"`

	// Zitadel machine account — for TangoKore API auth (not for Ziti)
	ZitadelIssuer       string `yaml:"zitadel_issuer,omitempty"`
	ZitadelClientID     string `yaml:"zitadel_client_id,omitempty"`
	ZitadelClientSecret string `yaml:"zitadel_client_secret,omitempty"`
}

func loadConfig(dir string) (config, error) {
	var cfg config
	data, err := os.ReadFile(filepath.Join(dir, "manifest.yaml"))
	if errors.Is(err, os.ErrNotExist) {
		// No manifest — return zero config (not an error; used in tests and pre-enrollment)
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("root: read manifest: %w", err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("root: parse manifest: %w", err)
	}
	return cfg, nil
}
