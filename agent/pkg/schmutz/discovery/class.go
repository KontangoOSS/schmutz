package discovery

import (
	"strings"
)

// Class identifies the trust anchor available on a device. The controller
// uses class to pick the right Bao policy and privilege ceiling.
//
// Classes are derived from layered evidence; never trust the agent's
// self-classification blindly — the controller should re-derive from the
// raw evidence fields.
type Class string

const (
	ClassTPMAttested Class = "tpm-attested" // bare metal + working TPM
	ClassDMIAttested Class = "dmi-attested" // bare metal, no TPM, real DMI
	ClassCloudVM     Class = "cloud-vm"     // known cloud provider
	ClassVM          Class = "vm"           // generic hypervisor
	ClassLXC         Class = "lxc"          // LXC container — needs parent attestation
	ClassDocker      Class = "docker"       // Docker container — image-anchored
	ClassUnattested  Class = "unattested"   // can't determine — lowest privilege
)

// ClassEvidence summarizes the indicators that drove the classification,
// so the controller can second-guess the agent.
type ClassEvidence struct {
	Class            Class    `json:"class"`
	HasTPM           bool     `json:"has_tpm"`
	DMIPresent       bool     `json:"dmi_present"`
	DMIProductName   string   `json:"dmi_product_name,omitempty"`
	DMIChassisType   string   `json:"dmi_chassis_type,omitempty"`
	CloudProvider    string   `json:"cloud_provider,omitempty"`
	CloudInitPresent bool     `json:"cloud_init_present"`
	ContainerEnv     string   `json:"container_env,omitempty"`
	DockerEnvPresent bool     `json:"docker_env_present"`
	VirtSystem       string   `json:"virt_system,omitempty"`
	CGroupHints      []string `json:"cgroup_hints,omitempty"`
}

// DetectClass derives the class from a populated Result. Callers should run
// L0+L1+L2 first; L3 and L4 are not required.
//
// All inputs come from the Result — DetectClass never peeks at /sys, /proc,
// or /dev directly. That keeps the function pure and testable: the same
// Result always produces the same class, regardless of the host running
// the test.
func DetectClass(r *Result) *ClassEvidence {
	ev := &ClassEvidence{Class: ClassUnattested}

	ev.HasTPM = boolFact(r, "has_tpm")
	ev.DMIProductName = stringFact(r, "dmi_product_name")
	ev.DMIChassisType = stringFact(r, "dmi_chassis_type")
	ev.DMIPresent = ev.DMIProductName != "" || stringFact(r, "dmi_sys_vendor") != ""
	ev.VirtSystem = stringFact(r, "virt_system")
	ev.ContainerEnv = stringFact(r, "container_env")
	ev.DockerEnvPresent = boolFact(r, "docker_env_present")
	if v, ok := r.Get("cgroup_hints"); ok {
		if sl, ok := v.([]string); ok {
			ev.CGroupHints = sl
		}
	}
	ev.CloudInitPresent = boolFact(r, "cloud_init_present")
	ev.CloudProvider = stringFact(r, "cloud_provider")
	if ev.CloudProvider == "" {
		ev.CloudProvider = detectCloudProvider(ev.DMIProductName)
	}

	// Containers first — they shadow everything else.
	if ev.DockerEnvPresent || hasHint(ev.CGroupHints, "docker") || hasHint(ev.CGroupHints, "containerd") {
		ev.Class = ClassDocker
		return ev
	}
	if ev.ContainerEnv == "lxc" || hasHint(ev.CGroupHints, "lxc") {
		ev.Class = ClassLXC
		return ev
	}

	// Then known clouds.
	if ev.CloudProvider != "" {
		ev.Class = ClassCloudVM
		return ev
	}

	// Generic VMs only when DMI clearly says so.
	// Note: gopsutil's host.VirtualizationSystem returns "kvm" on bare-metal
	// hosts that have CPU virt extensions enabled — it's not a reliable virt
	// indicator. We rely on DMI product strings instead.
	if ev.DMIPresent && isVMProductName(ev.DMIProductName) {
		ev.Class = ClassVM
		return ev
	}

	// Bare metal: TPM beats DMI.
	if ev.HasTPM {
		ev.Class = ClassTPMAttested
		return ev
	}
	if ev.DMIPresent {
		ev.Class = ClassDMIAttested
		return ev
	}

	return ev
}

// --- helpers (some duplicated from earlier; kept package-local to avoid
// pulling cross-file imports into class detection) ---

func boolFact(r *Result, key string) bool {
	v, ok := r.Get(key)
	if !ok {
		return false
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

func readContainerEnv() string {
	data, err := readFileBytes("/proc/1/environ")
	if err != nil {
		return ""
	}
	for _, e := range strings.Split(string(data), "\x00") {
		if strings.HasPrefix(e, "container=") {
			return strings.TrimPrefix(e, "container=")
		}
	}
	return ""
}

func detectCGroupHints() []string {
	data, err := readFileBytes("/proc/1/cgroup")
	if err != nil {
		return nil
	}
	var hints []string
	for _, line := range strings.Split(string(data), "\n") {
		for _, marker := range []string{"docker", "containerd", "lxc", "kubepods"} {
			if strings.Contains(line, marker) && !contains(hints, marker) {
				hints = append(hints, marker)
			}
		}
	}
	return hints
}

func detectCloudProvider(productName string) string {
	p := strings.ToLower(productName)
	switch {
	case strings.Contains(p, "droplet"):
		return "digitalocean"
	case strings.Contains(p, "amazon ec2"), strings.Contains(p, "amazon"):
		return "aws"
	case strings.Contains(p, "google"):
		return "gcp"
	case strings.Contains(p, "virtual machine") && strings.Contains(p, "microsoft"):
		return "azure"
	}
	if data, err := readFileBytes("/run/cloud-init/instance-data.json"); err == nil {
		s := strings.ToLower(string(data))
		switch {
		case strings.Contains(s, "digitalocean"):
			return "digitalocean"
		case strings.Contains(s, `"cloud_name": "aws"`), strings.Contains(s, `"cloud-name": "aws"`):
			return "aws"
		case strings.Contains(s, `"cloud_name": "gce"`), strings.Contains(s, `"cloud-name": "gce"`):
			return "gcp"
		case strings.Contains(s, `"cloud_name": "azure"`):
			return "azure"
		}
	}
	return ""
}

func isVMProductName(name string) bool {
	n := strings.ToLower(name)
	// "standard pc" is what QEMU+KVM puts in DMI by default — common on Proxmox
	// VMs, libvirt-managed KVMs, etc. Catch it alongside the named hypervisors.
	for _, m := range []string{
		"virtualbox", "vmware", "kvm", "qemu", "innotek",
		"xen", "bochs", "parallels", "standard pc",
	} {
		if strings.Contains(n, m) {
			return true
		}
	}
	return false
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func hasHint(hints []string, h string) bool { return contains(hints, h) }
