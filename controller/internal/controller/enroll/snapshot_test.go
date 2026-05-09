package enroll

import (
	"testing"
	"time"

	"github.com/KontangoOSS/schmutz/controller/internal/controller/common"
	"github.com/KontangoOSS/schmutz/controller/internal/controller/verify"
)

func TestBuildSnapshot_ContainsRequiredFields(t *testing.T) {
	reg := &common.Registration{
		ID:           "test-machine-uuid",
		Hostname:     "test-box",
		State:        "approved",
		RegisteredAt: time.Now().Unix(),
	}

	profile := &verify.MachineProfile{
		Hostname:     "test-box",
		OS:           "linux",
		HardwareHash: "abc123def456abcd",
		MACAddrs:     []string{"fc:5c:ee:c2:fb:90"},
	}

	decision := &verify.Decision{
		Approved: true,
		Stage:    0,
		Reason:   "fingerprint match",
	}

	conn := ConnectionMeta{
		SourceIP:  "10.0.0.1",
		PublicIP:  "166.140.13.79",
		JA4:       "t13d1715h2_abc",
		UserAgent: "schmutz/0.3.1",
	}

	snap := BuildSnapshot(reg, profile, decision, conn, "ctrl-1", "ziti-id-xyz", "websocket")

	if snap["machine_id"] != "test-machine-uuid" {
		t.Errorf("missing machine_id, got %v", snap["machine_id"])
	}
	if snap["ziti_identity_id"] != "ziti-id-xyz" {
		t.Errorf("missing ziti_identity_id, got %v", snap["ziti_identity_id"])
	}
	if snap["enrolled_by"] != "websocket" {
		t.Errorf("missing enrolled_by, got %v", snap["enrolled_by"])
	}
	if snap["node"] != "ctrl-1" {
		t.Errorf("missing node, got %v", snap["node"])
	}
	conn2, ok := snap["connection"].(map[string]interface{})
	if !ok {
		t.Fatal("connection field missing or wrong type")
	}
	if conn2["source_ip"] != "10.0.0.1" {
		t.Errorf("connection.source_ip wrong: %v", conn2["source_ip"])
	}
	hw, ok := snap["hardware"].(map[string]interface{})
	if !ok {
		t.Fatal("hardware field missing")
	}
	if hw["hardware_hash"] != "abc123def456abcd" {
		t.Errorf("hardware.hardware_hash wrong: %v", hw["hardware_hash"])
	}
	dec, ok := snap["decision"].(map[string]interface{})
	if !ok {
		t.Fatal("decision field missing")
	}
	if dec["approved"] != true {
		t.Errorf("decision.approved wrong: %v", dec["approved"])
	}
}

func TestBuildSnapshot_NilProfileHandled(t *testing.T) {
	reg := &common.Registration{ID: "x", Hostname: "h"}
	snap := BuildSnapshot(reg, nil, nil, ConnectionMeta{}, "ctrl-1", "", "websocket")
	if snap["machine_id"] != "x" {
		t.Errorf("expected machine_id=x, got %v", snap["machine_id"])
	}
	// Should not panic with nil profile/decision
}
