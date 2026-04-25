package discovery

import (
	"os"
	"strings"
	"time"

	gopsnet "github.com/shirou/gopsutil/v3/net"
)

// collectL1 gathers facts a normal user can read: network interfaces, MACs,
// IPs, default route, DNS resolvers, locale, timezone, container/cloud
// environment hints. Privileged metadata (DMI, disk serials) is in L2.
func collectL1(r *Result) {
	collectInterfaces(r)
	collectDNSAndGateway(r)
	collectLocaleAndTZ(r)
	collectEnvironmentHints(r)
}

// collectEnvironmentHints captures the small signals that drive class
// detection: container=lxc env on PID 1, /.dockerenv presence, cgroup
// markers, cloud-init data presence, cloud provider hint. All of these
// are world-readable on the systems that have them.
func collectEnvironmentHints(r *Result) {
	if env := readContainerEnv(); env != "" {
		r.Set("container_env", env, LayerL1)
	}
	if fileExists("/.dockerenv") {
		r.Set("docker_env_present", true, LayerL1)
	}
	if hints := detectCGroupHints(); len(hints) > 0 {
		r.Set("cgroup_hints", hints, LayerL1)
	}
	if fileExists("/var/lib/cloud/instance") || fileExists("/run/cloud-init/instance-data.json") {
		r.Set("cloud_init_present", true, LayerL1)
	}
}

// NIC describes a single network interface as seen from userspace.
type NIC struct {
	Name      string   `json:"name"`
	MAC       string   `json:"mac,omitempty"`
	MTU       int      `json:"mtu"`
	Up        bool     `json:"up"`
	Loopback  bool     `json:"loopback"`
	Virtual   bool     `json:"virtual"`
	Addresses []string `json:"addrs,omitempty"`
}

func collectInterfaces(r *Result) {
	ifaces, err := gopsnet.Interfaces()
	if err != nil {
		r.Err("interfaces", err)
		return
	}

	var nics []NIC
	var allMACs []string
	var ipv4, ipv6 []string

	for _, iface := range ifaces {
		isLoop := strings.Contains(strings.ToLower(strings.Join(iface.Flags, ",")), "loopback")
		isUp := strings.Contains(strings.ToLower(strings.Join(iface.Flags, ",")), "up")
		nic := NIC{
			Name:     iface.Name,
			MAC:      iface.HardwareAddr,
			MTU:      iface.MTU,
			Up:       isUp,
			Loopback: isLoop,
			Virtual:  isVirtualNIC(iface.Name),
		}
		for _, a := range iface.Addrs {
			nic.Addresses = append(nic.Addresses, a.Addr)
			ip := stripCIDR(a.Addr)
			if ip == "" || isLoop {
				continue
			}
			if strings.Contains(ip, ":") {
				ipv6 = append(ipv6, ip)
			} else {
				ipv4 = append(ipv4, ip)
			}
		}
		if nic.MAC != "" && !isLoop && nic.MAC != "00:00:00:00:00:00" {
			allMACs = append(allMACs, nic.MAC)
		}
		nics = append(nics, nic)
	}

	r.Set("interfaces", nics, LayerL1)
	r.Set("macs", uniqueLowerStrings(allMACs), LayerL1)
	if len(ipv4) > 0 {
		r.Set("ipv4_addrs", uniqueStrings(ipv4), LayerL1)
	}
	if len(ipv6) > 0 {
		r.Set("ipv6_addrs", uniqueStrings(ipv6), LayerL1)
	}
}

func collectDNSAndGateway(r *Result) {
	// /etc/resolv.conf — present on Linux and macOS, absent on Windows
	if data, err := os.ReadFile("/etc/resolv.conf"); err == nil {
		var dns []string
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "nameserver ") {
				ns := strings.TrimSpace(strings.TrimPrefix(line, "nameserver"))
				if ns != "" {
					dns = append(dns, ns)
				}
			}
		}
		if len(dns) > 0 {
			r.Set("dns_servers", uniqueStrings(dns), LayerL1)
		}
	}

	// Default gateway from /proc/net/route (Linux)
	if data, err := os.ReadFile("/proc/net/route"); err == nil {
		gw := parseDefaultGateway(string(data))
		if gw != "" {
			r.Set("gateway", gw, LayerL1)
		}
	}
}

func collectLocaleAndTZ(r *Result) {
	tzName, tzOffset := time.Now().Zone()
	if tzName != "" {
		r.Set("tz_name", tzName, LayerL1)
		r.Set("tz_offset", tzOffset, LayerL1)
	}
	if data, err := os.ReadFile("/etc/timezone"); err == nil {
		zone := strings.TrimSpace(string(data))
		if zone != "" {
			r.Set("timezone", zone, LayerL1)
		}
	} else if link, err := os.Readlink("/etc/localtime"); err == nil {
		// e.g. "/usr/share/zoneinfo/America/Los_Angeles"
		if i := strings.Index(link, "zoneinfo/"); i >= 0 {
			r.Set("timezone", link[i+len("zoneinfo/"):], LayerL1)
		}
	}

	// Locale
	if l := os.Getenv("LANG"); l != "" {
		r.Set("locale", strings.SplitN(l, ".", 2)[0], LayerL1)
	}
}

// --- helpers ---

func isVirtualNIC(name string) bool {
	for _, p := range []string{"docker", "veth", "br-", "virbr", "vnet", "tun", "tap", "ziti", "wg", "kube"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func stripCIDR(addr string) string {
	if i := strings.Index(addr, "/"); i >= 0 {
		return addr[:i]
	}
	return addr
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func uniqueLowerStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.ToLower(s)
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// parseDefaultGateway parses /proc/net/route (kernel route table) and returns
// the IPv4 default gateway as dotted-quad. The file format is one route per
// line, fields tab-separated: Iface Destination Gateway Flags ... where
// Destination=00000000 means default route. Gateway field is little-endian hex.
func parseDefaultGateway(s string) string {
	for _, line := range strings.Split(s, "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue
		}
		if fields[1] != "00000000" {
			continue
		}
		gwHex := fields[2]
		if len(gwHex) != 8 {
			continue
		}
		// little-endian hex to dotted-quad
		b := make([]byte, 4)
		for i := 0; i < 4; i++ {
			var d uint8
			c1 := gwHex[i*2]
			c2 := gwHex[i*2+1]
			d = hexNibble(c1)<<4 | hexNibble(c2)
			b[3-i] = d
		}
		return formatIP4(b)
	}
	return ""
}

func hexNibble(c byte) uint8 {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	}
	return 0
}

func formatIP4(b []byte) string {
	return itoa(int(b[0])) + "." + itoa(int(b[1])) + "." + itoa(int(b[2])) + "." + itoa(int(b[3]))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [4]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
