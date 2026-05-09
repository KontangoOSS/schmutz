//go:build linux

package discovery

import (
	"os"
	"strings"
)

// capPresent reads /proc/self/status and reports whether the named capability
// (e.g. "cap_net_admin") is in the effective set. Linux-only.
//
// The kernel exposes capabilities as a bitmap in CapEff; mapping bit numbers
// to names requires libcap. To stay zero-dep we read CapEff and compare to
// a tiny built-in name→bit table. Only the caps schmutz actually checks.
func capPresent(name string) bool {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return false
	}
	bit, ok := capBits[name]
	if !ok {
		return false
	}
	mask := uint64(1) << bit
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "CapEff:") {
			hex := strings.TrimSpace(strings.TrimPrefix(line, "CapEff:"))
			val := parseHex64(hex)
			return val&mask != 0
		}
	}
	return false
}

// Capability bit numbers — from <linux/capability.h>. Only the few we care
// about. Keep this list small; the goal is detection, not full coverage.
var capBits = map[string]uint8{
	"cap_chown":     0,
	"cap_net_admin": 12,
	"cap_net_raw":   13,
	"cap_sys_admin": 21,
	"cap_sys_rawio": 17,
}

func parseHex64(h string) uint64 {
	var v uint64
	for i := 0; i < len(h); i++ {
		c := h[i]
		var d uint64
		switch {
		case c >= '0' && c <= '9':
			d = uint64(c - '0')
		case c >= 'a' && c <= 'f':
			d = uint64(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = uint64(c-'A') + 10
		default:
			return 0
		}
		v = v<<4 | d
	}
	return v
}
