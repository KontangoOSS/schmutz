package join

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Platform abstracts OS-specific operations for the registration flow.
type Platform interface {
	TangoDir() string
	ZitiBinaryPath() string
	IdentityPath() string
	// Preflight checks all dependencies are met before proceeding.
	// Returns a list of missing dependencies (empty = all good).
	Preflight() []string
	InstallZiti(version string) error
	InstallService(identityFile string) error
	StartService() error
	EnsureDir() error
	// WaitForTunnel polls the tunnel status until it reports healthy or times out.
	// Uses the ziti v2 IPC agent (ziti agent stats).
	WaitForTunnel(timeout time.Duration) error
}

func DetectPlatform() (Platform, error) {
	switch runtime.GOOS {
	case "linux":
		return &LinuxPlatform{}, nil
	case "darwin":
		return &DarwinPlatform{}, nil
	case "windows":
		return &WindowsPlatform{}, nil
	default:
		return nil, fmt.Errorf("unsupported: %s/%s", runtime.GOOS, runtime.GOARCH)
	}
}

func zitiArchString() string {
	switch runtime.GOARCH {
	case "amd64":
		return "amd64"
	case "arm64":
		return "arm64"
	case "arm":
		return "arm"
	default:
		return runtime.GOARCH
	}
}

func zitiOSString() string {
	switch runtime.GOOS {
	case "darwin":
		return "darwin"
	case "windows":
		return "windows"
	default:
		return "linux"
	}
}

func binaryExtension() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func defaultTangoDir() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("ProgramData"), "tango")
	case "darwin":
		return "/usr/local/tango"
	default:
		return "/opt/tango"
	}
}
