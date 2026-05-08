// Package shared holds the wire/disk schemas every component (controller,
// agent, future tooling) reads or writes. One package per system, two
// types currently:
//
//   - Substrate (this file): per-deployment host configuration written
//     by the controller, read by the agent. Stored at
//     <namespace>/secret/apps/<app>/<deployment>/substrate.
//
//   - Blueprint (blueprint.go): per-app catalog metadata written by the
//     operator/automation, read by anyone with public/ access. Stored
//     at public/secret/applications/<app>.
//
// CROSS-REPO CONTRACT: this package is mirrored byte-for-byte between
// kore/ziti-base/server/internal/shared/ and kore/schmutz/src/internal/shared/.
// When changing one repo, change the other in the same PR. Go's internal/
// rule prevents importing across modules, and the agent + controller
// release cadences differ enough that pinning to a shared module would
// create more pain than copy-paste maintenance. Promote to a shared
// module the moment we have a third consumer or the first version-skew
// bug.
//
// SUBSTRATE — COMPLIANCE FOLLOW-UP (documented, not yet implemented):
// for finer-grained ACLs (e.g. operator can read .binds but not .routes),
// the single substrate record will be split into sibling keys at the
// deployment path:
//
//	<namespace>/secret/apps/<app>/<deployment>/substrate.binds
//	<namespace>/secret/apps/<app>/<deployment>/substrate.routes
//	<namespace>/secret/apps/<app>/<deployment>/substrate.posture
//
// Migration path: the Substrate.Version field. v1 = single-record. v2 =
// sibling keys. Readers branch on Version.
package shared

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

// SubstrateSchemaVersion is the wire-format version of Substrate. Bump
// when any field meaning changes; readers should refuse versions they
// don't understand.
const SubstrateSchemaVersion = 1

// Substrate is the full substrate record for one (namespace, app,
// deployment). Tenant + app + deployment are recorded redundantly in the
// body so a leaked/exported substrate can be self-described without its
// Bao path.
type Substrate struct {
	// Version pins the schema. Readers reject mismatched versions rather
	// than silently misinterpreting fields.
	Version int `json:"version" yaml:"version"`

	// Identity — redundant with the Bao path, but lets a leaked spec be
	// self-described. Readers should validate that these match the path
	// they read from to catch corruption / misplacement.
	Tenant     string `json:"tenant" yaml:"tenant"`
	App        string `json:"app" yaml:"app"`
	Deployment string `json:"deployment" yaml:"deployment"`

	// AppRef points at the public blueprint for this app:
	//   public/secret/applications/<app>
	// The agent reads the blueprint to obtain generic info (sizing,
	// dependencies, supported flavors). Optional — if empty, the agent
	// assumes <app> as the blueprint key.
	AppRef string `json:"app_ref,omitempty" yaml:"app_ref,omitempty"`

	// EntityID is the Bao identity-engine entity id this deployment is
	// bound to. Recorded for audit + drift detection. Optional.
	EntityID string `json:"entity_id,omitempty" yaml:"entity_id,omitempty"`

	// ZitiIdentity is the ziti identity name expected to host this
	// substrate. The agent on a host should validate that its own
	// identity matches before applying. Mismatch = misplaced substrate.
	ZitiIdentity string `json:"ziti_identity" yaml:"ziti_identity"`

	// IncludeBase: when true, the agent prepends the base Layer 1 binds
	// (ssh-<hostname>.tango → :22) before the declared Binds. This saves
	// every app blueprint from repeating the SSH bind explicitly. Defaults
	// to true; set to false only for special cases (e.g. an SSH-less
	// container or a machine where SSH is on a non-standard port).
	IncludeBase *bool `json:"include_base,omitempty" yaml:"include_base,omitempty"`

	// Binds: overlay services this host binds beyond the base layer.
	// The SSH bind is always prepended unless IncludeBase is false.
	// Order is informative; reconciler may apply in any order.
	Binds []Bind `json:"binds" yaml:"binds"`

	// Routes (optional): HTTP/L7 routes the local Caddy should expose,
	// terminating TLS for the listed overlay hostnames and forwarding
	// to a local upstream. Empty for hosts that only do raw TCP binds
	// (e.g. SSH-only).
	Routes []Route `json:"routes,omitempty" yaml:"routes,omitempty"`

	// Posture: minimum local conditions the agent self-checks before
	// declaring readiness. The agent does NOT enforce these against
	// dialers — that's the ziti control plane's job via posture-checks
	// attached to dial policies. The fields here mirror the same names
	// for operator clarity.
	Posture *Posture `json:"posture,omitempty" yaml:"posture,omitempty"`
}

