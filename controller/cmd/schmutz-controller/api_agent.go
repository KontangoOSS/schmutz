package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	agentmod "git.konoss.org/kore/schmutz/controller/internal/controller/agent"
	"git.konoss.org/kore/schmutz/controller/internal/service"
)

// Heartbeat handles POST /api/heartbeat.
//
// Open to all machines — no auth, no identity required. This is how unclaimed
// machines announce themselves and how enrolled machines fall back when the
// overlay is unreachable.
//
// Response: always JSON. Normally {"cmd":"noop"}. When the controller has
// queued a command (enroll JWT, set_interval, etc.) it is returned here and
// cleared — one delivery, consumed on receipt.
func (a *API) Heartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Agents POST a typed Event envelope. We accept both the new envelope format
	// and legacy bare Heartbeat for backwards compatibility during rollout.
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	machineID := r.Header.Get("X-Machine-ID")

	// Try Event envelope (zlib-compressed new agents, or raw JSON legacy).
	ev, evErr := service.DecodeEvent(body)
	if evErr == nil && ev.Type != "" {
		if ev.MachineID == "" {
			ev.MachineID = machineID
		}
		if ev.MachineID == "" {
			http.Error(w, "machine_id required", http.StatusBadRequest)
			return
		}
		machineID = ev.MachineID
		a.telemetry.RecordEvent(ev)
	} else {
		// Legacy bare Heartbeat (no envelope).
		var hb service.Heartbeat
		if err := json.Unmarshal(body, &hb); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if hb.MachineID == "" {
			hb.MachineID = machineID
		}
		if hb.MachineID == "" {
			http.Error(w, "machine_id required", http.StatusBadRequest)
			return
		}
		machineID = hb.MachineID
		a.telemetry.RecordHeartbeat(&hb)
	}

	resp := a.telemetry.NextResponse(machineID)

	// If no command queued, send hello — same interval the overlay sends on
	// connect. Agent uses it to set its tick rate. Nothing more.
	if resp.Cmd == "noop" {
		resp.Cmd = "hello"
		resp.Payload = agentmod.BuildHeartbeatHello(machineID, a.store)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// ClaimMachine handles POST /api/machines/claim/{id}.
//
// Queues an enroll command for the next heartbeat. The machine doesn't need
// to be enrolled or have a config channel — it picks up the command on its
// next POST /api/heartbeat.
//
// Body: {"method":"invite","token":"..."} or {"method":"approle","role_id":"...","secret_id":"..."}
// Auth: requires management bearer token (enforced by router middleware).
func (a *API) ClaimMachine(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	machineID := strings.TrimPrefix(r.URL.Path, "/api/machines/claim/")
	if machineID == "" {
		http.Error(w, "machine id required", http.StatusBadRequest)
		return
	}

	var body struct {
		Method   string `json:"method"`  // "invite", "approle", "oidc"
		Token    string `json:"token"`   // invite token
		RoleID   string `json:"role_id"` // AppRole
		SecretID string `json:"secret_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Build the enroll command payload — machine runs the install script
	// pointed at our join URL with these credentials embedded.
	payload, _ := json.Marshal(map[string]interface{}{
		"url":       a.cfg.ListenAddr,
		"method":    body.Method,
		"token":     body.Token,
		"role_id":   body.RoleID,
		"secret_id": body.SecretID,
	})
	a.telemetry.QueueCommand(machineID, service.HeartbeatResponse{
		Cmd:     "enroll",
		Payload: payload,
	})

	a.telemetry.SetStatus(machineID, service.StatusQuarantine)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "queued", "machine_id": machineID})
}

// LiveMachines handles GET /api/machines/live — returns machines with a
// heartbeat in the last 5 minutes, plus which ones have open config channels.
func (a *API) LiveMachines(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	beats := a.telemetry.LiveMachines(5 * time.Minute)
	connected := make(map[string]bool)
	for _, id := range a.telemetry.ConnectedMachines() {
		connected[id] = true
	}

	type row struct {
		MachineID  string                `json:"machine_id"`
		Hostname   string                `json:"hostname"`
		OS         string                `json:"os"`
		Arch       string                `json:"arch"`
		UptimeSecs int64                 `json:"uptime_sec"`
		CPUCores   int                   `json:"cpu_cores"`
		MemoryMB   int64                 `json:"memory_mb,omitempty"`
		LoadAvg1   float64               `json:"load_avg_1,omitempty"`
		Status     service.MachineStatus `json:"status"`
		SeenAt     time.Time             `json:"seen_at"`
		ConfigOpen bool                  `json:"config_open"`
	}

	rows := make([]row, 0, len(beats))
	for _, hb := range beats {
		rows = append(rows, row{
			MachineID:  hb.MachineID,
			Hostname:   hb.Hostname,
			OS:         hb.OS,
			Arch:       hb.Arch,
			UptimeSecs: hb.UptimeSecs,
			CPUCores:   hb.CPUCores,
			MemoryMB:   hb.MemoryMB,
			LoadAvg1:   hb.LoadAvg1,
			Status:     hb.Status,
			SeenAt:     hb.SeenAt,
			ConfigOpen: connected[hb.MachineID],
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"machines": rows,
		"total":    len(rows),
	})
}

// PushInstruction handles POST /api/machines/push/{id} — sends an instruction
// to a connected machine over its open config.tango channel.
//
// Body: {"type":"reload"} or {"type":"set_interval","payload":{"seconds":30}}
func (a *API) PushInstruction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	machineID := strings.TrimPrefix(r.URL.Path, "/api/machines/push/")
	if machineID == "" {
		http.Error(w, "machine id required", http.StatusBadRequest)
		return
	}

	var instr service.Instruction
	if err := json.NewDecoder(r.Body).Decode(&instr); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if instr.Type == "" {
		http.Error(w, "type required", http.StatusBadRequest)
		return
	}

	// Push instruction via config.tango TCP channel.
	if err := a.telemetry.Push(machineID, instr); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "sent", "machine_id": machineID, "type": instr.Type})
}
