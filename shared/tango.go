package shared

// Tango is the per-application catalog record stored at
//   public/<app>/blueprint.json
//
// Canonical schema source: kore/kustodian/openbao/docs/guides/catalog.md
//
// File format: JSON. Fields use short slug refs (e.g. "tg-md-1" rather
// than "public/sizing/tg-md-1"). Path roots are resolved via the catalog
// config at public/_catalog/config.json.
//

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// TangoSchemaVersion is the wire-format version. Bump when field
// meaning changes in a backward-incompatible way.
const TangoSchemaVersion = 2

// Tango is the structured catalog record for one application.
// Fields are grouped by concern to make authoring easier and to
// allow arrays where the old KV format required CSV strings.
type Tango struct {
	// --- top-level identity ---
	AppID  string `json:"app_id"`
	UUID   string `json:"uuid"`
	Schema int    `json:"schema_version,omitempty"`

	// Identity groups human-readable metadata.
	Identity TangoIdentity `json:"identity"`

	// Links groups all external URLs.
	Links TangoLinks `json:"links"`

	// Sizing declares resource tier constraints using short tier slugs
	// (e.g. "tg-md-1"). Resolved via catalog config path roots.
	Sizing TangoSizing `json:"sizing"`

	// Deployment declares platform and OS compatibility.
	Deployment TangoDeployment `json:"deployment"`

	// Runtime declares the operational contract: secrets, health, ports.
	Runtime TangoRuntime `json:"runtime"`

	// Catalog holds publishing metadata (when added, who maintains, active flag).
	Catalog TangoCatalog `json:"catalog"`
}

// TangoIdentity holds human-readable metadata.
type TangoIdentity struct {
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	// Category short slug, e.g. "operations". Resolved to public/categories/<slug>.
	Category string `json:"category"`
	// License short slug, e.g. "mit". Resolved to public/licenses/<slug>.
	License string `json:"license,omitempty"`
}

// TangoLinks groups all external URLs.
type TangoLinks struct {
	GitHub   string `json:"github,omitempty"`
	Forgejo  string `json:"forgejo,omitempty"`
	Upstream string `json:"upstream,omitempty"`
	Docs     string `json:"docs,omitempty"`
	Issues   string `json:"issues,omitempty"`
	Funding  string `json:"funding,omitempty"`
	Plugin   string `json:"plugin,omitempty"`
}

// TangoSizing declares resource tier constraints.
// Slugs resolve via catalog config: "tg-md-1" → "public/sizing/tg-md-1".
type TangoSizing struct {
	Min         string `json:"min"`
	Recommended string `json:"recommended"`
	Max         string `json:"max"`
}

// TangoDeployment declares where and how this app can be deployed.
type TangoDeployment struct {
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

// TangoRuntime declares the operational contract.
type TangoRuntime struct {
	// BaoSecretsPath is the path template for this app's secrets.
	// {deployment} is interpolated at enrollment time.
	// e.g. "kontango/secret/apps/ticketarr/{deployment}"
	BaoSecretsPath string `json:"bao_secrets_path"`

	// SecretRequirements is a typed array replacing the old CSV string.
	// Each entry is a (ref, group) pair. ref uses short slugs:
	//   "composites/database_credentials" → "public/secret-types/composites/..."
	SecretRequirements []SecretRequirement `json:"secret_requirements,omitempty"`

	// Health is the liveness check for this app.
	Health TangoHealth `json:"health,omitempty"`
}

// TangoHealth describes how to probe this app's liveness.
type TangoHealth struct {
	URL            string `json:"url,omitempty"`
	ExpectStatus   int    `json:"expect_status,omitempty"`
}

// TangoCatalog holds publishing metadata.
type TangoCatalog struct {
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
func (b *Tango) Validate() error {
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


// MarshalJSON produces the canonical blueprint.json representation.
func (b *Tango) MarshalJSON() ([]byte, error) {
	type Alias Tango
	b.Schema = TangoSchemaVersion
	return json.Marshal((*Alias)(b))
}

// UnmarshalJSON reads a blueprint.json file.
func (b *Tango) UnmarshalJSON(data []byte) error {
	type Alias Tango
	var a Alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*b = Tango(a)
	return nil
}

// JSON returns the canonical JSON bytes for this blueprint.
func (b *Tango) JSON() ([]byte, error) {
	return json.MarshalIndent(b, "", "  ")
}

