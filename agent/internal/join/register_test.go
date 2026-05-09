package join

import (
	"testing"
)

func TestGenerateUUID(t *testing.T) {
	id := generateUUID()
	if len(id) != 36 {
		t.Errorf("UUID length = %d, want 36", len(id))
	}
	parts := 0
	for _, c := range id {
		if c == '-' {
			parts++
		}
	}
	if parts != 4 {
		t.Errorf("UUID has %d dashes, want 4", parts)
	}
	if id == generateUUID() {
		t.Error("two UUIDs should not be equal")
	}
}

func TestGenerateNickname(t *testing.T) {
	nick := generateNickname()
	if len(nick) != 8 {
		t.Errorf("nickname length = %d, want 8", len(nick))
	}
	vowels := "aeiou"
	for i, c := range nick {
		isVowel := false
		for _, v := range vowels {
			if c == v {
				isVowel = true
				break
			}
		}
		if i%2 == 1 && !isVowel {
			t.Errorf("position %d (%c) should be a vowel", i, c)
		}
		if i%2 == 0 && isVowel {
			t.Errorf("position %d (%c) should be a consonant", i, c)
		}
	}
	if nick == generateNickname() {
		t.Error("two nicknames should not be equal")
	}
}

func TestNewRegistration(t *testing.T) {
	reg := NewRegistration("test-host")
	if reg.Hostname != "test-host" {
		t.Errorf("hostname = %q, want %q", reg.Hostname, "test-host")
	}
	if reg.State != StatePending {
		t.Errorf("state = %q, want %q", reg.State, StatePending)
	}
	if reg.ID == "" {
		t.Error("ID should not be empty")
	}
	if reg.Nickname == "" {
		t.Error("nickname should not be empty")
	}
	if reg.RegisteredAt == 0 {
		t.Error("registered_at should not be zero")
	}
}

