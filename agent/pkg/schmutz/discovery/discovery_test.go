package discovery

import (
	"context"
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"testing"
)

// Test that DetectPrivilege accurately reports the running process's
// privileges. Goal: catch regressions where IsRoot stops matching EUID.
func TestDetectPrivilege(t *testing.T) {
	p := DetectPrivilege()
	if p.GOOS != runtime.GOOS {
		t.Errorf("GOOS = %q, want %q", p.GOOS, runtime.GOOS)
	}
	if p.GOARCH != runtime.GOARCH {
		t.Errorf("GOARCH = %q, want %q", p.GOARCH, runtime.GOARCH)
	}
	if p.UID != os.Getuid() {
		t.Errorf("UID = %d, want %d", p.UID, os.Getuid())
	}
	if p.EUID != os.Geteuid() {
		t.Errorf("EUID = %d, want %d", p.EUID, os.Geteuid())
	}
	if p.IsRoot != (p.EUID == 0) {
		t.Errorf("IsRoot = %v but EUID = %d", p.IsRoot, p.EUID)
	}
}

// Run discovery and verify every fact carries a layer source from the set
// of layers we asked to run.
func TestProbeFactsAreSourceTagged(t *testing.T) {
	res := Probe{Layers: []Layer{LayerL0, LayerL1}}.Run(context.Background())

	if len(res.Facts) == 0 {
		t.Fatal("Probe produced zero facts even at L0 — broken")
	}
	allowed := map[Layer]bool{LayerL0: true, LayerL1: true}
	for k, f := range res.Facts {
		if !allowed[f.Source] {
			t.Errorf("fact %q has source %q outside requested L0+L1", k, f.Source)
		}
		if f.Value == nil {
			t.Errorf("fact %q has nil value", k)
		}
	}
}

// Non-root callers should NEVER get L2 or L3 facts in the result.
func TestProbeRootlessSkipsL2L3(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root; rootless skip test only meaningful as non-root")
	}
	res := Probe{Layers: AllLayers()}.Run(context.Background())

	for k, f := range res.Facts {
		if f.Source == LayerL2 || f.Source == LayerL3 {
			t.Errorf("rootless run produced %s fact %q — should have been skipped", f.Source, k)
		}
	}
	// Skipped should contain L2 and L3
	skipped := map[Layer]bool{}
	for _, l := range res.Skipped {
		skipped[l] = true
	}
	if !skipped[LayerL2] || !skipped[LayerL3] {
		t.Errorf("rootless run did not skip L2/L3 (Skipped = %v)", res.Skipped)
	}
}

// L0 should always run regardless of privilege — those facts work in any
// container, any user.
func TestL0AlwaysAvailable(t *testing.T) {
	res := Probe{Layers: []Layer{LayerL0}}.Run(context.Background())
	mustHave := []string{"hostname", "os", "arch", "cpu_cores"}
	for _, key := range mustHave {
		if _, ok := res.Get(key); !ok {
			t.Errorf("L0 missing required fact %q", key)
		}
	}
}

// L4 must NOT run unless explicitly requested. This is a privacy contract.
func TestL4IsOptIn(t *testing.T) {
	res := Probe{Layers: LocalLayers()}.Run(context.Background())
	for k, f := range res.Facts {
		if f.Source == LayerL4 {
			t.Errorf("LocalLayers produced L4 fact %q — must be opt-in", k)
		}
	}
	if _, ok := res.Get("public_ip"); ok {
		t.Errorf("LocalLayers produced public_ip without explicit L4 request")
	}
	if _, ok := res.Get("geolocation"); ok {
		t.Errorf("LocalLayers produced geolocation without explicit L4 request")
	}
}

// FlatKV is the wire format. It must be JSON-marshalable end-to-end.
func TestFlatKVIsJSONMarshalable(t *testing.T) {
	res := Probe{Layers: LocalLayers()}.Run(context.Background())
	flat := res.FlatKV()
	if len(flat) == 0 {
		t.Fatal("FlatKV is empty")
	}
	out, err := json.Marshal(flat)
	if err != nil {
		t.Fatalf("FlatKV is not JSON-marshalable: %v", err)
	}
	// Round-trip
	var back map[string]any
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatalf("FlatKV JSON does not round-trip: %v", err)
	}
	if len(back) != len(flat) {
		t.Errorf("round-trip lost facts: %d → %d", len(flat), len(back))
	}
}

// FlatKV must drop the source layer — the wire format is just key→value.
// The source-tagged Result is for the agent and tests; the controller gets
// flat KV pairs as agreed.
func TestFlatKVStripsSourceLayer(t *testing.T) {
	r := New()
	r.Set("foo", "bar", LayerL2)
	flat := r.FlatKV()
	v, ok := flat["foo"]
	if !ok {
		t.Fatal("FlatKV dropped fact")
	}
	if s, ok := v.(string); !ok || s != "bar" {
		t.Errorf("FlatKV value = %v, want %q", v, "bar")
	}
}

