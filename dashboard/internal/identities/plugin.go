// Package identities provides the combined Bao + Ziti identity view.
//
// Each Bao machine entry is the primary identity. The Ziti identity
// (matched by UUID name) shows the network state. Drill down shows
// all Ziti services, policies, and connection status for that machine.
package identities

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	baoclient "git.konoss.org/kore/schmutz/dashboard/pkg/browzer/bao"
	"git.konoss.org/kore/schmutz/dashboard/pkg/browzer/bff"
	"git.konoss.org/kore/schmutz/dashboard/pkg/browzer/ziti"
)

// CombinedIdentity merges Bao machine data with Ziti identity state.
type CombinedIdentity struct {
	// Bao fields
	ID           string         `json:"id"`
	Nickname     string         `json:"nickname"`
	Hostname     string         `json:"hostname"`
	State        string         `json:"state"`
	Source       string         `json:"source"`
	RegisteredAt any            `json:"registered_at"`
	OS           map[string]any `json:"os,omitempty"`
	Hardware     map[string]any `json:"hardware,omitempty"`
	Network      map[string]any `json:"network,omitempty"`
	Location     map[string]any `json:"location,omitempty"`

	// Ziti fields (nil if no matching Ziti identity)
	ZitiID                 string   `json:"ziti_id,omitempty"`
	ZitiOnline             bool     `json:"ziti_online"`
	ZitiHasSession         bool     `json:"ziti_has_session"`
	ZitiRoles              []string `json:"ziti_roles,omitempty"`
	ZitiType               string   `json:"ziti_type,omitempty"`
	ZitiDefaultHostingCost int      `json:"ziti_default_hosting_cost,omitempty"`

	// Derived
	HasZitiMatch bool `json:"has_ziti_match"`
}

type Plugin struct {
	Bao *baoclient.Client
	Hub interface {
		Snapshot() map[string]json.RawMessage
	}
}

func (p *Plugin) Name() string        { return "identities" }
func (p *Plugin) Description() string { return "Combined Bao + Ziti identity view with drill-down" }