// Bind declares one overlay service this host binds.
type Bind struct {
	// Service is the ziti service name (e.g. "inventree.tango").
	Service string `json:"service" yaml:"service"`

	// LocalAddr is the host:port the agent forwards to. Conventionally
	// 127.0.0.1:<port>; loopback-only is recommended so the only path
	// to the app is via the overlay.
	LocalAddr string `json:"local_addr" yaml:"local_addr"`

	// Proto is "tcp" or "udp". Default "tcp" if empty.
	Proto string `json:"proto,omitempty" yaml:"proto,omitempty"`
}

// Route declares one HTTP route fronted by the agent's local Caddy.
type Route struct {
	// Host is the overlay hostname Caddy answers for (e.g.
	// "inventree.tango"). Must match a Bind.Service for traffic to
	// actually reach this Route — the bind sources packets, the route
	// is what Caddy does with them after TLS termination.
	Host string `json:"host" yaml:"host"`

	// Upstream is the URL Caddy forwards to (e.g. "http://127.0.0.1:8000").
	Upstream string `json:"upstream" yaml:"upstream"`

	// TLS: if true, Caddy issues/uses a cert for Host. For overlay-only
	// services we typically use a ziti-issued cert, but plain HTTP
	// inside the overlay is also valid; default false.
	TLS bool `json:"tls,omitempty" yaml:"tls,omitempty"`
}

// Posture is the self-check section. Empty/nil = no minimums declared.
// Fields not specified are not checked.
type Posture struct {
	// MinOS pins the operating system family/version. Free-form; the
	// agent compares against runtime.GOOS / /etc/os-release. Empty = any.
	MinOS string `json:"min_os,omitempty" yaml:"min_os,omitempty"`

	// RequireProcesses is a list of processes (by absolute path) that
	// MUST be running on this host before the agent declares readiness.
	// Common entry: "/usr/local/bin/ziti-edge-tunnel".
	RequireProcesses []string `json:"require_processes,omitempty" yaml:"require_processes,omitempty"`

	// RequireFiles is a list of paths (with optional sha512) that MUST
	// exist + match. Empty hash = existence-only check.
	RequireFiles []FileCheck `json:"require_files,omitempty" yaml:"require_files,omitempty"`
}

// FileCheck is one file-existence + optional integrity check.
type FileCheck struct {
	Path   string `json:"path" yaml:"path"`
	Sha512 string `json:"sha512,omitempty" yaml:"sha512,omitempty"`
}

// EffectiveBinds returns the full bind list the agent should apply:
// Layer 1 (base SSH bind) followed by the declared app-specific binds.
// When IncludeBase is explicitly false, only the declared binds are returned.
//
// The SSH service name follows the convention ssh-<hostname>.tango where
// hostname is derived from the deployment slug. Callers that need to override
// the SSH bind (non-standard port, different hostname) should set IncludeBase
// to false and include their own SSH bind in Binds.
func (s *Substrate) EffectiveBinds() []Bind {
	if s.IncludeBase != nil && !*s.IncludeBase {
		return s.Binds
	}
	// Layer 1: SSH is always first — it's the baseline admin access path.
	// Service name: ssh-<deployment>.tango (e.g. ssh-prod-01.tango for a
	// deployment named prod-01, ssh-ref-ctrl-1.tango for ref-ctrl-1).
	sshBind := Bind{
		Service:   "ssh-" + s.Deployment + ".tango",
		LocalAddr: "127.0.0.1:22",
		Proto:     "tcp",
	}
	// Avoid duplicating the SSH bind if the operator already declared it.
	for _, b := range s.Binds {
		if b.Service == sshBind.Service {
			return s.Binds // operator's bind takes precedence
		}
	}
	return append([]Bind{sshBind}, s.Binds...)
}

// ----- validation -----


