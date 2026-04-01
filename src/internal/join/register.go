package join

import (
	"crypto/rand"
	"encoding/hex"
	"math/big"
	"time"
)

type MachineState string

const (
	StatePending MachineState = "pending"
	StateActive  MachineState = "active"
	StateBanned  MachineState = "banned"
)

// Registration is the server-side record. Stored in Bao, mirrored in Ziti appData.
type Registration struct {
	ID       string       `json:"id"`
	Nickname string       `json:"nickname"`
	Hostname string       `json:"hostname"`
	State    MachineState `json:"state"`

	RegisteredAt int64  `json:"registered_at"`
	SourceIP     string `json:"source_ip"`
	Timezone     string `json:"timezone,omitempty"`

	OS        string `json:"os"`
	OSVersion string `json:"os_version"`
	OSRef     string `json:"os_ref,omitempty"`

	Arch          string   `json:"arch,omitempty"`
	CPUInfo       string   `json:"cpu_info,omitempty"`
	MachineID     string   `json:"machine_id,omitempty"`
	SerialNumber  string   `json:"serial_number,omitempty"`
	KernelVersion string   `json:"kernel_version,omitempty"`
	HardwareHash  string   `json:"hardware_hash,omitempty"`
	MACAddrs      []string `json:"mac_addrs,omitempty"`

	Browser *BrowserData `json:"browser,omitempty"`
}

// BrowserData is web-specific telemetry from the join page.
type BrowserData struct {
	CanvasHash    string   `json:"canvas_hash,omitempty"`
	WebGLHash     string   `json:"webgl_hash,omitempty"`
	AudioHash     string   `json:"audio_hash,omitempty"`
	CompositeHash string   `json:"composite_hash,omitempty"`
	STUNIP        string   `json:"stun_ip,omitempty"`
	LocalIPs      []string `json:"local_ips,omitempty"`
	GPU           string   `json:"gpu,omitempty"`
	Screen        string   `json:"screen,omitempty"`
	Language      string   `json:"language,omitempty"`
	Cores         int      `json:"cores,omitempty"`
	Memory        int      `json:"memory,omitempty"`
	UserAgent     string   `json:"user_agent,omitempty"`
	Platform      string   `json:"platform,omitempty"`

	SessionDuration int      `json:"session_duration,omitempty"`
	Interactions    int      `json:"interactions,omitempty"`
	MouseDistance   int      `json:"mouse_distance,omitempty"`
	Geo             *GeoData `json:"geo,omitempty"`
}

type GeoData struct {
	Lat      float64 `json:"lat"`
	Lon      float64 `json:"lon"`
	Accuracy int     `json:"accuracy"`
}

// Request from the client. All fields sanitized by the handler.
type Request struct {
	Hostname      string       `json:"hostname"`
	OS            string       `json:"os"`
	OSVersion     string       `json:"os_version"`
	Arch          string       `json:"arch,omitempty"`
	Timezone      string       `json:"timezone,omitempty"`
	KernelVersion string       `json:"kernel_version,omitempty"`
	CPUInfo       string       `json:"cpu_info,omitempty"`
	MachineID     string       `json:"machine_id,omitempty"`
	SerialNumber  string       `json:"serial_number,omitempty"`
	HardwareHash  string       `json:"hardware_hash,omitempty"`
	MACAddrs      []string     `json:"mac_addrs,omitempty"`
	BrowserRaw    *BrowserData `json:"_browser,omitempty"`

	// Trusted enrollment — Bao AppRole credentials (optional)
	// If provided, the controller validates against Bao and skips quarantine.
	RoleID   string `json:"role_id,omitempty"`
	SecretID string `json:"secret_id,omitempty"`
}

// Response to the client. Minimal.
type Response struct {
	ID       string `json:"id"`
	Nickname string `json:"nickname,omitempty"`
	ZitiJWT  string `json:"ziti_jwt,omitempty"`
	Message  string `json:"message,omitempty"`
}

func NewRegistration(hostname string) *Registration {
	return &Registration{
		ID:           generateUUID(),
		Nickname:     generateNickname(),
		Hostname:     hostname,
		State:        StatePending,
		RegisteredAt: time.Now().Unix(),
	}
}

func generateUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b[:4]) + "-" + hex.EncodeToString(b[4:6]) + "-" +
		hex.EncodeToString(b[6:8]) + "-" + hex.EncodeToString(b[8:10]) + "-" + hex.EncodeToString(b[10:])
}

var (
	consonants = "bdfghjklmnprstvz"
	vowels     = "aeiou"
)

func generateNickname() string {
	b := make([]byte, 8)
	for i := range b {
		charset := consonants
		if i%2 == 1 {
			charset = vowels
		}
		idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		b[i] = charset[idx.Int64()]
	}
	return string(b)
}
