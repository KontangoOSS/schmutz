package service

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

const ottTTL = 10 * time.Minute

// OTTRecord holds the appetizer captured at download time.
// Valid for ottTTL — consumed on first successful use.
type OTTRecord struct {
	OTT        string
	JA4        string
	RealIP     string
	TLSVersion string
	UA         string
	IssuedAt   time.Time
	ZitiJWT    string // Ziti enrollment JWT from CreateIdentity (empty for download-path OTTs)
	ZitiID     string // Ziti identity ID for cleanup on failure (empty for download-path OTTs)
}

func (r *OTTRecord) Expired() bool {
	return time.Since(r.IssuedAt) > ottTTL
}

// OTTStore is an in-memory OTT registry with automatic expiry.
// Keyed by OTT token. Consumed (deleted) on successful cert issuance.
// OnExpire is called for any record that expires unconsumed and has a ZitiID —
// this cleans up the pre-created Ziti identity so abandoned enrollments don't accumulate.
type OTTStore struct {
	mu       sync.Mutex
	records  map[string]*OTTRecord
	OnExpire func(zitiID string) // called when a pre-created identity's OTT expires unused
}

func NewOTTStore() *OTTStore {
	s := &OTTStore{records: make(map[string]*OTTRecord)}
	go s.cleanupLoop()
	return s
}

// Issue generates a new OTT, stores the appetizer against it, returns the token.
// zitiJWT and zitiID are set when the /api/v1/start handler pre-creates a Ziti identity;
// they are empty for OTTs issued at download time (the Download handler).
func (s *OTTStore) Issue(ja4, realIP, tlsVersion, ua, zitiJWT, zitiID string) string {
	token := generateOTT()
	s.mu.Lock()
	s.records[token] = &OTTRecord{
		OTT:        token,
		JA4:        ja4,
		RealIP:     realIP,
		TLSVersion: tlsVersion,
		UA:         ua,
		IssuedAt:   time.Now(),
		ZitiJWT:    zitiJWT,
		ZitiID:     zitiID,
	}
	s.mu.Unlock()
	return token
}

// Consume looks up the OTT, validates it hasn't expired, deletes it, returns the record.
// Returns nil if not found or expired — single use.
func (s *OTTStore) Consume(token string) *OTTRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[token]
	if !ok || rec.Expired() {
		delete(s.records, token)
		return nil
	}
	delete(s.records, token)
	return rec
}

// Peek looks up without consuming — for inspection without invalidating.
func (s *OTTStore) Peek(token string) *OTTRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[token]
	if !ok || rec.Expired() {
		return nil
	}
	return rec
}

func (s *OTTStore) cleanupLoop() {
	for range time.Tick(time.Minute) {
		var expired []*OTTRecord
		s.mu.Lock()
		for token, rec := range s.records {
			if rec.Expired() {
				expired = append(expired, rec)
				delete(s.records, token)
			}
		}
		s.mu.Unlock()
		// Call OnExpire outside the lock — it may do network calls (delete Ziti identity).
		if s.OnExpire != nil {
			for _, rec := range expired {
				if rec.ZitiID != "" {
					s.OnExpire(rec.ZitiID)
				}
			}
		}
	}
}

func generateOTT() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}
