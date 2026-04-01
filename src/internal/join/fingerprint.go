package join

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Fingerprint holds system identifiers collected from standard OS commands.
type Fingerprint struct {
	Hostname      string   `json:"hostname"`
	OS            string   `json:"os"`
	Arch          string   `json:"arch"`
	MACAddrs      []string `json:"mac_addrs"`
	CPUInfo       string   `json:"cpu_info"`
	SerialNumber  string   `json:"serial_number"`
	MachineID     string   `json:"machine_id"`
	KernelVersion string   `json:"kernel_version"`
	HardwareHash  string   `json:"hardware_hash"`
}

// Collect gathers the machine fingerprint using standard OS commands.
func Collect() (*Fingerprint, error) {
	fp := &Fingerprint{
		OS:   runtime.GOOS,
		Arch: runtime.GOARCH,
	}
	fp.Hostname, _ = os.Hostname()

	switch runtime.GOOS {
	case "linux":
		collectLinux(fp)
	case "darwin":
		collectDarwin(fp)
	case "windows":
		collectWindows(fp)
	}

	fp.HardwareHash = computeHash(fp)
	return fp, nil
}

func collectLinux(fp *Fingerprint) {
	// MACs: ip -o link — standard on all modern Linux
	if out, err := exec.Command("ip", "-o", "link").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			// Format: "2: eth0: <...> link/ether aa:bb:cc:dd:ee:ff brd ..."
			if i := strings.Index(line, "link/ether "); i >= 0 {
				mac := strings.Fields(line[i+len("link/ether "):])[0]
				if mac != "00:00:00:00:00:00" {
					fp.MACAddrs = append(fp.MACAddrs, mac)
				}
			}
		}
	}

	// Machine ID
	fp.MachineID = readFileTrimmed("/etc/machine-id")

	// CPU: first "model name" line from /proc/cpuinfo
	if data, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "model name") {
				if parts := strings.SplitN(line, ":", 2); len(parts) == 2 {
					fp.CPUInfo = strings.TrimSpace(parts[1])
					break
				}
			}
		}
	}

	// Serial
	fp.SerialNumber = readFileTrimmed("/sys/class/dmi/id/product_serial")

	// Kernel
	if out, err := exec.Command("uname", "-r").Output(); err == nil {
		fp.KernelVersion = strings.TrimSpace(string(out))
	}
}

func collectDarwin(fp *Fingerprint) {
	// MACs: ifconfig — grab ether lines
	if out, err := exec.Command("ifconfig").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "ether ") {
				mac := strings.Fields(line)[1]
				fp.MACAddrs = append(fp.MACAddrs, mac)
			}
		}
	}

	// CPU
	if out, err := exec.Command("sysctl", "-n", "machdep.cpu.brand_string").Output(); err == nil {
		fp.CPUInfo = strings.TrimSpace(string(out))
	}

	// Serial
	if out, err := exec.Command("ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(line, "IOPlatformSerialNumber") {
				if parts := strings.SplitN(line, "=", 2); len(parts) == 2 {
					fp.SerialNumber = strings.Trim(strings.TrimSpace(parts[1]), `"`)
				}
			}
		}
	}

	// Machine ID — macOS uses IOPlatformUUID
	if out, err := exec.Command("ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(line, "IOPlatformUUID") {
				if parts := strings.SplitN(line, "=", 2); len(parts) == 2 {
					fp.MachineID = strings.Trim(strings.TrimSpace(parts[1]), `"`)
				}
			}
		}
	}

	// Kernel
	if out, err := exec.Command("uname", "-r").Output(); err == nil {
		fp.KernelVersion = strings.TrimSpace(string(out))
	}
}

func collectWindows(fp *Fingerprint) {
	// MACs: getmac /FO CSV /NH
	if out, err := exec.Command("getmac", "/FO", "CSV", "/NH").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Split(line, ",")
			if len(fields) > 0 {
				mac := strings.Trim(strings.TrimSpace(fields[0]), `"`)
				if len(mac) == 17 && mac != "" {
					// Convert XX-XX-XX-XX-XX-XX to xx:xx:xx:xx:xx:xx
					fp.MACAddrs = append(fp.MACAddrs, strings.ToLower(strings.ReplaceAll(mac, "-", ":")))
				}
			}
		}
	}

	// CPU + Serial via wmic
	if out, err := exec.Command("wmic", "cpu", "get", "Name", "/value").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(line, "Name=") {
				fp.CPUInfo = strings.TrimSpace(strings.TrimPrefix(line, "Name="))
			}
		}
	}
	if out, err := exec.Command("wmic", "bios", "get", "SerialNumber", "/value").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(line, "SerialNumber=") {
				fp.SerialNumber = strings.TrimSpace(strings.TrimPrefix(line, "SerialNumber="))
			}
		}
	}

	// Machine ID from registry
	if out, err := exec.Command("reg", "query", `HKLM\SOFTWARE\Microsoft\Cryptography`, "/v", "MachineGuid").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(line, "MachineGuid") {
				fields := strings.Fields(line)
				if len(fields) >= 3 {
					fp.MachineID = fields[len(fields)-1]
				}
			}
		}
	}

	// Kernel
	if out, err := exec.Command("ver").Output(); err == nil {
		fp.KernelVersion = strings.TrimSpace(string(out))
	}
}

func computeHash(fp *Fingerprint) string {
	h := sha256.New()
	for _, mac := range fp.MACAddrs {
		h.Write([]byte(mac))
	}
	h.Write([]byte(fp.SerialNumber))
	h.Write([]byte(fp.MachineID))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func readFileTrimmed(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// DetectOSVersion returns the OS version string using standard commands.
func DetectOSVersion() string {
	switch runtime.GOOS {
	case "linux":
		if data, err := os.ReadFile("/etc/os-release"); err == nil {
			for _, line := range bytes.Split(data, []byte("\n")) {
				if bytes.HasPrefix(line, []byte("PRETTY_NAME=")) {
					return strings.Trim(string(bytes.TrimPrefix(line, []byte("PRETTY_NAME="))), `"`)
				}
			}
		}
	case "darwin":
		if out, err := exec.Command("sw_vers", "-productVersion").Output(); err == nil {
			return "macOS " + strings.TrimSpace(string(out))
		}
	case "windows":
		if out, err := exec.Command("cmd", "/c", "ver").Output(); err == nil {
			return strings.TrimSpace(string(out))
		}
	}
	return runtime.GOOS + "/" + runtime.GOARCH
}
