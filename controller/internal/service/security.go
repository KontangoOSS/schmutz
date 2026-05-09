package service

import (
	"log"
	"sync"
	"time"
)

// SecurityService handles fingerprint validation, duplicate detection, and rate limiting.
type SecurityService struct {
	store *StoreService

	// Rate limiting — in-memory, per-IP
	rateMu    sync.Mutex
	rateCount map[string]*rateEntry
}

type rateEntry struct {
	count     int
	firstSeen time.Time
}

func NewSecurityService(store *StoreService) *SecurityService {
	return &SecurityService{
		store:     store,
		rateCount: make(map[string]*rateEntry),
	}
}

// CheckBanned returns true if any identifier (hostname, MAC, hardware hash) is banned.
func (s *SecurityService) CheckBanned(hostname, hardwareHash string, macs []string) bool {
	if s.store.IsBanned(hostname) {
		return true
	}
	if hardwareHash != "" && s.store.IsBanned(hardwareHash) {
		return true
	}
	for _, mac := range macs {
		if s.store.IsBanned(mac) {
			return true
		}
	}
	return false
}

// CheckDuplicate checks if a MAC address or machine ID is already registered under a different hostname.
func (s *SecurityService) CheckDuplicate(hostname, machineID string, macs []string) (conflictID string, isDuplicate bool) {
	// Check all registered machines for MAC/machine_id collision
	keys, err := s.store.List("secret", "identities/machines/")
	if err != nil {
		return "", false
	}
	for _, k := range keys {
		if k == "by-hostname/" {
			continue
		}
		data, err := s.store.GetMachine(k)
		if err != nil || data == nil {
			continue
		}

		storedHostname, _ := data["hostname"].(string)
		if storedHostname == hostname {
			continue // Same machine, not a conflict
		}

		// Check machine_id collision
		hw, _ := data["hardware"].(map[string]interface{})
		if hw != nil && machineID != "" {
			if storedMID, _ := hw["machine_id"].(string); storedMID == machineID && storedMID != "" {
				id, _ := data["id"].(string)
				log.Printf("security: machine_id collision %s (existing=%s, new=%s)", machineID, storedHostname, hostname)
				return id, true
			}
		}

		// Check MAC collision
		net, _ := data["network"].(map[string]interface{})
		if net != nil {
			storedMACs, _ := net["mac_addrs"].([]interface{})
			for _, sm := range storedMACs {
				smStr, _ := sm.(string)
				for _, newMAC := range macs {
					if smStr == newMAC && smStr != "" {
						id, _ := data["id"].(string)
						log.Printf("security: MAC collision %s (existing=%s, new=%s)", smStr, storedHostname, hostname)
						return id, true
					}
				}
			}
		}
	}
	return "", false
}

// ValidateFingerprint checks if a returning device's fingerprint matches its stored record.
func (s *SecurityService) ValidateFingerprint(machineID, hardwareHash string) bool {
	data, err := s.store.GetMachine(machineID)
	if err != nil || data == nil {
		return false // Unknown machine
	}
	hw, _ := data["hardware"].(map[string]interface{})
	if hw == nil {
		return true // No stored fingerprint to compare
	}
	storedFP, _ := hw["fingerprint"].(string)
	if storedFP == "" {
		return true // No stored fingerprint
	}
	return storedFP == hardwareHash
}

// RateLimit checks if an IP has exceeded the registration rate limit.
// Returns true if the request should be rejected.
func (s *SecurityService) RateLimit(ip string, maxPerWindow int, window time.Duration) bool {
	s.rateMu.Lock()
	defer s.rateMu.Unlock()

	now := time.Now()
	entry, exists := s.rateCount[ip]

	if !exists || now.Sub(entry.firstSeen) > window {
		s.rateCount[ip] = &rateEntry{count: 1, firstSeen: now}
		return false
	}

	entry.count++
	if entry.count > maxPerWindow {
		log.Printf("security: rate limit exceeded for %s (%d/%d in %s)", ip, entry.count, maxPerWindow, window)
		return true
	}
	return false
}

// Audit writes an enrollment event.
func (s *SecurityService) Audit(machineID, event, sourceIP string, details map[string]interface{}) {
	if details == nil {
		details = map[string]interface{}{}
	}
	details["source_ip"] = sourceIP
	if err := s.store.WriteAuditLog(machineID, event, details); err != nil {
		log.Printf("security: audit write failed: %v", err)
	}
}
