package enroll

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"git.konoss.org/kore/schmutz/controller/internal/controller/verify"
	"git.konoss.org/kore/schmutz/controller/internal/service"
)

// AgentEnrollRequest is the full enrollment request from a schmutz agent.
type AgentEnrollRequest struct {
	OTTRecord    *service.OTTRecord    // appetizer captured at download time
	Connection   verify.AgentConnection // current connection metadata
	CSR          string                 // PEM CSR — private key stays on agent, never transmitted here
	IdentityName string                 // Ziti identity name
}

// AgentEnrollResult is returned to the agent on success.
type AgentEnrollResult struct {
	// IdentityJSON is the assembled Ziti identity JSON.
	// Agent writes this alongside its locally-held private key.
	// Then runs: ziti tunnel run --identity identity.json
	IdentityJSON []byte
	ZitiID       string
}

// enrollmentClaims is the minimal JWT payload from the Ziti OTT JWT.
type enrollmentClaims struct {
	jwt.RegisteredClaims
	EnrollmentMethod string `json:"em"`
}

// EnrollAgent is Module 2 of the agent enrollment flow.
// Called only after Module 1 (VerifyDownload) passes.
//
// The agent generated the key pair and CSR locally — private key never leaves the agent.
// We forward the CSR to the Ziti controller's enrollment endpoint.
// Ziti CA signs the cert and returns it. We assemble and return the identity JSON.
//
// Uses Ziti Go SDK and enrollment REST API — no C SDK, no shelling out.
// Certificate chain is 100% Ziti CA (NetFoundry-compatible).
func (m *Module) EnrollAgent(req AgentEnrollRequest) (*AgentEnrollResult, error) {
	// Step 1: Create identity in Ziti, get OTT JWT back.
	// Only opaque/non-PII fields go into Ziti appData — visible to all management API users.
	// IP, MAC, and hostname are stored separately in Bao audit log (access-controlled).
	appData := map[string]interface{}{
		"zone":    "blue",
		"origin":  "agent",
		"source":  "to-go",
		"hw_hash": req.Connection.HWHash, // opaque hash — not PII
	}

	authToken, err := m.C.Ziti.Authenticate()
	if err != nil {
		return nil, fmt.Errorf("enroll agent: ziti auth: %w", err)
	}

	zitiID, ottJWT, err := m.C.Ziti.CreateIdentity(
		req.IdentityName,
		[]string{"blue-demo"},
		appData,
	)
	if err != nil {
		return nil, fmt.Errorf("enroll agent: create identity %q: %w", req.IdentityName, err)
	}

	log.Printf("enroll/agent: identity created name=%s id=%s", req.IdentityName, zitiID)

	// Step 2: Parse the OTT JWT to get the enrollment URL.
	// The JWT issuer + /edge/client/v1/enroll?method=ott&token=<jti> is the endpoint.
	enrollURL, err := parseEnrollURL(ottJWT)
	if err != nil {
		m.C.Ziti.DeleteIdentity(authToken, zitiID)
		return nil, fmt.Errorf("enroll agent: parse OTT JWT: %w", err)
	}

	log.Printf("enroll/agent: enrolling via %s csrLen=%d", enrollURL, len(req.CSR))

	// Step 3: POST the client's CSR to the Ziti enrollment endpoint.
	// Ziti CA signs it and returns the cert. Private key never touches this server.
	cert, caCert, err := submitCSR(enrollURL, req.CSR)
	if err != nil {
		m.C.Ziti.DeleteIdentity(authToken, zitiID)
		return nil, fmt.Errorf("enroll agent: CSR submission: %w", err)
	}

	log.Printf("enroll/agent: cert issued id=%s ip=%s", zitiID, req.Connection.RealIP)

	// Step 4: Assemble the identity JSON.
	// The agent combines this with its local private key to get a complete identity.
	// Structure matches what `ziti edge enroll` produces.
	identity := buildIdentityJSON(req.IdentityName, enrollURL, cert, caCert)

	// Patch ztAPI to public controller address so agent can bootstrap before overlay is up.
	if m.Enroll.PublicZtAPI != "" {
		identity = patchZtAPI(identity, m.Enroll.PublicZtAPI)
	}

	return &AgentEnrollResult{
		IdentityJSON: identity,
		ZitiID:       zitiID,
	}, nil
}

// SubmitCSR is the exported form of submitCSR for use by re-enrollment paths.
func SubmitCSR(enrollURL, csrPEM string) (cert, caCert string, err error) {
	return submitCSR(enrollURL, csrPEM)
}

// ParseEnrollURL is the exported form of parseEnrollURL.
func ParseEnrollURL(ottJWT string) (string, error) {
	return parseEnrollURL(ottJWT)
}

// BuildIdentityJSON is the exported form of buildIdentityJSON.
func BuildIdentityJSON(name, controllerURL, cert, caCert string) []byte {
	return buildIdentityJSON(name, controllerURL, cert, caCert)
}

