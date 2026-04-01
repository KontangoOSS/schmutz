package join

import (
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ProbeOS collects OS information for the "os" probe.
func ProbeOS() map[string]interface{} {
	h, _ := os.Hostname()
	tz := ""
	if loc := time.Now().Location(); loc != nil {
		tz = loc.String()
	}
	return map[string]interface{}{
		"type":       "os",
		"hostname":   h,
		"os":         runtime.GOOS,
		"os_version": DetectOSVersion(),
		"arch":       runtime.GOARCH,
		"kernel":     detectKernel(),
		"timezone":   tz,
	}
}

// ProbeHardware collects hardware information for the "hardware" probe.
func ProbeHardware() map[string]interface{} {
	fp, _ := Collect()
	result := map[string]interface{}{
		"type":      "hardware",
		"cpu_cores": runtime.NumCPU(),
	}
	if fp != nil {
		result["cpu_info"] = fp.CPUInfo
		result["machine_id"] = fp.MachineID
		result["serial"] = fp.SerialNumber
		result["hardware_hash"] = fp.HardwareHash
	}
	result["memory_mb"] = detectMemoryMB()
	result["disk_serials"] = detectDiskSerials()
	return result
}

// ProbeNetwork collects network information for the "network" probe.
func ProbeNetwork() map[string]interface{} {
	result := map[string]interface{}{
		"type": "network",
	}

	// Interfaces with IPs
	var interfaces []map[string]interface{}
	var macs []string
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		entry := map[string]interface{}{
			"name": iface.Name,
			"mac":  iface.HardwareAddr.String(),
			"up":   iface.Flags&net.FlagUp != 0,
		}
		if iface.HardwareAddr.String() != "" {
			macs = append(macs, iface.HardwareAddr.String())
		}
		addrs, _ := iface.Addrs()
		var ips []string
		for _, a := range addrs {
			ips = append(ips, a.String())
		}
		entry["ips"] = ips
		interfaces = append(interfaces, entry)
	}
	result["interfaces"] = interfaces
	result["macs"] = macs
	result["dns_servers"] = detectDNSServers()
	result["gateway"] = detectGateway()

	return result
}

// ProbeSystem collects system state for the "system" probe.
func ProbeSystem() map[string]interface{} {
	result := map[string]interface{}{
		"type": "system",
	}

	result["uptime_secs"] = detectUptime()
	result["boot_id"] = readFileTrim("/proc/sys/kernel/random/boot_id")
	result["locale"] = os.Getenv("LANG")
	result["ssh_host_keys"] = detectSSHHostKeys()
	result["open_ports"] = detectOpenPorts()
	result["package_count"] = detectPackageCount()

	return result
}

// RespondToProbe handles any probe request and returns the appropriate data.
func RespondToProbe(probe string) map[string]interface{} {
	switch probe {
	case "os":
		return ProbeOS()
	case "hardware":
		return ProbeHardware()
	case "network":
		return ProbeNetwork()
	case "system":
		return ProbeSystem()
	default:
		return map[string]interface{}{"type": "error", "reason": "unknown probe: " + probe}
	}
}

// --- Detection helpers ---

func detectKernel() string {
	switch runtime.GOOS {
	case "linux", "darwin":
		if out, err := exec.Command("uname", "-r").Output(); err == nil {
			return strings.TrimSpace(string(out))
		}
	case "windows":
		if out, err := exec.Command("cmd", "/c", "ver").Output(); err == nil {
			return strings.TrimSpace(string(out))
		}
	}
	return ""
}

func detectMemoryMB() int {
	switch runtime.GOOS {
	case "linux":
		if data, err := os.ReadFile("/proc/meminfo"); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "MemTotal:") {
					fields := strings.Fields(line)
					if len(fields) >= 2 {
						if kb, err := strconv.Atoi(fields[1]); err == nil {
							return kb / 1024
						}
					}
				}
			}
		}
	case "darwin":
		if out, err := exec.Command("sysctl", "-n", "hw.memsize").Output(); err == nil {
			if bytes, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64); err == nil {
				return int(bytes / 1024 / 1024)
			}
		}
	}
	return 0
}

func detectDiskSerials() []string {
	var serials []string
	switch runtime.GOOS {
	case "linux":
		if out, err := exec.Command("lsblk", "-ndo", "SERIAL").Output(); err == nil {
			for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				s := strings.TrimSpace(line)
				if s != "" {
					serials = append(serials, s)
				}
			}
		}
	}
	return serials
}

func detectDNSServers() []string {
	var servers []string
	if data, err := os.ReadFile("/etc/resolv.conf"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "nameserver ") {
				servers = append(servers, strings.TrimSpace(strings.TrimPrefix(line, "nameserver ")))
			}
		}
	}
	return servers
}

func detectGateway() string {
	switch runtime.GOOS {
	case "linux":
		if out, err := exec.Command("ip", "route", "show", "default").Output(); err == nil {
			fields := strings.Fields(string(out))
			for i, f := range fields {
				if f == "via" && i+1 < len(fields) {
					return fields[i+1]
				}
			}
		}
	case "darwin":
		if out, err := exec.Command("route", "-n", "get", "default").Output(); err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				if strings.Contains(line, "gateway:") {
					return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "gateway:"))
				}
			}
		}
	}
	return ""
}

func detectUptime() int {
	switch runtime.GOOS {
	case "linux":
		if data, err := os.ReadFile("/proc/uptime"); err == nil {
			fields := strings.Fields(string(data))
			if len(fields) > 0 {
				if secs, err := strconv.ParseFloat(fields[0], 64); err == nil {
					return int(secs)
				}
			}
		}
	}
	return 0
}

func detectSSHHostKeys() []string {
	var keys []string
	paths := []string{
		"/etc/ssh/ssh_host_ed25519_key.pub",
		"/etc/ssh/ssh_host_ecdsa_key.pub",
		"/etc/ssh/ssh_host_rsa_key.pub",
	}
	for _, p := range paths {
		if data, err := os.ReadFile(p); err == nil {
			key := strings.TrimSpace(string(data))
			if key != "" {
				keys = append(keys, key)
			}
		}
	}
	return keys
}

func detectOpenPorts() []int {
	var ports []int
	// Quick check common ports
	for _, port := range []int{22, 80, 443, 3000, 5432, 8080, 8200, 8443, 9090} {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 100*time.Millisecond)
		if err == nil {
			conn.Close()
			ports = append(ports, port)
		}
	}
	return ports
}

func detectPackageCount() int {
	switch runtime.GOOS {
	case "linux":
		// dpkg
		if out, err := exec.Command("dpkg-query", "-f", ".\n", "-W").Output(); err == nil {
			return len(strings.Split(strings.TrimSpace(string(out)), "\n"))
		}
		// rpm
		if out, err := exec.Command("rpm", "-qa").Output(); err == nil {
			return len(strings.Split(strings.TrimSpace(string(out)), "\n"))
		}
		// apk
		if out, err := exec.Command("apk", "list", "--installed").Output(); err == nil {
			return len(strings.Split(strings.TrimSpace(string(out)), "\n"))
		}
	}
	return 0
}

func readFileTrim(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
