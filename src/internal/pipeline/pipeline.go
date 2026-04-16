// Package pipeline provides a sequential step runner for the schmutz bootstrap process.
package pipeline

import (
	"fmt"

	"github.com/pterm/pterm"
)

// Step is the interface every bootstrap step must satisfy.
type Step interface {
	// Name returns a short human-readable identifier used in log output.
	Name() string
	// Run executes the step. Return a non-nil error to abort the pipeline.
	Run(ctx *Context) error
	// Skip returns true when the step should be silently bypassed.
	Skip(ctx *Context) bool
}

// Context carries shared state between pipeline steps.
type Context struct {
	Interactive bool
	OS          string
	Arch        string
	Platform    string
	Hostname    string
	Domain      string
	DepsOK      bool
	Identity    string
	Registered  bool
	JWT         string // enrollment JWT returned by the /enroll endpoint
	Tier        string // device tier returned by the /enroll endpoint (e.g. "sandbox")
}

// NewContext returns a Context with sensible defaults.
func NewContext() *Context {
	return &Context{
		Domain: "ctrl.konoss.org",
	}
}

// Pipeline runs a fixed list of Steps in order.
type Pipeline struct {
	steps []Step
}

// New creates a Pipeline from the provided steps.
func New(steps []Step) *Pipeline {
	return &Pipeline{steps: steps}
}

// Run executes each step in sequence. A skipped step is logged and bypassed.
// The first failing step aborts the run and returns an error that includes the
// step name.
func (p *Pipeline) Run(ctx *Context) error {
	for _, step := range p.steps {
		if step.Skip(ctx) {
			pterm.Info.Printf("skipping step: %s\n", step.Name())
			continue
		}

		pterm.Info.Printf("running step: %s\n", step.Name())
		if err := step.Run(ctx); err != nil {
			pterm.Error.Printf("step %s failed: %v\n", step.Name(), err)
			return fmt.Errorf("step %s: %w", step.Name(), err)
		}
		pterm.Success.Printf("step %s complete\n", step.Name())
	}
	return nil
}
