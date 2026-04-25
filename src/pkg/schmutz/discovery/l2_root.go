package discovery

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// collectL2 gathers root-readable facts: DMI hardware identifiers, disk
// serials, SSH host keys, listening ports, package counts, GPU identifiers.
// Caller is responsible for checking IsRoot before invoking.
func collectL2(r *Result) {
	collectDMI(r)
	collectDiskSerials(r)
	collectSSHHostKeys(r)
	collectListeningPorts(r)
	collectPackageCount(r)
	collectGPU(r)
	collectMachineID(r)
}

// collectDMI reads /sys/class/dmi/id/* — bare-metal hardware identifiers.
// On VMs/containers most fields are blank or contain virt-vendor strings.
func collectDMI(r *Result) {
	fields := []struct{ key, path string }{
		{"dmi_sys_vendor", "/sys/class/dmi/id/sys_vendor"},
		{"dmi_product_name", "/sys/class/dmi/id/product_name"},
		{"dmi_product_serial", "/sys/class/dmi/id/product_serial"},
		{"dmi_product_uuid", "/sys/class/dmi/id/product_uuid"},
		{"dmi_product_version", "/sys/class/dmi/id/product_version"},
		{"dmi_board_vendor", "/sys/class/dmi/id/board_vendor"},
		{"dmi_board_name", "/sys/class/dmi/id/board_name"},
		{"dmi_board_serial", "/sys/class/dmi/id/board_serial"},
		{"dmi_board_version", "/sys/class/dmi/id/board_version"},
		{"dmi_chassis_serial", "/sys/class/dmi/id/chassis_serial"},
		{"dmi_chassis_type", "/sys/class/dmi/id/chassis_type"},
		{"dmi_chassis_vendor", "/sys/class/dmi/id/chassis_vendor"},
		{"dmi_bios_vendor", "/sys/class/dmi/id/bios_vendor"},
		{"dmi_bios_version", "/sys/class/dmi/id/bios_version"},
		{"dmi_bios_date", "/sys/class/dmi/id/bios_date"},
	}
	for _, f := range fields {
		val := readFileTrimmed(f.path)
		if val == "" || isPlaceholderValue(val) {
			continue
		}
		r.Set(f.key, val, LayerL2)
	}
}

// collectDiskSerials reads /sys/block/*/device/serial — stable hardware IDs
// for physical disks. Loops, ramdisks, and floppy/dm devices are skipped.
func collectDiskSerials(r *Result) {
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		r.Err("disk_serials", err)
		return
	}
	type diskInfo struct {
		Name   string `json:"name"`
		Model  string `json:"model,omitempty"`
		Serial string `json:"serial,omitempty"`
		Size   string `json:"size_blocks,omitempty"`
	}
	var disks []diskInfo
	var serials []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") || strings.HasPrefix(name, "dm-") {
			continue
		}
		di := diskInfo{Name: name}
		di.Model = readFileTrimmed(filepath.Join("/sys/block", name, "device/model"))
		di.Serial = readFileTrimmed(filepath.Join("/sys/block", name, "device/serial"))
		di.Size = readFileTrimmed(filepath.Join("/sys/block", name, "size"))
		disks = append(disks, di)
		if di.Serial != "" {
			serials = append(serials, di.Serial)
		}
	}
	if len(disks) > 0 {
		r.Set("disks", disks, LayerL2)
	}
	if len(serials) > 0 {
		r.Set("disk_serials", serials, LayerL2)
	}
}

// collectSSHHostKeys reads the public SSH host keys. These are stable cryptographic
// machine identifiers — much harder to spoof than serials.
func collectSSHHostKeys(r *Result) {
	var keys []string
	for _, p := range []string{
		"/etc/ssh/ssh_host_ed25519_key.pub",
		"/etc/ssh/ssh_host_rsa_key.pub",
		"/etc/ssh/ssh_host_ecdsa_key.pub",
	} {
		if data, err := os.ReadFile(p); err == nil {
			k := strings.TrimSpace(string(data))
			if k != "" {
				keys = append(keys, k)
			}
		}
	}
	if len(keys) > 0 {
		r.Set("ssh_host_keys", keys, LayerL2)
	}
}

// collectListeningPorts reads /proc/net/tcp and /proc/net/tcp6 to find
// listening sockets without shelling out to ss/netstat.
func collectListeningPorts(r *Result) {
	ports := make(map[int]bool)
	for _, p := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for i, line := range strings.Split(string(data), "\n") {
			if i == 0 || line == "" {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 4 {
				continue
			}
			// state 0A = LISTEN
			if fields[3] != "0A" {
				continue
			}
			// local address is "AABBCCDD:PPPP" — port is hex
			if i := strings.LastIndex(fields[1], ":"); i >= 0 {
				portHex := fields[1][i+1:]
				if port := parseHexInt(portHex); port > 0 {
					ports[port] = true
				}
			}
		}
	}
	if len(ports) > 0 {
		out := make([]int, 0, len(ports))
		for p := range ports {
			out = append(out, p)
		}
		r.Set("listening_ports", out, LayerL2)
	}
}

