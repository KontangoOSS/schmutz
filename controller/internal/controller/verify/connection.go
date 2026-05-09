package verify

import (
	"crypto/tls"
	"net/http"
	"strings"
	"time"
)

// ConnectionFingerprint is everything we can see from an HTTP request
// before serving any content. Captured at the install endpoint.
type ConnectionFingerprint struct {
	// From the request
	SourceIP   string `json:"source_ip"`
	UserAgent  string `json:"user_agent"`
	Accept     string `json:"accept,omitempty"`
	AcceptLang string `json:"accept_lang,omitempty"`
	AcceptEnc  string `json:"accept_enc,omitempty"`
	Referer    string `json:"referer,omitempty"`
	Origin     string `json:"origin,omitempty"`

	// From TLS (if available via Caddy headers or direct TLS)
	TLSVersion  string `json:"tls_version,omitempty"`
	CipherSuite string `json:"cipher_suite,omitempty"`
	ServerName  string `json:"server_name,omitempty"`
	JA4         string `json:"ja4,omitempty"`

	// Timing
	Timestamp int64 `json:"timestamp"`

	// Classification
	IsBot      bool   `json:"is_bot"`
	IsCurl     bool   `json:"is_curl"`
	IsBrowser  bool   `json:"is_browser"`
	ClientType string `json:"client_type"` // curl, wget, browser, bot, unknown
}

// CaptureConnection extracts everything we can from an HTTP request.
func CaptureConnection(r *http.Request) *ConnectionFingerprint {
	fp := &ConnectionFingerprint{
		Timestamp:  time.Now().Unix(),
		UserAgent:  r.Header.Get("User-Agent"),
		Accept:     r.Header.Get("Accept"),
		AcceptLang: r.Header.Get("Accept-Language"),
		AcceptEnc:  r.Header.Get("Accept-Encoding"),
		Referer:    r.Header.Get("Referer"),
		Origin:     r.Header.Get("Origin"),
	}

	// Source IP — respect proxy headers
	fp.SourceIP = r.RemoteAddr
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		fp.SourceIP = strings.TrimSpace(strings.Split(fwd, ",")[0])
	}
	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		fp.SourceIP = realIP
	}
	// Strip port
	if idx := strings.LastIndex(fp.SourceIP, ":"); idx > 0 {
		if !strings.Contains(fp.SourceIP[idx:], "]") { // not IPv6
			fp.SourceIP = fp.SourceIP[:idx]
		}
	}

	// TLS info (if direct TLS, not behind proxy)
	if r.TLS != nil {
		fp.TLSVersion = tlsVersionName(r.TLS.Version)
		fp.CipherSuite = tls.CipherSuiteName(r.TLS.CipherSuite)
		fp.ServerName = r.TLS.ServerName
	}

	// Caddy/Cloudflare may pass these
	if ja4 := r.Header.Get("X-JA4"); ja4 != "" {
		fp.JA4 = ja4
	}
	if cfIP := r.Header.Get("CF-Connecting-IP"); cfIP != "" {
		fp.SourceIP = cfIP
	}

	// Classify the client
	fp.classify()

	return fp
}

func (fp *ConnectionFingerprint) classify() {
	ua := strings.ToLower(fp.UserAgent)

	switch {
	case strings.Contains(ua, "curl/"):
		fp.IsCurl = true
		fp.ClientType = "curl"
	case strings.Contains(ua, "wget/"):
		fp.ClientType = "wget"
	case strings.Contains(ua, "mozilla/") || strings.Contains(ua, "chrome/") || strings.Contains(ua, "safari/"):
		fp.IsBrowser = true
		fp.ClientType = "browser"
	case strings.Contains(ua, "go-http-client"):
		fp.ClientType = "go"
	case strings.Contains(ua, "python"):
		fp.ClientType = "python"
	case ua == "" || strings.Contains(ua, "scan") || strings.Contains(ua, "bot") || strings.Contains(ua, "zgrab") || strings.Contains(ua, "masscan"):
		fp.IsBot = true
		fp.ClientType = "bot"
	default:
		fp.ClientType = "unknown"
	}
}

// ScreenConnection runs the verify checks we can do with just connection data.
// Returns whether to proceed (serve the installer) or reject.
func (m *Module) ScreenConnection(fp *ConnectionFingerprint) (proceed bool, reason string) {
	// Bots — instant reject, no response
	if fp.IsBot {
		return false, "bot detected"
	}

	// Empty User-Agent — suspicious
	if fp.UserAgent == "" {
		return false, "no user-agent"
	}

	// Check if IP is banned
	if ban := m.Banned(fp.SourceIP); ban.Banned {
		return false, "ip banned"
	}

	// Check if JA4 is banned (if we have it)
	if fp.JA4 != "" {
		if ban := m.Banned(fp.JA4); ban.Banned {
			return false, "ja4 banned"
		}
	}

	return true, ""
}

// StoreConnectionFingerprint saves the connection fingerprint for later correlation.
// Keyed by source IP + timestamp so we can match it when the WebSocket connects.
func (m *Module) StoreConnectionFingerprint(fp *ConnectionFingerprint) {
	key := "identities/sessions/" + fp.SourceIP
	m.C.Bao.Put("secret", key, map[string]interface{}{
		"source_ip":    fp.SourceIP,
		"user_agent":   fp.UserAgent,
		"client_type":  fp.ClientType,
		"ja4":          fp.JA4,
		"tls_version":  fp.TLSVersion,
		"cipher_suite": fp.CipherSuite,
		"timestamp":    fp.Timestamp,
		"accept":       fp.Accept,
		"accept_lang":  fp.AcceptLang,
	})
}

// CorrelateConnection checks if a WebSocket connection matches a previous install request.
func (m *Module) CorrelateConnection(wsIP string) (*ConnectionFingerprint, bool) {
	data, err := m.C.Bao.Get("secret", "identities/sessions/"+wsIP)
	if err != nil || data == nil {
		return nil, false
	}

	fp := &ConnectionFingerprint{
		SourceIP:   strFromMap(data, "source_ip"),
		UserAgent:  strFromMap(data, "user_agent"),
		ClientType: strFromMap(data, "client_type"),
		JA4:        strFromMap(data, "ja4"),
		TLSVersion: strFromMap(data, "tls_version"),
	}
	if ts, ok := data["timestamp"].(float64); ok {
		fp.Timestamp = int64(ts)
	}

	// Only correlate if the install request was recent (within 5 minutes)
	if time.Now().Unix()-fp.Timestamp > 300 {
		return nil, false
	}

	return fp, true
}

func strFromMap(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

func tlsVersionName(v uint16) string {
	switch v {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return "unknown"
	}
}
