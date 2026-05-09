// Package forgejo is the hub's client for the Forgejo API.
//
// The hub treats the Forgejo `public` org as the application catalog:
// one repo per app, each repo containing blueprint.yaml, schmutz.yml,
// and deployments/<tenant>/<deployment>/deployment.yaml files.
//
// This client is intentionally narrow — only what the hub needs.
// It is not a general-purpose Forgejo SDK.
//
// Auth: all calls use the token passed to NewClient. The hub reads its
// token from Bao at startup (secret/data/forgejo/hub-token in root ns).
package forgejo

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"git.konoss.org/kore/schmutz/shared"
)

// ErrNotFound is returned when the requested resource does not exist
// in Forgejo (HTTP 404).
var ErrNotFound = errors.New("forgejo: not found")

// Client talks to one Forgejo instance against one catalog org.
type Client struct {
	base  string       // e.g. http://10.11.30.30:3000
	org   string       // e.g. "public"
	token string
	hc    *http.Client
}

// NewClient creates a Forgejo client.
// base is the Forgejo root URL (no trailing slash).
// org is the catalog org name ("public").
// token is a Forgejo API token with read+write on the org.
func NewClient(base, org, token string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Client{
		base:  strings.TrimRight(base, "/"),
		org:   org,
		token: token,
		hc:    &http.Client{Timeout: timeout},
	}
}

// AppSummary is the lightweight view returned by ListApps.
type AppSummary struct {
	AppID       string `json:"app_id"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	ForgejoURL  string `json:"forgejo_url"`
}

// DeploymentRecord is the git-facing shape of
// deployments/<tenant>/<deployment>/deployment.json
//
// WHAT LIVES HERE (git, public catalog repo):
//   The operator approval gate — structural fields only, no host details,
//   no runtime-assigned identity. Merging this file = operator approval.
//
// WHAT LIVES IN BAO (secret/deployments/<tenant>/<app>/<dep>):
//   Runtime details written by the hub at claim time: host IP/ID, Ziti
//   identity name, Bao entity ID, claimed_at. These are sensitive
//   infrastructure details that must not appear in a public git repo.
//
// The hub reads git for the approval check (status must be "pending"),
// then writes runtime details to Bao. UpdateDeployment only updates
// status + approved_by/at in git — never host details.
type DeploymentRecord struct {
	Tenant     string `yaml:"tenant"     json:"tenant"`
	App        string `yaml:"app"        json:"app"`
	Deployment string `yaml:"deployment" json:"deployment"`
	Flavor     string `yaml:"flavor"     json:"flavor"`
	Status     string `yaml:"status"     json:"status"` // pending|active|decommissioned
	Version    string `yaml:"version,omitempty" json:"version,omitempty"`
	Platform   string `yaml:"platform,omitempty" json:"platform,omitempty"` // e.g. proxmox/lxc/medium

	// Operator approval audit trail — safe for git.
	ApprovedBy string `yaml:"approved_by" json:"approved_by"`
	ApprovedAt string `yaml:"approved_at" json:"approved_at"`
}

// DeploymentRuntime is the Bao-resident half of a deployment record.
// Written by the hub at claim time to:
//   secret/deployments/<tenant>/<app>/<deployment>
// Readable only by the deployment's own scoped policy token.
type DeploymentRuntime struct {
	Tenant       string `json:"tenant"`
	App          string `json:"app"`
	Deployment   string `json:"deployment"`
	ZitiIdentity string `json:"ziti_identity"`
	EntityID     string `json:"entity_id"`
	ClaimedAt    string `json:"claimed_at"`

	// Host details — kept out of git.
	Host *DeploymentHost `json:"host,omitempty"`
}

// DeploymentHost captures where the deployment runs.
type DeploymentHost struct {
	LXCID       int    `yaml:"lxc_id,omitempty"       json:"lxc_id,omitempty"`
	ProxmoxNode string `yaml:"proxmox_node,omitempty" json:"proxmox_node,omitempty"`
	IP          string `yaml:"ip,omitempty"           json:"ip,omitempty"`
	Hostname    string `yaml:"hostname,omitempty"     json:"hostname,omitempty"`
	DropletID   int    `yaml:"droplet_id,omitempty"   json:"droplet_id,omitempty"`
	Region      string `yaml:"region,omitempty"       json:"region,omitempty"`
}

// ----- catalog reads -----

// ListApps lists all active applications in the catalog org.
// It reads each repo's blueprint.yaml and returns the subset with active=true.
// Repos without a blueprint.yaml (e.g. the initial auto-init README-only state)
// are silently skipped.
func (c *Client) ListApps(ctx context.Context) ([]AppSummary, error) {
	// Paginate repos in the org.
	type repoSummary struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		HTMLURL     string `json:"html_url"`
		Archived    bool   `json:"archived"`
	}
	var repos []repoSummary
	page := 1
	for {
		body, status, err := c.do(ctx, "GET",
			fmt.Sprintf("/api/v1/repos/search?q=&topic=false&limit=50&page=%d&owner=%s", page, url.QueryEscape(c.org)),
			nil)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("forgejo: list repos: status %d: %s", status, body)
		}
		var resp struct {
			Data []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				HTMLURL     string `json:"html_url"`
				Archived    bool   `json:"archived"`
				Owner       struct {
					Login string `json:"login"`
				} `json:"owner"`
			} `json:"data"`
			OK bool `json:"ok"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("forgejo: decode repos: %w", err)
		}
		added := 0
		for _, r := range resp.Data {
			if r.Owner.Login != c.org {
				continue
			}
			if !r.Archived {
				repos = append(repos, repoSummary{r.Name, r.Description, r.HTMLURL, r.Archived})
				added++
			}
		}
		if len(resp.Data) < 50 {
			break
		}
		page++
	}

	var out []AppSummary
	for _, r := range repos {
		bp, err := c.GetTango(ctx, r.Name)
		if errors.Is(err, ErrNotFound) {
			continue // repo exists but no blueprint.yaml yet
		}
		if err != nil {
			continue // parse error — skip silently, operator needs to fix the yaml
		}
		if !bp.Catalog.Active {
			continue
		}
		out = append(out, AppSummary{
			AppID:       bp.AppID,
			DisplayName: bp.Identity.DisplayName,
			Description: bp.Identity.Description,
			ForgejoURL:  r.HTMLURL,
		})
	}
	return out, nil
}

