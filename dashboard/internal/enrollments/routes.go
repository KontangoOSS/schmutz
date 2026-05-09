package enrollments

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"git.konoss.org/kore/schmutz/dashboard/internal/store"
	"git.konoss.org/kore/schmutz/dashboard/pkg/browzer/bff"
)

var controllerClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	},
}

// PendingMachine is a simplified view of an identity waiting for operator action.
// Status values:
//   "booting"  — blue-demo role: Hook OS booted, OS not installed yet, QR claim in progress
//   "pending"  — quarantine role: schmutz enrolled, waiting for operator approval
type PendingMachine struct {
	ID         string   `json:"id"`
	MachineID  string   `json:"machine_id"`
	Name       string   `json:"name"`
	Nickname   string   `json:"nickname"`
	Attributes []string `json:"attributes"`
	Online     bool     `json:"online"`
	Status     string   `json:"status"`
	MAC        string   `json:"mac,omitempty"`
	Source     string   `json:"source,omitempty"`
}

// zitiPasswordAuth gets a management API token via password auth directly.
func (p *Plugin) zitiPasswordAuth() (string, error) {
	if p.ZitiAddr == "" || p.ZitiPass == "" {
		return "", fmt.Errorf("ZitiAddr/ZitiPass not configured")
	}
	body, _ := json.Marshal(map[string]string{"username": p.ZitiUser, "password": p.ZitiPass})
	url := "https://" + p.ZitiAddr + "/edge/management/v1/authenticate?method=password"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := controllerClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result struct {
		Data struct{ Token string `json:"token"` } `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.Data.Token, nil
}

func (p *Plugin) listEnrollments(w http.ResponseWriter, r *http.Request) {
	tok, err := p.zitiPasswordAuth()
	if err != nil || p.Ziti == nil {
		// Fallback to local postgres
		status := r.URL.Query().Get("status")
		recs, err := p.Store.ListEnrollments(r.Context(), status)
		if err != nil {
			bff.WriteError(w, 500, "list enrollments failed", err)
			return
		}
		if recs == nil {
			recs = []store.EnrollmentRecord{}
		}
		bff.WriteJSON(w, recs)
		return
	}

	// Fetch both quarantine (pending approval) and blue-demo (booting, pre-OS) in one pass.
	// Both are identity stages that need operator visibility — just different actions available.
	type zitiIdentity struct {
		ID                  string                 `json:"id"`
		Name                string                 `json:"name"`
		RoleAttributes      []string               `json:"roleAttributes"`
		HasAPISession       bool                   `json:"hasApiSession"`
		HasEdgeRouterConnection bool               `json:"hasEdgeRouterConnection"`
		AppData             map[string]interface{} `json:"appData"`
	}

	fetch := func(filter string) []zitiIdentity {
		raw, err := p.Ziti.Get(tok, `/edge/management/v1/identities?filter=`+filter+`&limit=100`)
		if err != nil {
			return nil
		}
		var result struct {
			Data []zitiIdentity `json:"data"`
		}
		json.Unmarshal(raw, &result)
		return result.Data
	}

	quarantine := fetch(`anyOf(roleAttributes)+%3D+%22quarantine%22`)
	booting := fetch(`anyOf(roleAttributes)+%3D+%22blue-demo%22`)

	out := make([]PendingMachine, 0, len(quarantine)+len(booting))

	toMachine := func(i zitiIdentity, status string) PendingMachine {
		// Nickname: prefer host-* role attr, fall back to appData nickname
		nick := ""
		for _, a := range i.RoleAttributes {
			if len(a) > 5 && a[:5] == "host-" {
				nick = a[5:]
				break
			}
		}
		if nick == "" && i.AppData != nil {
			nick, _ = i.AppData["nickname"].(string)
		}
		mac := ""
		source := ""
		if i.AppData != nil {
			mac, _ = i.AppData["mac"].(string)
			source, _ = i.AppData["source"].(string)
		}
		return PendingMachine{
			ID:         i.ID,
			MachineID:  i.ID,
			Name:       i.Name,
			Nickname:   nick,
			Attributes: i.RoleAttributes,
			Online:     i.HasEdgeRouterConnection,
			Status:     status,
			MAC:        mac,
			Source:     source,
		}
	}

	for _, i := range quarantine {
		out = append(out, toMachine(i, "pending"))
	}
	for _, i := range booting {
		out = append(out, toMachine(i, "booting"))
	}

	bff.WriteJSON(w, out)
}

func (p *Plugin) liveTelemetry(w http.ResponseWriter, r *http.Request) {
	bff.WriteJSON(w, p.Memory.AllLive())
}

func (p *Plugin) approve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MachineID string `json:"machine_id"`
		App       string `json:"app"`
		Env       string `json:"env"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.MachineID == "" {
		bff.WriteError(w, 400, "machine_id required", err)
		return
	}
	if err := p.callControllerApprove(r.Context(), req.MachineID, req.App, req.Env); err != nil {
		bff.WriteError(w, 502, "controller approve failed", err)
		return
	}
	if p.Store != nil {
		p.Store.UpdateEnrollmentStatus(r.Context(), req.MachineID, "approved", "reviewer approved", "reviewer")
	}
	bff.WriteJSON(w, map[string]string{"machine_id": req.MachineID, "status": "approved"})
}

func (p *Plugin) deny(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MachineID string `json:"machine_id"`
		Reason    string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.MachineID == "" {
		bff.WriteError(w, 400, "machine_id required", err)
		return
	}
	if req.Reason == "" {
		req.Reason = "denied by reviewer"
	}
	if err := p.callController(r.Context(), "deny", req.MachineID, req.Reason); err != nil {
		bff.WriteError(w, 502, "controller deny failed", err)
		return
	}
	if err := p.Store.UpdateEnrollmentStatus(r.Context(), req.MachineID, "denied", req.Reason, "reviewer"); err != nil {
		bff.WriteError(w, 500, "update status failed", err)
		return
	}
	bff.WriteJSON(w, map[string]string{"machine_id": req.MachineID, "status": "denied"})
}

func (p *Plugin) flush(w http.ResponseWriter, r *http.Request) {
	p.Memory.ForceFlush()
	bff.WriteJSON(w, map[string]string{"status": "flushed"})
}

func (p *Plugin) callControllerApprove(ctx context.Context, machineID, app, env string) error {
	body, _ := json.Marshal(map[string]string{
		"machine_id": machineID,
		"app":        app,
		"env":        env,
	})
	return p.postController(ctx, "approve", body)
}

func (p *Plugin) callController(ctx context.Context, action, machineID, reason string) error {
	body, _ := json.Marshal(map[string]string{
		"machine_id": machineID,
		"reason":     reason,
	})
	return p.postController(ctx, action, body)
}

func (p *Plugin) postController(ctx context.Context, action string, body []byte) error {
	url := fmt.Sprintf("%s/api/ops/%s", p.ControllerURL, action)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := controllerClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("controller returned %d", resp.StatusCode)
	}
	return nil
}
