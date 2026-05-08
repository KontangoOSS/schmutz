package shared

// Blueprint is the per-application catalog record stored at
//   public/<app>/blueprint.json
//
// Canonical schema source: kore/kustodian/openbao/docs/guides/catalog.md
//
// File format: JSON. Fields use short slug refs (e.g. "tg-md-1" rather
// than "public/sizing/tg-md-1"). Path roots are resolved via the catalog
// config at public/_catalog/config.json.
//
// CROSS-REPO CONTRACT: mirrored byte-for-byte between
// kore/ziti-base/server/internal/shared/ and kore/schmutz/src/internal/shared/.
// Change both repos in the same PR.

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// BlueprintSchemaVersion is the wire-format version. Bump when field
// meaning changes in a backward-incompatible way.
const BlueprintSchemaVersion = 2

// Blueprint is the structured catalog record for one application.
// Fields are grouped by concern to make authoring easier and to
// allow arrays where the old KV format required CSV strings.
type Blueprint struct {
	// --- top-level identity ---
	AppID  string `json:"app_id"`
	UUID   string `json:"uuid"`
	Schema int    `json:"schema_version,omitempty"`

	// Identity groups human-readable metadata.
	Identity BlueprintIdentity `json:"identity"`

	// Links groups all external URLs.
	Links BlueprintLinks `json:"links"`

	// Sizing declares resource tier constraints using short tier slugs
	// (e.g. "tg-md-1"). Resolved via catalog config path roots.
	Sizing BlueprintSizing `json:"sizing"`

	// Deployment declares platform and OS compatibility.
	Deployment BlueprintDeployment `json:"deployment"`

	// Runtime declares the operational contract: secrets, health, ports.
	Runtime BlueprintRuntime `json:"runtime"`

	// Catalog holds publishing metadata (when added, who maintains, active flag).
	Catalog BlueprintCatalog `json:"catalog"`
}

// BlueprintIdentity holds human-readable metadata.
type BlueprintIdentity struct {
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	// Category short slug, e.g. "operations". Resolved to public/categories/<slug>.
	Category string `json:"category"`
	// License short slug, e.g. "mit". Resolved to public/licenses/<slug>.
	License string `json:"license,omitempty"`
}

// BlueprintLinks groups all external URLs.
type BlueprintLinks struct {
	GitHub   string `json:"github,omitempty"`
	Forgejo  string `json:"forgejo,omitempty"`
	Upstream string `json:"upstream,omitempty"`
	Docs     string `json:"docs,omitempty"`
	Issues   string `json:"issues,omitempty"`
	Funding  string `json:"funding,omitempty"`
	Plugin   string `json:"plugin,omitempty"`
}

// BlueprintSizing declares resource tier constraints.
// Slugs resolve via catalog config: "tg-md-1" → "public/sizing/tg-md-1".
type BlueprintSizing struct {
	Min         string `json:"min"`
	Recommended string `json:"recommended"`
	Max         string `json:"max"`
}

// BlueprintDeployment declares where and how this app can be deployed.
type BlueprintDeployment struct {
	// DefaultOS short slug, e.g. "ubuntu-24.04".
	DefaultOS string `json:"default_os,omitempty"`
	// DefaultPlatform short slug, e.g. "proxmox/lxc/medium".
	DefaultPlatform string `json:"default_platform,omitempty"`
	// CompatiblePlatforms is a proper array of short platform slugs.
	CompatiblePlatforms []string `json:"compatible_platforms,omitempty"`
	// Types lists available deployment configs, e.g. ["docker-compose","binary"].
	Types []string `json:"types,omitempty"`
	// DockerSupported is a convenience flag for quick filtering.
	DockerSupported bool `json:"docker_supported,omitempty"`
}

// BlueprintRuntime declares the operational contract.
type BlueprintRuntime struct {
	// BaoSecretsPath is the path template for this app's secrets.
	// {deployment} is interpolated at enrollment time.
	// e.g. "kontango/secret/apps/ticketarr/{deployment}"
	BaoSecretsPath string `json:"bao_secrets_path"`

	// SecretRequirements is a typed array replacing the old CSV string.
	// Each entry is a (ref, group) pair. ref uses short slugs:
	//   "composites/database_credentials" → "public/secret-types/composites/..."
	SecretRequirements []SecretRequirement `json:"secret_requirements,omitempty"`

	// Health is the liveness check for this app.
	Health BlueprintHealth `json:"health,omitempty"`
}

