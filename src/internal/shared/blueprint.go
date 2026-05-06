package shared

// Blueprint is the per-application catalog record at
//   public/secret/applications/<app_id>
//
// Authoritative schema source: kore/kustodian/openbao/docs/guides/catalog.md
// (the application-catalog design doc). When the catalog adds fields, add
// them here; when this struct gains fields without updating catalog.md,
// the doc is wrong and the code will drift from operator expectations.
//
// On-disk shape: every value in Bao KV v2 is a string. Booleans are
// "true"/"false", integers are decimal strings, CSVs are comma-separated.
// FromKV parses the raw map into typed fields; ToKV writes the inverse so
// programmatic writes round-trip. Bao's storage being string-only is a
// hard constraint; consumers should never see the raw shape outside this
// package.
//
// HOISTING RULE (from catalog.md):
//   - app-wide facts -> Blueprint (this struct)
//   - deployment-type-stable facts -> deployment config
//   - per-version diffs -> release overrides only
// We model only the Blueprint here. Release + deployment-config records
// are separate paths and outside the agent's read scope; pipeline tooling
// will define those if/when needed.

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// BlueprintSchemaVersion is the wire-format version of Blueprint. Bump
// when any field meaning changes; readers should refuse versions they
// don't understand. Currently the on-disk records carry no explicit
// `version` field — v1 is implicit. When we ship v2, we'll add a
// `schema_version=2` key and have FromKV branch on it.
const BlueprintSchemaVersion = 1

// Blueprint is the per-app catalog record. Fields map 1:1 to catalog.md's
// "Blueprint fields" table. Reference fields keep their raw path (e.g.
// "public/categories/operations") — callers resolve lazily.
type Blueprint struct {
	// --- identity ---
	AppID       string `json:"app_id"       yaml:"app_id"`
	UUID        string `json:"uuid"         yaml:"uuid"`
	DisplayName string `json:"display_name" yaml:"display_name"`
	Description string `json:"description"  yaml:"description"`

	// --- categorization ---
	Category        string `json:"category"          yaml:"category"`         // ref -> public/categories/<name>
	UpstreamLicense string `json:"upstream_license"  yaml:"upstream_license"` // ref -> public/licenses/<spdx>

	// --- source URLs ---
	GithubURL        string `json:"github_url"          yaml:"github_url"`
	ForgejoURL       string `json:"forgejo_url"         yaml:"forgejo_url"`
	UpstreamRepo     string `json:"upstream_repo"       yaml:"upstream_repo"`
	UpstreamHomepage string `json:"upstream_homepage"   yaml:"upstream_homepage"`
	UpstreamDocs     string `json:"upstream_docs"       yaml:"upstream_docs"`
	IssuesURL        string `json:"issues_url"          yaml:"issues_url"`
	FundingURL       string `json:"funding_url"         yaml:"funding_url"`
	PluginURL        string `json:"plugin_url"          yaml:"plugin_url"`
	SchmutzFlavorURL string `json:"schmutz_flavor_url"  yaml:"schmutz_flavor_url"`
	LicenseURL       string `json:"license_url,omitempty" yaml:"license_url,omitempty"` // optional: direct LICENSE link

	// --- sizing (refs) ---
	SizingMin         string `json:"sizing_min"          yaml:"sizing_min"`         // ref -> public/sizing/<tier>
	SizingRecommended string `json:"sizing_recommended"  yaml:"sizing_recommended"` // ref
	SizingMax         string `json:"sizing_max"          yaml:"sizing_max"`         // ref

	// --- deployment defaults ---
	DefaultOS                     string   `json:"default_os"                       yaml:"default_os"`                       // ref
	DefaultDeploymentPlatform     string   `json:"default_deployment_platform"      yaml:"default_deployment_platform"`      // ref
	CompatibleDeploymentPlatforms []string `json:"compatible_deployment_platforms"  yaml:"compatible_deployment_platforms"`  // []ref

	// --- runtime contract ---
	BaoSecretsPath     string              `json:"bao_secrets_path"     yaml:"bao_secrets_path"`
	SecretRequirements []SecretRequirement `json:"secret_requirements"  yaml:"secret_requirements"`
	HealthURL          string              `json:"health_url"           yaml:"health_url"`
	HealthExpectStatus int                 `json:"health_expect_status" yaml:"health_expect_status"`
	DockerSupported    bool                `json:"docker_supported"     yaml:"docker_supported"`

	// --- meta ---
	Stargazers         int    `json:"stargazers,omitempty"           yaml:"stargazers,omitempty"`
	UpstreamLatestTag  string `json:"upstream_latest_tag,omitempty"  yaml:"upstream_latest_tag,omitempty"`
	UpstreamLastUpdate string `json:"upstream_last_update,omitempty" yaml:"upstream_last_update,omitempty"`
	CatalogAdded       string `json:"catalog_added"                  yaml:"catalog_added"`         // YYYY-MM-DD
	CatalogMaintainer  string `json:"catalog_maintainer"             yaml:"catalog_maintainer"`    // "kontango" or "awesome-selfhosted"
	Active             bool   `json:"active"                         yaml:"active"`                // gate: deploy-ready vs discovery
	DiscoverySource    string `json:"discovery_source,omitempty"     yaml:"discovery_source,omitempty"`
}

