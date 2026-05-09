package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/KontangoOSS/schmutz/controller/internal/service"
)

// startSelfMonitor publishes controller health as telemetry frames every 10s.
func startSelfMonitor(tel *service.TCPTelemetryService, store *service.StoreService, ziti *service.ZitiService, nodeName string) {
	if tel == nil {
		return
	}

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			publishControllerPulse(tel, store, ziti, nodeName)
		}
	}()
}

func publishControllerPulse(tel *service.TCPTelemetryService, store *service.StoreService, ziti *service.ZitiService, nodeName string) {
	h, _ := os.Hostname()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	kv := map[string]string{
		"hostname":   h,
		"node":       nodeName,
		"os":         runtime.GOOS,
		"arch":       runtime.GOARCH,
		"cpus":       fmt.Sprintf("%d", runtime.NumCPU()),
		"goroutines": fmt.Sprintf("%d", runtime.NumGoroutine()),
		"heap_mb":    fmt.Sprintf("%d", mem.HeapAlloc/1024/1024),
		"role":       "controller",
	}

	// Load average
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		parts := strings.Fields(string(data))
		if len(parts) > 0 {
			kv["load"] = parts[0]
		}
	}

	// Memory
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "MemTotal:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					kv["mem_total_kb"] = fields[1]
				}
			}
			if strings.HasPrefix(line, "MemAvailable:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					kv["mem_avail_kb"] = fields[1]
				}
			}
		}
	}

	// Uptime
	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		parts := strings.Fields(string(data))
		if len(parts) > 0 {
			kv["up"] = strings.Split(parts[0], ".")[0]
		}
	}

	// Bao health (only if store is configured)
	if store != nil {
		baoData, _ := store.Client().Sys().Health()
		if baoData != nil {
			kv["bao.sealed"] = fmt.Sprintf("%v", baoData.Sealed)
			kv["bao.initialized"] = fmt.Sprintf("%v", baoData.Initialized)
		}
	}

	// Publish directly into telemetry service (no NATS broker needed).
	tel.RecordSelf("controller-"+nodeName, "system", kv)
}