// GetTango reads and parses tango.json for an app.
// Returns ErrNotFound if the file doesn't exist.
func (c *Client) GetTango(ctx context.Context, app string) (*shared.Tango, error) {
	raw, err := c.readFile(ctx, app, "tango.json")
	if err != nil {
		return nil, err
	}
	var bp shared.Tango
	if err := bp.UnmarshalJSON(raw); err != nil {
		return nil, fmt.Errorf("forgejo: parse tango.json for %s: %w", app, err)
	}
	return &bp, nil
}

// GetDeployment reads deployments/<tenant>/<deployment>/deployment.json.
// Returns ErrNotFound if the record doesn't exist.
func (c *Client) GetDeployment(ctx context.Context, app, tenant, deployment string) (*DeploymentRecord, error) {
	path := fmt.Sprintf("deployments/%s/%s/deployment.json", tenant, deployment)
	raw, err := c.readFile(ctx, app, path)
	if err != nil {
		return nil, err
	}
	var rec DeploymentRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return nil, fmt.Errorf("forgejo: parse deployment.json for %s/%s/%s: %w", app, tenant, deployment, err)
	}
	return &rec, nil
}

// GetSchmutz implements the Level 2 → Level 3 cascade:
//   1. Try deployments/<tenant>/<dep>/schmutz.yml  (L3 override)
//   2. Fall back to schmutz.yml at repo root        (L2 default)
//   3. ErrNotFound only if both are absent
//
// This is the single read path for substrate — callers never need to
// implement the cascade themselves.
func (c *Client) GetSchmutz(ctx context.Context, app, tenant, deployment string) (*shared.Schmutz, error) {
	// L3: deployment-specific override
	l3path := fmt.Sprintf("deployments/%s/%s/schmutz.yml", tenant, deployment)
	if raw, err := c.readFile(ctx, app, l3path); err == nil {
		return parseSubstrate(raw, app+" L3 "+l3path)
	}
	// L2: app-level default
	if raw, err := c.readFile(ctx, app, "schmutz.yml"); err == nil {
		return parseSubstrate(raw, app+" L2 schmutz.yml")
	}
	return nil, ErrNotFound
}

