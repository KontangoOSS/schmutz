package substrate

import (
	"fmt"
	"strings"

	"github.com/KontangoOSS/schmutz/internal/shared"
)

// planLog returns a multi-line, log-ready description of the
// reconciliation plan implied by a substrate. v1's watcher prints this
// every successful poll; the actual reconciler (separate package) will
// reuse the same formatter when entering dry-run mode.
//
// The format is deliberately verbose-but-scannable: an operator should
// be able to read the log line and know exactly what the agent thinks
// it should be doing without cross-referencing Bao.
func planLog(s *shared.Schmutz) string {
	return planLogWithZone(s, "")
}

func planLogWithZone(s *shared.Schmutz, zone string) string {
	if s == nil {
		return "substrate: plan: <nil>"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "substrate: plan for %s/%s/%s (ziti=%s):\n",
		s.Tenant, s.App, s.Deployment, s.ZitiIdentity)

	// Use EffectiveBinds so the implicit SSH bind is always shown.
	binds := s.EffectiveBinds(zone)
	if len(binds) == 0 {
		fmt.Fprintln(&b, "  binds:   (none)")
	} else {
		fmt.Fprintf(&b, "  binds:   %d service(s)\n", len(binds))
		maxSvc := 0
		for _, bind := range binds {
			if len(bind.Service) > maxSvc {
				maxSvc = len(bind.Service)
			}
		}
		for _, bind := range binds {
			proto := bind.Proto
			if proto == "" {
				proto = "tcp"
			}
			fmt.Fprintf(&b, "    - %-*s -> %s (%s)\n",
				maxSvc, bind.Service, bind.LocalAddr, proto)
		}
	}

	// Routes
	if len(s.Routes) == 0 {
		fmt.Fprintln(&b, "  routes:  (none)")
	} else {
		fmt.Fprintf(&b, "  routes:  %d caddy route(s)\n", len(s.Routes))
		maxHost := 0
		for _, r := range s.Routes {
			if len(r.Host) > maxHost {
				maxHost = len(r.Host)
			}
		}
		for _, r := range s.Routes {
			tls := "no-tls"
			if r.TLS {
				tls = "tls"
			}
			fmt.Fprintf(&b, "    - %-*s -> %s (%s)\n",
				maxHost, r.Host, r.Upstream, tls)
		}
	}

	// Posture
	if s.Posture == nil {
		fmt.Fprintln(&b, "  posture: (none declared)")
	} else {
		fmt.Fprintln(&b, "  posture:")
		if s.Posture.MinOS != "" {
			fmt.Fprintf(&b, "    - min_os: %s\n", s.Posture.MinOS)
		}
		for _, p := range s.Posture.RequireProcesses {
			fmt.Fprintf(&b, "    - require process: %s\n", p)
		}
		for _, fc := range s.Posture.RequireFiles {
			if fc.Sha512 == "" {
				fmt.Fprintf(&b, "    - require file:    %s\n", fc.Path)
			} else {
				fmt.Fprintf(&b, "    - require file:    %s (sha512=%s...)\n",
					fc.Path, fc.Sha512[:16])
			}
		}
	}

	// Dependencies
	if len(s.Dependencies) > 0 {
		fmt.Fprintf(&b, "  deps:    %d declared\n", len(s.Dependencies))
		for _, d := range s.Dependencies {
			opt := ""
			if d.Optional {
				opt = " (optional)"
			}
			ref := d.Ref
			if d.Version != "" {
				ref += "@" + d.Version
			}
			fmt.Fprintf(&b, "    - [%s] %s%s\n", d.Kind, ref, opt)
		}
	}

	fmt.Fprintln(&b, "  >> NO ACTIONS TAKEN (read-only mode)")
	return strings.TrimRight(b.String(), "\n")
}
