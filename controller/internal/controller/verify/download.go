package verify

import (
	"strings"

	"git.konoss.org/kore/schmutz/controller/internal/service"
)

// DownloadRecord is the appetizer captured at binary download time.
// Compared against the agent's connection when it calls home.
type DownloadRecord struct {
	OTT        string // one-time token issued at download
	JA4        string // TLS fingerprint seen by Caddy
	RealIP     string // true client IP
	TLSVersion string // TLS version negotiated
	UA         string // User-Agent at download
}

// AgentConnection is what the server observes when the agent connects via Ziti.
type AgentConnection struct {
	JA4        string // TLS fingerprint of this connection
	RealIP     string // IP of this connection
	TLSVersion string // TLS version
	UA         string // User-Agent reported by agent
	Hostname   string // machine hostname
	MAC        string // primary MAC address
	HWHash     string // hardware fingerprint hash
}

// VerifyDownload checks whether the agent connecting now matches
// the appetizer captured when the binary was downloaded.
//
// It answers: is this the same person/machine that downloaded schmutz?
//
// Matching strategy (any one is sufficient for blue-demo trust):
//  1. Same IP   — strongest: same machine, short TTL makes spoofing hard
//  2. Same JA4  — same TLS client stack even from different network (mobile data, VPN)
//  3. Same UA   — weakest fallback, but still a signal
//
// Returns a Verdict. Caller decides what to do with it.
func VerifyDownload(rec *service.OTTRecord, conn AgentConnection) Verdict {
	if rec == nil {
		return Verdict{
			Check:  "download_match",
			Passed: false,
			Reason: "OTT not found or expired",
		}
	}

	// Check 1: IP match
	if rec.RealIP != "" && conn.RealIP != "" && rec.RealIP == conn.RealIP {
		return Verdict{
			Check:      "download_match",
			Passed:     true,
			Confidence: "high",
			Reason:     "IP match between download and connection",
		}
	}

	// Check 2: JA4 TLS fingerprint match
	if rec.JA4 != "" && conn.JA4 != "" && rec.JA4 == conn.JA4 {
		return Verdict{
			Check:      "download_match",
			Passed:     true,
			Confidence: "medium",
			Reason:     "JA4 fingerprint match (different IP — likely mobile/VPN)",
		}
	}

	// Check 3: UA match — weakest but still corroborating
	if rec.UA != "" && conn.UA != "" && normalizeUA(rec.UA) == normalizeUA(conn.UA) {
		return Verdict{
			Check:      "download_match",
			Passed:     true,
			Confidence: "low",
			Reason:     "User-Agent match only — flagged for review",
		}
	}

	return Verdict{
		Check:  "download_match",
		Passed: false,
		Reason: "no signal match between download appetizer and current connection",
		Data: map[string]string{
			"download_ip":  rec.RealIP,
			"connect_ip":   conn.RealIP,
			"download_ja4": rec.JA4,
			"connect_ja4":  conn.JA4,
		},
	}
}

// normalizeUA strips version numbers for loose UA family comparison.
func normalizeUA(ua string) string {
	// Extract just the product token family — ignore version churn
	ua = strings.ToLower(ua)
	for _, token := range []string{"schmutz/", "go-http-client/", "curl/", "wget/"} {
		if strings.Contains(ua, token) {
			return token
		}
	}
	return ua
}
