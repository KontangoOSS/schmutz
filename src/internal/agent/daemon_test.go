package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestNewDaemonFailsWithoutIdentity verifies that NewDaemon returns an error
// when the identity file path does not exist on disk.
func TestNewDaemonFailsWithoutIdentity(t *testing.T) {
	cfg := DefaultConfig()
	cfg.IdentityPath = "/tmp/schmutz-test-does-not-exist-identity.json"

	// Ensure the file really isn't there.
	os.Remove(cfg.IdentityPath)

	_, err := NewDaemon(cfg)
	if err == nil {
		t.Fatal("expected error for non-existent identity path, got nil")
	}
}

// TestNewDaemonSucceeds verifies that NewDaemon returns a valid daemon when
// the identity path points to a file that exists.
func TestNewDaemonSucceeds(t *testing.T) {
	dir := t.TempDir()
	idPath := filepath.Join(dir, "identity.json")
	if err := os.WriteFile(idPath, []byte(`{}`), 0600); err != nil {
		t.Fatalf("failed to create temp identity file: %v", err)
	}

	cfg := DefaultConfig()
	cfg.IdentityPath = idPath

	d, err := NewDaemon(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d == nil {
		t.Fatal("expected non-nil daemon")
	}
}

// TestDefaultConfig verifies that DefaultConfig returns the expected defaults.
func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.TelemetryTarget != "telemetry-stream.tango" {
		t.Errorf("TelemetryTarget: got %q, want %q", cfg.TelemetryTarget, "telemetry-stream.tango")
	}
	if cfg.APIPort != 8080 {
		t.Errorf("APIPort: got %d, want 8080", cfg.APIPort)
	}
	if cfg.SSHPort != 22 {
		t.Errorf("SSHPort: got %d, want 22", cfg.SSHPort)
	}
	if cfg.CollectInterval != 5*time.Second {
		t.Errorf("CollectInterval: got %v, want 5s", cfg.CollectInterval)
	}
}

// TestDaemonStartStop creates a daemon with a temp identity, starts it in a
// goroutine, confirms it is running, then stops it and confirms it has stopped.
func TestDaemonStartStop(t *testing.T) {
	dir := t.TempDir()
	idPath := filepath.Join(dir, "identity.json")
	if err := os.WriteFile(idPath, []byte(`{}`), 0600); err != nil {
		t.Fatalf("failed to create temp identity file: %v", err)
	}

	cfg := DefaultConfig()
	cfg.IdentityPath = idPath

	d, err := NewDaemon(cfg)
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}

	runErr := make(chan error, 1)
	go func() {
		runErr <- d.Run()
	}()

	// Wait for the daemon to enter the running state.
	deadline := time.Now().Add(2 * time.Second)
	for !d.IsRunning() {
		if time.Now().After(deadline) {
			t.Fatal("daemon did not become running within 2s")
		}
		time.Sleep(5 * time.Millisecond)
	}

	if !d.IsRunning() {
		t.Error("expected daemon to be running before Stop()")
	}

	d.Stop()

	select {
	case err := <-runErr:
		if err != nil {
			t.Errorf("Run() returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not stop within 2s after Stop()")
	}

	if d.IsRunning() {
		t.Error("expected daemon to be stopped after Stop()")
	}
}

// TestHandleServiceChangeAddsAPI verifies that an Added event for an -api
// service is reflected in ActiveServices.
func TestHandleServiceChangeAddsAPI(t *testing.T) {
	dir := t.TempDir()
	idPath := filepath.Join(dir, "identity.json")
	if err := os.WriteFile(idPath, []byte(`{}`), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	cfg := DefaultConfig()
	cfg.IdentityPath = idPath
	d, err := NewDaemon(cfg)
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}

	d.HandleServiceChange(ServiceEvent{Name: "telemetry-api.tango", Added: true})

	svcs := d.ActiveServices()
	if len(svcs) != 1 || svcs[0] != "telemetry-api.tango" {
		t.Errorf("expected [telemetry-api.tango], got %v", svcs)
	}
}

// TestHandleServiceChangeRemoves verifies that a removal event deletes the
// service from ActiveServices.
func TestHandleServiceChangeRemoves(t *testing.T) {
	dir := t.TempDir()
	idPath := filepath.Join(dir, "identity.json")
	if err := os.WriteFile(idPath, []byte(`{}`), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	cfg := DefaultConfig()
	cfg.IdentityPath = idPath
	d, err := NewDaemon(cfg)
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}

	d.HandleServiceChange(ServiceEvent{Name: "mgmt-ssh.tango", Added: true})
	d.HandleServiceChange(ServiceEvent{Name: "mgmt-ssh.tango", Added: false})

	svcs := d.ActiveServices()
	if len(svcs) != 0 {
		t.Errorf("expected empty ActiveServices after removal, got %v", svcs)
	}
}
