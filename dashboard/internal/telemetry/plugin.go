package telemetry

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"git.konoss.org/kore/schmutz/dashboard/pkg/browzer/bff"
)

// Plugin is a BFF plugin exposing SSE stream and snapshot endpoints.
type Plugin struct {
	Hub *Hub
}

func (p *Plugin) Name() string        { return "telemetry-stream" }
func (p *Plugin) Description() string { return "Live telemetry via SSE from relay.tango" }

func (p *Plugin) Register(router *bff.Router) {
	// SSE stream — browser connects and receives live frames
	router.HandleRaw("GET /api/telemetry/stream", p.sseHandler)
	// Snapshot — polling fallback, served from hub snapshot
	router.HandleRaw("GET /api/telemetry/live", p.snapshotHandler)
}

func (p *Plugin) sseHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ch := p.Hub.Subscribe()
	defer p.Hub.Unsubscribe(ch)

	// Send current snapshot immediately so browser has data before first frame arrives.
	snap := p.Hub.Snapshot()
	for key, payload := range snap {
		fmt.Fprintf(w, "data: {\"key\":%q,\"payload\":%s}\n\n", key, payload)
	}
	flusher.Flush()

	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		case frame, ok := <-ch:
			if !ok {
				return
			}
			b, err := json.Marshal(frame)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
		}
	}
}

func (p *Plugin) snapshotHandler(w http.ResponseWriter, r *http.Request) {
	snap := p.Hub.Snapshot()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(snap)
}