// SecretRequirement is one entry in the secret_requirements CSV. The CSV
// element shape is `<composite-or-primitive-ref>:<group>` where:
//   - Ref is e.g. "public/secret-types/composites/database_credentials"
//   - Group is the path segment under kontango/secret/apps/<app>/<group>/
//     where the runtime secret will live (e.g. "database").
type SecretRequirement struct {
	Ref   string `json:"ref"   yaml:"ref"`
	Group string `json:"group" yaml:"group"`
}

// FromKV parses a raw Bao KV v2 data map (string keys, string values) into
// a Blueprint. Unknown keys are ignored — forward compatibility for fields
// we haven't modeled yet. Empty / missing fields become zero-values.
//
// Errors signal invalid SHAPES of known fields (e.g. unparseable bool or
// int). Empty strings for typed fields parse to zero, not error.
func (b *Blueprint) FromKV(kv map[string]string) error {
	*b = Blueprint{}

	// strings — direct copy
	b.AppID = kv["app_id"]
	b.UUID = kv["uuid"]
	b.DisplayName = kv["display_name"]
	b.Description = kv["description"]
	b.Category = kv["category"]
	b.UpstreamLicense = kv["upstream_license"]
	b.GithubURL = kv["github_url"]
	b.ForgejoURL = kv["forgejo_url"]
	b.UpstreamRepo = kv["upstream_repo"]
	b.UpstreamHomepage = kv["upstream_homepage"]
	b.UpstreamDocs = kv["upstream_docs"]
	b.IssuesURL = kv["issues_url"]
	b.FundingURL = kv["funding_url"]
	b.PluginURL = kv["plugin_url"]
	b.SchmutzFlavorURL = kv["schmutz_flavor_url"]
	b.LicenseURL = kv["license_url"]
	b.SizingMin = kv["sizing_min"]
	b.SizingRecommended = kv["sizing_recommended"]
	b.SizingMax = kv["sizing_max"]
	b.DefaultOS = kv["default_os"]
	b.DefaultDeploymentPlatform = kv["default_deployment_platform"]
	b.BaoSecretsPath = kv["bao_secrets_path"]
	b.HealthURL = kv["health_url"]
	b.UpstreamLatestTag = kv["upstream_latest_tag"]
	b.UpstreamLastUpdate = kv["upstream_last_update"]
	b.CatalogAdded = kv["catalog_added"]
	b.CatalogMaintainer = kv["catalog_maintainer"]
	b.DiscoverySource = kv["discovery_source"]

	// CSV of refs
	if raw := strings.TrimSpace(kv["compatible_deployment_platforms"]); raw != "" {
		for _, p := range strings.Split(raw, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				b.CompatibleDeploymentPlatforms = append(b.CompatibleDeploymentPlatforms, p)
			}
		}
	}

	// CSV of <ref>:<group>
	if raw := strings.TrimSpace(kv["secret_requirements"]); raw != "" {
		for i, item := range strings.Split(raw, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			parts := strings.SplitN(item, ":", 2)
			if len(parts) != 2 {
				return fmt.Errorf("blueprint: secret_requirements[%d] %q must be <ref>:<group>", i, item)
			}
			ref := strings.TrimSpace(parts[0])
			group := strings.TrimSpace(parts[1])
			if ref == "" || group == "" {
				return fmt.Errorf("blueprint: secret_requirements[%d] %q has empty ref or group", i, item)
			}
			b.SecretRequirements = append(b.SecretRequirements, SecretRequirement{Ref: ref, Group: group})
		}
	}

	// bools
	var err error
	if b.Active, err = parseBool(kv["active"]); err != nil {
		return fmt.Errorf("blueprint: active: %w", err)
	}
	if b.DockerSupported, err = parseBool(kv["docker_supported"]); err != nil {
		return fmt.Errorf("blueprint: docker_supported: %w", err)
	}

	// ints
	if b.HealthExpectStatus, err = parseIntOptional(kv["health_expect_status"]); err != nil {
		return fmt.Errorf("blueprint: health_expect_status: %w", err)
	}
	if b.Stargazers, err = parseIntOptional(kv["stargazers"]); err != nil {
		return fmt.Errorf("blueprint: stargazers: %w", err)
	}
	return nil
}

