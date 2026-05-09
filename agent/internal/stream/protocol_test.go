package stream

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	var buf bytes.Buffer

	enc, err := NewEncoder(&buf)
	if err != nil {
		t.Fatal(err)
	}

	// Send a system message
	sys := SystemData{
		Hostname:   "test-host",
		CPUPercent: 42.5,
		MemTotal:   16 * 1024 * 1024 * 1024,
		MemUsed:    8 * 1024 * 1024 * 1024,
		MemPercent: 50.0,
		LoadAvg1:   1.5,
		Uptime:     86400,
		NumCPU:     4,
	}
	if err := enc.Send(MsgSystem, sys); err != nil {
		t.Fatal(err)
	}

	// Decode it
	dec, err := NewDecoder(&buf)
	if err != nil {
		t.Fatal(err)
	}

	msg, err := dec.Receive()
	if err != nil {
		t.Fatal(err)
	}

	if msg.Type != MsgSystem {
		t.Fatalf("expected MsgSystem, got %d", msg.Type)
	}
	if msg.Timestamp.IsZero() {
		t.Fatal("timestamp should not be zero")
	}

	var decoded SystemData
	if err := json.Unmarshal(msg.Payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Hostname != "test-host" {
		t.Fatalf("expected test-host, got %s", decoded.Hostname)
	}
	if decoded.CPUPercent != 42.5 {
		t.Fatalf("expected 42.5, got %f", decoded.CPUPercent)
	}
}

func TestHeartbeatNoPayload(t *testing.T) {
	var buf bytes.Buffer

	enc, _ := NewEncoder(&buf)
	enc.Send(MsgHeartbeat, nil)

	dec, _ := NewDecoder(&buf)
	msg, err := dec.Receive()
	if err != nil {
		t.Fatal(err)
	}
	if msg.Type != MsgHeartbeat {
		t.Fatalf("expected heartbeat, got %d", msg.Type)
	}
	if len(msg.Payload) != 0 {
		t.Fatalf("heartbeat should have no payload, got %d bytes", len(msg.Payload))
	}
}

func TestCompressionRatio(t *testing.T) {
	sys := SystemData{
		Hostname:   "production-server-001",
		CPUPercent: 42.5,
		MemTotal:   68719476736,
		MemUsed:    34359738368,
		MemPercent: 50.0,
		LoadAvg1:   1.5,
		LoadAvg5:   2.0,
		LoadAvg15:  1.8,
		Uptime:     8640000,
		NumCPU:     16,
	}

	raw, _ := json.Marshal(sys)
	rawSize := len(raw)

	var buf bytes.Buffer
	enc, _ := NewEncoder(&buf)
	enc.Send(MsgSystem, sys)
	wireSize := buf.Len()

	ratio := float64(wireSize) / float64(rawSize) * 100
	t.Logf("raw JSON: %d bytes, wire: %d bytes (%.1f%%)", rawSize, wireSize, ratio)

	// Wire should be smaller than raw JSON + header (compression helps)
	if wireSize > rawSize+HeaderSize+20 {
		t.Fatalf("compression not working: wire %d > raw %d + header", wireSize, rawSize)
	}
}

func TestMultipleMessages(t *testing.T) {
	var buf bytes.Buffer
	enc, _ := NewEncoder(&buf)

	// Send 3 different types
	enc.Send(MsgHeartbeat, nil)
	enc.Send(MsgSystem, SystemData{Hostname: "a", CPUPercent: 10})
	enc.Send(MsgNetwork, NetworkData{Interfaces: []NetInterface{{Name: "eth0", BytesSent: 1000}}})

	dec, _ := NewDecoder(&buf)

	msg1, _ := dec.Receive()
	msg2, _ := dec.Receive()
	msg3, _ := dec.Receive()

	if msg1.Type != MsgHeartbeat {
		t.Fatalf("msg1: expected heartbeat, got %s", MsgName(msg1.Type))
	}
	if msg2.Type != MsgSystem {
		t.Fatalf("msg2: expected system, got %s", MsgName(msg2.Type))
	}
	if msg3.Type != MsgNetwork {
		t.Fatalf("msg3: expected network, got %s", MsgName(msg3.Type))
	}
}

func TestMsgName(t *testing.T) {
	if MsgName(MsgHeartbeat) != "heartbeat" {
		t.Fatal("wrong name")
	}
	if MsgName(MsgSystem) != "system" {
		t.Fatal("wrong name")
	}
	if MsgName(0x42) != "unknown" {
		t.Fatal("should be unknown")
	}
}