func parseSubstrate(raw []byte, label string) (*shared.Schmutz, error) {
	var spec shared.Schmutz
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		return nil, fmt.Errorf("forgejo: parse substrate %s: %w", label, err)
	}
	if spec.Version == 0 {
		spec.Version = shared.SchmutzSchemaVersion
	}
	return &spec, nil
}

// ── catalog reference lookups ─────────────────────────────────────────────────

// CatalogConfig holds the full _catalog/config.json structure.
type CatalogConfig struct {
	Paths         map[string]string `json:"paths"`
	Defaults      CatalogDefaults   `json:"defaults"`
	SchemaVersion int               `json:"schema_version"`
}

// CatalogDefaults carries operational defaults from _catalog/config.json.
type CatalogDefaults struct {
	Zone           string `json:"zone"`            // Ziti overlay TLD, default "tango"
	OverlayTLD     string `json:"overlay_tld"`     // alias for Zone
	BaoSecretsPath string `json:"bao_secrets_path"` // full path template
}

// Zone returns the configured overlay TLD, defaulting to "tango".
func (d CatalogDefaults) ZoneOrDefault() string {
	if d.Zone != "" {
		return d.Zone
	}
	if d.OverlayTLD != "" {
		return d.OverlayTLD
	}
	return "tango"
}

// GetCatalogConfig reads _catalog/config.json from the catalog repo.
func (c *Client) GetCatalogConfig(ctx context.Context) (*CatalogConfig, error) {
	raw, err := c.readFile(ctx, "_catalog", "config.json")
	if err != nil {
		return nil, err
	}
	var cfg CatalogConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("forgejo: parse catalog config: %w", err)
	}
	return &cfg, nil
}

// GetSizingTier reads a single sizing tier, e.g. "tg-md-1".
// Returns the parsed JSON as map[string]any — consumers pick the fields they need.
func (c *Client) GetSizingTier(ctx context.Context, tier string) (map[string]any, error) {
	path := fmt.Sprintf("sizing/%s.json", tier)
	raw, err := c.readFile(ctx, "_catalog", path)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("forgejo: parse sizing %s: %w", tier, err)
	}
	return m, nil
}

// GetPlatformTier reads a single platform tier, e.g. "proxmox/lxc/medium".
// The id is the path under platforms/ without .json.
func (c *Client) GetPlatformTier(ctx context.Context, platformID string) (map[string]any, error) {
	path := fmt.Sprintf("platforms/%s.json", platformID)
	raw, err := c.readFile(ctx, "_catalog", path)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("forgejo: parse platform %s: %w", platformID, err)
	}
	return m, nil
}

// GetSecretType reads a single secret type, e.g. "composites/database_credentials".
func (c *Client) GetSecretType(ctx context.Context, typeID string) (map[string]any, error) {
	path := fmt.Sprintf("secret-types/%s.json", typeID)
	raw, err := c.readFile(ctx, "_catalog", path)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("forgejo: parse secret type %s: %w", typeID, err)
	}
	return m, nil
}

