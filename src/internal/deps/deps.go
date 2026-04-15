// Package deps implements the dependency-check bootstrap step.
// It reads a manifest of required and optional binaries, packages, and kernel
// features embedded at compile time, and reports missing items.
package deps

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"

	"github.com/KontangoOSS/schmutz/internal/pipeline"
	"github.com/pterm/pterm"
	"gopkg.in/yaml.v3"
)

//go:embed manifest.yaml
var manifestBytes []byte

// Manifest is the top-level structure for the embedded YAML.
type Manifest struct {
	Binaries       []BinaryDep        `yaml:"binaries"`
	Packages       []PackageDep       `yaml:"packages"`
	KernelFeatures []KernelFeatureDep `yaml:"kernel_features"`
}

// BinaryDep describes a required or optional binary.
type BinaryDep struct {
	Name     string `yaml:"name"`
	Required bool   `yaml:"required"`
}

// PackageDep describes a required or optional system package.
type PackageDep struct {
	Name     string `yaml:"name"`
	Required bool   `yaml:"required"`
}

// KernelFeatureDep describes a required or optional kernel feature.
type KernelFeatureDep struct {
	Name     string `yaml:"name"`
	Check    string `yaml:"check"`
	Module   string `yaml:"module"`
	Required bool   `yaml:"required"`
}

// Step implements the deps bootstrap step.
type Step struct{}

// New returns a new deps Step.
func New() *Step {
	return &Step{}
}

// Name returns the display name of this step.
func (s *Step) Name() string {
	return "Check Dependencies"
}

// Skip returns true when dependencies have already been verified this session.
func (s *Step) Skip(ctx *pipeline.Context) bool {
	return ctx.DepsOK
}

// Run checks all entries in the embedded manifest and returns an error if any
// required dependency is absent. Missing optional dependencies emit a warning.
func (s *Step) Run(ctx *pipeline.Context) error {
	m, err := LoadManifest()
	if err != nil {
		return fmt.Errorf("failed to load dependency manifest: %w", err)
	}

	var missing []string

	for _, b := range m.Binaries {
		found, path := CheckBinary(b.Name)
		if found {
			pterm.Debug.Printf("binary %q found at %s\n", b.Name, path)
			continue
		}
		if b.Required {
			missing = append(missing, fmt.Sprintf("binary %q", b.Name))
		} else {
			pterm.Warning.Printf("optional binary %q not found\n", b.Name)
		}
	}

	for _, p := range m.Packages {
		found := checkPackage(p.Name)
		if found {
			pterm.Debug.Printf("package %q is installed\n", p.Name)
			continue
		}
		if p.Required {
			missing = append(missing, fmt.Sprintf("package %q", p.Name))
		} else {
			pterm.Warning.Printf("optional package %q not installed\n", p.Name)
		}
	}

	for _, kf := range m.KernelFeatures {
		found := checkKernelFeature(kf)
		if found {
			pterm.Debug.Printf("kernel feature %q available\n", kf.Name)
			continue
		}
		if kf.Required {
			missing = append(missing, fmt.Sprintf("kernel feature %q", kf.Name))
		} else {
			pterm.Warning.Printf("optional kernel feature %q not available\n", kf.Name)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required dependencies: %v", missing)
	}

	ctx.DepsOK = true
	return nil
}

// LoadManifest parses the embedded manifest.yaml and returns the result.
// Exported so tests can inspect the manifest directly.
func LoadManifest() (*Manifest, error) {
	var m Manifest
	if err := yaml.Unmarshal(manifestBytes, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// CheckBinary uses exec.LookPath to determine whether name exists on PATH.
// Returns (found, resolvedPath). Exported for testing.
func CheckBinary(name string) (bool, string) {
	path, err := exec.LookPath(name)
	if err != nil {
		return false, ""
	}
	return true, path
}

// checkPackage returns true if name is installed according to dpkg or rpm.
func checkPackage(name string) bool {
	// Try dpkg first (Debian/Ubuntu).
	if err := exec.Command("dpkg", "-s", name).Run(); err == nil {
		return true
	}
	// Fall back to rpm (RHEL/Fedora/CentOS).
	if err := exec.Command("rpm", "-q", name).Run(); err == nil {
		return true
	}
	return false
}

// checkKernelFeature returns true if the feature is available.
// It checks kf.Check (filesystem path) first, then kf.Module via modprobe --dry-run.
func checkKernelFeature(kf KernelFeatureDep) bool {
	if kf.Check != "" {
		if _, err := os.Stat(kf.Check); err == nil {
			return true
		}
	}
	if kf.Module != "" {
		if err := exec.Command("modprobe", "--dry-run", kf.Module).Run(); err == nil {
			return true
		}
	}
	return false
}
