package escalate_test

import (
	"os"
	"testing"

	"github.com/KontangoOSS/schmutz/internal/escalate"
	"github.com/KontangoOSS/schmutz/internal/pipeline"
)

func TestName(t *testing.T) {
	s := escalate.New()
	if s.Name() != "Escalate Privileges" {
		t.Errorf("expected %q, got %q", "Escalate Privileges", s.Name())
	}
}

func TestSkipsWhenRoot(t *testing.T) {
	s := escalate.New()
	ctx := pipeline.NewContext()

	// This test can only pass when running as root
	if os.Getuid() == 0 {
		if !s.Skip(ctx) {
			t.Error("skip should return true when running as root")
		}
	} else {
		t.Skip("test requires root privileges")
	}
}

func TestDoesNotSkipWhenNotRoot(t *testing.T) {
	s := escalate.New()
	ctx := pipeline.NewContext()

	// This test can only pass when running as non-root
	if os.Getuid() != 0 {
		if s.Skip(ctx) {
			t.Error("skip should return false when not running as root")
		}
	} else {
		t.Skip("test requires non-root execution")
	}
}
