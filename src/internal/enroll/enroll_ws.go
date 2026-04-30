package enroll

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"

	ziti "github.com/openziti/sdk-golang/ziti"
	zitiEnroll "github.com/openziti/sdk-golang/ziti/enroll"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// WSEnrollConfig is everything RegisterWS needs from the caller.
type WSEnrollConfig struct {
	// ControllerURL is the join controller base URL, e.g. https://join.kontango.net
	// Always on port 443 — goes through Caddy, impossible to block via port filtering.
	ControllerURL string

	// IdentityPath is where to write the completed identity JSON.
	IdentityPath string

	// DeviceInfo is hardware/OS data collected by the caller.
	Info DeviceInfo

	// TermsAccepted MUST be true. Callers must obtain explicit T&C consent before
	// calling RegisterWS — interactive (prompt) or flag-based (--yes).
	TermsAccepted bool

	// AgentVersion is the caller's binary version string.
	AgentVersion string

	// Profile is the enrollment profile name requested.
	Profile string

	// Tags are arbitrary operator key/value pairs attached to the enrollment telemetry.
	Tags map[string]string

	// Identity fields — stored permanently in the Ziti identity appData.
	// These are attached once at enrollment and survive cert rotation.
	Slug    string  // human-readable name for this machine (e.g. "web-1", "gpu-box")
	Invitee string  // who vouched for this machine — email or username of the inviter
	Lat     float64 // approximate latitude at enroll time (0 = not provided)
	Long    float64 // approximate longitude at enroll time (0 = not provided)
}

