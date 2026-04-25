package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// Probe runs the requested layers and returns a combined Result. Layers
// without sufficient privilege are silently skipped — their absence shows
// up in Result.Skipped.
type Probe struct {
	// Layers selects which collection layers to run. Use AllLayers() for the
	// full picture, LocalLayers() for an offline-safe set, or a custom list
	// like []Layer{LayerL0, LayerL1} for unprivileged callers.
	Layers []Layer

	// L4Cfg controls L4 (network) probes. Ignored if LayerL4 not in Layers.
	L4Cfg L4Config
}

// Run executes the probe and returns the merged result.
func (p Probe) Run(ctx context.Context) *Result {
	r := New()
	r.Privilege = DetectPrivilege()

	requested := p.Layers
	if len(requested) == 0 {
		requested = LocalLayers()
	}

	for _, layer := range requested {
		switch layer {
		case LayerL0:
			collectL0(r)
			r.Layers = append(r.Layers, LayerL0)
		case LayerL1:
			collectL1(r)
			r.Layers = append(r.Layers, LayerL1)
		case LayerL2:
			if r.Privilege.IsRoot {
				collectL2(r)
				r.Layers = append(r.Layers, LayerL2)
			} else {
				r.Skipped = append(r.Skipped, LayerL2)
			}
		case LayerL3:
			if r.Privilege.IsRoot {
				collectL3(r)
				r.Layers = append(r.Layers, LayerL3)
			} else {
				r.Skipped = append(r.Skipped, LayerL3)
			}
		case LayerL4:
			collectL4(ctx, r, p.L4Cfg)
			r.Layers = append(r.Layers, LayerL4)
		}
	}

	// Class is derived from L0+L1+L2 evidence — must run after layers above.
	if cls := DetectClass(r); cls != nil {
		r.Set("class", string(cls.Class), LayerL0)
		r.Set("class_evidence", cls, LayerL0)
	}

	// Hardware hash — kept compatible with the v0.3.0 inputs (MACs+serial+machine_id).
	r.Set("hardware_hash", computeHWHash(r), LayerL0)

	return r
}

// computeHWHash matches the v0.3.0 algorithm: lowercased MACs (sorted) +
// serial + machine_id, sha256, first 16 hex chars. Stable across re-enrollment
// for already-known devices.
func computeHWHash(r *Result) string {
	var macs []string
	if v, ok := r.Get("macs"); ok {
		if sl, ok := v.([]string); ok {
			macs = append(macs, sl...)
		}
	}
	sort.Strings(macs)

	serial := stringFact(r, "dmi_product_serial")
	if serial == "" {
		serial = stringFact(r, "dmi_board_serial")
	}
	machineID := stringFact(r, "machine_id")

	h := sha256.New()
	h.Write([]byte(strings.Join(macs, ",")))
	h.Write([]byte("|"))
	h.Write([]byte(serial))
	h.Write([]byte("|"))
	h.Write([]byte(machineID))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func stringFact(r *Result, key string) string {
	v, ok := r.Get(key)
	if !ok {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