// collectPackageCount counts installed packages by sniffing common pkg DB
// locations. Cheap signal that fingerprints "is this a fresh OS or a real
// workload box?" without needing a package manager binary.
func collectPackageCount(r *Result) {
	candidates := []struct {
		path   string
		method string // "lines" or "files"
	}{
		{"/var/lib/dpkg/status", "dpkg-status"},
		{"/var/lib/rpm/Packages", "rpm-bdb"},
		{"/var/lib/pacman/local", "pacman-dir"},
		{"/var/db/pkg", "portage-dir"},
	}
	for _, c := range candidates {
		switch c.method {
		case "dpkg-status":
			if data, err := os.ReadFile(c.path); err == nil {
				count := strings.Count(string(data), "\nPackage: ")
				if count > 0 {
					r.Set("package_count", count, LayerL2)
					r.Set("package_manager", "dpkg", LayerL2)
					return
				}
			}
		case "pacman-dir", "portage-dir":
			if entries, err := os.ReadDir(c.path); err == nil && len(entries) > 0 {
				r.Set("package_count", len(entries), LayerL2)
				r.Set("package_manager", strings.SplitN(c.method, "-", 2)[0], LayerL2)
				return
			}
		}
	}
}

// collectGPU reads /sys/class/drm/card*/device/{vendor,device,uevent}.
// Returns one GPU entry per discrete card. PCI IDs only — no driver-loaded
// metrics, those need GPU-vendor libraries.
func collectGPU(r *Result) {
	entries, err := os.ReadDir("/sys/class/drm")
	if err != nil {
		return
	}
	type gpuInfo struct {
		Card     string `json:"card"`
		Vendor   string `json:"vendor_id,omitempty"`   // PCI vendor (0x10de = NVIDIA, 0x1002 = AMD, 0x8086 = Intel)
		Device   string `json:"device_id,omitempty"`   // PCI device
		Driver   string `json:"driver,omitempty"`
		Subsys   string `json:"subsystem,omitempty"`   // e.g. "pci"
	}
	var gpus []gpuInfo
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "card") || strings.Contains(name, "-") {
			// "card0" yes, "card0-DP-1" no (those are connectors)
			continue
		}
		base := filepath.Join("/sys/class/drm", name, "device")
		g := gpuInfo{Card: name}
		g.Vendor = readFileTrimmed(filepath.Join(base, "vendor"))
		g.Device = readFileTrimmed(filepath.Join(base, "device"))
		// driver is a symlink to /sys/bus/pci/drivers/<name>
		if link, err := os.Readlink(filepath.Join(base, "driver")); err == nil {
			g.Driver = filepath.Base(link)
		}
		// uevent has SUBSYSTEM=
		if data, err := os.ReadFile(filepath.Join(base, "uevent")); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "SUBSYSTEM=") {
					g.Subsys = strings.TrimPrefix(line, "SUBSYSTEM=")
				}
			}
		}
		if g.Vendor != "" || g.Driver != "" {
			gpus = append(gpus, g)
		}
	}
	if len(gpus) > 0 {
		r.Set("gpus", gpus, LayerL2)
	}
}

// collectMachineID reads /etc/machine-id. systemd writes it on first boot;
// stable across reboots until the OS is reinstalled. Many containers inherit
// from their host — useful as a fingerprint but not a trust anchor for LXC.
func collectMachineID(r *Result) {
	if id := readFileTrimmed("/etc/machine-id"); id != "" {
		r.Set("machine_id", id, LayerL2)
	}
}

// --- helpers ---

func readFileTrimmed(p string) string {
	data, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// isPlaceholderValue catches the OEM placeholders BIOSes ship with.
// Treating these as "unknown" prevents the agent from reporting fake data.
func isPlaceholderValue(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "none", "system serial number", "to be filled by o.e.m.",
		"default string", "not specified", "not applicable", "unknown",
		"oem", "system product name", "system manufacturer",
		"system version", "to be filled by oem", "type1productconfigid":
		return true
	}
	return false
}

func parseHexInt(h string) int {
	v := 0
	for i := 0; i < len(h); i++ {
		c := h[i]
		var d int
		switch {
		case c >= '0' && c <= '9':
			d = int(c - '0')
		case c >= 'a' && c <= 'f':
			d = int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = int(c-'A') + 10
		default:
			return 0
		}
		v = v*16 + d
	}
	return v
}

var _ = fmt.Sprintf // keep fmt import if helpers grow
