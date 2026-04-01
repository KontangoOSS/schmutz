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
	Status   string          `json:"status"`
	ID       string          `json:"id"`
	Nickname string          `json:"nickname"`
	Identity json.RawMessage `json:"identity"`
	Config   struct {
		Hosts  []string               `json:"hosts"`
		Tunnel map[string]interface{} `json:"tunnel"`
	} `json:"config"`
	Reason string `json:"reason,omitempty"`
}

// SSEEnroll sends all probe data in one POST and reads SSE events back.
// This is the v2 enrollment path — one request, streaming response.
func SSEEnroll(url, method, session, roleID, secretID string) (*EnrollResult, error) {
	// Collect all probe data upfront
	os := ProbeOS()
	hw := ProbeHardware()
	net := ProbeNetwork()
	sys := ProbeSystem()

	// Build the combined payload
	payload := map[string]interface{}{
		"method":  method,
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
					ID       string          `json:"id"`
					Nickname string          `json:"nickname"`
					Status   string          `json:"status"`
					Identity json.RawMessage `json:"identity"`
					Config   struct {
						Hosts  []string               `json:"hosts"`
						Tunnel map[string]interface{} `json:"tunnel"`
					} `json:"config"`
				}
				json.Unmarshal([]byte(data), &id)
				result.ID = id.ID
				result.Nickname = id.Nickname
				result.Status = id.Status
				result.Identity = id.Identity
				result.Config.Hosts = id.Config.Hosts
				result.Config.Tunnel = id.Config.Tunnel

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
