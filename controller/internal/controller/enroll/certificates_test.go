package enroll

import (
	"testing"
)

// TestHasAdminAttribute tests the admin attribute detection
func TestHasAdminAttribute(t *testing.T) {
	tests := []struct {
		name       string
		attributes []string
		expected   bool
	}{
		{"admin present", []string{"admin"}, true},
		{"stage-3 present", []string{"stage-3"}, true},
		{"admin and others", []string{"stage-1", "admin", "user"}, true},
		{"stage-3 and others", []string{"user", "stage-3"}, true},
		{"no admin attrs", []string{"user", "viewer"}, false},
		{"empty attributes", []string{}, false},
		{"nil attributes", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasAdminAttribute(tt.attributes)
			if result != tt.expected {
				t.Errorf("hasAdminAttribute(%v) = %v, want %v", tt.attributes, result, tt.expected)
			}
		})
	}
}

// TestExtractCertData tests certificate data extraction from Bao response
func TestExtractCertData(t *testing.T) {
	tests := []struct {
		name    string
		data    map[string]interface{}
		wantCrt bool
		wantKey bool
	}{
		{
			name: "complete response",
			data: map[string]interface{}{
				"certificate": "-----BEGIN CERTIFICATE-----\nMIIC...",
				"private_key": "-----BEGIN RSA PRIVATE KEY-----\nMIIE...",
				"expiration":  "2027-04-08T01:24:17Z",
			},
			wantCrt: true,
			wantKey: true,
		},
		{
			name: "missing private key",
			data: map[string]interface{}{
				"certificate": "-----BEGIN CERTIFICATE-----\nMIIC...",
			},
			wantCrt: true,
			wantKey: false,
		},
		{
			name:    "empty response",
			data:    map[string]interface{}{},
			wantCrt: false,
			wantKey: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractCertData(tt.data)
			if result == nil {
				t.Fatal("extractCertData returned nil")
			}

			if (result.Certificate != "") != tt.wantCrt {
				t.Errorf("Certificate presence = %v, want %v", result.Certificate != "", tt.wantCrt)
			}
			if (result.PrivateKey != "") != tt.wantKey {
				t.Errorf("PrivateKey presence = %v, want %v", result.PrivateKey != "", tt.wantKey)
			}
		})
	}
}

// TestCertsForResponse tests JSON response formatting
func TestCertsForResponse(t *testing.T) {
	tests := []struct {
		name     string
		certs    map[string]*CertBundle
		expected int // number of layers
	}{
		{
			name: "nil certs",
			certs: nil,
			expected: 0,
		},
		{
			name: "empty certs",
			certs: map[string]*CertBundle{},
			expected: 0,
		},
		{
			name: "base and lab certs",
			certs: map[string]*CertBundle{
				"base": {
					Certificate: "cert-base",
					PrivateKey:  "key-base",
				},
				"lab": {
					Certificate: "cert-lab",
					PrivateKey:  "key-lab",
				},
			},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := certsForResponse(tt.certs)
			if len(result) != tt.expected {
				t.Errorf("certsForResponse length = %d, want %d", len(result), tt.expected)
			}

			// Verify structure of response
			for layer, bundle := range result {
				if bundle == nil {
					t.Errorf("layer %s has nil bundle", layer)
				}
				m, ok := bundle.(map[string]string)
				if !ok {
					t.Errorf("layer %s response is not map[string]string", layer)
				}
				// Verify required fields
				if _, ok := m["certificate"]; !ok {
					t.Errorf("layer %s missing certificate field", layer)
				}
				if _, ok := m["private_key"]; !ok {
					t.Errorf("layer %s missing private_key field", layer)
				}
			}
		})
	}
}

// TestCertBundleJSON tests CertBundle JSON marshaling
func TestCertBundleJSON(t *testing.T) {
	bundle := &CertBundle{
		Certificate: "-----BEGIN CERTIFICATE-----",
		PrivateKey:  "-----BEGIN RSA PRIVATE KEY-----",
		IssuedAt:    "2026-04-08T01:24:17Z",
		ExpiresAt:   "2027-04-08T01:24:17Z",
	}

	if bundle.Certificate == "" {
		t.Error("Certificate not set")
	}
	if bundle.PrivateKey == "" {
		t.Error("PrivateKey not set")
	}
	if bundle.IssuedAt == "" {
		t.Error("IssuedAt not set")
	}
	if bundle.ExpiresAt == "" {
		t.Error("ExpiresAt not set")
	}
}