// RegisterWS enrolls this device using the v1 WebSocket protocol.
//
// The private key is generated on the client by the Ziti SDK and NEVER transmitted.
// The server pre-creates a Ziti identity at /api/v1/start time and returns the OTT JWT
// at /api/v1/ws time. The client uses the JWT to enroll directly with Ziti's enrollment
// endpoint — posting a CSR, receiving cert+ca, and assembling the complete identity file.
//
// Flow:
//  1. Assert root — needed for full hardware fingerprinting
//  2. Assert T&C accepted by caller
//  3. POST /api/v1/start → get OTT (server captures JA4+IP fingerprint AND pre-creates Ziti identity)
//  4. WebSocket /api/v1/ws — to-go mode, send OTT + telemetry tags (no CSR)
//  5. Server verifies OTT, returns Ziti OTT JWT + public_zt_api in response
//  6. Client calls Ziti SDK enroll.Enroll() — generates key locally, posts CSR to Ziti, gets cert+ca
//  7. Marshal the resulting ziti.Config to JSON — write directly to disk (SDK assembles the identity)
func RegisterWS(ctx context.Context, cfg WSEnrollConfig) (*EnrollResult, error) {
	if cfg.ControllerURL == "" {
		return nil, fmt.Errorf("enroll/ws: controller_url required")
	}
	// T&C must be explicitly accepted before we do anything.
	// Interactive callers prompt the user; non-interactive callers pass --yes.
	if !cfg.TermsAccepted {
		return nil, fmt.Errorf("enroll/ws: terms of service not accepted — pass --yes to accept")
	}

	// Root is required for full hardware fingerprinting (disk serials, SSH host keys,
	// DMI data). Without root the fingerprint is weak and won't survive re-enrollment.
	if os.Getuid() != 0 {
		return nil, fmt.Errorf("enroll/ws: must run as root (sudo schmutz enroll) — required for hardware fingerprinting")
	}

	base := strings.TrimRight(cfg.ControllerURL, "/")

	// Step 3: touch /api/v1/start — server captures JA4+IP at this HTTPS request
	// AND pre-creates a Ziti identity, returning OTT in the header.
	ott, err := touchStart(ctx, base, cfg)
	if err != nil {
		return nil, fmt.Errorf("enroll/ws: start: %w", err)
	}

	// Step 4: open WebSocket to /api/v1/ws and send to-go enrollment message.
	// No CSR in this message — the server returns the Ziti JWT for direct enrollment.
	wsURL := strings.Replace(base, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL += "/api/v1/ws"

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("enroll/ws: dial %s: %w", wsURL, err)
	}
	defer conn.CloseNow()

	msg := wsEnrollMsg{
		Mode:          "to-go",
		Origin:        originFromEnv(),
		Type:          "hello",
		TermsAccepted: true, // already validated above
		OTT:           ott,
		HWHash:        cfg.Info.Fingerprint,
		Hostname:      cfg.Info.Hostname,
		// Telemetry tags — logged by server, correlate with telemetry stream
		AgentVersion: cfg.AgentVersion,
		OS:           cfg.Info.OS,
		Arch:         cfg.Info.Arch,
		Platform:     cfg.Info.Platform,
		Profile:      cfg.Profile,
		Tags:         cfg.Tags,
		// Identity fields — stored in Ziti appData, permanent on the identity
		Slug:    cfg.Slug,
		Invitee: cfg.Invitee,
		Lat:     cfg.Lat,
		Long:    cfg.Long,
	}
	if msg.OS == "" {
		msg.OS = runtime.GOOS
	}
	if msg.Arch == "" {
		msg.Arch = runtime.GOARCH
	}

	if err := wsjson.Write(ctx, conn, msg); err != nil {
		return nil, fmt.Errorf("enroll/ws: send: %w", err)
	}

	// Step 5: read response — server returns Ziti OTT JWT for direct enrollment.
	var resp struct {
		Status      string `json:"status"`
		Message     string `json:"message"`
		ZitiJWT     string `json:"ziti_jwt"`
		PublicZtAPI string `json:"public_zt_api"`
	}
	if err := wsjson.Read(ctx, conn, &resp); err != nil {
		return nil, fmt.Errorf("enroll/ws: read response: %w", err)
	}
	conn.Close(websocket.StatusNormalClosure, "")

	if resp.Status != "jwt_issued" {
		return nil, fmt.Errorf("enroll/ws: controller returned status=%q: %s", resp.Status, resp.Message)
	}
	if resp.ZitiJWT == "" {
		return nil, fmt.Errorf("enroll/ws: empty ziti_jwt in response")
	}

	// Step 6: use the Ziti Go SDK to enroll directly.
	// The SDK generates the key locally, posts CSR to Ziti's enrollment endpoint,
	// receives cert+ca, and returns a complete ziti.Config.
	zitiCfg, err := enrollWithZitiSDK(resp.ZitiJWT, resp.PublicZtAPI)
	if err != nil {
		return nil, fmt.Errorf("enroll/ws: ziti sdk enroll: %w", err)
	}

	// Step 7: marshal the config — this IS the identity file. SDK assembled everything.
	identityJSON, err := json.MarshalIndent(zitiCfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("enroll/ws: marshal identity: %w", err)
	}

	if cfg.IdentityPath != "" {
		if err := WriteIdentity(identityJSON, cfg.IdentityPath); err != nil {
			return nil, err
		}
	}

	return &EnrollResult{
		IdentityJSON: identityJSON,
		Status:       "enrolled",
	}, nil
}

