package verify

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// MachineProfile is the complete fingerprint of a machine.
// Collected over the WebSocket conversation. Stored in Bao.
// Used for future identification without any credentials.
type MachineProfile struct {
	// Connection metadata (observed, not self-reported)
	JA4        string `json:"ja4,omitempty"`
	SourceIP   string `json:"source_ip,omitempty"`
	TLSVersion string `json:"tls_version,omitempty"`
	UserAgent  string `json:"user_agent,omitempty"`
	Latency    int    `json:"latency_ms,omitempty"`

	// Hardware (self-reported, cross-referenced)
	Hostname     string   `json:"hostname"`
	OS           string   `json:"os"`
	OSVersion    string   `json:"os_version"`
	Arch         string   `json:"arch"`
	Kernel       string   `json:"kernel,omitempty"`
	CPUModel     string   `json:"cpu_model,omitempty"`
	CPUCores     int      `json:"cpu_cores,omitempty"`
	MemoryMB     int      `json:"memory_mb,omitempty"`
	MachineID    string   `json:"machine_id,omitempty"`
	SerialNumber string   `json:"serial_number,omitempty"`
	MACAddrs     []string `json:"mac_addrs,omitempty"`
	DiskSerials  []string `json:"disk_serials,omitempty"`

	// Network
	Interfaces []NetworkInterface `json:"interfaces,omitempty"`
	DNSServers []string           `json:"dns_servers,omitempty"`
	Gateway    string             `json:"gateway,omitempty"`

	// System state
	Uptime       int      `json:"uptime_secs,omitempty"`
	BootID       string   `json:"boot_id,omitempty"`
	Timezone     string   `json:"timezone,omitempty"`
	Locale       string   `json:"locale,omitempty"`
	SSHHostKeys  []string `json:"ssh_host_keys,omitempty"`
	OpenPorts    []int    `json:"open_ports,omitempty"`
	PackageCount int      `json:"package_count,omitempty"`

	// Composite hashes (computed by controller)
	HardwareHash string `json:"hardware_hash"` // MACs + serial + machine_id
	SystemHash   string `json:"system_hash"`   // OS + kernel + arch + cpu + memory
	NetworkHash  string `json:"network_hash"`  // interfaces + DNS + gateway
	FullHash     string `json:"full_hash"`     // everything combined
}

type NetworkInterface struct {
	Name string   `json:"name"`
	MAC  string   `json:"mac,omitempty"`
	IPs  []string `json:"ips,omitempty"`
	Up   bool     `json:"up"`
}

// ComputeHashes calculates all composite fingerprint hashes.
// Called after all data is collected.
func (p *MachineProfile) ComputeHashes() {
	p.HardwareHash = hashStrings(append(p.MACAddrs, p.SerialNumber, p.MachineID)...)
	p.SystemHash = hashStrings(p.OS, p.OSVersion, p.Kernel, p.Arch, p.CPUModel, fmt.Sprintf("%d", p.MemoryMB))
	p.NetworkHash = hashStrings(append(p.DNSServers, p.Gateway)...)
	p.FullHash = hashStrings(p.HardwareHash, p.SystemHash, p.NetworkHash,
		p.Hostname, p.BootID, p.Timezone, strings.Join(p.SSHHostKeys, ","))
}

// Match compares this profile against a stored profile.
// Returns a confidence score (0-100) and which signals matched.
type MatchResult struct {
	Score   int      `json:"score"`   // 0-100
	Signals int      `json:"signals"` // how many signals checked
	Matched int      `json:"matched"` // how many matched
	Details []Signal `json:"details"`
}

type Signal struct {
	Name    string `json:"name"`
	Weight  int    `json:"weight"`
	Matched bool   `json:"matched"`
}

