package enroll

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"

	"github.com/KontangoOSS/schmutz/controller/internal/controller/verify"
)

// HandleInstall serves the install script after fingerprinting the connection.
// This is the first gate — bots and banned IPs never get the installer.
func (m *Module) HandleInstall(w http.ResponseWriter, r *http.Request) {
	// 1. Fingerprint the connection
	fp := verify.CaptureConnection(r)

	// 2. Screen — should we serve this?
	proceed, reason := m.V.ScreenConnection(fp)
	if !proceed {
		log.Printf("install: rejected %s (%s) — %s", fp.SourceIP, fp.ClientType, reason)
		// Silent drop — no error, no body, just close
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// 3. Generate a one-time session token
	sessionToken := generateSessionToken()

	// 4. Store the fingerprint + session token for WebSocket correlation
	fp.JA4 = r.Header.Get("X-JA4") // Caddy/Cloudflare may provide
	m.V.StoreConnectionFingerprint(fp)

	// Store session token → IP mapping (Bao with in-memory fallback).
	m.storeSession(sessionToken, fp.SourceIP)

	log.Printf("install: serving to %s (%s) session=%s", fp.SourceIP, fp.ClientType, sessionToken[:8]+"...")

	// 5. Serve the install script with embedded session token and BASE_URL
	scheme := "https"
	if fwd := r.Header.Get("X-Forwarded-Proto"); fwd != "" {
		scheme = fwd
	}
	host := r.Host
	if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
		host = fwd
	}
	baseURL := scheme + "://" + host

	zitiVer := m.ZitiVersion
	if zitiVer == "" {
		zitiVer = "latest"
	}

	baoVer := m.BaoVersion
	if baoVer == "" {
		baoVer = "2.5.2"
	}

	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, installScriptV2, baseURL, sessionToken, zitiVer, baoVer)
}

// GitHubRelease is the base URL for release downloads.
var _ = "" // placeholder, set from config

// generateSessionToken creates a cryptographic one-time session token.
func generateSessionToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}
