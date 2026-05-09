package enroll

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"time"
)

// ProgressMessenger standardizes sending progress messages during device enrollment.
// Supports both SSE streaming (during enrollment) and Ziti-based messaging (post-enrollment).
type ProgressMessenger struct {
	deviceID  string
	tag       string
	zitiConn  io.WriteCloser // Ziti connection for post-enrollment messages
	logger    *log.Logger
	isZitiBased bool
}

// NewProgressMessenger creates a new progress messenger for SSE streaming.
func NewProgressMessenger(deviceID, tag string) *ProgressMessenger {
	return &ProgressMessenger{
		deviceID:    deviceID,
		tag:         tag,
		isZitiBased: false,
		logger:      log.New(log.Writer(), "[progress] ", log.LstdFlags),
	}
}

// NewZitiProgressMessenger creates a progress messenger using Ziti SDK.
// The conn parameter is a Ziti service connection (net.Conn from ziti.Dial).
func NewZitiProgressMessenger(deviceID, tag string, conn io.WriteCloser) *ProgressMessenger {
	return &ProgressMessenger{
		deviceID:    deviceID,
		tag:         tag,
		zitiConn:    conn,
		isZitiBased: true,
		logger:      log.New(log.Writer(), "[progress] ", log.LstdFlags),
	}
}

// ProgressUpdate represents a single progress update message.
// Works over both SSE (as JSON in event data) and Ziti (as newline-delimited JSON).
type ProgressUpdate struct {
	Step      string `json:"step"`
	Tag       string `json:"tag"`
	DeviceID  string `json:"device_id"`
	Hostname  string `json:"hostname,omitempty"`
	Nickname  string `json:"nickname,omitempty"`
	Status    string `json:"status,omitempty"`
	Error     string `json:"error,omitempty"`
	Timestamp int64  `json:"timestamp"`
}

// Send writes a progress message.
// For SSE: writes as SSE event to the given writer.
// For Ziti: sends as newline-delimited JSON through the Ziti connection.
func (pm *ProgressMessenger) Send(w io.Writer, step string, data map[string]interface{}) error {
	update := ProgressUpdate{
		Step:      step,
		Tag:       pm.tag,
		DeviceID:  pm.deviceID,
		Timestamp: time.Now().Unix(),
	}

	// Merge additional data
	for k, v := range data {
		switch k {
		case "hostname":
			if s, ok := v.(string); ok {
				update.Hostname = s
			}
		case "nickname":
			if s, ok := v.(string); ok {
				update.Nickname = s
			}
		case "status":
			if s, ok := v.(string); ok {
				update.Status = s
			}
		case "error":
			if s, ok := v.(string); ok {
				update.Error = s
			}
		}
	}

	b, err := json.Marshal(update)
	if err != nil {
		return err
	}

	// SSE format: event: progress\ndata: {json}\n\n
	fmt.Fprintf(w, "event: progress\ndata: %s\n\n", string(b))
	pm.logger.Printf("%s → %s", pm.deviceID, step)
	return nil
}

// SendViaZiti sends a progress message through Ziti SDK.
// Used post-enrollment when device has a Ziti identity.
// Format: newline-delimited JSON (NDJSON) for streaming.
func (pm *ProgressMessenger) SendViaZiti(step string, data map[string]interface{}) error {
	if !pm.isZitiBased || pm.zitiConn == nil {
		return fmt.Errorf("not a ziti-based messenger or connection closed")
	}

	update := ProgressUpdate{
		Step:      step,
		Tag:       pm.tag,
		DeviceID:  pm.deviceID,
		Timestamp: time.Now().Unix(),
	}

	// Merge additional data
	for k, v := range data {
		switch k {
		case "hostname":
			if s, ok := v.(string); ok {
				update.Hostname = s
			}
		case "nickname":
			if s, ok := v.(string); ok {
				update.Nickname = s
			}
		case "status":
			if s, ok := v.(string); ok {
				update.Status = s
			}
		case "error":
			if s, ok := v.(string); ok {
				update.Error = s
			}
		}
	}

	b, err := json.Marshal(update)
	if err != nil {
		return err
	}

	// Send as newline-delimited JSON
	_, err = fmt.Fprintf(pm.zitiConn, "%s\n", string(b))
	if err != nil {
		pm.logger.Printf("%s → %s (ziti send failed: %v)", pm.deviceID, step, err)
		return err
	}

	pm.logger.Printf("%s → %s (via ziti)", pm.deviceID, step)
	return nil
}

// Close closes the underlying Ziti connection if present.
func (pm *ProgressMessenger) Close() error {
	if pm.zitiConn != nil {
		return pm.zitiConn.Close()
	}
	return nil
}

// Log writes a progress message to the logger.
func (pm *ProgressMessenger) Log(step string, details ...interface{}) {
	msg := fmt.Sprintf("%s → %s", pm.deviceID, step)
	if len(details) > 0 {
		msg = fmt.Sprintf("%s: %v", msg, details)
	}
	pm.logger.Println(msg)
}

// SSEProgressWriter wraps an http.ResponseWriter and Flusher for streaming progress.
// Used in enrollment stream to send progress events with proper SSE formatting.
type SSEProgressWriter struct {
	w io.Writer
	m *ProgressMessenger
}

// NewSSEProgressWriter creates a new SSE progress writer.
func NewSSEProgressWriter(w io.Writer, pm *ProgressMessenger) *SSEProgressWriter {
	return &SSEProgressWriter{w: w, m: pm}
}

// Write sends a progress message as an SSE event.
func (sw *SSEProgressWriter) Write(step string, data map[string]interface{}) error {
	return sw.m.Send(sw.w, step, data)
}

// Standard progress steps used throughout enrollment
const (
	ProgressVerifying          = "verifying"
	ProgressCreatingIdentity   = "creating-identity"
	ProgressIssuingCerts       = "issuing-certificates"
	ProgressEnrolling          = "enrolling"
	ProgressDownloadingZiti    = "downloading-ziti"
	ProgressZitiInstalled      = "ziti-installed"
	ProgressTunnelStarting     = "tunnel-starting"
	ProgressTunnelConnected    = "tunnel-connected"
	ProgressDownloadingAgent   = "downloading-agent"
	ProgressAgentDownloaded    = "agent-downloaded"
	ProgressAgentStarting      = "agent-starting"
	ProgressAgentStarted       = "agent-started"
	ProgressDownloadingPortal  = "downloading-portal"
	ProgressPortalStarted      = "portal-started"
	ProgressDownloadingCaddy   = "downloading-caddy"
	ProgressCaddyConfigured    = "caddy-configured"
	ProgressComplete           = "complete"
	ProgressError              = "error"
)
