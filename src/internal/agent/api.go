package agent

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"runtime"

	syscollector "github.com/KontangoOSS/schmutz/internal/collector"
	"github.com/shirou/gopsutil/v3/host"
)

// NewAPIHandler returns an http.Handler with device management endpoints.
func NewAPIHandler() http.Handler {
	mux := http.NewServeMux()

	// GET /health → {"status":"ok"}
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
	})

	// GET /status → basic host info
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		hostname, _ := os.Hostname()
		info, _ := host.Info()
		var uptime uint64
		var platform string
		if info != nil {
			uptime = info.Uptime
			platform = info.Platform
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"hostname": hostname,
			"os":       runtime.GOOS,
			"arch":     runtime.GOARCH,
			"platform": platform,
			"uptime":   uptime,
		})
	})

	// GET /info → full machine info from CollectSystem
	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		sys, err := syscollector.CollectSystem()
		if err != nil {
			http.Error(w, "collection failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sys) //nolint:errcheck
	})

	// GET /metrics → latest snapshot from CollectAll
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		snapshot := syscollector.CollectAll()
		// Convert uint8 keys to string names for a readable JSON response.
		out := make(map[string]interface{}, len(snapshot))
		for k, v := range snapshot {
			out[msgName(k)] = v
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out) //nolint:errcheck
	})

	return mux
}

// ServeAPI serves HTTP on the provided listener. Blocks until listener closes.
func ServeAPI(ln net.Listener) error {
	srv := &http.Server{Handler: NewAPIHandler()}
	return srv.Serve(ln)
}

// msgName maps a stream message type byte to a human-readable key.
// Mirrors stream.MsgName without importing the stream package.
func msgName(t uint8) string {
	switch t {
	case 0x01:
		return "heartbeat"
	case 0x02:
		return "system"
	case 0x03:
		return "network"
	case 0x04:
		return "process"
	case 0x05:
		return "disk"
	case 0x06:
		return "service"
	case 0x07:
		return "log"
	case 0x08:
		return "event"
	case 0xFF:
		return "custom"
	default:
		return "unknown"
	}
}
