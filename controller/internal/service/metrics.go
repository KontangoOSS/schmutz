package service

import (
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// MetricsService provides machine and cluster observability.
type MetricsService struct {
	ziti  *ZitiService
	store *StoreService
}

func NewMetricsService(ziti *ZitiService, store *StoreService) *MetricsService {
	return &MetricsService{ziti: ziti, store: store}
}

// --- Cluster status ---

// ClusterStatus returns the Ziti Raft cluster health.
func (m *MetricsService) ZitiCluster(token string) (map[string]interface{}, error) {
	// Get controller list and their state
	routers, err := m.ziti.ListRouters(token)
	if err != nil {
		return nil, err
	}

	online := 0
	for _, r := range routers {
		if r.IsOnline {
			online++
		}
	}

	identities, _ := m.ziti.ListIdentities(token, 1)
	services, _ := m.ziti.ListServices(token)
	terminators, _ := m.ziti.ListTerminators(token, "")

	return map[string]interface{}{
		"routers_total":  len(routers),
		"routers_online": online,
		"identities":     len(identities),
		"services":       len(services),
		"terminators":    len(terminators),
	}, nil
}

// BaoCluster returns the Bao Raft cluster health.
func (m *MetricsService) BaoCluster() (map[string]interface{}, error) {
	seal, err := m.store.SealStatus()
	if err != nil {
		return nil, err
	}

	leader, err := m.store.Leader()
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"sealed":         seal["sealed"],
		"initialized":    seal["initialized"],
		"version":        seal["version"],
		"cluster_name":   seal["cluster_name"],
		"is_leader":      leader["is_self"],
		"leader_address": leader["leader_address"],
	}, nil
}

// --- Machine metrics ---

// MachineMetrics returns local machine stats.
func (m *MetricsService) MachineMetrics() map[string]interface{} {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	metrics := map[string]interface{}{
		"go": map[string]interface{}{
			"goroutines": runtime.NumGoroutine(),
			"cpus":       runtime.NumCPU(),
			"heap_alloc": mem.HeapAlloc,
			"heap_sys":   mem.HeapSys,
			"gc_cycles":  mem.NumGC,
			"go_version": runtime.Version(),
		},
		"os":   runtime.GOOS,
		"arch": runtime.GOARCH,
	}

	// System metrics (Linux)
	if runtime.GOOS == "linux" {
		if uptime, err := readFile("/proc/uptime"); err == nil {
			parts := strings.Fields(uptime)
			if len(parts) > 0 {
				metrics["uptime_seconds"] = parts[0]
			}
		}
		if load, err := readFile("/proc/loadavg"); err == nil {
			metrics["load"] = strings.TrimSpace(load)
		}
		if meminfo, err := exec.Command("free", "-b").Output(); err == nil {
			for _, line := range strings.Split(string(meminfo), "\n") {
				if strings.HasPrefix(line, "Mem:") {
					fields := strings.Fields(line)
					if len(fields) >= 4 {
						metrics["memory_total"] = fields[1]
						metrics["memory_used"] = fields[2]
						metrics["memory_free"] = fields[3]
					}
				}
			}
		}
		if df, err := exec.Command("df", "-B1", "/").Output(); err == nil {
			lines := strings.Split(string(df), "\n")
			if len(lines) >= 2 {
				fields := strings.Fields(lines[1])
				if len(fields) >= 4 {
					metrics["disk_total"] = fields[1]
					metrics["disk_used"] = fields[2]
					metrics["disk_free"] = fields[3]
				}
			}
		}
	}

	return metrics
}

// --- Network speed (basic) ---

// NetworkLatency measures round-trip time to key services.
func (m *MetricsService) NetworkLatency() map[string]interface{} {
	results := map[string]interface{}{}

	// Measure Bao latency
	start := time.Now()
	m.store.SealStatus()
	results["bao_ms"] = time.Since(start).Milliseconds()

	// Measure Ziti latency
	start = time.Now()
	m.ziti.Authenticate()
	results["ziti_ms"] = time.Since(start).Milliseconds()

	return results
}

// --- Summary ---

// FullStatus returns a complete status snapshot.
func (m *MetricsService) FullStatus(token string) map[string]interface{} {
	zitiCluster, _ := m.ZitiCluster(token)
	baoCluster, _ := m.BaoCluster()

	return map[string]interface{}{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"ziti":      zitiCluster,
		"bao":       baoCluster,
		"machine":   m.MachineMetrics(),
		"latency":   m.NetworkLatency(),
	}
}

func readFile(path string) (string, error) {
	out, err := exec.Command("cat", path).Output()
	return string(out), err
}