func (p *Plugin) Register(router *bff.Router) {
	z := router.Ziti
	b := p.Bao

	// List all combined identities
	router.Handle("GET /api/v1/identities", func(token string, w http.ResponseWriter, r *http.Request) {
		combined, err := buildCombinedList(z, token, b)
		if err != nil {
			bff.WriteError(w, 502, "build identity list failed", err)
			return
		}
		bff.WriteJSON(w, combined)
	})

	// Get single combined identity with full drill-down
	router.Handle("GET /api/v1/identities/{id}", func(token string, w http.ResponseWriter, r *http.Request) {
		machineID := r.PathValue("id")
		detail, err := buildIdentityDetail(z, token, b, machineID)
		if err != nil {
			bff.WriteError(w, 404, "identity not found", err)
			return
		}
		bff.WriteJSON(w, detail)
	})

	// Enrollment snapshot — full immutable record from Bao assets/ namespace
	router.Handle("GET /api/v1/identities/{id}/snapshot", func(token string, w http.ResponseWriter, r *http.Request) {
		machineID := r.PathValue("id")
		data, err := b.GetFromMount("assets", "identity/enrollment/"+machineID)
		if err != nil || data == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"snapshot not found"}`))
			return
		}
		bff.WriteJSON(w, data)
	})

	// Live telemetry — latest frames for this machine from the SSE hub snapshot
	router.HandleRaw("GET /api/v1/identities/{id}/telemetry", func(w http.ResponseWriter, r *http.Request) {
		machineID := r.PathValue("id")
		if p.Hub == nil {
			bff.WriteJSON(w, map[string]interface{}{"machine_id": machineID, "frames": nil})
			return
		}
		all := p.Hub.Snapshot()
		frames := map[string]json.RawMessage{}
		prefix := machineID + "/"
		for k, v := range all {
			if strings.HasPrefix(k, prefix) {
				frameType := strings.TrimPrefix(k, prefix)
				frames[frameType] = v
			}
		}
		bff.WriteJSON(w, map[string]interface{}{
			"machine_id": machineID,
			"frames":     frames,
		})
	})
}

func buildCombinedList(z *ziti.Client, token string, b *baoclient.Client) ([]CombinedIdentity, error) {
	// Load Bao machines
	machineIDs, err := b.List("identities/machines/")
	if err != nil {
		return nil, fmt.Errorf("list bao machines: %w", err)
	}

	// Load Ziti identities
	raw, err := z.Get(token, "/edge/management/v1/identities?limit=500")
	if err != nil {
		return nil, fmt.Errorf("list ziti identities: %w", err)
	}
	var zitiEnv struct {
		Data []json.RawMessage `json:"data"`
	}
	json.Unmarshal(raw, &zitiEnv)

	// Index Ziti identities by name (which is the Bao machine UUID)
	zitiByName := map[string]json.RawMessage{}
	for _, raw := range zitiEnv.Data {
		var ident struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		json.Unmarshal(raw, &ident)
		zitiByName[ident.Name] = raw
	}

	var combined []CombinedIdentity

	for _, mid := range machineIDs {
		mid = strings.TrimSuffix(mid, "/")
		if mid == "by-hostname" {
			continue
		}

		ci := CombinedIdentity{ID: mid}

		// Load Bao machine data (keys only for list view, not full secrets)
		baoData, err := b.Get("identities/machines/" + mid)
		if err == nil && baoData != nil {
			if v, ok := baoData["nickname"].(string); ok {
				ci.Nickname = v
			}
			if v, ok := baoData["hostname"].(string); ok {
				ci.Hostname = v
			}
			if v, ok := baoData["state"].(string); ok {
				ci.State = v
			}
			if v, ok := baoData["source"].(string); ok {
				ci.Source = v
			}
			ci.RegisteredAt = baoData["registered_at"]
			if v, ok := baoData["os"].(map[string]any); ok {
				ci.OS = v
			}
			if v, ok := baoData["hardware"].(map[string]any); ok {
				ci.Hardware = v
			}
			if v, ok := baoData["location"].(map[string]any); ok {
				ci.Location = v
			}
		}

		// Match with Ziti identity
		if zitiRaw, ok := zitiByName[mid]; ok {
			ci.HasZitiMatch = true
			var zi struct {
				ID                      string   `json:"id"`
				HasEdgeRouterConnection bool     `json:"hasEdgeRouterConnection"`
				HasAPISession           bool     `json:"hasApiSession"`
				RoleAttributes          []string `json:"roleAttributes"`
				Type                    struct{ Name string } `json:"type"`
				DefaultHostingCost      int      `json:"defaultHostingCost"`
			}
			json.Unmarshal(zitiRaw, &zi)
			ci.ZitiID = zi.ID
			ci.ZitiOnline = zi.HasEdgeRouterConnection
			ci.ZitiHasSession = zi.HasAPISession
			ci.ZitiRoles = zi.RoleAttributes
			ci.ZitiType = zi.Type.Name
			ci.ZitiDefaultHostingCost = zi.DefaultHostingCost
		}

		combined = append(combined, ci)
	}

	// Sort: online first, then by nickname
	sort.Slice(combined, func(i, j int) bool {
		if combined[i].ZitiOnline != combined[j].ZitiOnline {
			return combined[i].ZitiOnline
		}
		return combined[i].Nickname < combined[j].Nickname
	})

	return combined, nil
}

type IdentityDetail struct {
	CombinedIdentity

	// Full Bao data
	BaoData map[string]any `json:"bao_data,omitempty"`

	// Ziti drill-down
	ZitiServices []any `json:"ziti_services,omitempty"`
	ZitiFull     any   `json:"ziti_full,omitempty"`

	// Known host info (if exists)
	KnownHost map[string]any `json:"known_host,omitempty"`
}

func buildIdentityDetail(z *ziti.Client, token string, b *baoclient.Client, machineID string) (*IdentityDetail, error) {
	list, err := buildCombinedList(z, token, b)
	if err != nil {
		return nil, err
	}

	var ci *CombinedIdentity
	for i := range list {
		if list[i].ID == machineID {
			ci = &list[i]
			break
		}
	}
	if ci == nil {
		return nil, fmt.Errorf("machine %s not found", machineID)
	}

	detail := &IdentityDetail{CombinedIdentity: *ci}

	// Full Bao machine data
	baoData, err := b.Get("identities/machines/" + machineID)
	if err == nil {
		detail.BaoData = baoData
	}

	// Check known host
	if ci.Hostname != "" {
		knownData, err := b.Get("identities/known/" + ci.Hostname)
		if err == nil && knownData != nil {
			detail.KnownHost = knownData
		}
	}

	// Ziti drill-down: get full identity + services
	if ci.HasZitiMatch {
		// Full Ziti identity
		fullRaw, err := z.Get(token, "/edge/management/v1/identities/"+ci.ZitiID)
		if err == nil {
			var env struct{ Data any `json:"data"` }
			json.Unmarshal(fullRaw, &env)
			detail.ZitiFull = env.Data
		}

		// Services this identity can access
		svcRaw, err := z.Get(token, fmt.Sprintf("/edge/management/v1/identities/%s/services", ci.ZitiID))
		if err == nil {
			var env struct{ Data []any `json:"data"` }
			json.Unmarshal(svcRaw, &env)
			detail.ZitiServices = env.Data
		}
	}

	return detail, nil
}
