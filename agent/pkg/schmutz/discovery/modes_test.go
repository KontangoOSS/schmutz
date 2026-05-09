package discovery

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"testing"
)

// These tests document the contract between the two install modes:
// rootful (full L0-L3 collection) and rootless (L0+L1 only). They run on
// whatever the test machine actually is and assert the right behavior for
// the current privilege level.

// Required-by-controller fact keys we always want to ship even in
// degraded mode. The current schmutz-controller hard-rejects on bad
// hostname; everything else is opportunistic. This list is what we
// promise to provide whenever we have the privilege to read it.
var L0RequiredKeys = []string{
	"hostname",
	"os",
	"arch",
	"cpu_cores",
	"hardware_hash",
}

// Keys that must appear when running as root (L2/L3). If any of these
// are absent on a Linux box with reasonable hardware, something
// regressed in the L2 collector.
var L2ExpectedKeys = []string{
	"machine_id",
	"dmi_sys_vendor",
}

// TestModeRootless asserts that a non-root probe captures the L0 floor
// without any L2/L3 facts.
func TestModeRootless(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("rootless mode test only runs as non-root")
	}
	res := Probe{Layers: AllLayers()}.Run(context.Background())

	for _, k := range L0RequiredKeys {
		if _, ok := res.Get(k); !ok {
			t.Errorf("rootless mode missing required L0 fact %q", k)
		}
	}
	for _, k := range L2ExpectedKeys {
		if _, ok := res.Get(k); ok {
			t.Errorf("rootless mode has L2 fact %q — should have been skipped", k)
		}
	}
	if res.Privilege.IsRoot {
		t.Error("rootless test reports IsRoot=true")
	}
	if !contains(layerStrings(res.Skipped), string(LayerL2)) {
		t.Error("rootless run did not skip L2")
	}
	if !contains(layerStrings(res.Skipped), string(LayerL3)) {
		t.Error("rootless run did not skip L3")
	}
}

// TestModeRootful asserts that a root probe captures L0+L1+L2+L3.
func TestModeRootful(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("rootful mode test only runs as root")
	}
	res := Probe{Layers: AllLayers()}.Run(context.Background())

	for _, k := range L0RequiredKeys {
		if _, ok := res.Get(k); !ok {
			t.Errorf("rootful mode missing required L0 fact %q", k)
		}
	}
	if !res.Privilege.IsRoot {
		t.Error("rootful test reports IsRoot=false")
	}
	// Don't strictly require L2 keys — a Docker container under root
	// won't have DMI. But if any L2 fact is present, log it.
	hasL2 := false
	for _, f := range res.Facts {
		if f.Source == LayerL2 {
			hasL2 = true
			break
		}
	}
	t.Logf("rootful run has L2 facts: %v", hasL2)
	if len(res.Skipped) > 0 {
		t.Errorf("rootful run skipped layers %v — should have run all", res.Skipped)
	}
}

// TestPayloadShape asserts the wire format the agent will send is well-formed
// JSON, has no nil values, and the FlatKV merge is lossless.
func TestPayloadShape(t *testing.T) {
	res := Probe{Layers: LocalLayers()}.Run(context.Background())
	flat := res.FlatKV()

	// Round-trip JSON
	bs, err := json.Marshal(flat)
	if err != nil {
		t.Fatalf("FlatKV marshal: %v", err)
	}
	var back map[string]any
	if err := json.Unmarshal(bs, &back); err != nil {
		t.Fatalf("FlatKV unmarshal: %v", err)
	}

	// All keys present after round-trip
	flatKeys := keys(flat)
	backKeys := keys(back)
	sort.Strings(flatKeys)
	sort.Strings(backKeys)
	if len(flatKeys) != len(backKeys) {
		t.Errorf("round-trip key count: %d → %d", len(flatKeys), len(backKeys))
	}

	// No nil values reach the wire
	for k, v := range flat {
		if v == nil {
			t.Errorf("FlatKV contains nil value for key %q", k)
		}
	}

	// hostname must be a non-empty string — the controller's only hard requirement
	hn, ok := flat["hostname"]
	if !ok {
		t.Error("FlatKV missing hostname")
	} else if s, ok := hn.(string); !ok || s == "" {
		t.Errorf("hostname has wrong shape: %T %v", hn, hn)
	}

	// hardware_hash must be a non-empty string of 16 hex chars
	hh, ok := flat["hardware_hash"]
	if !ok {
		t.Error("FlatKV missing hardware_hash")
	} else if s, ok := hh.(string); !ok || len(s) != 16 {
		t.Errorf("hardware_hash has wrong shape: %T %v", hh, hh)
	}
}

// TestModeProgression asserts that running the same probe twice produces
// identical hardware hashes and substantially identical fact sets. Discovery
// is supposed to be deterministic for the long-tail facts; only L4 / live
// metrics should change.
func TestModeProgression(t *testing.T) {
	r1 := Probe{Layers: LocalLayers()}.Run(context.Background())
	r2 := Probe{Layers: LocalLayers()}.Run(context.Background())

	hh1 := stringFact(r1, "hardware_hash")
	hh2 := stringFact(r2, "hardware_hash")
	if hh1 != hh2 {
		t.Errorf("hardware_hash drifted between runs: %q → %q", hh1, hh2)
	}

	// Class must be stable — we don't want flapping class membership.
	c1 := stringFact(r1, "class")
	c2 := stringFact(r2, "class")
	if c1 != c2 {
		t.Errorf("class drifted between runs: %q → %q", c1, c2)
	}
}

// TestPrivilegeAccuracy: privilege detection must reflect actual euid.
func TestPrivilegeAccuracy(t *testing.T) {
	res := Probe{Layers: []Layer{LayerL0}}.Run(context.Background())
	if res.Privilege.EUID != os.Geteuid() {
		t.Errorf("Privilege.EUID = %d, real = %d", res.Privilege.EUID, os.Geteuid())
	}
	if res.Privilege.IsRoot != (os.Geteuid() == 0) {
		t.Errorf("Privilege.IsRoot mismatched euid")
	}
}

// --- helpers ---

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func layerStrings(ls []Layer) []string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = string(l)
	}
	return out
}
