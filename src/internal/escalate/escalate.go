package escalate

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/pterm/pterm"

	"github.com/KontangoOSS/schmutz/internal/pipeline"
)

// Step implements the escalate bootstrap step.
type Step struct{}

// New returns a new escalate Step.
func New() *Step {
	return &Step{}
}

// Name returns the display name of this step.
func (s *Step) Name() string {
	return "Escalate Privileges"
}

// Skip returns true when already running as root.
func (s *Step) Skip(ctx *pipeline.Context) bool {
	return os.Getuid() == 0
}

// Run checks if we are root and re-execs with sudo if not.
func (s *Step) Run(ctx *pipeline.Context) error {
	pterm.Warning.Println("This bootstrap requires root privileges. Re-executing with sudo...")

	// Find sudo in PATH
	sudoPath, err := exec.LookPath("sudo")
	if err != nil {
		return fmt.Errorf("sudo not found in PATH: %w", err)
	}

	// Re-execute with sudo, preserving all arguments and environment
	args := append([]string{sudoPath, "-E", "--"}, os.Args...)
	if err := syscall.Exec(sudoPath, args, os.Environ()); err != nil {
		return fmt.Errorf("failed to re-exec with sudo: %w", err)
	}

	// This line should never be reached if Exec succeeds
	return nil
}
