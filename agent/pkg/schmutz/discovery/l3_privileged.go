package discovery

import (
	"os"
	"strings"
)

// collectL3 covers facts that need root *and* extra capabilities (CAP_SYS_RAWIO,
// CAP_NET_RAW, etc.) — TPM probing, raw netlink dumps, kmod inspection.
//
// Deliberately conservative — we don't want a faulty TPM library to crash
// enrollment. TPM detection here is presence-only; real PCR quote happens in
// the future composite-identity flow.
func collectL3(r *Result) {
	collectTPM(r)
	collectKmodHints(r)
}

// collectTPM detects a usable TPM device. Presence of /dev/tpm0 (kernel char
// device) indicates the kernel sees a TPM; /dev/tpmrm0 means there's a TPM 2.0
// resource manager. We also peek at sysfs for the vendor and version.
func collectTPM(r *Result) {
	type tpmInfo struct {
		Device         string `json:"device,omitempty"`
		ResourceMgr    bool   `json:"has_resource_mgr"`
		ManufacturerID string `json:"manufacturer_id,omitempty"`
		Version        string `json:"version,omitempty"`
	}
	t := tpmInfo{}
	hasTPM := false
	if fileExists("/dev/tpm0") {
		t.Device = "/dev/tpm0"
		hasTPM = true
	}
	if fileExists("/dev/tpmrm0") {
		t.ResourceMgr = true
		hasTPM = true
	}
	if !hasTPM {
		return
	}
	// sysfs metadata — best-effort
	for _, base := range []string{"/sys/class/tpm/tpm0", "/sys/class/tpm/tpmrm0"} {
		if mfr := readFileTrimmed(base + "/manufacturer_id"); mfr != "" {
			t.ManufacturerID = mfr
			break
		}
	}
	for _, base := range []string{"/sys/class/tpm/tpm0", "/sys/class/tpm/tpmrm0"} {
		if v := readFileTrimmed(base + "/tpm_version_major"); v != "" {
			t.Version = v
			break
		}
	}
	r.Set("tpm", t, LayerL3)
	r.Set("has_tpm", true, LayerL3)
}

// collectKmodHints reports a small subset of kernel modules useful for
// fingerprinting (virt drivers, crypto modules, security modules). The full
// module list is too noisy and inconsistent across distros.
func collectKmodHints(r *Result) {
	data, err := os.ReadFile("/proc/modules")
	if err != nil {
		return
	}
	wanted := map[string]bool{
		// Virtualization drivers — confirm what hypervisor we're under
		"virtio_net": true, "virtio_blk": true, "virtio_pci": true, "virtio_console": true,
		"hv_netvsc": true, "hv_storvsc": true, "hv_vmbus": true,
		"vmw_balloon": true, "vmw_vmci": true, "vmwgfx": true,
		"xen_blkfront": true, "xen_netfront": true,
		// Security / crypto modules of note
		"tpm_tis": true, "tpm_crb": true, "tpm": true,
		"apparmor": true, "selinux": true, "ima": true,
		// Hardware
		"nvidia": true, "amdgpu": true, "i915": true,
	}
	var hits []string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && wanted[fields[0]] {
			hits = append(hits, fields[0])
		}
	}
	if len(hits) > 0 {
		r.Set("kmod_hints", hits, LayerL3)
	}
}