// ToKV is the inverse of FromKV: produce a string-only map suitable for
// writing back to Bao KV v2. Zero values become "" or "false"/"0" as
// appropriate; consumers that round-trip should be aware that empty
// distinct from unset is not preserved.
func (b *Blueprint) ToKV() map[string]string {
	kv := map[string]string{
		"app_id":                          b.AppID,
		"uuid":                            b.UUID,
		"display_name":                    b.DisplayName,
		"description":                     b.Description,
		"category":                        b.Category,
		"upstream_license":                b.UpstreamLicense,
		"github_url":                      b.GithubURL,
		"forgejo_url":                     b.ForgejoURL,
		"upstream_repo":                   b.UpstreamRepo,
		"upstream_homepage":               b.UpstreamHomepage,
		"upstream_docs":                   b.UpstreamDocs,
		"issues_url":                      b.IssuesURL,
		"funding_url":                     b.FundingURL,
		"plugin_url":                      b.PluginURL,
		"schmutz_flavor_url":              b.SchmutzFlavorURL,
		"license_url":                     b.LicenseURL,
		"sizing_min":                      b.SizingMin,
		"sizing_recommended":              b.SizingRecommended,
		"sizing_max":                      b.SizingMax,
		"default_os":                      b.DefaultOS,
		"default_deployment_platform":     b.DefaultDeploymentPlatform,
		"compatible_deployment_platforms": strings.Join(b.CompatibleDeploymentPlatforms, ","),
		"bao_secrets_path":                b.BaoSecretsPath,
		"secret_requirements":             secretReqsToCSV(b.SecretRequirements),
		"health_url":                      b.HealthURL,
		"health_expect_status":            intOrEmpty(b.HealthExpectStatus),
		"docker_supported":                strconv.FormatBool(b.DockerSupported),
		"stargazers":                      intOrEmpty(b.Stargazers),
		"upstream_latest_tag":             b.UpstreamLatestTag,
		"upstream_last_update":            b.UpstreamLastUpdate,
		"catalog_added":                   b.CatalogAdded,
		"catalog_maintainer":              b.CatalogMaintainer,
		"active":                          strconv.FormatBool(b.Active),
		"discovery_source":                b.DiscoverySource,
	}
	return kv
}

// ----- validation -----

