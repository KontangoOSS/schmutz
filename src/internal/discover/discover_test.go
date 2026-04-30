package discover_test

import (
	"testing"

	"github.com/KontangoOSS/schmutz/internal/discover"
)

func TestScanLocalhost_ReturnsResults(t *testing.T) {
	targets, err := discover.ScanLocalhost([]uint16{19999, 29999, 39999})
	if err != nil {
		t.Fatalf("ScanLocalhost: unexpected error: %v", err)
	}
	_ = targets
}

func TestScanLocalhost_CommonPorts(t *testing.T) {
	targets, err := discover.ScanLocalhost(discover.CommonPorts)
	if err != nil {
		t.Fatalf("ScanLocalhost: unexpected error: %v", err)
	}
	_ = targets
}
