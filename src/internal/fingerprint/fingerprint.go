package fingerprint

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

// NodeClaim is the identity proof generated at install time.
// It captures immutable facts about the node that the controller
// can verify later. The node encrypts this with the controller's
// public key — only the controller can read it.
type NodeClaim struct {
	// When this claim was generated (install time)
	Timestamp time.Time `json:"ts"`

	// Public IP as declared by the installer
	PublicIP string `json:"public_ip"`

	// External IP as seen by the internet (ifconfig.me)
	ExternalIP string `json:"external_ip"`

	// Hardware fingerprint — CPU, RAM, disk serial, MAC addresses.
	// Not for security (these can be spoofed in VMs), but for
	// inventory tracking and anomaly detection. If a node's hardware
	// fingerprint changes, something is wrong.
	MachineID string `json:"machine_id"` // /etc/machine-id (systemd, unique per install)
	MACs      string `json:"macs"`       // sorted, hashed MAC addresses
	CPUModel  string `json:"cpu_model"`

	// 32 bytes of entropy generated at install time.
	// Combined with the other fields, makes each claim globally unique
	// even if two nodes have identical hardware.
	Nonce string `json:"nonce"`

	// Region as declared by the installer
	Region string `json:"region"`

	// The node name (for correlation only, not trust)
	NodeName string `json:"node_name"`
}

// Generate collects system facts and builds a NodeClaim.
func Generate(publicIP, externalIP, region, nodeName string) (*NodeClaim, error) {
	machineID, _ := os.ReadFile("/etc/machine-id")
	cpuModel := readCPUModel()
	macs := collectMACs()

	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	return &NodeClaim{
		Timestamp:  time.Now().UTC(),
		PublicIP:   publicIP,
		ExternalIP: externalIP,
		MachineID:  strings.TrimSpace(string(machineID)),
		MACs:       hashMACs(macs),
		CPUModel:   cpuModel,
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Region:     region,
		NodeName:   nodeName,
	}, nil
}

// Seal encrypts the claim with the controller's RSA public key.
// Only the controller (holding the private key) can unseal it.
// Returns base64-encoded ciphertext.
func (c *NodeClaim) Seal(pubKeyPEM []byte) (string, error) {
	block, _ := pem.Decode(pubKeyPEM)
	if block == nil {
		return "", fmt.Errorf("no PEM block found in public key")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse public key: %w", err)
	}

	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return "", fmt.Errorf("not an RSA public key")
	}

	plaintext, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("marshal claim: %w", err)
	}

	// The claim is small enough for OAEP with SHA-256.
	// If claims grow larger, switch to hybrid encryption
	// (AES envelope encrypted with RSA).
	ciphertext, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, rsaPub, plaintext, nil)
	if err != nil {
		return "", fmt.Errorf("encrypt: %w", err)
	}

	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Unseal decrypts a sealed claim using the controller's RSA private key.
func Unseal(sealed string, privKeyPEM []byte) (*NodeClaim, error) {
	block, _ := pem.Decode(privKeyPEM)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in private key")
	}

	priv, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		// Try PKCS8 format
		key, err2 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err2 != nil {
			return nil, fmt.Errorf("parse private key: %w (pkcs1: %v)", err2, err)
		}
		var ok bool
		priv, ok = key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("not an RSA private key")
		}
	}

	ciphertext, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return nil, fmt.Errorf("decode base64: %w", err)
	}

	plaintext, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, priv, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	var claim NodeClaim
	if err := json.Unmarshal(plaintext, &claim); err != nil {
		return nil, fmt.Errorf("unmarshal claim: %w", err)
	}

	return &claim, nil
}

// Digest returns a short hex hash of the claim for use as a node identifier.
// This is deterministic — same claim always produces the same digest.
func (c *NodeClaim) Digest() string {
	data, _ := json.Marshal(c)
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:8])
}

func readCPUModel() string {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return "unknown"
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "model name") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return "unknown"
}

func collectMACs() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var macs []string
	for _, iface := range ifaces {
		// Skip loopback and virtual interfaces
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		mac := iface.HardwareAddr.String()
		if mac != "" {
			macs = append(macs, mac)
		}
	}
	return macs
}

func hashMACs(macs []string) string {
	joined := strings.Join(macs, ",")
	h := sha256.Sum256([]byte(joined))
	return fmt.Sprintf("%x", h[:8])
}
