package config

import (
	"strings"
	"testing"
)

func TestLoad_RequiredVars(t *testing.T) {
	t.Setenv("BAO_ADDR", "https://bao.example:8200")
	t.Setenv("BAO_TOKEN", "test-token")
	t.Setenv("ZITI_API", "https://ziti.example:443")
	t.Setenv("ZITI_USERNAME", "admin")
	t.Setenv("ZITI_PASSWORD", "test-pw")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if c.BaoAddr != "https://bao.example:8200" {
		t.Errorf("BaoAddr = %q, want %q", c.BaoAddr, "https://bao.example:8200")
	}
	if c.ListenAddr != "127.0.0.1:8765" {
		t.Errorf("ListenAddr default = %q, want 127.0.0.1:8765", c.ListenAddr)
	}
}

func TestLoad_MissingRequired(t *testing.T) {
	// Unset all required vars; we want to verify that ALL get reported.
	t.Setenv("BAO_ADDR", "")
	t.Setenv("BAO_TOKEN", "")
	t.Setenv("ZITI_API", "")
	t.Setenv("ZITI_USERNAME", "")
	t.Setenv("ZITI_PASSWORD", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing vars")
	}
	for _, want := range []string{"BAO_ADDR", "BAO_TOKEN", "ZITI_API", "ZITI_USERNAME", "ZITI_PASSWORD"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err.Error(), want)
		}
	}
}

func TestLoad_PartialMissing(t *testing.T) {
	// Set most but not all; verify ONLY the missing one is reported.
	t.Setenv("BAO_ADDR", "https://bao.example:8200")
	t.Setenv("BAO_TOKEN", "test-token")
	t.Setenv("ZITI_API", "")
	t.Setenv("ZITI_USERNAME", "admin")
	t.Setenv("ZITI_PASSWORD", "test-pw")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing ZITI_API")
	}
	if !strings.Contains(err.Error(), "ZITI_API") {
		t.Errorf("error %q should mention ZITI_API", err.Error())
	}
	for _, present := range []string{"BAO_ADDR", "BAO_TOKEN", "ZITI_USERNAME", "ZITI_PASSWORD"} {
		if strings.Contains(err.Error(), present) {
			t.Errorf("error %q should not mention %s (it's set)", err.Error(), present)
		}
	}
}

func TestLoad_BaoDefaults(t *testing.T) {
	t.Setenv("BAO_ADDR", "https://bao.example:8200")
	t.Setenv("BAO_TOKEN", "test-token")
	t.Setenv("ZITI_API", "https://ziti.example:443")
	t.Setenv("ZITI_USERNAME", "admin")
	t.Setenv("ZITI_PASSWORD", "test-pw")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if c.BaoMount != "secret" {
		t.Errorf("BaoMount default = %q, want secret", c.BaoMount)
	}
	if c.BaoTokenPrefix != "enroll-tokens" {
		t.Errorf("BaoTokenPrefix default = %q, want enroll-tokens", c.BaoTokenPrefix)
	}
}

func TestLoad_SchmutzDefaults(t *testing.T) {
	envSetMinimum(t)
	t.Setenv("SCHMUTZ_LISTEN_ADDR", "")
	t.Setenv("SCHMUTZ_ZITI_IDENTITY_FILE", "")
	t.Setenv("SCHMUTZ_SERVICE_NAME", "")
	t.Setenv("SCHMUTZ_BOOTSTRAP", "")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.SchmutzListenAddr != "127.0.0.1:8766" {
		t.Errorf("SchmutzListenAddr = %q", c.SchmutzListenAddr)
	}
	if c.SchmutzServiceName != "admin.tango" {
		t.Errorf("SchmutzServiceName = %q", c.SchmutzServiceName)
	}
	if c.SchmutzZitiIdentityFile != "/etc/schmutz/server-identity.json" {
		t.Errorf("SchmutzZitiIdentityFile = %q", c.SchmutzZitiIdentityFile)
	}
	if c.SchmutzBootstrap {
		t.Error("SchmutzBootstrap should default false")
	}
}

func TestLoad_SchmutzBootstrapMode(t *testing.T) {
	// Bootstrap mode does NOT require Bao or Ziti env vars.
	t.Setenv("BAO_ADDR", "")
	t.Setenv("BAO_TOKEN", "")
	t.Setenv("ZITI_API", "")
	t.Setenv("ZITI_USERNAME", "")
	t.Setenv("ZITI_PASSWORD", "")
	t.Setenv("SCHMUTZ_BOOTSTRAP", "1")

	c, err := Load()
	if err != nil {
		t.Fatalf("bootstrap mode Load failed: %v", err)
	}
	if !c.SchmutzBootstrap {
		t.Error("SchmutzBootstrap should be true")
	}
}

func envSetMinimum(t *testing.T) {
	t.Helper()
	t.Setenv("BAO_ADDR", "https://bao.tango:8200")
	t.Setenv("BAO_TOKEN", "tok")
	t.Setenv("ZITI_API", "https://ctrl.tango:1280")
	t.Setenv("ZITI_USERNAME", "admin")
	t.Setenv("ZITI_PASSWORD", "pw")
	t.Setenv("SCHMUTZ_BOOTSTRAP", "")
}
