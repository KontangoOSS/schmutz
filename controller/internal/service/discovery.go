package service

import (
	"log"
	"strings"
)

// DiscoveryService matches unknown devices to known entities by fingerprint.
// This is the auto-claim logic — no credentials needed, just hardware identity.
type DiscoveryService struct {
	store *StoreService
}

func NewDiscoveryService(store *StoreService) *DiscoveryService {
	return &DiscoveryService{store: store}
}

// MatchResult contains the result of a device discovery match.
type MatchResult struct {
	Matched    bool
	EntityID   string
	Nickname   string
	Profile    string
	Attributes []string
	MatchType  string // "fingerprint", "machine_id", "mac", "hostname"
	Confidence string // "high", "medium", "low"
}

// MatchDevice tries to find a known entity for an unknown device.
// Checks in order of confidence: fingerprint > machine_id > MAC > hostname.
func (d *DiscoveryService) MatchDevice(hostname, machineID, hardwareHash string, macs []string) *MatchResult {
	// 1. Hardware fingerprint — highest confidence
	if hardwareHash != "" {
		data, err := d.store.Get("secret", "identities/devices/by-fingerprint/"+hardwareHash)
		if err == nil && data != nil {
			return d.buildResult(data, "fingerprint", "high")
		}
	}

	// 2. Machine ID — high confidence (unique per OS install)
	if machineID != "" {
		data, err := d.store.Get("secret", "identities/devices/by-machine-id/"+machineID)
		if err == nil && data != nil {
			return d.buildResult(data, "machine_id", "high")
		}
	}

	// 3. Known machine by hostname — medium confidence
	known, err := d.store.GetKnownMachine(hostname)
	if err == nil && known != nil {
		profile, _ := known["profile"].(string)
		entityID, _ := known["entity_id"].(string)
		if profile != "" {
			attrs := d.lookupProfileAttrs(profile)
			return &MatchResult{
				Matched:    true,
				EntityID:   entityID,
				Profile:    profile,
				Attributes: attrs,
				MatchType:  "hostname",
				Confidence: "medium",
			}
		}
	}

	// 4. MAC address scan — low confidence (MACs can be spoofed)
	for _, mac := range macs {
		// Scan existing devices for MAC match
		keys, err := d.store.List("secret", "identities/machines/")
		if err != nil {
			continue
		}
		for _, k := range keys {
			if k == "by-hostname/" {
				continue
			}
			k = strings.TrimSuffix(k, "/")
			machine, err := d.store.GetMachine(k)
			if err != nil || machine == nil {
				continue
			}
			net, _ := machine["network"].(map[string]interface{})
			if net == nil {
				continue
			}
			storedMACs, _ := net["mac_addrs"].([]interface{})
			for _, sm := range storedMACs {
				if smStr, _ := sm.(string); smStr == mac && mac != "" {
					entityID, _ := machine["bao_entity_id"].(string)
					nickname, _ := machine["nickname"].(string)
					return &MatchResult{
						Matched:    true,
						EntityID:   entityID,
						Nickname:   nickname,
						MatchType:  "mac",
						Confidence: "low",
					}
				}
			}
		}
	}

	return &MatchResult{Matched: false}
}

// AutoClaim takes a match result and applies it to a Ziti identity.
func (d *DiscoveryService) AutoClaim(zitiSvc *ZitiService, token, zitiID string, match *MatchResult) error {
	if !match.Matched || len(match.Attributes) == 0 {
		return nil
	}

	// Only auto-claim high confidence matches
	if match.Confidence == "low" {
		log.Printf("  discovery: low confidence match (%s) — not auto-claiming", match.MatchType)
		return nil
	}

	// Update Ziti identity attributes
	if err := zitiSvc.UpdateIdentity(token, zitiID, match.Attributes); err != nil {
		return err
	}

	log.Printf("  discovery: auto-claimed via %s (confidence=%s) → %v",
		match.MatchType, match.Confidence, match.Attributes)
	return nil
}

func (d *DiscoveryService) buildResult(data map[string]interface{}, matchType, confidence string) *MatchResult {
	entityID, _ := data["entity_id"].(string)
	nickname, _ := data["nickname"].(string)

	// Look up the entity to get the profile
	var profile string
	var attrs []string
	if entityID != "" {
		entityData, err := d.store.Get("secret", "identities/entities/"+entityID)
		if err == nil && entityData != nil {
			profile, _ = entityData["profile"].(string)
		}
		// If no KV entity, try Bao identity engine metadata
	}
	if profile != "" {
		attrs = d.lookupProfileAttrs(profile)
	}

	return &MatchResult{
		Matched:    true,
		EntityID:   entityID,
		Nickname:   nickname,
		Profile:    profile,
		Attributes: attrs,
		MatchType:  matchType,
		Confidence: confidence,
	}
}

func (d *DiscoveryService) lookupProfileAttrs(profile string) []string {
	profileData, err := d.store.GetProfile(profile)
	if err != nil || profileData == nil {
		return nil
	}
	attrStr, _ := profileData["attributes"].(string)
	if attrStr == "" {
		return nil
	}
	return strings.Split(attrStr, ",")
}
