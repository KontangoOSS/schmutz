package common

import (
	"crypto/rand"
	"encoding/hex"
	"math/big"
	"time"
)

// Registration is the server-side enrollment record.
type Registration struct {
	ID            string   `json:"id"`
	Nickname      string   `json:"nickname"`
	Hostname      string   `json:"hostname"`
	State         string   `json:"state"`
	RegisteredAt  int64    `json:"registered_at"`
	SourceIP      string   `json:"source_ip"`
	OS            string   `json:"os"`
	OSVersion     string   `json:"os_version"`
	Arch          string   `json:"arch"`
	CPUInfo       string   `json:"cpu_info"`
	MachineID     string   `json:"machine_id"`
	HardwareHash  string   `json:"hardware_hash"`
	MACAddrs      []string `json:"mac_addrs"`
	KernelVersion string   `json:"kernel_version"`
	Timezone      string   `json:"timezone"`
	BaoEntityID   string   `json:"bao_entity_id,omitempty"`
	Trusted       bool     `json:"trusted,omitempty"`
}

func NewRegistration(hostname string) *Registration {
	return &Registration{
		ID:           generateUUID(),
		Nickname:     GenerateNickname(),
		Hostname:     hostname,
		State:        "pending",
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

func GenerateNickname() string {
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
