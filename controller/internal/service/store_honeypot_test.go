package service_test

import (
	"testing"

	"github.com/KontangoOSS/schmutz/controller/internal/service"
)

func TestHoneypotRoundtrip(t *testing.T) {
	s := service.NewTestStore()

	fp := "abc123fingerprint"
	data := map[string]interface{}{
		"id":          "uuid-001",
		"fingerprint": fp,
		"state":       "pending",
		"ip":          "1.2.3.4",
	}

	if err := s.PutHoneypot(fp, data); err != nil {
		t.Fatalf("PutHoneypot: %v", err)
	}

	got, err := s.GetHoneypot(fp)
	if err != nil {
		t.Fatalf("GetHoneypot: %v", err)
	}
	if got == nil {
		t.Fatal("GetHoneypot: got nil")
	}
	if got["id"] != "uuid-001" {
		t.Errorf("id: want uuid-001, got %v", got["id"])
	}
}

func TestGetHoneypotMissing(t *testing.T) {
	s := service.NewTestStore()
	got, err := s.GetHoneypot("does-not-exist")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing key, got %v", got)
	}
}
