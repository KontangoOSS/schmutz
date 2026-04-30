package discover_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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

// Task 2: HTTP OpenAPI probe
func TestProbeOpenAPI_NotFound(t *testing.T) {
	spec, err := discover.ProbeOpenAPI("127.0.0.1", 19998, false)
	if err != nil {
		t.Fatalf("ProbeOpenAPI: expected nil error for closed port, got: %v", err)
	}
	if spec != nil {
		t.Fatalf("ProbeOpenAPI: expected nil spec for closed port")
	}
}

// Task 3: Stub generator
func TestGenerateStub_SSH(t *testing.T) {
	stub := discover.GenerateStub(discover.ServiceTarget{Port: 22, Protocol: "ssh", Host: "127.0.0.1"}, "my-machine")
	if stub == nil {
		t.Fatal("GenerateStub: expected non-nil stub for SSH")
	}
	if stub.Info.Title == "" {
		t.Error("GenerateStub: expected non-empty title")
	}
}

func TestGenerateStub_HTTP(t *testing.T) {
	stub := discover.GenerateStub(discover.ServiceTarget{Port: 8080, Protocol: "http", Host: "127.0.0.1"}, "my-machine")
	if stub == nil {
		t.Fatal("GenerateStub: expected non-nil stub for HTTP")
	}
}

func TestGenerateStub_Unknown(t *testing.T) {
	stub := discover.GenerateStub(discover.ServiceTarget{Port: 9999, Protocol: "unknown", Host: "127.0.0.1"}, "my-machine")
	if stub == nil {
		t.Fatal("GenerateStub: expected non-nil stub even for unknown protocol")
	}
}

// Task 4: Discovery result assembly
func TestBuildResult_EmptyTargets(t *testing.T) {
	result := discover.BuildResult([]discover.ServiceTarget{}, "test-slug")
	if result == nil {
		t.Fatal("BuildResult: expected non-nil result even with no targets")
	}
	if result.Slug != "test-slug" {
		t.Errorf("BuildResult: expected slug 'test-slug', got %q", result.Slug)
	}
	if len(result.Services) != 0 {
		t.Errorf("BuildResult: expected 0 services, got %d", len(result.Services))
	}
}

func TestBuildResult_WithTargets(t *testing.T) {
	targets := []discover.ServiceTarget{
		{Port: 22, Protocol: "ssh", Host: "127.0.0.1"},
		{Port: 8080, Protocol: "http", Host: "127.0.0.1"},
	}
	result := discover.BuildResult(targets, "mybox")
	if len(result.Services) != 2 {
		t.Fatalf("BuildResult: expected 2 services, got %d", len(result.Services))
	}
	if result.Services[0].Port != 22 {
		t.Errorf("BuildResult: expected port 22 first, got %d", result.Services[0].Port)
	}
}

// Task 5: Publisher
func TestPublishLocal_WritesFile(t *testing.T) {
	dir := t.TempDir()
	result := &discover.DiscoveryResult{Slug: "test", Services: []discover.ServiceSchema{}}
	p := discover.NewPublisher(dir, "", "")
	if err := p.PublishLocal(result); err != nil {
		t.Fatalf("PublishLocal: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "schema.json"))
	if err != nil {
		t.Fatalf("schema.json not written: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("schema.json is empty")
	}
}

// Task 6: Discoverer background goroutine
func TestDiscoverer_StopsCleanly(t *testing.T) {
	dir := t.TempDir()
	d := discover.NewDiscoverer(dir, "", "")
	done := make(chan struct{})
	go func() {
		d.Run()
		close(done)
	}()
	d.Stop()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Discoverer.Run() did not stop within 5s after Stop()")
	}
}
