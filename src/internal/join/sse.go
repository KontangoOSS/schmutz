package join

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// EnrollResult is the outcome of an SSE enrollment.
type EnrollResult struct {
	Status       string          `json:"status"`
	ID           string          `json:"id"`
	AppID        string          `json:"app_id,omitempty"`
	Nickname     string          `json:"nickname"`
	Identity     json.RawMessage `json:"identity"`
	Config       struct {
		Hosts  []string               `json:"hosts"`
		Tunnel map[string]interface{} `json:"tunnel"`
	} `json:"config"`
	Reason       string   `json:"reason,omitempty"`
	Services     []string `json:"services,omitempty"`
	SSHPublicKey string   `json:"ssh_public_key,omitempty"`
	ClaimURL     string   `json:"claim_url,omitempty"`
}

// SSEEvent is a single event from the enrollment stream.
type SSEEvent struct {
	Kind       string // verify, progress, decision, identity, error
	Check      string // for verify events
	Passed     bool   // for verify events
	Confidence string // for verify events
	Step       string // for progress events
	Status     string // for decision / identity events
	Reason     string // for decision / error events
	Result     *EnrollResult // populated on identity event
}

// SSEEnrollStream is like SSEEnroll but emits events to eventFn as they arrive.
// Returns the final EnrollResult when the identity event is received.
// eventFn may be called from a goroutine; it must be safe to call concurrently.
func SSEEnrollStream(url, method, session, roleID, secretID string, eventFn func(SSEEvent)) (*EnrollResult, error) {
	os := ProbeOS()
	hw := ProbeHardware()
	net := ProbeNetwork()
	sys := ProbeSystem()

	payload := map[string]interface{}{"method": method, "session": session}
	if roleID != "" {
		payload["role_id"] = roleID
		payload["secret_id"] = secretID
	}
	for _, m := range []map[string]interface{}{os, hw, net, sys} {
		for k, v := range m {
			if k != "type" {
				payload[k] = v
			}
		}
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url+"/api/enroll/stream", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	var result *EnrollResult
	var currentEvent string

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			currentEvent = strings.TrimPrefix(line, "event: ")
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		switch currentEvent {
		case "verify":
			var v struct {
				Check      string `json:"check"`
				Passed     bool   `json:"passed"`
				Confidence string `json:"confidence"`
				Reason     string `json:"reason"`
			}
			json.Unmarshal([]byte(data), &v)
			if eventFn != nil {
				eventFn(SSEEvent{Kind: "verify", Check: v.Check, Passed: v.Passed, Confidence: v.Confidence, Reason: v.Reason})
			}
		case "progress":
			var p struct{ Step string `json:"step"` }
			json.Unmarshal([]byte(data), &p)
			if eventFn != nil {
				eventFn(SSEEvent{Kind: "progress", Step: p.Step})
			}
		case "decision":
			var d struct {
				Status string `json:"status"`
				Reason string `json:"reason"`
			}
			json.Unmarshal([]byte(data), &d)
			if eventFn != nil {
				eventFn(SSEEvent{Kind: "decision", Status: d.Status, Reason: d.Reason})
			}
			if d.Status == "rejected" {
				return nil, fmt.Errorf("rejected: %s", d.Reason)
			}
		case "identity":
			result = &EnrollResult{}
			var id struct {
				ID           string          `json:"id"`
				AppID        string          `json:"app_id"`
				Nickname     string          `json:"nickname"`
				Status       string          `json:"status"`
				Identity     json.RawMessage `json:"identity"`
				Config       struct {
					Hosts  []string               `json:"hosts"`
					Tunnel map[string]interface{} `json:"tunnel"`
				} `json:"config"`
				Services     []string `json:"services"`
				SSHPublicKey string   `json:"ssh_public_key"`
				ClaimURL     string   `json:"claim_url"`
			}
			json.Unmarshal([]byte(data), &id)
			result.ID = id.ID
			result.AppID = id.AppID
			result.Nickname = id.Nickname
			result.Status = id.Status
			result.Identity = id.Identity
			result.Config.Hosts = id.Config.Hosts
			result.Config.Tunnel = id.Config.Tunnel
			result.Services = id.Services
			result.SSHPublicKey = id.SSHPublicKey
			result.ClaimURL = id.ClaimURL
			if eventFn != nil {
				eventFn(SSEEvent{Kind: "identity", Status: id.Status, Result: result})
			}
		case "error":
			var e struct{ Reason string `json:"reason"` }
			json.Unmarshal([]byte(data), &e)
			if eventFn != nil {
				eventFn(SSEEvent{Kind: "error", Reason: e.Reason})
			}
			return nil, fmt.Errorf("server: %s", e.Reason)
		}
		currentEvent = ""
	}

	if result == nil {
		return nil, fmt.Errorf("no identity received")
	}
	return result, nil
}

