package heuristics

import (
	"context"
	"strconv"
	"strings"

	"git.konoss.org/kore/schmutz/dashboard/internal/store"
)

type Decision struct {
	Status     string
	Reason     string
	Score      *int
	Confidence string
}

type EnrollEvent struct {
	MachineID      string `json:"machine_id"`
	Hostname       string `json:"hostname"`
	SourceIP       string `json:"source_ip"`
	Status         string `json:"status"`
	Reason         string `json:"reason"`
	ZitiIdentityID string `json:"ziti_identity_id"`
	HardwareHash   string `json:"hardware_hash"`
	SystemHash     string `json:"system_hash"`
	NetworkHash    string `json:"network_hash"`
	FullHash       string `json:"full_hash"`
	// PriorSeenCount is set by the subscriber to the seen_count BEFORE this enrollment's upsert.
	// 0 means genuinely new device — never seen before.
	PriorSeenCount int `json:"-"`
	// PriorIPKnown is true if the source IP was seen from a DIFFERENT machine before this enrollment.
	PriorIPKnown bool `json:"-"`
}

func Evaluate(ctx context.Context, s *store.Store, ev EnrollEvent) Decision {
	if strings.ToLower(ev.Status) == "rejected" {
		return Decision{Status: "rejected", Reason: ev.Reason}
	}

	if ev.HardwareHash != "" {
		denied, err := s.HasDeniedRecord(ctx, ev.HardwareHash)
		if err != nil {
			return Decision{Status: "pending", Reason: "db error checking denied records"}
		}
		if denied {
			return Decision{Status: "pending", Reason: "prior denied record for hardware_hash"}
		}
	}

	// Rule 3: returning device — hardware_hash seen before this enrollment.
	if ev.HardwareHash != "" && ev.PriorSeenCount > 0 {
		score := 100
		return Decision{
			Status:     "auto_approved",
			Reason:     "exact hardware_hash match (seen_count=" + strconv.Itoa(ev.PriorSeenCount) + ")",
			Score:      &score,
			Confidence: "high",
		}
	}

	// Rule 4b: independent match score >= 80 → auto_approved even if hardware_hash is new
	score := computeScore(ctx, ev, s)
	if score >= 80 {
		return Decision{
			Status:     "auto_approved",
			Reason:     "fingerprint match score >= 80",
			Score:      &score,
			Confidence: "high",
		}
	}

	if strings.ToLower(ev.Status) == "approved" {
		score := 80
		return Decision{
			Status:     "auto_approved",
			Reason:     "controller approved",
			Score:      &score,
			Confidence: "high",
		}
	}

	// Rule 5: new hardware but source IP was seen from a different machine before this enrollment.
	if ev.PriorIPKnown {
		score := 40
		return Decision{
			Status:     "auto_approved",
			Reason:     "new hardware_hash but known source_ip",
			Score:      &score,
			Confidence: "medium",
		}
	}

	return Decision{Status: "pending", Reason: "new hardware_hash and new source_ip"}
}

// computeScore computes a 0-100 weighted similarity score for the enrollment event
// against any stored fingerprint for the same machine (by hardware_hash).
// Returns 0 if no stored fingerprint is found.
func computeScore(ctx context.Context, ev EnrollEvent, s *store.Store) int {
	if ev.HardwareHash == "" {
		return 0
	}
	stored, err := s.GetFingerprintByHardwareHash(ctx, ev.HardwareHash)
	if err != nil || stored == nil {
		return 0
	}

	type check struct {
		weight  int
		matched bool
	}
	checks := []check{
		{25, ev.HardwareHash != "" && ev.HardwareHash == stored.HardwareHash},
		{20, ev.MachineID != "" && ev.MachineID == stored.MachineID},
		{15, false}, // serial not in EnrollEvent
		{10, false}, // ssh_host_keys not in EnrollEvent
		{8, false},  // mac_overlap not in EnrollEvent
		{5, ev.NetworkHash != "" && ev.NetworkHash == stored.NetworkHash},
		{5, ev.Hostname != "" && ev.Hostname == stored.Hostname},
		{5, false},  // cpu_model not in EnrollEvent
		{3, false},  // os_version not in EnrollEvent
		{2, false},  // memory_mb not in EnrollEvent
		{2, false},  // timezone not in EnrollEvent
	}

	total, matched := 0, 0
	for _, c := range checks {
		total += c.weight
		if c.matched {
			matched += c.weight
		}
	}
	if total == 0 {
		return 0
	}
	return (matched * 100) / total
}