// GetComposeFile reads the docker-compose.yml for a deployment.
// Always at deployments/<tenant>/<dep>/docker-compose.yml — compose files
// are deployment-specific, never at root level.
func (c *Client) GetComposeFile(ctx context.Context, app, tenant, deployment string) ([]byte, error) {
	path := fmt.Sprintf("deployments/%s/%s/docker-compose.yml", tenant, deployment)
	raw, err := c.readFile(ctx, app, path)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// GetEnvTemplate reads the .env.template for a deployment.
// Always at deployments/<tenant>/<dep>/.env.template — templates are
// deployment-specific. Values are not stored here; they come from Bao.
func (c *Client) GetEnvTemplate(ctx context.Context, app, tenant, deployment string) ([]byte, error) {
	path := fmt.Sprintf("deployments/%s/%s/.env.template", tenant, deployment)
	raw, err := c.readFile(ctx, app, path)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// UpdateDeployment updates the git deployment.json — only the fields that
// belong in git (status, approved_by, approved_at, version). Runtime details
// (host, ziti_identity, entity_id, claimed_at) are stored in Bao, not git.
func (c *Client) UpdateDeployment(ctx context.Context, app, tenant, deployment, commitMsg string, updates map[string]string) error {
	path := fmt.Sprintf("deployments/%s/%s/deployment.json", tenant, deployment)
	raw, sha, err := c.readFileWithSHA(ctx, app, path)
	if err != nil {
		return err
	}
	var rec DeploymentRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return fmt.Errorf("forgejo: parse deployment.json for update: %w", err)
	}
	for k, v := range updates {
		switch k {
		case "status":
			rec.Status = v
		case "approved_by":
			rec.ApprovedBy = v
		case "approved_at":
			rec.ApprovedAt = v
		case "version":
			rec.Version = v
		}
	}
	newContent, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("forgejo: marshal updated deployment.json: %w", err)
	}
	return c.writeFile(ctx, app, path, sha, newContent, commitMsg)
}

// ----- low-level helpers -----

func (c *Client) readFile(ctx context.Context, repo, path string) ([]byte, error) {
	raw, _, err := c.readFileWithSHA(ctx, repo, path)
	return raw, err
}

func (c *Client) readFileWithSHA(ctx context.Context, repo, filePath string) ([]byte, string, error) {
	apiPath := fmt.Sprintf("/api/v1/repos/%s/%s/contents/%s",
		url.PathEscape(c.org), url.PathEscape(repo), filePath)
	body, status, err := c.do(ctx, "GET", apiPath, nil)
	if err != nil {
		return nil, "", err
	}
	if status == http.StatusNotFound {
		return nil, "", ErrNotFound
	}
	if status != http.StatusOK {
		return nil, "", fmt.Errorf("forgejo: read %s/%s: status %d: %s",
			repo, filePath, status, body)
	}
	var resp struct {
		Content  string `json:"content"`  // base64-encoded
		Encoding string `json:"encoding"` // "base64"
		SHA      string `json:"sha"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, "", fmt.Errorf("forgejo: decode file response: %w", err)
	}
	// Forgejo may include newlines in the base64 blob.
	clean := strings.ReplaceAll(resp.Content, "\n", "")
	decoded, err := base64.StdEncoding.DecodeString(clean)
	if err != nil {
		return nil, "", fmt.Errorf("forgejo: base64-decode %s/%s: %w", repo, filePath, err)
	}
	return decoded, resp.SHA, nil
}

func (c *Client) writeFile(ctx context.Context, repo, filePath, sha string, content []byte, message string) error {
	apiPath := fmt.Sprintf("/api/v1/repos/%s/%s/contents/%s",
		url.PathEscape(c.org), url.PathEscape(repo), filePath)
	body := map[string]string{
		"message": message,
		"content": base64.StdEncoding.EncodeToString(content),
		"branch":  "main",
		"sha":     sha,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	respBody, status, err := c.do(ctx, "PUT", apiPath, payload)
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return fmt.Errorf("forgejo: write %s/%s: status %d: %s",
			repo, filePath, status, respBody)
	}
	return nil
}

func (c *Client) do(ctx context.Context, method, path string, payload []byte) ([]byte, int, error) {
	var rdr io.Reader
	if payload != nil {
		rdr = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return nil, 0, fmt.Errorf("forgejo: new request: %w", err)
	}
	req.Header.Set("Authorization", "token "+c.token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("forgejo: call %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return body, resp.StatusCode, nil
}