// SSEEnroll sends all probe data in one POST and reads SSE events back.
// This is the v2 enrollment path — one request, streaming response.
func SSEEnroll(url, method, slug, session, roleID, secretID string) (*EnrollResult, error) {
	// Collect all probe data upfront
	os := ProbeOS()
	hw := ProbeHardware()
	net := ProbeNetwork()
	sys := ProbeSystem()

	// Build the combined payload
	payload := map[string]interface{}{
		"method":  method,
		"slug":    slug,
		"session": session,
	}
	if roleID != "" {
		payload["role_id"] = roleID
		payload["secret_id"] = secretID
	}

	// Merge all probe data into the payload
	for k, v := range os {
		if k != "type" {
			payload[k] = v
		}
	}
	for k, v := range hw {
		if k != "type" {
			payload[k] = v
		}
	}
	for k, v := range net {
		if k != "type" {
			payload[k] = v
		}
	}
	for k, v := range sys {
		if k != "type" {
			payload[k] = v
		}
	}

	body, _ := json.Marshal(payload)

	log.Printf("  hostname:    %s", payload["hostname"])
	log.Printf("  os:          %s", payload["os_version"])
	log.Printf("  arch:        %s", payload["arch"])
	if hw, ok := payload["hardware_hash"].(string); ok && hw != "" {
		log.Printf("  fingerprint: %s", hw)
	}

	// POST with Accept: text/event-stream
	req, _ := http.NewRequest("POST", url+"/api/enroll/stream", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// Read SSE events
	scanner := bufio.NewScanner(resp.Body)
	var result *EnrollResult
	var currentEvent string

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			currentEvent = strings.TrimPrefix(line, "event: ")
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			switch currentEvent {
			case "verify":
				var v struct {
					Check      string `json:"check"`
					Passed     bool   `json:"passed"`
					Confidence string `json:"confidence"`
					Reason     string `json:"reason"`
				}
				json.Unmarshal([]byte(data), &v)
				status := "✓"
				if !v.Passed {
					status = "✗"
				}
				log.Printf("  verify: %s %s", v.Check, status)

			case "progress":
				var p struct {
					Step string `json:"step"`
				}
				json.Unmarshal([]byte(data), &p)
				log.Printf("  %s…", p.Step)

			case "decision":
				var d struct {
					Status string `json:"status"`
					Reason string `json:"reason"`
				}
				json.Unmarshal([]byte(data), &d)
				log.Printf("  decision: %s (%s)", d.Status, d.Reason)
				if d.Status == "rejected" {
					return nil, fmt.Errorf("rejected: %s", d.Reason)
				}

			case "identity":
				result = &EnrollResult{}
				var id struct {
					ID           string          `json:"id"`
					AppID        string          `json:"app_id"`
					Nickname     string          `json:"nickname"`
					Status       string          `json:"status"`
					Identity     json.RawMessage `json:"identity"`
					Config       struct {
						Hosts  []string               `json:"hosts"`
						Tunnel map[string]interface{} `json:"tunnel"`
					} `json:"config"`
					Services     []string `json:"services"`
					SSHPublicKey string   `json:"ssh_public_key"`
					ClaimURL     string   `json:"claim_url"`
				}
				json.Unmarshal([]byte(data), &id)
				result.ID = id.ID
				result.AppID = id.AppID
				result.Nickname = id.Nickname
				result.Status = id.Status
				result.Identity = id.Identity
				result.Config.Hosts = id.Config.Hosts
				result.Config.Tunnel = id.Config.Tunnel
				result.Services = id.Services
				result.SSHPublicKey = id.SSHPublicKey
				result.ClaimURL = id.ClaimURL

			case "error":
				var e struct {
					Reason string `json:"reason"`
				}
				json.Unmarshal([]byte(data), &e)
				return nil, fmt.Errorf("server: %s", e.Reason)
			}

			currentEvent = ""
		}
	}

	if result == nil {
		return nil, fmt.Errorf("no identity received")
	}
	return result, nil
}