// enrollWithZitiSDK uses the Ziti Go SDK to perform client-side enrollment.
//
// Two distinct URLs are in play:
//   - Enrollment URL: JWT issuer (port 1280) — presents Ziti CA cert, used for CSR submission
//     and CA bundle fetch. Must NOT be overridden — the SDK validates TLS against the Ziti CA.
//   - ztAPI in identity.json: publicZtAPI (port 443 via Caddy ALPN mux) — what ziti tunnel
//     uses for ongoing controller connections. Patched into cfg.ZtAPI after Enroll() returns.
func enrollWithZitiSDK(zitiJWT, publicZtAPI string) (*ziti.Config, error) {
	claims, jwtToken, err := zitiEnroll.ParseToken(zitiJWT)
	if err != nil {
		return nil, fmt.Errorf("parse jwt: %w", err)
	}

	// Do NOT override claims.Issuer — the SDK uses it to fetch the Ziti CA bundle and
	// submit the CSR. The issuer points to port 1280 which presents the Ziti CA cert.
	// Overriding it to 443 (LE cert) causes CA validation to fail.

	var keyAlg ziti.KeyAlgVar
	_ = keyAlg.Set("EC")

	flags := zitiEnroll.EnrollmentFlags{
		Token:     claims,
		JwtToken:  jwtToken,
		JwtString: zitiJWT,
		KeyAlg:    keyAlg,
	}

	cfg, err := zitiEnroll.Enroll(flags)
	if err != nil {
		return nil, fmt.Errorf("ziti enroll: %w (check that the Ziti enrollment endpoint is reachable)", err)
	}

	// Patch ztAPI to the public 443 address so ziti tunnel connects through Caddy.
	// This is separate from enrollment — the tunnel uses the ziti-ctrl ALPN channel
	// on 443, which Caddy muxes to port 1280. Port 1280 never needs to be public.
	if publicZtAPI != "" {
		cfg.ZtAPI = publicZtAPI
	}

	return cfg, nil
}

// wsEnrollMsg matches the server-side walkInMsg struct.
// Fields map 1:1 — keep in sync with api_walkin.go.
// No CSR field — the client enrolls directly with the Ziti SDK using the JWT returned by the server.
type wsEnrollMsg struct {
	Mode          string            `json:"mode"`
	Origin        string            `json:"origin"`
	Type          string            `json:"type"`
	TermsAccepted bool              `json:"terms_accepted"`
	OTT           string            `json:"ott"`
	HWHash        string            `json:"hw_hash,omitempty"`
	Hostname      string            `json:"hostname,omitempty"`
	AgentVersion  string            `json:"agent_version,omitempty"`
	OS            string            `json:"os,omitempty"`
	Arch          string            `json:"arch,omitempty"`
	Platform      string            `json:"platform,omitempty"`
	Profile       string            `json:"profile,omitempty"`
	Tags          map[string]string `json:"tags,omitempty"`
	// Identity fields — written into Ziti appData, permanent on the identity
	Slug    string  `json:"slug,omitempty"`
	Invitee string  `json:"invitee,omitempty"`
	Lat     float64 `json:"lat,omitempty"`
	Long    float64 `json:"long,omitempty"`
}

// touchStart POSTs to /api/v1/start and returns the OTT from the response header.
// The endpoint is on port 443 behind Caddy — the same port as HTTPS, impossible to
// block without breaking all web traffic. Caddy captures the connection fingerprint here.
// The server also pre-creates a Ziti identity and stores the OTT JWT in the OTT record.
func touchStart(ctx context.Context, base string, cfg WSEnrollConfig) (string, error) {
	body, _ := json.Marshal(map[string]string{
		"type":    originFromEnv(),
		"hw_hash": cfg.Info.Fingerprint,
		"version": cfg.AgentVersion,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		base+"/api/v1/start",
		strings.NewReader(string(body)),
	)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "schmutz-agent/"+cfg.AgentVersion)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("POST /api/v1/start: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("POST /api/v1/start: HTTP %d", resp.StatusCode)
	}

	ott := resp.Header.Get("X-Enrollment-Token")
	if ott == "" {
		return "", fmt.Errorf("POST /api/v1/start: missing X-Enrollment-Token header")
	}
	return ott, nil
}

// originFromEnv returns the origin string for the current process.
// Hook/PXE sets SCHMUTZ_ORIGIN=hook; agent defaults to "agent".
func originFromEnv() string {
	if v := os.Getenv("SCHMUTZ_ORIGIN"); v == "hook" {
		return "hook"
	}
	return "agent"
}
