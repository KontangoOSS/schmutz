package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/KontangoOSS/schmutz/controller/internal/service"
)

// --- Machines ---

func (a *API) Machine(w http.ResponseWriter, r *http.Request) {
	if !a.requireStore(w) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/machines/")

	// Sub-resource: /api/machines/by-hostname/{name}
	if strings.HasPrefix(id, "by-hostname/") {
		hostname := strings.TrimPrefix(id, "by-hostname/")
		machineID, nickname, exists := a.store.LookupByHostname(hostname)
		if !exists {
			errJSON(w, "not found", http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"id": machineID, "nickname": nickname})
		return
	}

	switch r.Method {
	case http.MethodGet:
		data, err := a.store.GetMachine(id)
		if err != nil || data == nil {
			errJSON(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(data)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Profiles ---

func (a *API) Profiles(w http.ResponseWriter, r *http.Request) {
	if !a.requireStore(w) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		data, err := a.store.ListProfiles()
		jsonOrErr(w, map[string]interface{}{"profiles": data}, err)
	case http.MethodPost:
		var req struct {
			Name string                 `json:"name"`
			Data map[string]interface{} `json:"data"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.store.PutProfile(req.Name, req.Data)
		jsonOrErr(w, map[string]string{"status": "created"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) Profile(w http.ResponseWriter, r *http.Request) {
	if !a.requireStore(w) {
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/api/profiles/")
	switch r.Method {
	case http.MethodGet:
		data, err := a.store.GetProfile(name)
		jsonOrErr(w, data, err)
	case http.MethodPut:
		var req map[string]interface{}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.store.PutProfile(name, req)
		jsonOrErr(w, map[string]string{"status": "updated"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Debug ---

// DebugEvents returns the recent event ring buffers for a machine.
// GET /api/debug/events/<machineID>           — all event types
// GET /api/debug/events/<machineID>/<type>    — specific type (hb, net, log)
func (a *API) DebugEvents(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/debug/events/")
	parts := strings.SplitN(path, "/", 2)
	machineID := parts[0]
	evType := ""
	if len(parts) == 2 {
		evType = parts[1]
	}

	w.Header().Set("Content-Type", "application/json")

	if evType != "" {
		events := a.telemetry.RecentEvents(machineID, evType)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"machine_id": machineID,
			"type":       evType,
			"count":      len(events),
			"events":     events,
		})
		return
	}

	// All types
	result := map[string]interface{}{
		"machine_id": machineID,
		"hb":         a.telemetry.RecentEvents(machineID, "hb"),
		"net":        a.telemetry.RecentEvents(machineID, "net"),
		"log":        a.telemetry.RecentEvents(machineID, "log"),
	}
	// Also include live heartbeat summary
	live := a.telemetry.LiveMachines(24 * time.Hour)
	for _, m := range live {
		if m.MachineID == machineID {
			result["live"] = m
			break
		}
	}
	json.NewEncoder(w).Encode(result)
}

// --- Known machines ---

func (a *API) KnownMachine(w http.ResponseWriter, r *http.Request) {
	if !a.requireStore(w) {
		return
	}
	hostname := strings.TrimPrefix(r.URL.Path, "/api/known/")
	data, err := a.store.GetKnownMachine(hostname)
	if err != nil || data == nil {
		errJSON(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// --- Security ---

func (a *API) SecurityBan(w http.ResponseWriter, r *http.Request) {
	if !a.requireStore(w) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/security/bans/")
	switch r.Method {
	case http.MethodGet:
		banned := a.store.IsBanned(id)
		json.NewEncoder(w).Encode(map[string]bool{"banned": banned})
	case http.MethodPut:
		var req struct {
			Reason string `json:"reason"`
		}
		parseJSON(r, w, &req)
		err := a.store.BanDevice(id, req.Reason)
		jsonOrErr(w, map[string]string{"status": "banned"}, err)
	case http.MethodDelete:
		err := a.store.UnbanDevice(id)
		jsonOrErr(w, map[string]string{"status": "unbanned"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) SecurityCheckDuplicate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Hostname  string   `json:"hostname"`
		MachineID string   `json:"machine_id"`
		MACs      []string `json:"macs"`
	}
	if !parseJSON(r, w, &req) {
		return
	}
	conflictID, isDup := a.security.CheckDuplicate(req.Hostname, req.MachineID, req.MACs)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"duplicate": isDup, "conflict_id": conflictID,
	})
}

func (a *API) SecurityValidateFingerprint(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MachineID    string `json:"machine_id"`
		HardwareHash string `json:"hardware_hash"`
	}
	if !parseJSON(r, w, &req) {
		return
	}
	valid := a.security.ValidateFingerprint(req.MachineID, req.HardwareHash)
	json.NewEncoder(w).Encode(map[string]bool{"valid": valid})
}

func (a *API) SecurityAudit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MachineID string                 `json:"machine_id"`
		Event     string                 `json:"event"`
		SourceIP  string                 `json:"source_ip"`
		Details   map[string]interface{} `json:"details"`
	}
	if !parseJSON(r, w, &req) {
		return
	}
	a.security.Audit(req.MachineID, req.Event, req.SourceIP, req.Details)
	okJSON(w, "logged")
}

// --- Discovery ---

func (a *API) DiscoveryMatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Hostname     string   `json:"hostname"`
		MachineID    string   `json:"machine_id"`
		HardwareHash string   `json:"hardware_hash"`
		MACs         []string `json:"macs"`
	}
	if !parseJSON(r, w, &req) {
		return
	}
	match := a.discovery.MatchDevice(req.Hostname, req.MachineID, req.HardwareHash, req.MACs)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(match)
}

func (a *API) DiscoveryAutoClaim(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ZitiID string      `json:"ziti_id"`
		Match  *matchInput `json:"match"`
	}
	if !parseJSON(r, w, &req) {
		return
	}
	token, err := a.ziti.Authenticate()
	if err != nil {
		errJSON(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Build MatchResult from input
	mr := &matchResult{
		Matched:    true,
		EntityID:   req.Match.EntityID,
		Attributes: req.Match.Attributes,
		MatchType:  req.Match.MatchType,
		Confidence: req.Match.Confidence,
	}
	err = a.discovery.AutoClaim(a.ziti, token, req.ZitiID, mr)
	jsonOrErr(w, map[string]string{"status": "claimed"}, err)
}

// matchInput is the JSON shape for auto-claim requests.
type matchInput struct {
	EntityID   string   `json:"entity_id"`
	Attributes []string `json:"attributes"`
	MatchType  string   `json:"match_type"`
	Confidence string   `json:"confidence"`
}

// matchResult wraps DiscoveryService.MatchResult for the AutoClaim call.
type matchResult = service.MatchResult

// --- Metrics ---

func (a *API) MetricsStatus(w http.ResponseWriter, r *http.Request) {
	token, err := a.ziti.Authenticate()
	if err != nil {
		errJSON(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data := a.metrics.FullStatus(token)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func (a *API) MetricsZiti(w http.ResponseWriter, r *http.Request) {
	token, err := a.ziti.Authenticate()
	if err != nil {
		errJSON(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data, err := a.metrics.ZitiCluster(token)
	jsonOrErr(w, data, err)
}

func (a *API) MetricsBao(w http.ResponseWriter, r *http.Request) {
	data, err := a.metrics.BaoCluster()
	jsonOrErr(w, data, err)
}

func (a *API) MetricsMachine(w http.ResponseWriter, r *http.Request) {
	data := a.metrics.MachineMetrics()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func (a *API) MetricsLatency(w http.ResponseWriter, r *http.Request) {
	data := a.metrics.NetworkLatency()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// --- Catalog ---

func (a *API) CatalogApplications(w http.ResponseWriter, r *http.Request) {
	if !a.requireStore(w) {
		return
	}
	apps, err := a.store.List("public", "applications/")
	if err != nil {
		errJSON(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Deduplicate (Bao lists both "name" and "name/" entries)
	seen := map[string]bool{}
	var names []string
	for _, k := range apps {
		name := strings.TrimSuffix(k, "/")
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"applications": names})
}

func (a *API) CatalogApplication(w http.ResponseWriter, r *http.Request) {
	if !a.requireStore(w) {
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/api/catalog/applications/")
	data, err := a.store.Get("public", "applications/"+name)
	if err != nil || data == nil {
		errJSON(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// --- Ops: Approve / Deny ---

// Approve promotes a quarantined identity to #lan, writes to Bao, and tags app/env.
func (a *API) Approve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		MachineID string `json:"machine_id"`
		App       string `json:"app"`
		Env       string `json:"env"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.MachineID == "" {
		http.Error(w, "machine_id required", http.StatusBadRequest)
		return
	}

	token, err := a.ziti.Authenticate()
	if err != nil {
		errJSON(w, "ziti auth failed", http.StatusBadGateway)
		return
	}

	// Resolve machine_id → Ziti internal ID + pull appData (has hostname, MACs, SSH keys, slug).
	zitiID := req.MachineID
	var appData map[string]interface{}
	if id, _, ad, err := a.ziti.GetIdentityByName(token, req.MachineID); err == nil && id != "" {
		zitiID = id
		appData = ad
	} else {
		// req.MachineID may already be a zitiID — fetch appData directly
		if ad, err := a.ziti.GetIdentityAppData(token, zitiID); err == nil {
			appData = ad
		}
	}

	// Step 4 (existing): remove quarantine, add lan
	if err := a.ziti.UpdateIdentityRemoveAttr(token, zitiID, "quarantine"); err != nil {
		errJSON(w, fmt.Sprintf("remove quarantine: %v", err), http.StatusInternalServerError)
		return
	}
	if err := a.ziti.UpdateIdentityAddAttr(token, zitiID, "lan"); err != nil {
		errJSON(w, fmt.Sprintf("add lan: %v", err), http.StatusInternalServerError)
		return
	}

	// Step 6: tag app and env on the Ziti identity if provided
	if req.App != "" {
		a.ziti.UpdateIdentityAddAttr(token, zitiID, "app="+req.App)
	}
	if req.Env != "" {
		a.ziti.UpdateIdentityAddAttr(token, zitiID, "env="+req.Env)
	}

	// Steps 5 & 7: write machine record + SSH keys to Bao
	if a.store != nil && appData != nil {
		nickname, _ := appData["nickname"].(string)
		hostname, _ := appData["hostname"].(string)

		// Pick the first real MAC (skip all-zeros, broadcast, virtual)
		mac := ""
		if macs, ok := appData["macs"].([]interface{}); ok {
			for _, m := range macs {
				s, _ := m.(string)
				if s != "" && s != "ff:ff:ff:ff:ff:ff" && s != "0" &&
					len(s) == 17 && !strings.HasPrefix(s, "fe:") {
					mac = s
					break
				}
			}
		}

		// Build the Bao machine record — keyed by nickname (slug) for easy lookup
		key := nickname
		if key == "" {
			key = zitiID
		}
		record := map[string]interface{}{
			"ziti_id":      zitiID,
			"hostname":     hostname,
			"mac":          mac,
			"nickname":     nickname,
			"approved_at":  time.Now().UTC().Format(time.RFC3339),
			"state":        "approved",
			"controller_url": "https://join.kontango.net",
		}
		if req.App != "" {
			record["app"] = req.App
		}
		if req.Env != "" {
			record["env"] = req.Env
		}
		// Carry over fields written at enrollment time
		for _, field := range []string{"hostname", "os", "os_version", "arch", "machine_id", "source", "registered_at"} {
			if v, ok := appData[field]; ok {
				record[field] = v
			}
		}
		a.store.PutMachine(key, record)

		// Step 7: SSH host keys — stored separately for easy retrieval
		if keys, ok := appData["ssh_host_keys"].([]interface{}); ok && len(keys) > 0 {
			keyMap := map[string]interface{}{"hostname": hostname, "ziti_id": zitiID}
			for i, k := range keys {
				keyMap[fmt.Sprintf("key_%d", i)] = k
			}
			a.store.Put("secret", "identities/machines/"+key+"/ssh_host_keys", keyMap)
		}

		log.Printf("approve: wrote Bao record for %s (ziti_id=%s mac=%s)", key, zitiID, mac)
	}

	jsonOrErr(w, map[string]string{
		"status":     "approved",
		"machine_id": req.MachineID,
		"ziti_id":    zitiID,
	}, nil)
}

// Deny disables a quarantined identity by deleting it.
func (a *API) Deny(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		MachineID string `json:"machine_id"`
		Reason    string `json:"reason"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.MachineID == "" {
		http.Error(w, "machine_id required", http.StatusBadRequest)
		return
	}
	token, err := a.ziti.Authenticate()
	if err != nil {
		errJSON(w, "ziti auth failed", http.StatusBadGateway)
		return
	}
	zitiID := req.MachineID
	if id, _, _, err := a.ziti.GetIdentityByName(token, req.MachineID); err == nil && id != "" {
		zitiID = id
	}
	if err := a.ziti.DeleteIdentity(token, zitiID); err != nil {
		errJSON(w, fmt.Sprintf("delete identity: %v", err), http.StatusInternalServerError)
		return
	}
	jsonOrErr(w, map[string]string{"status": "denied", "machine_id": req.MachineID, "ziti_id": zitiID}, nil)
}

// Unused import guard
var _ = time.Now