// BlueprintHealth describes how to probe this app's liveness.
type BlueprintHealth struct {
	URL            string `json:"url,omitempty"`
	ExpectStatus   int    `json:"expect_status,omitempty"`
}

// BlueprintCatalog holds publishing metadata.
type BlueprintCatalog struct {
	Added           string `json:"added"`                      // YYYY-MM-DD
	Maintainer      string `json:"maintainer"`
	Active          bool   `json:"active"`
	DiscoverySource string `json:"discovery_source,omitempty"`
	Stargazers      int    `json:"stargazers,omitempty"`
	LatestTag       string `json:"latest_tag,omitempty"`
	LastUpdate      string `json:"last_update,omitempty"`
}

// ----- validation -----

var uuidV4Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var tierPattern = regexp.MustCompile(`^tg-(xs|sm|md|lg|xl)-[0-9]+$`)
var datePattern = regexp.MustCompile(`^\d{4}-\d{2}(-\d{2})?$`)

// Validate returns nil if the blueprint is internally consistent.
// Active blueprints are held to stricter requirements than discovery entries.
func (b *Blueprint) Validate() error {
	if b == nil {
		return errors.New("blueprint: nil")
	}
	if !slugPattern.MatchString(b.AppID) {
		return fmt.Errorf("blueprint: app_id %q is not a valid slug", b.AppID)
	}
	if !uuidV4Pattern.MatchString(strings.ToLower(b.UUID)) {
		return fmt.Errorf("blueprint: uuid %q is not a valid UUIDv4", b.UUID)
	}
	if b.Identity.DisplayName == "" {
		return errors.New("blueprint: identity.display_name required")
	}
	if b.Identity.Category == "" {
		return errors.New("blueprint: identity.category required")
	}
	// Sizing tier validation (if present)
	for field, val := range map[string]string{
		"sizing.min":         b.Sizing.Min,
		"sizing.recommended": b.Sizing.Recommended,
		"sizing.max":         b.Sizing.Max,
	} {
		if val != "" && !tierPattern.MatchString(val) {
			return fmt.Errorf("blueprint: %s %q must match tg-(xs|sm|md|lg|xl)-N", field, val)
		}
	}
	// Active blueprints require more fields
	if b.Catalog.Active {
		if b.Runtime.BaoSecretsPath == "" {
			return errors.New("blueprint: active=true requires runtime.bao_secrets_path")
		}
		if b.Runtime.Health.URL != "" {
			s := b.Runtime.Health.ExpectStatus
			if s < 100 || s > 599 {
				return fmt.Errorf("blueprint: runtime.health.expect_status %d out of range", s)
			}
		}
		for i, sr := range b.Runtime.SecretRequirements {
			if sr.Ref == "" {
				return fmt.Errorf("blueprint: runtime.secret_requirements[%d].ref required", i)
			}
			if !groupPattern.MatchString(sr.Group) {
				return fmt.Errorf("blueprint: runtime.secret_requirements[%d].group %q invalid", i, sr.Group)
			}
		}
	}
	if b.Catalog.Added != "" && !datePattern.MatchString(b.Catalog.Added) {
		return fmt.Errorf("blueprint: catalog.added %q must be YYYY-MM-DD", b.Catalog.Added)
	}
	return nil
}

// ResolveRef expands a short secret-type ref to its full Bao path.
// e.g. "composites/database_credentials" → "public/secret-types/composites/database_credentials"
// The root is configurable but defaults to "public/secret-types".
func ResolveRef(shortRef, root string) string {
	if root == "" {
		root = "public/secret-types"
	}
	return root + "/" + shortRef
}

// ResolveTier expands a sizing tier slug to its full Bao path.
func ResolveTier(slug, root string) string {
	if root == "" {
		root = "public/sizing"
	}
	if slug == "" {
		return ""
	}
	return root + "/" + slug
}

// ----- legacy interop -----

