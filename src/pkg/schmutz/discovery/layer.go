// Package discovery collects device facts in layered passes — each layer
// represents a privilege tier required to read the underlying source.
//
// L0 facts (hostname, kernel, runtime CPU) work in any container, any user.
// L1 facts (interfaces, DNS, timezone) work as a normal user.
// L2 facts (DMI, disk serials, SSH host keys) need root.
// L3 facts (TPM, raw netlink) need root plus extra capabilities.
// L4 facts (public IP, geolocation) require outbound network and explicitly
// reveal the device's existence to third parties — opt-in only.
//
// Every fact in the output carries the layer it came from, so the controller
// can score trust appropriately. A USB-stick attacker can fake L0 freely; L2+
// signals are harder to fabricate, and TPM (L3) attestation is cryptographic.
package discovery

import (
	"os"
	"runtime"
)

// Layer identifies the privilege tier a fact required to collect.
type Layer string

const (
	LayerL0 Layer = "L0" // universal — any user, any container
	LayerL1 Layer = "L1" // user — non-root local data
	LayerL2 Layer = "L2" // root — privileged local data
	LayerL3 Layer = "L3" // root + caps — TPM, raw devices
	LayerL4 Layer = "L4" // network — outbound probes (opt-in)
)

// AllLayers returns every layer in collection order.
func AllLayers() []Layer {
	return []Layer{LayerL0, LayerL1, LayerL2, LayerL3, LayerL4}
}

// LocalLayers returns layers L0–L3 — everything that doesn't require an
// outbound network probe. Default for unattended enrollment.
func LocalLayers() []Layer {
	return []Layer{LayerL0, LayerL1, LayerL2, LayerL3}
}

// Privilege summarizes what the agent's current process can actually do.
type Privilege struct {
	UID         int    `json:"uid"`
	EUID        int    `json:"euid"`
	GID         int    `json:"gid"`
	IsRoot      bool   `json:"is_root"`
	GOOS        string `json:"goos"`
	GOARCH      string `json:"goarch"`
	HasNetAdmin bool   `json:"has_net_admin,omitempty"` // CAP_NET_ADMIN, when detectable
	HasSysAdmin bool   `json:"has_sys_admin,omitempty"` // CAP_SYS_ADMIN, when detectable
}

// DetectPrivilege returns the current process's privilege summary. Capability
// detection is best-effort and Linux-only.
func DetectPrivilege() Privilege {
	p := Privilege{
		UID:    os.Getuid(),
		EUID:   os.Geteuid(),
		GID:    os.Getgid(),
		GOOS:   runtime.GOOS,
		GOARCH: runtime.GOARCH,
	}
	p.IsRoot = p.EUID == 0
	if runtime.GOOS == "linux" && p.IsRoot {
		// Root almost always has these; absence here means the process was
		// dropped (e.g. via systemd CapabilityBoundingSet=). Best-effort read.
		p.HasNetAdmin = capPresent("cap_net_admin")
		p.HasSysAdmin = capPresent("cap_sys_admin")
	}
	return p
}

// Fact is a single discovered key/value with its source layer. Marshals to
// {"value": ..., "source": "L2"} so the controller can re-tier facts later.
type Fact struct {
	Value  any   `json:"value"`
	Source Layer `json:"source"`
}

// Result is the merged output of one or more layer probes.
type Result struct {
	Privilege Privilege         `json:"privilege"`
	Layers    []Layer           `json:"layers_run"`
	Skipped   []Layer           `json:"layers_skipped,omitempty"`
	Facts     map[string]Fact   `json:"facts"`
	Errors    map[string]string `json:"errors,omitempty"`
}

// New returns an empty Result with maps initialized.
func New() *Result {
	return &Result{
		Facts:  make(map[string]Fact),
		Errors: make(map[string]string),
	}
}

// Set stores a fact with its source layer. Overwrites silently — caller must
// check for existing keys if that matters.
func (r *Result) Set(key string, value any, layer Layer) {
	if value == nil {
		return
	}
	if s, ok := value.(string); ok && s == "" {
		return
	}
	if sl, ok := value.([]string); ok && len(sl) == 0 {
		return
	}
	r.Facts[key] = Fact{Value: value, Source: layer}
}

// Err records a non-fatal error from a layer probe so the controller can see
// what was attempted but failed.
func (r *Result) Err(key string, err error) {
	if err == nil {
		return
	}
	r.Errors[key] = err.Error()
}

// Get returns the typed value for a key plus a found flag.
func (r *Result) Get(key string) (any, bool) {
	f, ok := r.Facts[key]
	if !ok {
		return nil, false
	}
	return f.Value, true
}

// FlatKV returns the facts as a plain map[string]any — what the enrollment
// stream actually wants on the wire. Source layers are dropped; use Result
// directly when source matters.
func (r *Result) FlatKV() map[string]any {
	out := make(map[string]any, len(r.Facts))
	for k, f := range r.Facts {
		out[k] = f.Value
	}
	return out
}
