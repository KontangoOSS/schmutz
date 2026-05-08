package shared

import "regexp"

// Shared regex patterns and common types used across Blueprint and Substrate.

var (
	// zitiIdentityPattern: "machine-" + 8 lowercase hex chars.
	zitiIdentityPattern = regexp.MustCompile(`^machine-[0-9a-f]{8}$`)

	// slugPattern: lowercase alnum + dashes, no leading/trailing dash.
	// Used for tenant, app, deployment names.
	slugPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

	// groupPattern: like slug but also allows underscores.
	// Used for Bao secret group names (e.g. "site_url", "secret_key").
	groupPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9_-]*[a-z0-9])?$`)

	// hostnamePattern: .tango overlay hostname.
	hostnamePattern = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)+tango$`)
)

// SecretRequirement is one entry in a blueprint's secret_requirements list.
// Short ref format:
//   "composites/database_credentials" → public/secret-types/composites/database_credentials
//   "primitives/symmetric_key"        → public/secret-types/primitives/symmetric_key
//
// Group is the path segment under kontango/secret/apps/<app>/<deployment>/<group>/.
type SecretRequirement struct {
	Ref   string `json:"ref"   yaml:"ref"`
	Group string `json:"group" yaml:"group"`
}