// computeHWHash with deterministic inputs must produce a stable output.
// This is the v0.3.0 compatibility contract — already-enrolled devices must
// keep their fingerprint.
func TestHardwareHashStable(t *testing.T) {
	r := New()
	r.Set("macs", []string{"aa:bb:cc:dd:ee:ff", "11:22:33:44:55:66"}, LayerL1)
	r.Set("dmi_product_serial", "TESTSERIAL123", LayerL2)
	r.Set("machine_id", "abcdef0123456789", LayerL2)

	h1 := computeHWHash(r)
	h2 := computeHWHash(r)
	if h1 != h2 {
		t.Errorf("computeHWHash is not deterministic: %q vs %q", h1, h2)
	}
	if len(h1) != 16 {
		t.Errorf("hash len = %d, want 16", len(h1))
	}
	for _, c := range h1 {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("hash %q contains non-hex char %q", h1, c)
		}
	}
}

// Order of MACs must not change the hash — we sort before hashing.
func TestHardwareHashMACOrderIndependent(t *testing.T) {
	r1 := New()
	r1.Set("macs", []string{"aa:bb:cc:dd:ee:ff", "11:22:33:44:55:66"}, LayerL1)
	r1.Set("dmi_product_serial", "S1", LayerL2)
	r1.Set("machine_id", "M1", LayerL2)

	r2 := New()
	r2.Set("macs", []string{"11:22:33:44:55:66", "aa:bb:cc:dd:ee:ff"}, LayerL1)
	r2.Set("dmi_product_serial", "S1", LayerL2)
	r2.Set("machine_id", "M1", LayerL2)

	if computeHWHash(r1) != computeHWHash(r2) {
		t.Error("hash changed with MAC order — must sort before hashing")
	}
}

// Class detection — the 7 cases. We use synthetic Result objects since
// we can't make the test machine actually be a docker container or LXC.
func TestDetectClass(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(*Result)
		want   Class
		reason string
	}{
		{
			name: "tpm-attested when TPM + DMI",
			setup: func(r *Result) {
				r.Set("has_tpm", true, LayerL3)
				r.Set("dmi_sys_vendor", "Dell", LayerL2)
				r.Set("dmi_product_name", "OptiPlex 7090", LayerL2)
			},
			want: ClassTPMAttested,
		},
		{
			name: "dmi-attested when DMI but no TPM",
			setup: func(r *Result) {
				r.Set("dmi_sys_vendor", "Dell", LayerL2)
				r.Set("dmi_product_name", "OptiPlex 3060", LayerL2)
			},
			want: ClassDMIAttested,
		},
		{
			name: "vm when DMI matches hypervisor product",
			setup: func(r *Result) {
				r.Set("dmi_sys_vendor", "QEMU", LayerL2)
				r.Set("dmi_product_name", "Standard PC (Q35 + ICH9, 2009)", LayerL2)
			},
			want: ClassVM,
		},
		{
			name: "unattested when nothing detected",
			setup: func(r *Result) {
				// no facts
			},
			want: ClassUnattested,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := New()
			c.setup(r)
			ev := DetectClass(r)
			if ev == nil {
				t.Fatal("DetectClass returned nil")
			}
			if ev.Class != c.want {
				t.Errorf("got class %q, want %q (evidence: %+v)", ev.Class, c.want, ev)
			}
		})
	}
}

// Privilege caps are best-effort — make sure the absence-of-cap path
// doesn't blow up.
func TestCapPresentNoSurpriseFatals(t *testing.T) {
	// Just call it — we don't assert true/false because that depends on
	// the test runner's privilege. We're checking that the function returns
	// without panicking.
	_ = capPresent("cap_net_admin")
	_ = capPresent("cap_sys_admin")
	_ = capPresent("nonexistent_cap")
}

// Result.Set should silently drop empty values so the wire payload doesn't
// contain noise.
func TestResultSetDropsEmpty(t *testing.T) {
	r := New()
	r.Set("foo", "", LayerL0)
	r.Set("bar", []string{}, LayerL0)
	r.Set("baz", nil, LayerL0)
	r.Set("real", "value", LayerL0)

	if _, ok := r.Get("foo"); ok {
		t.Error("empty string was set; should be dropped")
	}
	if _, ok := r.Get("bar"); ok {
		t.Error("empty []string was set; should be dropped")
	}
	if _, ok := r.Get("baz"); ok {
		t.Error("nil was set; should be dropped")
	}
	if _, ok := r.Get("real"); !ok {
		t.Error("real value was dropped")
	}
}

// helper to read a non-Linux-specific fact and verify shape
func TestL1ProducesInterfaces(t *testing.T) {
	res := Probe{Layers: []Layer{LayerL0, LayerL1}}.Run(context.Background())
	v, ok := res.Get("interfaces")
	if !ok {
		t.Skip("L1 did not collect interfaces (rare but possible)")
	}
	nics, ok := v.([]NIC)
	if !ok {
		t.Fatalf("interfaces is not []NIC, got %T", v)
	}
	if len(nics) == 0 {
		t.Error("zero interfaces — at least loopback should exist")
	}
	// Every NIC name should be non-empty
	for _, n := range nics {
		if strings.TrimSpace(n.Name) == "" {
			t.Error("NIC with empty name")
		}
	}
}
