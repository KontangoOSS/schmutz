package enroll

import (
	"time"

	"github.com/KontangoOSS/schmutz/controller/internal/controller/common"
	"github.com/KontangoOSS/schmutz/controller/internal/controller/verify"
)

// ConnectionMeta holds observed connection details for the enrolling machine.
type ConnectionMeta struct {
	SourceIP    string                 `json:"source_ip"`
	PublicIP    string                 `json:"public_ip"`
	JA4         string                 `json:"ja4"`
	TLSVersion  string                 `json:"tls_version"`
	UserAgent   string                 `json:"user_agent"`
	Geolocation map[string]interface{} `json:"geolocation,omitempty"`
}

// BuildSnapshot assembles a point-in-time enrollment snapshot from all available
// data sources. The returned map is suitable for JSON marshalling or KV storage.
func BuildSnapshot(
	reg *common.Registration,
	profile *verify.MachineProfile,
	decision *verify.Decision,
	conn ConnectionMeta,
	node, zitiID, enrolledBy string,
) map[string]interface{} {
	snap := map[string]interface{}{
		"machine_id":      reg.ID,
		"ziti_identity_id": zitiID,
		"enrolled_at":     time.Now().UTC().Format(time.RFC3339),
		"enrolled_by":     enrolledBy,
		"node":            node,
		"zitadel_sub":     nil,
		"connection": map[string]interface{}{
			"source_ip":   conn.SourceIP,
			"public_ip":   conn.PublicIP,
			"ja4":         conn.JA4,
			"tls_version": conn.TLSVersion,
			"user_agent":  conn.UserAgent,
		},
	}

	if profile != nil {
		snap["hardware"] = map[string]interface{}{
			"hardware_hash": profile.HardwareHash,
			"full_hash":     profile.FullHash,
			"serial_number": profile.SerialNumber,
			"cpu_model":     profile.CPUModel,
			"cpu_cores":     profile.CPUCores,
			"memory_mb":     profile.MemoryMB,
			"disk_serials":  profile.DiskSerials,
			"macs":          profile.MACAddrs,
			"ssh_host_keys": profile.SSHHostKeys,
		}

		snap["os"] = map[string]interface{}{
			"hostname":   profile.Hostname,
			"os":         profile.OS,
			"os_version": profile.OSVersion,
			"kernel":     profile.Kernel,
			"arch":       profile.Arch,
			"timezone":   profile.Timezone,
			"locale":     profile.Locale,
		}

		snap["network"] = map[string]interface{}{
			"macs":        profile.MACAddrs,
			"interfaces":  profile.Interfaces,
			"dns_servers": profile.DNSServers,
			"gateway":     profile.Gateway,
		}

		snap["system"] = map[string]interface{}{
			"uptime_secs":   profile.Uptime,
			"boot_id":       profile.BootID,
			"ssh_host_keys": profile.SSHHostKeys,
			"open_ports":    profile.OpenPorts,
			"package_count": profile.PackageCount,
		}

		snap["ephemeral_compute"] = isEphemeralCompute(profile)
	}

	if decision != nil {
		verdicts := make([]map[string]interface{}, 0, len(decision.Verdicts))
		for _, v := range decision.Verdicts {
			verdicts = append(verdicts, map[string]interface{}{
				"check":      v.Check,
				"passed":     v.Passed,
				"confidence": v.Confidence,
				"reason":     v.Reason,
			})
		}
		snap["decision"] = map[string]interface{}{
			"approved":   decision.Approved,
			"quarantine": decision.Quarantine,
			"stage":      decision.Stage,
			"reason":     decision.Reason,
			"verdicts":   verdicts,
		}
		snap["attestation_class"] = detectAttestationClass(profile)
	}

	return snap
}

// detectAttestationClass returns "dmi-attested" if the profile contains hardware
// identifiers that can be cross-referenced, otherwise "unattested".
func detectAttestationClass(profile *verify.MachineProfile) string {
	if profile == nil {
		return "unattested"
	}
	if profile.SerialNumber != "" || len(profile.DiskSerials) > 0 {
		return "dmi-attested"
	}
	return "unattested"
}

// isEphemeralCompute returns true for machines that have no persistent hardware
// identifiers — typically cloud VMs or containers without disk serial access.
func isEphemeralCompute(profile *verify.MachineProfile) bool {
	if profile == nil {
		return true
	}
	return len(profile.DiskSerials) == 0 && profile.SerialNumber == ""
}
