package agent

import (
	"testing"
)

func TestLocalPortForService(t *testing.T) {
	tests := []struct {
		name     string
		service  string
		wantPort string
		wantOK   bool
	}{
		{"ssh prefix", "ssh-web-1", "127.0.0.1:22", true},
		{"http prefix", "http-web-1", "127.0.0.1:80", true},
		{"https prefix", "https-web-1", "127.0.0.1:443", true},
		{"unknown prefix", "smtp-web-1", "", false},
		{"empty", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := localPortForService(tt.service)
			if ok != tt.wantOK {
				t.Errorf("localPortForService(%q) ok=%v, want %v", tt.service, ok, tt.wantOK)
			}
			if got != tt.wantPort {
				t.Errorf("localPortForService(%q) port=%q, want %q", tt.service, got, tt.wantPort)
			}
		})
	}
}
