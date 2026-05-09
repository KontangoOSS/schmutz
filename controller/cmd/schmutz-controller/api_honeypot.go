package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"
)

// honeypotStore is the subset of StoreService used by honeypot handlers.
// Allows test injection without a real Bao connection.
type honeypotStore interface {
	PutHoneypot(fingerprint string, data map[string]interface{}) error
	GetHoneypot(fingerprint string) (map[string]interface{}, error)
}

// AuthCheck returns the enrollment status for a browser fingerprint.
// Called by 404rd on every return visit with a _zt cookie.
//
// GET /api/auth/check?fingerprint=<value>
// Response: {"status":"pending|approved|enrolled|denied","id":"<machine-id>"}
func (a *API) AuthCheck(w http.ResponseWriter, r *http.Request) {
	fp := r.URL.Query().Get("fingerprint")
	if fp == "" {
		errJSON(w, "fingerprint required", http.StatusBadRequest)
		return
	}

	rec, err := a.hpStore.GetHoneypot(fp)
	if err != nil {
		log.Printf("auth/check: store error for %s: %v", fp, err)
		errJSON(w, "store error", http.StatusInternalServerError)
		return
	}
	if rec == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "denied"})
		return
	}

	id, _ := rec["id"].(string)
	state, _ := rec["state"].(string)
	ott, _ := rec["ott"].(string)

	status := "pending"
	switch state {
	case "approved":
		status = "approved"
	case "active", "enrolled":
		status = "enrolled"
	case "banned", "rejected":
		status = "denied"
	}

	resp := map[string]string{
		"status": status,
		"id":     id,
	}
	if ott != "" && status == "approved" {
		resp["ott"] = ott
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HoneypotApprove approves a browser fingerprint enrollment.
// It sets the honeypot record state to "approved", provisions a Ziti identity,
// and stores the returned OTT so AuthCheck can deliver it to 404rd.
//
// POST /api/honeypot/approve/{fingerprint}
// Response: {"status":"approved","ott":"<jwt>"}
func (a *API) HoneypotApprove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	fp := strings.TrimPrefix(r.URL.Path, "/api/honeypot/approve/")
	if fp == "" {
		errJSON(w, "fingerprint required", http.StatusBadRequest)
		return
	}

	rec, err := a.hpStore.GetHoneypot(fp)
	if err != nil {
		log.Printf("honeypot/approve: store error for %s: %v", fp, err)
		errJSON(w, "store error", http.StatusInternalServerError)
		return
	}
	if rec == nil {
		errJSON(w, "fingerprint not found", http.StatusNotFound)
		return
	}

	// Already approved — return existing OTT if present
	if state, _ := rec["state"].(string); state == "approved" || state == "enrolled" {
		ott, _ := rec["ott"].(string)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": state, "ott": ott})
		return
	}

	// Provision Ziti identity for this browser fingerprint.
	// If Ziti is unavailable (e.g. TEST_MODE), approve without OTT — mock services don't need it.
	identityName := "browser-" + fp
	if a.ziti != nil {
		_, jwt, err := a.ziti.CreateIdentity(identityName, []string{"browzer"}, map[string]interface{}{
			"source":      "browser-enrollment",
			"fingerprint": fp,
		})
		if err != nil {
			log.Printf("honeypot/approve: ziti unavailable for %s, approving without OTT: %v", fp, err)
		} else {
			rec["ott"] = jwt
			log.Printf("honeypot/approve: provisioned ziti identity %s for fp=%s", identityName, fp)
		}
	}

	rec["state"] = "approved"
	rec["approved_at"] = time.Now().Unix()

	if err := a.hpStore.PutHoneypot(fp, rec); err != nil {
		log.Printf("honeypot/approve: store update failed for %s: %v", fp, err)
		errJSON(w, "store error", http.StatusInternalServerError)
		return
	}

	ott, _ := rec["ott"].(string)
	log.Printf("honeypot/approve: approved fp=%s", fp)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "approved", "ott": ott})
}

// HoneypotPush receives honeypot events from 404rd.
// Only "enrollment" type events are stored as honeypot records for auth/check lookups.
//
// POST /api/honeypot/push
// Body: {"type":"enrollment","controller":"ctrl-1","ip":"1.2.3.4","data":{...}}
// Response: 204 No Content
func (a *API) HoneypotPush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		Type       string                 `json:"type"`
		Controller string                 `json:"controller"`
		IP         string                 `json:"ip"`
		Data       map[string]interface{} `json:"data"`
	}
	if !parseJSON(r, w, &payload) {
		return
	}

	if payload.Type == "enrollment" && payload.Data != nil {
		fp, _ := payload.Data["fingerprint"].(string)
		if fp != "" {
			record := map[string]interface{}{
				"state":       "pending",
				"ip":          payload.IP,
				"controller":  payload.Controller,
				"received_at": time.Now().Unix(),
			}
			for k, v := range payload.Data {
				record[k] = v
			}
			if err := a.hpStore.PutHoneypot(fp, record); err != nil {
				log.Printf("honeypot/push: store error for %s: %v", fp, err)
			} else {
				log.Printf("honeypot/push: stored enrollment for fp=%s ip=%s", fp, payload.IP)
			}
		}
	}

	w.WriteHeader(http.StatusNoContent)
}
