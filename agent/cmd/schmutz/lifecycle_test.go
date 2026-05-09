package main

import (
	"strings"
	"testing"
)

// The unit template is a contract — operators expect specific lines to be
// present and unchanged. Test the substitution without touching real
// /etc/systemd/.
func TestSystemdUnitTemplateSubstitution(t *testing.T) {
	content := systemdUnitTemplate
	content = strings.ReplaceAll(content, "{{BIN}}", "/usr/local/bin/schmutz")
	content = strings.ReplaceAll(content, "{{COMMAND}}", "start")
	content = strings.ReplaceAll(content, "{{EXTRA}}", "")

	mustContain := []string{
		"[Unit]",
		"Description=Schmutz Ziti agent",
		"After=network-online.target",
		"[Service]",
		"ExecStart=/usr/local/bin/schmutz start",
		"Restart=on-failure",
		"[Install]",
		"WantedBy=multi-user.target",
	}
	for _, s := range mustContain {
		if !strings.Contains(content, s) {
			t.Errorf("unit template missing line: %q", s)
		}
	}

	// Should not contain any unsubstituted placeholders
	for _, ph := range []string{"{{BIN}}", "{{COMMAND}}", "{{EXTRA}}"} {
		if strings.Contains(content, ph) {
			t.Errorf("unit template still has placeholder: %s", ph)
		}
	}
}

func TestSystemdUnitTemplateTproxyMode(t *testing.T) {
	content := systemdUnitTemplate
	content = strings.ReplaceAll(content, "{{BIN}}", "/usr/local/bin/schmutz")
	content = strings.ReplaceAll(content, "{{COMMAND}}", "tunnel tproxy")
	content = strings.ReplaceAll(content, "{{EXTRA}}",
		"AmbientCapabilities=CAP_NET_ADMIN CAP_NET_BIND_SERVICE\nCapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_BIND_SERVICE\n")

	mustContain := []string{
		"ExecStart=/usr/local/bin/schmutz tunnel tproxy",
		"AmbientCapabilities=CAP_NET_ADMIN",
		"CapabilityBoundingSet=CAP_NET_ADMIN",
	}
	for _, s := range mustContain {
		if !strings.Contains(content, s) {
			t.Errorf("tproxy unit missing: %q", s)
		}
	}
}