// Match compares two profiles and scores the similarity.
func (p *MachineProfile) Match(stored *MachineProfile) *MatchResult {
	result := &MatchResult{}

	checks := []struct {
		name   string
		weight int
		match  bool
	}{
		{"hardware_hash", 25, p.HardwareHash != "" && p.HardwareHash == stored.HardwareHash},
		{"machine_id", 20, p.MachineID != "" && p.MachineID == stored.MachineID},
		{"serial_number", 15, p.SerialNumber != "" && p.SerialNumber == stored.SerialNumber},
		{"hostname", 5, p.Hostname != "" && p.Hostname == stored.Hostname},
		{"os_version", 3, p.OSVersion != "" && p.OSVersion == stored.OSVersion},
		{"cpu_model", 5, p.CPUModel != "" && p.CPUModel == stored.CPUModel},
		{"memory_mb", 2, p.MemoryMB > 0 && p.MemoryMB == stored.MemoryMB},
		{"timezone", 2, p.Timezone != "" && p.Timezone == stored.Timezone},
		{"network_hash", 5, p.NetworkHash != "" && p.NetworkHash == stored.NetworkHash},
		{"ssh_host_keys", 10, matchSSHKeys(p.SSHHostKeys, stored.SSHHostKeys)},
		{"mac_overlap", 8, macOverlap(p.MACAddrs, stored.MACAddrs) > 0},
	}

	totalWeight := 0
	matchedWeight := 0
	for _, c := range checks {
		totalWeight += c.weight
		if c.match {
			matchedWeight += c.weight
			result.Matched++
		}
		result.Signals++
		result.Details = append(result.Details, Signal{
			Name: c.name, Weight: c.weight, Matched: c.match,
		})
	}

	if totalWeight > 0 {
		result.Score = (matchedWeight * 100) / totalWeight
	}
	return result
}

// Confidence returns a confidence level based on the match score.
func (r *MatchResult) Confidence() string {
	switch {
	case r.Score >= 80:
		return "high"
	case r.Score >= 50:
		return "medium"
	case r.Score >= 20:
		return "low"
	default:
		return "none"
	}
}

// --- Helpers ---

