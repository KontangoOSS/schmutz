package service_test

import (
	"strings"
	"testing"

	"github.com/KontangoOSS/schmutz/internal/pipeline"
	"github.com/KontangoOSS/schmutz/internal/service"
)

// TestName verifies the step returns the correct display name.
func TestName(t *testing.T) {
	s := service.New()
	if s.Name() != "Setup Service" {
		t.Errorf("expected %q, got %q", "Setup Service", s.Name())
	}
}

// TestSkipsWhenNotRegistered verifies Skip returns true when ctx.Registered
// is false.
func TestSkipsWhenNotRegistered(t *testing.T) {
	s := service.New()
	ctx := pipeline.NewContext()
	ctx.Registered = false
	if !s.Skip(ctx) {
		t.Error("expected Skip to return true when ctx.Registered is false")
	}
}

// TestDoesNotSkipWhenRegistered verifies Skip returns false when
// ctx.Registered is true.
func TestDoesNotSkipWhenRegistered(t *testing.T) {
	s := service.New()
	ctx := pipeline.NewContext()
	ctx.Registered = true
	if s.Skip(ctx) {
		t.Error("expected Skip to return false when ctx.Registered is true")
	}
}

// TestUnitContainsRestart verifies GenerateUnit output contains "Restart=always".
func TestUnitContainsRestart(t *testing.T) {
	unit := service.GenerateUnit("/usr/local/bin/schmutz", "ctrl.konoss.org")
	if !strings.Contains(unit, "Restart=always") {
		t.Error("expected unit to contain 'Restart=always'")
	}
}

// TestUnitContainsDomain verifies GenerateUnit includes the specified domain.
func TestUnitContainsDomain(t *testing.T) {
	domain := "my.domain"
	unit := service.GenerateUnit("/usr/local/bin/schmutz", domain)
	if !strings.Contains(unit, domain) {
		t.Errorf("expected unit to contain domain %q", domain)
	}
}

// TestUnitContainsWaitRole verifies GenerateUnit output contains "wait-role".
func TestUnitContainsWaitRole(t *testing.T) {
	unit := service.GenerateUnit("/usr/local/bin/schmutz", "ctrl.konoss.org")
	if !strings.Contains(unit, "wait-role") {
		t.Error("expected unit to contain 'wait-role'")
	}
}