var uuidV4Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// groupPattern is permissive of snake_case (e.g. "site_url", "secret_key")
// because that's what real catalog records use. slugPattern (lowercase
// alnum + dashes) is for tenant/app/deployment names and other places
// where strictness is appropriate.
var groupPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9_-]*[a-z0-9])?$`)

// catalogRefPattern matches the "public/<section>/<...>" shape used for
// every cross-reference. Permissive on the tail so platform-tier paths
// like "public/deployment-platforms/proxmox/lxc/medium" pass.
var catalogRefPattern = regexp.MustCompile(`^public/[a-z0-9-]+(/[a-z0-9_.-]+)+$`)

// dateYYYYMMDDPattern accepts both YYYY-MM and YYYY-MM-DD. catalog.md
// allows either for `released`; YYYY-MM-DD is the convention for
// `catalog_added`.
var dateYYYYMMDDPattern = regexp.MustCompile(`^\d{4}-\d{2}(-\d{2})?$`)

// Validate returns nil if b is internally consistent. catalog.md doesn't
// require all fields on every blueprint — discovery entries (active=false)
// can omit health_url, secret_requirements, etc. — so most fields are
// "validate IF non-empty." app_id, uuid, display_name, category, active
// are required.
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
	if b.DisplayName == "" {
		return errors.New("blueprint: display_name required")
	}
	if !catalogRefPattern.MatchString(b.Category) {
		return fmt.Errorf("blueprint: category %q is not a public/<section>/<...> ref", b.Category)
	}
	if b.UpstreamLicense != "" && !catalogRefPattern.MatchString(b.UpstreamLicense) {
		return fmt.Errorf("blueprint: upstream_license %q is not a public/<section>/<...> ref", b.UpstreamLicense)
	}

	// Sizing refs: required on active blueprints; optional on discovery
	// entries (catalog.md doesn't actually require them on discovery,
	// but in practice all real records have them). Validate IF present.
	for name, val := range map[string]string{
		"sizing_min":         b.SizingMin,
		"sizing_recommended": b.SizingRecommended,
		"sizing_max":         b.SizingMax,
		"default_os":         b.DefaultOS,
	} {
		if val != "" && !catalogRefPattern.MatchString(val) {
			return fmt.Errorf("blueprint: %s %q is not a public/<section>/<...> ref", name, val)
		}
	}
	for i, p := range b.CompatibleDeploymentPlatforms {
		if !catalogRefPattern.MatchString(p) {
			return fmt.Errorf("blueprint: compatible_deployment_platforms[%d] %q is not a ref", i, p)
		}
	}

	// Runtime contract: only required on active=true.
	if b.Active {
		if b.BaoSecretsPath == "" {
			return errors.New("blueprint: active=true requires bao_secrets_path")
		}
		if b.HealthURL != "" && (b.HealthExpectStatus < 100 || b.HealthExpectStatus > 599) {
			return fmt.Errorf("blueprint: health_expect_status %d out of HTTP range", b.HealthExpectStatus)
		}
		for i, sr := range b.SecretRequirements {
			if !catalogRefPattern.MatchString(sr.Ref) {
				return fmt.Errorf("blueprint: secret_requirements[%d].ref %q is not a public/<section>/<...> ref", i, sr.Ref)
			}
			if !groupPattern.MatchString(sr.Group) {
				return fmt.Errorf("blueprint: secret_requirements[%d].group %q is not a valid group name", i, sr.Group)
			}
		}
	}

	if b.CatalogAdded != "" && !dateYYYYMMDDPattern.MatchString(b.CatalogAdded) {
		return fmt.Errorf("blueprint: catalog_added %q must be YYYY-MM-DD", b.CatalogAdded)
	}
	return nil
}

// ----- helpers -----

func parseBool(s string) (bool, error) {
	if s == "" {
		return false, nil
	}
	switch strings.ToLower(s) {
	case "true", "t", "1", "yes", "y":
		return true, nil
	case "false", "f", "0", "no", "n":
		return false, nil
	}
	return false, fmt.Errorf("not a boolean: %q", s)
}

func parseIntOptional(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.Atoi(s)
}

func intOrEmpty(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

func secretReqsToCSV(rs []SecretRequirement) string {
	if len(rs) == 0 {
		return ""
	}
	parts := make([]string, len(rs))
	for i, r := range rs {
		parts[i] = r.Ref + ":" + r.Group
	}
	return strings.Join(parts, ",")
}
