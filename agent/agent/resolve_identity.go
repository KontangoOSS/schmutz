package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// zitiTunnelDir is where ziti-edge-tunnel writes operational identities.
// Overridable from tests.
var zitiTunnelDir = "/etc/ziti"

// resolveZitiIdentityPath returns the best Ziti identity file to use for
// data plane operations (hosting overlay services via ZitiHost).
//
// Resolution order:
//  1. <zitiTunnelDir>/machine-*.json — operational tunnel identity installed
//     by ziti-edge-tunnel on LAN hosts. Already connected, already has the
//     right router topology. Preferred when present.
//  2. schmutzDir/identity.json — hub-enrolled identity. Used on DO/cloud
//     hosts that have direct connectivity to the ref-cluster routers.
//
// The Bao identity (schmutzDir/identity.json) is always used separately
// for the bao-jwt flow regardless of which identity is chosen here.
func resolveZitiIdentityPath(schmutzDir string) string {
	matches, err := filepath.Glob(filepath.Join(zitiTunnelDir, "machine-*.json"))
	if err == nil {
		for _, path := range matches {
			if isValidZitiIdentity(path) {
				return path
			}
		}
	}
	return filepath.Join(schmutzDir, "identity.json")
}

// isValidZitiIdentity returns true if the file exists and contains a
// parseable Ziti identity with a non-empty ztAPI field.
func isValidZitiIdentity(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var id struct {
		ZtAPI string `json:"ztAPI"`
	}
	if err := json.Unmarshal(data, &id); err != nil {
		return false
	}
	return id.ZtAPI != ""
}
