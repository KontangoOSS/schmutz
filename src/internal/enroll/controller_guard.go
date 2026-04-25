package enroll

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// IsControllerNode reports whether this machine is running a Ziti controller
// or router managed by the Kontango stack. The schmutz agent must never be
// installed on a controller node — controllers manage their own Ziti identities,
// and running a separate agent there would create conflicts.
//
// Detection probes both filesystem markers (controller config files, router
// config files at the standard Kontango paths) and live process state via
// systemctl. Either signal triggers a positive result.
func IsControllerNode() bool {
	for _, pattern := range []string{
		"/opt/kontango/ziti/ctrl-*/ctrl-*/ctrl.yaml",
		"/etc/kontango/ziti/router/config.yaml",
	} {
		if matches, _ := filepath.Glob(pattern); len(matches) > 0 {
			return true
		}
	}
	for _, unit := range []string{"kontango-ziti-controller", "kontango-ziti-router"} {
		if out, err := exec.Command("systemctl", "is-active", unit).Output(); err == nil {
			if strings.TrimSpace(string(out)) == "active" {
				return true
			}
		}
	}
	return false
}
