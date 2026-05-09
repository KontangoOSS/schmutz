// Package collector gathers system telemetry using gopsutil and returns
// stream protocol types ready for transmission.
package collector

import (
	"runtime"

	"git.konoss.org/kore/schmutz/agent/internal/stream"
	gopsnet "github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/process"
)

// CollectSystem gathers CPU, memory, load, and uptime data.
func CollectSystem() (*stream.SystemData, error) {
	cpuPercents, err := cpu.Percent(0, false)
	if err != nil {
		return nil, err
	}
	var cpuPct float64
	if len(cpuPercents) > 0 {
		cpuPct = cpuPercents[0]
	}

	vmem, err := mem.VirtualMemory()
	if err != nil {
		return nil, err
	}

	avg, err := load.Avg()
	if err != nil {
		return nil, err
	}

	info, err := host.Info()
	if err != nil {
		return nil, err
	}

	return &stream.SystemData{
		Hostname:   info.Hostname,
		CPUPercent: cpuPct,
		MemTotal:   vmem.Total,
		MemUsed:    vmem.Used,
		MemPercent: vmem.UsedPercent,
		LoadAvg1:   avg.Load1,
		LoadAvg5:   avg.Load5,
		LoadAvg15:  avg.Load15,
		Uptime:     info.Uptime,
		NumCPU:     runtime.NumCPU(),
	}, nil
}

// CollectNetwork gathers network interface stats, skipping loopback.
func CollectNetwork() (*stream.NetworkData, error) {
	counters, err := gopsnet.IOCounters(true)
	if err != nil {
		return nil, err
	}

	var ifaces []stream.NetInterface
	for _, c := range counters {
		if c.Name == "lo" {
			continue
		}
		ifaces = append(ifaces, stream.NetInterface{
			Name:      c.Name,
			BytesSent: c.BytesSent,
			BytesRecv: c.BytesRecv,
		})
	}

	return &stream.NetworkData{Interfaces: ifaces}, nil
}

// CollectDisk gathers disk mount usage, skipping partitions with 0 total bytes.
func CollectDisk() (*stream.DiskData, error) {
	partitions, err := disk.Partitions(false)
	if err != nil {
		return nil, err
	}

	var mounts []stream.DiskMount
	for _, p := range partitions {
		usage, err := disk.Usage(p.Mountpoint)
		if err != nil {
			continue
		}
		if usage.Total == 0 {
			continue
		}
		mounts = append(mounts, stream.DiskMount{
			Path:        p.Mountpoint,
			Total:       usage.Total,
			Used:        usage.Used,
			UsedPercent: usage.UsedPercent,
			Fstype:      p.Fstype,
		})
	}

	return &stream.DiskData{Mounts: mounts}, nil
}

// CollectProcesses counts total and running processes.
func CollectProcesses() (*stream.ProcessData, error) {
	procs, err := process.Processes()
	if err != nil {
		return nil, err
	}

	total := len(procs)
	running := 0
	for _, p := range procs {
		status, err := p.Status()
		if err != nil {
			continue
		}
		// gopsutil returns a slice of status strings
		for _, s := range status {
			if s == "running" || s == "R" {
				running++
				break
			}
		}
	}

	return &stream.ProcessData{
		Total:   total,
		Running: running,
	}, nil
}

// Snapshot is a full point-in-time collection of all telemetry data.
type Snapshot struct {
	System  *stream.SystemData  `json:"system,omitempty"`
	Network *stream.NetworkData `json:"network,omitempty"`
	Disk    *stream.DiskData    `json:"disk,omitempty"`
	Process *stream.ProcessData `json:"process,omitempty"`
}

// CollectAll gathers all available telemetry in one call.
// Partial failures are silently skipped — best effort.
func CollectAll() *Snapshot {
	s := &Snapshot{}
	s.System, _ = CollectSystem()
	s.Network, _ = CollectNetwork()
	s.Disk, _ = CollectDisk()
	s.Process, _ = CollectProcesses()
	return s
}
