// Package detect provides a pipeline step that identifies the OS, CPU
// architecture, platform type, and hostname of the current machine.
package detect

import (
	"os"
	"runtime"
	"strings"

	"github.com/KontangoOSS/schmutz/internal/pipeline"
	"github.com/pterm/pterm"
)

// Step detects machine properties and populates the shared pipeline context.
type Step struct{}

// New returns a ready-to-use Detect step.
func New() *Step {
	return &Step{}
}

// Name returns the human-readable step label.
func (s *Step) Name() string { return "Detect Machine" }

// Skip always returns false — detection must run every time.
func (s *Step) Skip(_ *pipeline.Context) bool { return false }

// Run populates ctx.OS, ctx.Arch, ctx.Hostname, and ctx.Platform.
func (s *Step) Run(ctx *pipeline.Context) error {
	ctx.OS = runtime.GOOS
	ctx.Arch = runtime.GOARCH

	hostname, err := os.Hostname()
	if err != nil {
		return err
	}
	ctx.Hostname = hostname

	ctx.Platform = detectPlatform()

	pterm.Info.Printf("os=%s arch=%s platform=%s hostname=%s\n",
		ctx.OS, ctx.Arch, ctx.Platform, ctx.Hostname)
	return nil
}

// detectPlatform probes well-known files and flags to classify the execution
// environment. Priority order: docker → lxc → cloud → vm → baremetal.
func detectPlatform() string {
	// Docker: control file written by the container runtime.
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return "docker"
	}

	// LXC: the init process environment contains container=lxc.
	if data, err := os.ReadFile("/proc/1/environ"); err == nil {
		// environ entries are NUL-separated.
		for _, entry := range strings.Split(string(data), "\x00") {
			if entry == "container=lxc" {
				return "lxc"
			}
		}
	}

	// Cloud / VM: DMI product name is a reliable indicator on most kernels.
	if data, err := os.ReadFile("/sys/class/dmi/id/product_name"); err == nil {
		name := strings.ToLower(strings.TrimSpace(string(data)))
		switch {
		case strings.Contains(name, "droplet"):
			return "cloud" // DigitalOcean
		case strings.Contains(name, "google compute"):
			return "cloud" // GCP
		case strings.Contains(name, "amazon ec2"):
			return "cloud" // AWS
		case strings.Contains(name, "virtualbox"),
			strings.Contains(name, "vmware"),
			strings.Contains(name, "kvm"),
			strings.Contains(name, "qemu"):
			return "vm"
		}
	}

	// VM fallback: the CPU "hypervisor" flag is set by most hypervisors even
	// when DMI is unavailable.
	if data, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "flags") && strings.Contains(line, "hypervisor") {
				return "vm"
			}
		}
	}

	return "baremetal"
}
