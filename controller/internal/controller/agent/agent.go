// Package agent implements the controller-side bindings for the two agent services.
//
// Event model — the config channel is a sparse event stream, not a state dump:
//
//	connect  → {"type":"hello","payload":{...}}  — everything controller knows about this machine
//	on change → {"type":"set_interval","payload":{"seconds":30}}
//	on demand → {"type":"reload"} etc.
//
// The hello payload is scoped to what the agent can usefully act on:
// interval, nickname, state, OS ref, profile name. Never role attributes,
// never credentials, never Ziti policy data — those are controller-only.
package agent

import (
	"encoding/json"
	"log"

	"github.com/openziti/sdk-golang/ziti/edge"

	"git.konoss.org/kore/schmutz/controller/internal/service"
)

// HelloPayload is what the agent receives on connect and in every heartbeat response.
// Carries whatever the controller knows about this machine — scoped to operational use.
// Empty fields are omitted so unclaimed machines get a minimal payload.
type HelloPayload struct {
	Interval int    `json:"interval"`
	Nickname string `json:"nickname,omitempty"`
	State    string `json:"state,omitempty"`
	Profile  string `json:"profile,omitempty"`
	OSRef    string `json:"os_ref,omitempty"`   // e.g. "public/os/linux/ubuntu/24.04"
	OSVer    string `json:"os_ver,omitempty"`   // e.g. "Ubuntu 24.04 LTS"
	Arch     string `json:"arch,omitempty"`
	CPU      string `json:"cpu,omitempty"`
}

// StartConfigListener binds config.tango and streams events to agents.
func StartConfigListener(zt *service.ZitiTransport, tel *service.TelemetryService, store *service.StoreService) {
	listener, err := zt.ListenEdge("config.tango")
	if err != nil {
		log.Printf("agent: config listen: %v", err)
		return
	}
	go func() {
		for {
			conn, err := listener.AcceptEdge()
			if err != nil {
				log.Printf("agent: config accept: %v", err)
				return
			}
			go handleConfig(conn, tel, store)
		}
	}()
}

func handleConfig(conn edge.Conn, tel *service.TelemetryService, store *service.StoreService) {
	defer conn.Close()

	identityName := conn.GetDialerIdentityName()

	hello := BuildHello(identityName, store)
	payload, _ := json.Marshal(service.Instruction{
		Type:    "hello",
		Payload: mustJSON(hello),
	})
	conn.Write(append(payload, '\n'))

	instrCh, done := tel.RegisterConfigConn(identityName)
	defer tel.UnregisterConfigConn(identityName)

	for {
		select {
		case <-done:
			return
		case payload, ok := <-instrCh:
			if !ok {
				return
			}
			if _, err := conn.Write(payload); err != nil {
				return
			}
		}
	}
}

// BuildHello assembles the hello payload from whatever the controller knows
// about this machine. Gracefully handles unknown/unclaimed machines —
// they just get the default interval.
func BuildHello(machineID string, store *service.StoreService) HelloPayload {
	p := HelloPayload{Interval: 60}

	machine, err := store.GetMachine(machineID)
	if err != nil || machine == nil {
		return p
	}

	p.Nickname = strField(machine, "nickname")
	p.State = strField(machine, "state")

	if os, ok := machine["os"].(map[string]interface{}); ok {
		p.OSRef = strField(os, "ref")
		p.OSVer = strField(os, "version")
	}
	if hw, ok := machine["hardware"].(map[string]interface{}); ok {
		p.Arch = strField(hw, "arch")
		p.CPU = strField(hw, "cpu")
	}

	// Interval and profile from machine record or its profile.
	if v, ok := toInt(machine["heartbeat_interval"]); ok {
		p.Interval = v
	}
	if profileName, ok := machine["profile"].(string); ok && profileName != "" {
		p.Profile = profileName
		if profile, err := store.GetProfile(profileName); err == nil && profile != nil {
			if v, ok := toInt(profile["heartbeat_interval"]); ok && p.Interval == 60 {
				p.Interval = v
			}
		}
	}

	return p
}

// HelloPayload is also returned in heartbeat responses — same path, HTTP or overlay.
func BuildHeartbeatHello(machineID string, store *service.StoreService) json.RawMessage {
	return mustJSON(BuildHello(machineID, store))
}

func mustJSON(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func strField(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		return int(n), true
	case string:
		var i int
		if err := json.Unmarshal([]byte(n), &i); err == nil {
			return i, true
		}
	}
	return 0, false
}
