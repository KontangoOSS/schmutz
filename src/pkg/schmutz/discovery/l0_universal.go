package discovery

import (
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
)

// collectL0 gathers facts that work for any user, in any container, on any OS.
// These facts are easy to spoof — never base trust decisions on them alone.
func collectL0(r *Result) {
	if hn, err := os.Hostname(); err == nil && hn != "" {
		r.Set("hostname", hn, LayerL0)
	}
	r.Set("os", runtime.GOOS, LayerL0)
	r.Set("arch", runtime.GOARCH, LayerL0)
	r.Set("go_version", runtime.Version(), LayerL0)
	r.Set("cpu_cores", runtime.NumCPU(), LayerL0)
	r.Set("ts", time.Now().UTC().Format(time.RFC3339), LayerL0)

	// gopsutil host info — kernel version, platform, uptime, boot time
	if info, err := host.Info(); err == nil {
		r.Set("kernel", info.KernelVersion, LayerL0)
		r.Set("kernel_arch", info.KernelArch, LayerL0)
		r.Set("os_platform", info.Platform, LayerL0)
		r.Set("os_platform_family", info.PlatformFamily, LayerL0)
		r.Set("os_version", info.PlatformVersion, LayerL0)
		r.Set("uptime_secs", int64(info.Uptime), LayerL0)
		r.Set("boot_time", time.Unix(int64(info.BootTime), 0).UTC().Format(time.RFC3339), LayerL0)
		if info.HostID != "" {
			r.Set("host_id", info.HostID, LayerL0)
		}
		r.Set("virt_system", info.VirtualizationSystem, LayerL0)
		r.Set("virt_role", info.VirtualizationRole, LayerL0)
	} else {
		r.Err("host_info", err)
	}

	// Detailed CPU info
	if cpuInfo, err := cpu.Info(); err == nil && len(cpuInfo) > 0 {
		c := cpuInfo[0]
		r.Set("cpu_model", strings.TrimSpace(c.ModelName), LayerL0)
		r.Set("cpu_vendor", c.VendorID, LayerL0)
		r.Set("cpu_family", c.Family, LayerL0)
		r.Set("cpu_mhz", c.Mhz, LayerL0)
		r.Set("cpu_physical_count", len(cpuInfo), LayerL0)
		if len(c.Flags) > 0 {
			// Trim — full flag list is huge and changes between kernels
			interesting := []string{}
			wanted := map[string]bool{
				"vmx": true, "svm": true, "avx": true, "avx2": true, "avx512f": true,
				"sha_ni": true, "aes": true, "rdrand": true, "rdseed": true,
				"vme": true, "smap": true, "smep": true,
			}
			for _, f := range c.Flags {
				if wanted[f] {
					interesting = append(interesting, f)
				}
			}
			if len(interesting) > 0 {
				r.Set("cpu_flags", interesting, LayerL0)
			}
		}
	} else if err != nil {
		r.Err("cpu_info", err)
	}

	// Memory totals
	if vm, err := mem.VirtualMemory(); err == nil {
		r.Set("mem_total_bytes", vm.Total, LayerL0)
	}
	if sm, err := mem.SwapMemory(); err == nil {
		r.Set("swap_total_bytes", sm.Total, LayerL0)
	}
}