func hashStrings(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		if p != "" {
			h.Write([]byte(p))
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func matchSSHKeys(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	set := map[string]bool{}
	for _, k := range a {
		set[k] = true
	}
	for _, k := range b {
		if set[k] {
			return true
		}
	}
	return false
}

func macOverlap(a, b []string) int {
	set := map[string]bool{}
	for _, m := range a {
		set[strings.ToLower(m)] = true
	}
	count := 0
	for _, m := range b {
		if set[strings.ToLower(m)] {
			count++
		}
	}
	return count
}

// --- Storage ---

// StoreMachineProfile saves a machine profile to Bao for future matching.
func (m *Module) StoreMachineProfile(machineID string, profile *MachineProfile) error {
	profile.ComputeHashes()

	// Store the full profile
	data := map[string]interface{}{
		"hostname": profile.Hostname, "os": profile.OS, "os_version": profile.OSVersion,
		"arch": profile.Arch, "kernel": profile.Kernel,
		"cpu_model": profile.CPUModel, "cpu_cores": profile.CPUCores,
		"memory_mb": profile.MemoryMB, "machine_id": profile.MachineID,
		"serial_number": profile.SerialNumber, "timezone": profile.Timezone,
		"hardware_hash": profile.HardwareHash, "system_hash": profile.SystemHash,
		"network_hash": profile.NetworkHash, "full_hash": profile.FullHash,
		"mac_addrs": profile.MACAddrs, "ssh_host_keys": profile.SSHHostKeys,
		"source_ip": profile.SourceIP, "ja4": profile.JA4,
	}
	if err := m.C.Bao.Put("secret", "identities/profiles/machine/"+machineID, data); err != nil {
		return err
	}

	// Index by hardware hash for fast lookup
	if profile.HardwareHash != "" {
		m.C.Bao.Put("secret", "identities/profiles/by-hardware/"+profile.HardwareHash,
			map[string]interface{}{"machine_id": machineID})
	}
	// Index by full hash
	if profile.FullHash != "" {
		m.C.Bao.Put("secret", "identities/profiles/by-fullhash/"+profile.FullHash,
			map[string]interface{}{"machine_id": machineID})
	}
	// Index by SSH host key
	for _, key := range profile.SSHHostKeys {
		keyHash := hashStrings(key)
		m.C.Bao.Put("secret", "identities/profiles/by-sshkey/"+keyHash,
			map[string]interface{}{"machine_id": machineID, "key": key})
	}

	return nil
}

// FindMachineByProfile searches for a matching stored profile.
// Returns the machine ID and match result if found.
func (m *Module) FindMachineByProfile(profile *MachineProfile) (machineID string, result *MatchResult, err error) {
	profile.ComputeHashes()

	// Fast path: exact hardware hash match
	if profile.HardwareHash != "" {
		data, _ := m.C.Bao.Get("secret", "identities/profiles/by-hardware/"+profile.HardwareHash)
		if data != nil {
			if mid, ok := data["machine_id"].(string); ok {
				stored, _ := m.loadProfile(mid)
				if stored != nil {
					return mid, profile.Match(stored), nil
				}
			}
		}
	}

	// Fast path: SSH host key match
	for _, key := range profile.SSHHostKeys {
		keyHash := hashStrings(key)
		data, _ := m.C.Bao.Get("secret", "identities/profiles/by-sshkey/"+keyHash)
		if data != nil {
			if mid, ok := data["machine_id"].(string); ok {
				stored, _ := m.loadProfile(mid)
				if stored != nil {
					return mid, profile.Match(stored), nil
				}
			}
		}
	}

	// Fast path: full hash match
	if profile.FullHash != "" {
		data, _ := m.C.Bao.Get("secret", "identities/profiles/by-fullhash/"+profile.FullHash)
		if data != nil {
			if mid, ok := data["machine_id"].(string); ok {
				stored, _ := m.loadProfile(mid)
				if stored != nil {
					return mid, profile.Match(stored), nil
				}
			}
		}
	}

	return "", nil, nil
}

func (m *Module) loadProfile(machineID string) (*MachineProfile, error) {
	data, err := m.C.Bao.Get("secret", "identities/profiles/machine/"+machineID)
	if err != nil || data == nil {
		return nil, err
	}

	p := &MachineProfile{}
	p.Hostname, _ = data["hostname"].(string)
	p.OS, _ = data["os"].(string)
	p.OSVersion, _ = data["os_version"].(string)
	p.Arch, _ = data["arch"].(string)
	p.Kernel, _ = data["kernel"].(string)
	p.CPUModel, _ = data["cpu_model"].(string)
	p.MachineID, _ = data["machine_id"].(string)
	p.SerialNumber, _ = data["serial_number"].(string)
	p.Timezone, _ = data["timezone"].(string)
	p.HardwareHash, _ = data["hardware_hash"].(string)
	p.SystemHash, _ = data["system_hash"].(string)
	p.NetworkHash, _ = data["network_hash"].(string)
	p.FullHash, _ = data["full_hash"].(string)
	p.SourceIP, _ = data["source_ip"].(string)
	p.JA4, _ = data["ja4"].(string)

	if macs, ok := data["mac_addrs"].([]interface{}); ok {
		for _, mac := range macs {
			if s, ok := mac.(string); ok {
				p.MACAddrs = append(p.MACAddrs, s)
			}
		}
	}
	if keys, ok := data["ssh_host_keys"].([]interface{}); ok {
		for _, key := range keys {
			if s, ok := key.(string); ok {
				p.SSHHostKeys = append(p.SSHHostKeys, s)
			}
		}
	}
	if cores, ok := data["cpu_cores"].(float64); ok {
		p.CPUCores = int(cores)
	}
	if mem, ok := data["memory_mb"].(float64); ok {
		p.MemoryMB = int(mem)
	}

	return p, nil
}

// sortedKeys returns sorted keys of a string map (for deterministic hashing).
func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