// PatchZtAPI is the exported form of patchZtAPI.
func PatchZtAPI(identity []byte, publicAPI string) []byte {
	return patchZtAPI(identity, publicAPI)
}

// submitCSR posts the PEM CSR to the Ziti enrollment endpoint and returns
// the signed cert PEM and CA cert PEM. This is the correct way to use a
// client-generated CSR — the private key never leaves the client.
func submitCSR(enrollURL, csrPEM string) (cert, caCert string, err error) {
	// Ziti enrollment endpoint expects the PEM CSR as application/x-pem-file.
	// This is what the Ziti Go SDK uses internally (sdk-golang/ziti/enroll/enroll.go).
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	resp, err := client.Post(enrollURL, "application/x-pem-file", bytes.NewReader([]byte(csrPEM)))
	if err != nil {
		return "", "", fmt.Errorf("POST %s: %w", enrollURL, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("enrollment endpoint %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Data struct {
			Cert   string `json:"cert"`
			CACert string `json:"ca"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", "", fmt.Errorf("parse enrollment response: %w", err)
	}

	// If the CA cert wasn't returned separately, extract it from the cert chain.
	// The cert field contains: client cert + intermediate CA + (sometimes) root CA.
	// The tunnel needs the root CA in the ca field to verify the controller's TLS cert.
	cert = result.Data.Cert
	caCert = result.Data.CACert
	if caCert == "" && cert != "" {
		caCert = extractCACertFromChain(cert)
	}

	return cert, caCert, nil
}

// parseEnrollURL extracts the enrollment URL from the OTT JWT.
// The JWT issuer field contains the controller base URL.
func parseEnrollURL(ottJWT string) (string, error) {
	// Parse without signature verification — we trust the JWT came from our own Ziti cluster
	// (we issued the OTT ourselves via CreateIdentity). We DO validate expiry so expired
	// enrollment windows are rejected before we even try to submit the CSR.
	p := jwt.NewParser(jwt.WithExpirationRequired())
	var claims enrollmentClaims
	_, _, err := p.ParseUnverified(ottJWT, &claims)
	if err != nil {
		return "", fmt.Errorf("parse JWT: %w", err)
	}
	// Manual expiry check since ParseUnverified skips claim validation
	if claims.ExpiresAt != nil && claims.ExpiresAt.Before(time.Now()) {
		return "", fmt.Errorf("enrollment JWT expired at %s", claims.ExpiresAt.Time)
	}

	issuer := claims.Issuer
	if issuer == "" {
		return "", fmt.Errorf("JWT missing issuer")
	}
	jti := claims.ID
	if jti == "" {
		return "", fmt.Errorf("JWT missing jti (token ID)")
	}
	method := claims.EnrollmentMethod
	if method == "" {
		method = "ott"
	}

	// Build enrollment URL: <issuer>/edge/client/v1/enroll?method=ott&token=<jti>
	base := strings.TrimRight(issuer, "/")
	return fmt.Sprintf("%s/edge/client/v1/enroll?method=%s&token=%s", base, method, jti), nil
}

// buildIdentityJSON assembles the identity JSON that the agent writes alongside its private key.
func buildIdentityJSON(name, controllerURL, cert, caCert string) []byte {
	// Extract base controller URL (strip path)
	ctrlBase := controllerURL
	if idx := strings.Index(ctrlBase, "/edge/"); idx > 0 {
		ctrlBase = ctrlBase[:idx]
	}

	doc := map[string]interface{}{
		"ztAPI": ctrlBase + "/edge/client/v1",
		"id": map[string]interface{}{
			"cert": cert,
			"ca":   caCert,
			// key field intentionally absent — agent holds the private key locally
			// agent sets: "key": "path/to/private.key"
		},
		"configTypes": []string{"all"},
	}
	b, _ := json.MarshalIndent(doc, "", "  ")
	return b
}

// extractCACertFromChain pulls the last certificate from a PEM chain.
// In Ziti's enrollment response, the cert field contains:
//   client cert → intermediate CA → (root CA)
// The last cert in the chain is what the tunnel needs in the ca field
// to verify the controller's TLS certificate.
func extractCACertFromChain(certChain string) string {
	var lastBlock []byte
	rest := []byte(certChain)
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		lastBlock = pem.EncodeToMemory(block)
	}
	return string(lastBlock)
}

// patchZtAPI updates the ztAPI field so the agent can reach the controller
// via the public address before the overlay is established.
func patchZtAPI(identity []byte, publicAPI string) []byte {
	var doc map[string]interface{}
	if json.Unmarshal(identity, &doc) != nil {
		return identity
	}
	doc["ztAPI"] = publicAPI
	b, err := json.Marshal(doc)
	if err != nil {
		return identity
	}
	return b
}
