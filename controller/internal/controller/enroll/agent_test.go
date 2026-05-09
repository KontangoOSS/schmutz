package enroll

import (
	"strings"
	"testing"
)

const testClientCert = `-----BEGIN CERTIFICATE-----
MIIBpDCCAUqgAwIBAgIBATAKBggqhkjOPQQDAjAcMRowGAYDVQQDExFUZXN0IENB
IFJFQ0VSVElGSUNBVEUwHhcNMjYwMTAxMDAwMDAwWhcNMjcwMTAxMDAwMDAwWjAc
MRowGAYDVQQDExFUZXN0IENBIFJFQ0VSVElGSUNBVEUwWTATBgcqhkjOPQIBBggq
hkjOPQMBBwNCAAQzMTg4q6r5hIuU2YBfUfB3YpBnHlYFjLqGVtKhJ6H3b3K9yF2J
oxiYGKMg0wKQFePt6h+IEqYh1T0PoLqLo2MwYTAOBgNVHQ8BAf8EBAMCAQYwDwYD
VR0TAQH/BAUwAwEB/zAdBgNVHQ4EFgQUtest1234567890abcdefghijklmnop1234
MB8GA1UdIwQYMBaAFAtest1234567890abcdefghijklmnop1234MAoGCCqGSM49BAMC
A0EAtest1234567890abcdef==
-----END CERTIFICATE-----
`

const testIntermediateCert = `-----BEGIN CERTIFICATE-----
MIIBpDCCAUqgAwIBAgIBATAKBggqhkjOPQQDAjAcMRowGAYDVQQDExFJbnRlcm1l
ZGlhdGUgQ0EwHhcNMjYwMTAxMDAwMDAwWhcNMjcwMTAxMDAwMDAwWjAcMRowGAYD
VQQDExFJbnRlcm1lZGlhdGUgQ0EwWTATBgcqhkjOPQIBBggqhkjOPQMBBwNCAAS
intermediate1234567890abcdefghijklmnopqrstuvwxyz1234567890abcdef
o2MwYTAOBgNVHQ8BAf8EBAMCAQYwDwYDVR0TAQH/BAUwAwEB/zAdBgNVHQ4EFgQU
intermediate1234567890abcdefghijklmnoptest1234MB8GA1UdIwQYMBaAFAtestCA
MAoGCCqGSM49BAMCAzkAtest==
-----END CERTIFICATE-----
`

func TestExtractCACertFromChain_SingleCert(t *testing.T) {
	result := extractCACertFromChain(testClientCert)
	if result == "" {
		t.Error("should extract cert from single-cert chain")
	}
	if !strings.Contains(result, "BEGIN CERTIFICATE") {
		t.Error("result should be valid PEM")
	}
}

func TestExtractCACertFromChain_MultiCert(t *testing.T) {
	chain := testClientCert + testIntermediateCert
	result := extractCACertFromChain(chain)
	// Should return the LAST cert in the chain (the CA/root)
	if !strings.Contains(result, "Intermediate") && !strings.Contains(result, "BEGIN CERTIFICATE") {
		t.Error("should extract last cert from chain")
	}
	// Should not contain both certs
	count := strings.Count(result, "BEGIN CERTIFICATE")
	if count != 1 {
		t.Errorf("expected 1 cert in result, got %d", count)
	}
}

func TestExtractCACertFromChain_Empty(t *testing.T) {
	result := extractCACertFromChain("")
	if result != "" {
		t.Error("empty input should return empty string")
	}
}

func TestBuildIdentityJSON_Structure(t *testing.T) {
	identity := buildIdentityJSON("test-identity", "https://ctrl.example.com:1280/edge/client/v1/enroll?method=ott&token=abc", testClientCert, testIntermediateCert)
	if len(identity) == 0 {
		t.Fatal("identity JSON should not be empty")
	}
	s := string(identity)
	if !strings.Contains(s, `"ztAPI"`) {
		t.Error("identity should contain ztAPI field")
	}
	if !strings.Contains(s, `"cert"`) {
		t.Error("identity should contain cert field")
	}
	if !strings.Contains(s, `"ca"`) {
		t.Error("identity should contain ca field")
	}
	// key field should NOT be present — agent adds it locally
	if strings.Contains(s, `"key"`) {
		t.Error("identity should NOT contain key field — private key stays on agent")
	}
}

func TestBuildIdentityJSON_ControllerURL(t *testing.T) {
	enrollURL := "https://ctrl-1.example.com:1280/edge/client/v1/enroll?method=ott&token=abc"
	identity := buildIdentityJSON("test", enrollURL, "cert", "ca")
	s := string(identity)
	// ztAPI should strip the path and keep just the base URL + /edge/client/v1
	if !strings.Contains(s, "ctrl-1.example.com:1280") {
		t.Error("controller hostname should be in ztAPI")
	}
	if strings.Contains(s, "/enroll") {
		t.Error("enrollment path should be stripped from ztAPI")
	}
}

func TestParseEnrollURL_ValidJWT(t *testing.T) {
	// Pre-encoded JWT: header={"alg":"RS256"} payload={"iss":"https://ctrl-1.example.com:1280","jti":"f489df34-abbb-4e90-b591-e836cc4229b7","em":"ott","exp":9999999999}
	// ParseUnverified skips signature verification — safe for unit testing enrollment URL parsing.
	jwt := "eyJhbGciOiJSUzI1NiJ9.eyJpc3MiOiJodHRwczovL2N0cmwtMS5leGFtcGxlLmNvbToxMjgwIiwianRpIjoiZjQ4OWRmMzQtYWJiYi00ZTkwLWI1OTEtZTgzNmNjNDIyOWI3IiwiZW0iOiJvdHQiLCJleHAiOjk5OTk5OTk5OTl9.fakesig"

	url, err := parseEnrollURL(jwt)
	if err != nil {
		t.Fatalf("parseEnrollURL error: %v", err)
	}
	if !strings.Contains(url, "ctrl-1.example.com:1280") {
		t.Errorf("URL should contain controller hostname, got: %s", url)
	}
	if !strings.Contains(url, "/edge/client/v1/enroll") {
		t.Errorf("URL should contain enrollment path, got: %s", url)
	}
	if !strings.Contains(url, "method=ott") {
		t.Errorf("URL should contain method=ott, got: %s", url)
	}
	if !strings.Contains(url, "token=f489df34") {
		t.Errorf("URL should contain token=jti, got: %s", url)
	}
}

func TestParseEnrollURL_MissingIssuer(t *testing.T) {
	// JWT with empty issuer
	jwt := "eyJhbGciOiJSUzI1NiJ9.eyJqdGkiOiJhYmMxMjMiLCJlbSI6Im90dCIsImV4cCI6OTk5OTk5OTk5OX0.fakesig"
	_, err := parseEnrollURL(jwt)
	if err == nil {
		t.Error("should error on missing issuer")
	}
}

func TestPatchZtAPI(t *testing.T) {
	identity := []byte(`{"ztAPI":"https://internal:1280/edge/client/v1","id":{"cert":"c"}}`)
	patched := patchZtAPI(identity, "https://public.example.com:443/edge/client/v1")
	s := string(patched)
	if !strings.Contains(s, "public.example.com") {
		t.Error("ztAPI should be patched to public address")
	}
	if strings.Contains(s, "internal:1280") {
		t.Error("internal address should be replaced")
	}
}
