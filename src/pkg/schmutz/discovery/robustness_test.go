package discovery

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestProbeNeverPanics — discovery must never panic, regardless of what the
// host does or doesn't have. Catch any new fact collector that doesn't
// recover from missing /proc, /sys, or sysctl entries.
func TestProbeNeverPanics(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Probe.Run panicked: %v", r)
		}
	}()
	for _, layers := range [][]Layer{
		{LayerL0},
		{LayerL0, LayerL1},
		{LayerL0, LayerL1, LayerL2, LayerL3},
		AllLayers(),
	} {
		_ = Probe{Layers: layers}.Run(context.Background())
	}
}

// TestEmptyResultStillSerializes — even with zero facts collected, JSON
// marshal must not fail. Defends against changes that accidentally make
// FlatKV produce non-marshalable values (channels, funcs, etc.).
func TestEmptyResultStillSerializes(t *testing.T) {
	r := New()
	flat := r.FlatKV()
	if _, err := json.Marshal(flat); err != nil {
		t.Errorf("empty FlatKV failed to marshal: %v", err)
	}
}

// TestNoLayerLeak — facts in Result.Facts must use one of the defined
// Layer values. Catches any collector that writes a typo'd layer.
func TestNoLayerLeak(t *testing.T) {
	res := Probe{Layers: AllLayers()}.Run(context.Background())
	valid := map[Layer]bool{
		LayerL0: true, LayerL1: true, LayerL2: true, LayerL3: true, LayerL4: true,
	}
	for k, f := range res.Facts {
		if !valid[f.Source] {
			t.Errorf("fact %q has invalid source layer %q", k, f.Source)
		}
	}
}

// TestErrorsAreNotFatal — a failing layer probe should record an error
// in Result.Errors and continue, not abort.
func TestErrorsAreNotFatal(t *testing.T) {
	res := Probe{Layers: LocalLayers()}.Run(context.Background())
	// At least L0 must produce facts even if L2 fails (e.g., no DMI on this box)
	if len(res.Facts) == 0 {
		t.Error("Probe produced zero facts — at least L0 should always work")
	}
}

// TestFactValuesAreJSONSafe — every fact value must be JSON-encodable
// without losing information. Prevents future fact types (channels, funcs,
// time.Time without UTC) from sneaking in.
func TestFactValuesAreJSONSafe(t *testing.T) {
	res := Probe{Layers: AllLayers()}.Run(context.Background())
	for k, f := range res.Facts {
		if _, err := json.Marshal(f.Value); err != nil {
			t.Errorf("fact %q is not JSON-marshalable: %v (value=%T)", k, err, f.Value)
		}
	}
}

// TestL4Disabled by default — important security/privacy contract.
// LocalLayers must NOT issue any outbound HTTP request.
func TestL4DefaultIsLocal(t *testing.T) {
	res := Probe{Layers: LocalLayers()}.Run(context.Background())
	wantNotPresent := []string{"public_ip", "geolocation"}
	for _, k := range wantNotPresent {
		if _, ok := res.Get(k); ok {
			t.Errorf("LocalLayers leaked L4 fact %q", k)
		}
	}
}

// TestHostnameAlwaysPresent — schmutz-controller's only hard requirement.
// Even if everything else fails, hostname must be in the payload, even if
// it's just "localhost".
func TestHostnameAlwaysPresent(t *testing.T) {
	res := Probe{Layers: []Layer{LayerL0}}.Run(context.Background())
	hn, ok := res.Get("hostname")
	if !ok {
		t.Fatal("hostname missing")
	}
	s, ok := hn.(string)
	if !ok {
		t.Fatalf("hostname is %T, want string", hn)
	}
	if strings.TrimSpace(s) == "" {
		t.Errorf("hostname is empty/whitespace: %q", s)
	}
}