// FromKV parses the old flat map[string]string KV format (Bao KV v2
// string-only records) into a Blueprint. Used to migrate existing
// public/ Bao entries to the new JSON format.
//
// Deprecated: new blueprints should be authored as blueprint.json.
// This method exists only for the one-time migration and will be
// removed once all blueprints are in JSON format.
func (b *Blueprint) FromKV(kv map[string]string) error {
	*b = Blueprint{Schema: BlueprintSchemaVersion}
	b.AppID = kv["app_id"]
	b.UUID = kv["uuid"]
	b.Identity = BlueprintIdentity{
		DisplayName: kv["display_name"],
		Description: kv["description"],
		Category:    stripPrefix(kv["category"], "public/categories/"),
		License:     stripPrefix(kv["upstream_license"], "public/licenses/"),
	}
	b.Links = BlueprintLinks{
		GitHub:   kv["github_url"],
		Forgejo:  kv["forgejo_url"],
		Upstream: kv["upstream_repo"],
		Docs:     kv["upstream_docs"],
		Issues:   kv["issues_url"],
		Funding:  kv["funding_url"],
		Plugin:   kv["plugin_url"],
	}
	b.Sizing = BlueprintSizing{
		Min:         stripPrefix(kv["sizing_min"], "public/sizing/"),
		Recommended: stripPrefix(kv["sizing_recommended"], "public/sizing/"),
		Max:         stripPrefix(kv["sizing_max"], "public/sizing/"),
	}
	def := stripPrefix(kv["default_deployment_platform"], "public/deployment-platforms/")
	var compat []string
	for _, p := range strings.Split(kv["compatible_deployment_platforms"], ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			compat = append(compat, stripPrefix(p, "public/deployment-platforms/"))
		}
	}
	b.Deployment = BlueprintDeployment{
		DefaultOS:           stripPrefix(kv["default_os"], "public/os/"),
		DefaultPlatform:     def,
		CompatiblePlatforms: compat,
		DockerSupported:     kv["docker_supported"] == "true",
	}
	var secReqs []SecretRequirement
	for _, item := range strings.Split(kv["secret_requirements"], ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		parts := strings.SplitN(item, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("blueprint: secret_requirements %q: want <ref>:<group>", item)
		}
		secReqs = append(secReqs, SecretRequirement{
			Ref:   stripPrefix(strings.TrimSpace(parts[0]), "public/secret-types/"),
			Group: strings.TrimSpace(parts[1]),
		})
	}
	expectStatus, _ := parseIntOptional(kv["health_expect_status"])
	b.Runtime = BlueprintRuntime{
		BaoSecretsPath:     kv["bao_secrets_path"],
		SecretRequirements: secReqs,
		Health: BlueprintHealth{
			URL:          kv["health_url"],
			ExpectStatus: expectStatus,
		},
	}
	active := kv["active"] == "true"
	stars, _ := parseIntOptional(kv["stargazers"])
	b.Catalog = BlueprintCatalog{
		Added:           kv["catalog_added"],
		Maintainer:      kv["catalog_maintainer"],
		Active:          active,
		DiscoverySource: kv["discovery_source"],
		Stargazers:      stars,
		LatestTag:       kv["upstream_latest_tag"],
		LastUpdate:      kv["upstream_last_update"],
	}
	return nil
}

// MarshalJSON produces the canonical blueprint.json representation.
func (b *Blueprint) MarshalJSON() ([]byte, error) {
	type Alias Blueprint
	b.Schema = BlueprintSchemaVersion
	return json.Marshal((*Alias)(b))
}

// UnmarshalJSON reads a blueprint.json file.
func (b *Blueprint) UnmarshalJSON(data []byte) error {
	type Alias Blueprint
	var a Alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*b = Blueprint(a)
	return nil
}

// JSON returns the canonical JSON bytes for this blueprint.
func (b *Blueprint) JSON() ([]byte, error) {
	return json.MarshalIndent(b, "", "  ")
}

// ----- helpers -----

func stripPrefix(s, prefix string) string {
	return strings.TrimPrefix(s, prefix)
}

func parseIntOptional(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	var n int
	_, err := fmt.Sscan(s, &n)
	return n, err
}
