package join

import (
	"runtime"
	"testing"
)

func TestDetectPlatform(t *testing.T) {
	p, err := DetectPlatform()
	if err != nil {
		t.Fatalf("DetectPlatform() error: %v", err)
	}
	if p == nil {
		t.Fatal("returned nil")
	}
	switch runtime.GOOS {
	case "linux":
		if _, ok := p.(*LinuxPlatform); !ok {
			t.Errorf("expected *LinuxPlatform, got %T", p)
		}
	case "darwin":
		if _, ok := p.(*DarwinPlatform); !ok {
			t.Errorf("expected *DarwinPlatform, got %T", p)
		}
	case "windows":
		if _, ok := p.(*WindowsPlatform); !ok {
			t.Errorf("expected *WindowsPlatform, got %T", p)
		}
	}
}

func TestPlatformPaths(t *testing.T) {
	p, _ := DetectPlatform()
	if p == nil {
		t.Skip("unsupported")
	}
	if p.TangoDir() == "" {
		t.Error("TangoDir empty")
	}
	if p.ZitiBinaryPath() == "" {
		t.Error("ZitiBinaryPath empty")
	}
	if p.IdentityPath() == "" {
		t.Error("IdentityPath empty")
	}
}