// Validate returns nil if s is internally consistent and within the
// MVP's accepted shape. Errors are informative; an operator should be
// able to fix the spec from the message alone.
func (s *Substrate) Validate() error {
	if s == nil {
		return errors.New("substrate: nil spec")
	}
	if s.Version != SubstrateSchemaVersion {
		return fmt.Errorf("substrate: unsupported version %d (this build expects %d)", s.Version, SubstrateSchemaVersion)
	}
	if !slugPattern.MatchString(s.Tenant) {
		return fmt.Errorf("substrate: tenant %q is not a valid slug", s.Tenant)
	}
	if !slugPattern.MatchString(s.App) {
		return fmt.Errorf("substrate: app %q is not a valid slug", s.App)
	}
	if !slugPattern.MatchString(s.Deployment) {
		return fmt.Errorf("substrate: deployment %q is not a valid slug", s.Deployment)
	}
	if !zitiIdentityPattern.MatchString(s.ZitiIdentity) {
		return fmt.Errorf("substrate: ziti_identity %q does not match machine-XXXXXXXX", s.ZitiIdentity)
	}
	if len(s.Binds) == 0 {
		return errors.New("substrate: at least one bind is required")
	}
	bindHosts := map[string]struct{}{}
	for i, b := range s.Binds {
		if err := b.Validate(); err != nil {
			return fmt.Errorf("substrate: binds[%d]: %w", i, err)
		}
		if _, dup := bindHosts[b.Service]; dup {
			return fmt.Errorf("substrate: binds[%d]: duplicate service %q", i, b.Service)
		}
		bindHosts[b.Service] = struct{}{}
	}
	for i, r := range s.Routes {
		if err := r.Validate(); err != nil {
			return fmt.Errorf("substrate: routes[%d]: %w", i, err)
		}
		// Every Route.Host must correspond to a Bind.Service — otherwise
		// the route refers to traffic that won't be sourced.
		if _, ok := bindHosts[r.Host]; !ok {
			return fmt.Errorf("substrate: routes[%d]: host %q has no matching bind", i, r.Host)
		}
	}
	if s.Posture != nil {
		if err := s.Posture.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Validate checks one Bind.
func (b Bind) Validate() error {
	if !hostnamePattern.MatchString(b.Service) {
		return fmt.Errorf("service %q must be a .tango hostname (e.g. inventree.tango)", b.Service)
	}
	if b.LocalAddr == "" {
		return errors.New("local_addr required")
	}
	host, port, err := net.SplitHostPort(b.LocalAddr)
	if err != nil {
		return fmt.Errorf("local_addr %q: %w", b.LocalAddr, err)
	}
	if host == "" {
		return fmt.Errorf("local_addr %q: missing host (use 127.0.0.1 for loopback-only)", b.LocalAddr)
	}
	if port == "" {
		return fmt.Errorf("local_addr %q: missing port", b.LocalAddr)
	}
	switch strings.ToLower(b.Proto) {
	case "", "tcp", "udp":
	default:
		return fmt.Errorf("proto %q: must be tcp or udp", b.Proto)
	}
	return nil
}

// Validate checks one Route.
func (r Route) Validate() error {
	if !hostnamePattern.MatchString(r.Host) {
		return fmt.Errorf("host %q must be a .tango hostname", r.Host)
	}
	if r.Upstream == "" {
		return errors.New("upstream required")
	}
	if !strings.HasPrefix(r.Upstream, "http://") && !strings.HasPrefix(r.Upstream, "https://") {
		return fmt.Errorf("upstream %q must start with http:// or https://", r.Upstream)
	}
	return nil
}

// Validate checks Posture.
func (p *Posture) Validate() error {
	for i, fc := range p.RequireFiles {
		if fc.Path == "" {
			return fmt.Errorf("posture.require_files[%d]: path required", i)
		}
		if !strings.HasPrefix(fc.Path, "/") {
			return fmt.Errorf("posture.require_files[%d]: path %q must be absolute", i, fc.Path)
		}
		if fc.Sha512 != "" && len(fc.Sha512) != 128 {
			return fmt.Errorf("posture.require_files[%d]: sha512 %q must be 128 hex chars", i, fc.Sha512)
		}
	}
	for i, p := range p.RequireProcesses {
		if !strings.HasPrefix(p, "/") {
			return fmt.Errorf("posture.require_processes[%d]: %q must be absolute path", i, p)
		}
	}
	return nil
}

// MatchesPath returns nil if the substrate's identity fields match the
// Bao path it was loaded from. Used to detect misplaced records.
//
// expectedTenant/expectedApp/expectedDeployment come from the path the
// reader actually fetched, not from the body.
func (s *Substrate) MatchesPath(expectedTenant, expectedApp, expectedDeployment string) error {
	if s.Tenant != expectedTenant {
		return fmt.Errorf("substrate: tenant %q in body doesn't match path %q", s.Tenant, expectedTenant)
	}
	if s.App != expectedApp {
		return fmt.Errorf("substrate: app %q in body doesn't match path %q", s.App, expectedApp)
	}
	if s.Deployment != expectedDeployment {
		return fmt.Errorf("substrate: deployment %q in body doesn't match path %q", s.Deployment, expectedDeployment)
	}
	return nil
}
