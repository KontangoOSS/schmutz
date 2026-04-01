package classifier

import (
	"net"
	"path/filepath"

	"github.com/KontangoOSS/schmutz/internal/config"
)

// Match checks if a connection matches a rule based on SNI, JA4, and source IP.
func Match(rule *config.Rule, sni, ja4 string, srcIP net.IP) bool {
	// Check SNI pattern
	if rule.SNI != nil {
		if !matchSNI(*rule.SNI, sni) {
			return false
		}
	}

	// Check JA4 allowlist (if set, JA4 must be in the list)
	if len(rule.JA4) > 0 {
		if !contains(rule.JA4, ja4) {
			return false
		}
	}

	// Check JA4 denylist (if set, JA4 must NOT be in the list)
	if len(rule.JA4Not) > 0 {
		if contains(rule.JA4Not, ja4) {
			return false
		}
	}

	// Check source CIDR
	if len(rule.SrcCIDR) > 0 {
		if !matchCIDR(rule.SrcCIDR, srcIP) {
			return false
		}
	}

	return true
}

func matchSNI(pattern, sni string) bool {
	if pattern == "*" {
		return true
	}
	if pattern == "" {
		return sni == ""
	}
	// filepath.Match handles *.share.example.io patterns
	ok, _ := filepath.Match(pattern, sni)
	return ok
}

func matchCIDR(cidrs []string, ip net.IP) bool {
	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}
