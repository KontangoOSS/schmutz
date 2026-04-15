package pipeline_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/KontangoOSS/schmutz/internal/pipeline"
)

// mockStep is a test helper that lets tests inject run and skip behaviour.
type mockStep struct {
	name   string
	runFn  func(ctx *pipeline.Context) error
	skipFn func(ctx *pipeline.Context) bool
}

func (m *mockStep) Name() string { return m.name }

func (m *mockStep) Run(ctx *pipeline.Context) error {
	if m.runFn != nil {
		return m.runFn(ctx)
	}
	return nil
}

func (m *mockStep) Skip(ctx *pipeline.Context) bool {
	if m.skipFn != nil {
		return m.skipFn(ctx)
	}
	return false
}

// TestPipelineRunsAllSteps — two steps both run in order.
func TestPipelineRunsAllSteps(t *testing.T) {
	order := []string{}

	step1 := &mockStep{
		name:  "step-one",
		runFn: func(_ *pipeline.Context) error { order = append(order, "step-one"); return nil },
	}
	step2 := &mockStep{
		name:  "step-two",
		runFn: func(_ *pipeline.Context) error { order = append(order, "step-two"); return nil },
	}

	p := pipeline.New([]pipeline.Step{step1, step2})
	ctx := pipeline.NewContext()

	if err := p.Run(ctx); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(order) != 2 {
		t.Fatalf("expected 2 steps to run, got %d", len(order))
	}
	if order[0] != "step-one" || order[1] != "step-two" {
		t.Fatalf("unexpected order: %v", order)
	}
}

// TestPipelineSkipsStep — first step skipped, second runs.
func TestPipelineSkipsStep(t *testing.T) {
	ran := []string{}

	step1 := &mockStep{
		name:   "skipped",
		skipFn: func(_ *pipeline.Context) bool { return true },
		runFn:  func(_ *pipeline.Context) error { ran = append(ran, "skipped"); return nil },
	}
	step2 := &mockStep{
		name:  "ran",
		runFn: func(_ *pipeline.Context) error { ran = append(ran, "ran"); return nil },
	}

	p := pipeline.New([]pipeline.Step{step1, step2})
	ctx := pipeline.NewContext()

	if err := p.Run(ctx); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(ran) != 1 || ran[0] != "ran" {
		t.Fatalf("expected only 'ran' to execute, got: %v", ran)
	}
}

// TestPipelineStopsOnError — first fails, second never runs, error includes step name.
func TestPipelineStopsOnError(t *testing.T) {
	secondRan := false

	step1 := &mockStep{
		name:  "failing-step",
		runFn: func(_ *pipeline.Context) error { return errors.New("boom") },
	}
	step2 := &mockStep{
		name:  "should-not-run",
		runFn: func(_ *pipeline.Context) error { secondRan = true; return nil },
	}

	p := pipeline.New([]pipeline.Step{step1, step2})
	ctx := pipeline.NewContext()

	err := p.Run(ctx)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "failing-step") {
		t.Fatalf("error message should contain step name 'failing-step', got: %v", err)
	}
	if secondRan {
		t.Fatal("second step should not have run after first failed")
	}
}

// TestContextCarriesState — first step sets OS field, second reads it.
func TestContextCarriesState(t *testing.T) {
	step1 := &mockStep{
		name:  "set-os",
		runFn: func(ctx *pipeline.Context) error { ctx.OS = "linux"; return nil },
	}

	var readOS string
	step2 := &mockStep{
		name:  "read-os",
		runFn: func(ctx *pipeline.Context) error { readOS = ctx.OS; return nil },
	}

	p := pipeline.New([]pipeline.Step{step1, step2})
	ctx := pipeline.NewContext()

	if err := p.Run(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if readOS != "linux" {
		t.Fatalf("expected OS='linux', got '%s'", readOS)
	}
}
