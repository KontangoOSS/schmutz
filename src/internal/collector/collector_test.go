package collector

import (
	"testing"

	"github.com/KontangoOSS/schmutz/internal/stream"
)

func TestCollectSystemPopulated(t *testing.T) {
	data, err := CollectSystem()
	if err != nil {
		t.Fatalf("CollectSystem() error: %v", err)
	}
	if data == nil {
		t.Fatal("CollectSystem() returned nil")
	}
	if data.Hostname == "" {
		t.Error("Hostname is empty")
	}
	if data.NumCPU <= 0 {
		t.Errorf("NumCPU = %d, want > 0", data.NumCPU)
	}
}

func TestCollectNetworkHasInterfaces(t *testing.T) {
	data, err := CollectNetwork()
	if err != nil {
		t.Fatalf("CollectNetwork() error: %v", err)
	}
	if data == nil {
		t.Fatal("CollectNetwork() returned nil")
	}
	if len(data.Interfaces) == 0 {
		t.Error("expected at least one non-loopback interface, got none")
	}
	for _, iface := range data.Interfaces {
		if iface.Name == "lo" {
			t.Errorf("loopback interface 'lo' should be excluded")
		}
	}
}

func TestCollectDiskHasMounts(t *testing.T) {
	data, err := CollectDisk()
	if err != nil {
		t.Fatalf("CollectDisk() error: %v", err)
	}
	if data == nil {
		t.Fatal("CollectDisk() returned nil")
	}
	if len(data.Mounts) == 0 {
		t.Error("expected at least one disk mount, got none")
	}
	for _, m := range data.Mounts {
		if m.Total == 0 {
			t.Errorf("mount %q has Total=0, should have been skipped", m.Path)
		}
	}
}

func TestCollectAllReturnsAllTypes(t *testing.T) {
	result := CollectAll()

	keys := []uint8{stream.MsgSystem, stream.MsgNetwork, stream.MsgDisk, stream.MsgProcess}
	for _, k := range keys {
		if _, ok := result[k]; !ok {
			t.Errorf("CollectAll() missing key 0x%02x (%s)", k, stream.MsgName(k))
		}
	}
}
