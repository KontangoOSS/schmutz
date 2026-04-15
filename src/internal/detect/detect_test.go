package detect_test

import (
	"runtime"
	"testing"

	"github.com/KontangoOSS/schmutz/internal/detect"
	"github.com/KontangoOSS/schmutz/internal/pipeline"
)

func newCtx() *pipeline.Context {
	return pipeline.NewContext()
}

func runStep(t *testing.T) *pipeline.Context {
	t.Helper()
	ctx := newCtx()
	s := detect.New()
	if err := s.Run(ctx); err != nil {
		t.Fatalf("Run() returned unexpected error: %v", err)
	}
	return ctx
}

func TestSetsOS(t *testing.T) {
	ctx := runStep(t)
	if ctx.OS != runtime.GOOS {
		t.Errorf("ctx.OS = %q, want %q", ctx.OS, runtime.GOOS)
	}
}

func TestSetsArch(t *testing.T) {
	ctx := runStep(t)
	if ctx.Arch != runtime.GOARCH {
		t.Errorf("ctx.Arch = %q, want %q", ctx.Arch, runtime.GOARCH)
	}
}

func TestSetsPlatform(t *testing.T) {
	valid := map[string]bool{
		"docker":    true,
		"lxc":       true,
		"vm":        true,
		"cloud":     true,
		"baremetal": true,
	}
	ctx := runStep(t)
	if !valid[ctx.Platform] {
		t.Errorf("ctx.Platform = %q, want one of: docker, lxc, vm, cloud, baremetal", ctx.Platform)
	}
}

func TestSetsHostname(t *testing.T) {
	ctx := runStep(t)
	if ctx.Hostname == "" {
		t.Error("ctx.Hostname is empty, want non-empty string")
	}
}

func TestNeverSkips(t *testing.T) {
	s := detect.New()
	ctx := newCtx()
	if s.Skip(ctx) {
		t.Error("Skip() returned true, want false")
	}
}
