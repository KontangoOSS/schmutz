package service

import (
	"sync"
	"testing"
	"time"
)

func TestOTTStore_IssueAndConsume(t *testing.T) {
	s := NewOTTStore()
	token := s.Issue("t13d...", "1.2.3.4", "TLS 1.3", "schmutz/1.0", "", "")
	if len(token) != 64 {
		t.Fatalf("expected 64-char token, got %d", len(token))
	}
	rec := s.Consume(token)
	if rec == nil {
		t.Fatal("expected record after consume")
	}
	if rec.JA4 != "t13d..." || rec.RealIP != "1.2.3.4" {
		t.Errorf("record fields wrong: %+v", rec)
	}
}

func TestOTTStore_SingleUse(t *testing.T) {
	s := NewOTTStore()
	token := s.Issue("ja4", "ip", "tls", "ua", "", "")
	s.Consume(token) // first consume
	second := s.Consume(token)
	if second != nil {
		t.Error("second consume should return nil (single-use)")
	}
}

func TestOTTStore_UnknownToken(t *testing.T) {
	s := NewOTTStore()
	rec := s.Consume("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if rec != nil {
		t.Error("unknown token should return nil")
	}
}

func TestOTTStore_PeekDoesNotConsume(t *testing.T) {
	s := NewOTTStore()
	token := s.Issue("ja4", "ip", "tls", "ua", "", "")
	peek := s.Peek(token)
	if peek == nil {
		t.Fatal("peek should find token")
	}
	// Consume should still work after peek
	rec := s.Consume(token)
	if rec == nil {
		t.Error("peek should not consume — token should still be available")
	}
}

func TestOTTStore_TTLExpiry(t *testing.T) {
	s := NewOTTStore()
	token := s.Issue("ja4", "ip", "tls", "ua", "", "")
	// Manually expire the record
	s.mu.Lock()
	s.records[token].IssuedAt = time.Now().Add(-11 * time.Minute)
	s.mu.Unlock()

	rec := s.Consume(token)
	if rec != nil {
		t.Error("expired token should return nil")
	}
}

func TestOTTStore_WithZitiFields(t *testing.T) {
	s := NewOTTStore()
	token := s.Issue("ja4", "ip", "tls", "ua", "eyJhbGciOiJSUzI1NiJ9...", "ziti-id-abc123")
	rec := s.Consume(token)
	if rec == nil {
		t.Fatal("expected record after consume")
	}
	if rec.ZitiJWT != "eyJhbGciOiJSUzI1NiJ9..." {
		t.Errorf("ZitiJWT wrong: %q", rec.ZitiJWT)
	}
	if rec.ZitiID != "ziti-id-abc123" {
		t.Errorf("ZitiID wrong: %q", rec.ZitiID)
	}
}

func TestOTTStore_ConcurrentAccess(t *testing.T) {
	s := NewOTTStore()
	var wg sync.WaitGroup
	tokens := make([]string, 50)
	for i := range tokens {
		tokens[i] = s.Issue("ja4", "ip", "tls", "ua", "", "")
	}

	// Concurrently consume all tokens
	results := make([]bool, len(tokens))
	for i, tok := range tokens {
		wg.Add(1)
		go func(idx int, t string) {
			defer wg.Done()
			results[idx] = s.Consume(t) != nil
		}(i, tok)
	}
	wg.Wait()

	// Every token should have been consumed exactly once
	consumed := 0
	for _, r := range results {
		if r {
			consumed++
		}
	}
	if consumed != len(tokens) {
		t.Errorf("expected %d successful consumes, got %d", len(tokens), consumed)
	}
}

func TestOTTStore_UniqueTokens(t *testing.T) {
	s := NewOTTStore()
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		tok := s.Issue("j", "i", "t", "u", "", "")
		if seen[tok] {
			t.Fatalf("duplicate token generated at iteration %d", i)
		}
		seen[tok] = true
	}
}
