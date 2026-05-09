package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// PulseState handles GET /api/pulse/live — returns KV state for all machines.
func (a *API) PulseState(w http.ResponseWriter, r *http.Request) {
	allKV := a.telemetry.AllMachineKV()

	type machineInfo struct {
		MachineID string            `json:"machine_id"`
		KV        map[string]string `json:"kv"`
		Hostname  string            `json:"hostname,omitempty"`
		LastSeen  string            `json:"last_seen,omitempty"`
	}

	machines := make([]machineInfo, 0, len(allKV))
	for id, kv := range allKV {
		_, seen := a.telemetry.MachineKV(id)
		info := machineInfo{
			MachineID: id,
			KV:        kv,
			Hostname:  kv["system:hostname"],
		}
		if !seen.IsZero() {
			info.LastSeen = seen.Format(time.RFC3339Nano)
		}
		machines = append(machines, info)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"machines": machines,
		"total":    len(machines),
	})
}

// PulseMachine handles GET /api/pulse/{id} — returns KV state for one machine.
func (a *API) PulseMachine(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/pulse/")
	if id == "" || id == "live" {
		a.PulseState(w, r)
		return
	}

	kv, seen := a.telemetry.MachineKV(id)
	if kv == nil {
		http.Error(w, "machine not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"machine_id": id,
		"kv":         kv,
		"last_seen":  seen.Format(time.RFC3339Nano),
	})
}
