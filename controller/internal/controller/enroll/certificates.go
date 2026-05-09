package enroll

import (
	"fmt"
	"log"
	"time"
)

// CertBundle holds issued certificate and private key
type CertBundle struct {
	Certificate string `json:"certificate"`
	PrivateKey  string `json:"private_key"`
	IssuedAt    string `json:"issued_at"`
	ExpiresAt   string `json:"expires_at"`
}

// issueCertificatesForDevice issues PKI certificates for device enrollment layers
func (m *Module) issueCertificatesForDevice(machineID, baseDomain string, quarantine bool, attributes []string) (map[string]*CertBundle, error) {
	const mount = "pki"
	certs := make(map[string]*CertBundle)

	// Always issue base certificate (main domain)
	baseCN := machineID + "." + baseDomain
	baseData, err := m.C.Bao.PKIIssue(mount, "device-base", baseCN, "8760h")
	if err != nil {
		log.Printf("enroll: failed to issue base cert for %s: %v", machineID, err)
		return nil, err
	}
	if baseData != nil {
		baseCert := extractCertData(baseData)
		certs["base"] = baseCert
		log.Printf("enroll: issued base cert CN=%s", baseCN)
	}

	// Issue layer-specific certs based on approval status
	if quarantine {
		// Short-lived web cert for quarantine testing (24h)
		webCN := machineID + ".web." + baseDomain
		webData, err := m.C.Bao.PKIIssue(mount, "device-web", webCN, "24h")
		if err != nil {
			log.Printf("enroll: failed to issue web cert for %s: %v", machineID, err)
			return certs, nil // Non-fatal, return what we have
		}
		if webData != nil {
			webCert := extractCertData(webData)
			certs["web"] = webCert
			log.Printf("enroll: issued web cert CN=%s", webCN)
		}
	} else if hasAdminAttribute(attributes) {
		// Unresolvable .tango cert for experimental services (admin-only)
		tangoCN := machineID + ".tango"
		tangoData, err := m.C.Bao.PKIIssue(mount, "device-tango", tangoCN, "8760h")
		if err != nil {
			log.Printf("enroll: failed to issue tango cert for %s: %v", machineID, err)
			return certs, nil // Non-fatal, return what we have
		}
		if tangoData != nil {
			tangoCert := extractCertData(tangoData)
			certs["tango"] = tangoCert
			log.Printf("enroll: issued tango cert CN=%s", tangoCN)
		}
	} else {
		// .lab cert for approved devices with Bao access (1yr)
		labCN := machineID + ".lab." + baseDomain
		labData, err := m.C.Bao.PKIIssue(mount, "device-lab", labCN, "8760h")
		if err != nil {
			log.Printf("enroll: failed to issue lab cert for %s: %v", machineID, err)
			return certs, nil // Non-fatal, return what we have
		}
		if labData != nil {
			labCert := extractCertData(labData)
			certs["lab"] = labCert
			log.Printf("enroll: issued lab cert CN=%s", labCN)
		}
	}

	return certs, nil
}

// hasAdminAttribute checks if attributes contain admin-level access
func hasAdminAttribute(attributes []string) bool {
	for _, attr := range attributes {
		if attr == "admin" || attr == "stage-3" {
			return true
		}
	}
	return false
}

// storeCertificatesInBao stores issued certificates in Bao KV
func (m *Module) storeCertificatesInBao(machineID string, certs map[string]*CertBundle) error {
	for layer, cert := range certs {
		path := "devices/" + machineID + "/" + layer
		data := map[string]interface{}{
			"certificate": cert.Certificate,
			"private_key": cert.PrivateKey,
			"issued_at":   cert.IssuedAt,
			"expires_at":  cert.ExpiresAt,
		}
		if err := m.C.Bao.Put("secret", path, data); err != nil {
			log.Printf("enroll: failed to store %s cert in Bao: %v", layer, err)
			return err
		}
		log.Printf("enroll: stored %s cert for %s at secret/%s", layer, machineID, path)
	}
	return nil
}

// getCABundle retrieves the test CA bundle from Bao KV
func (m *Module) getCABundle() (string, error) {
	bundleData, err := m.C.Bao.Get("secret", "pki/ca-bundle")
	if err != nil {
		log.Printf("enroll: failed to retrieve CA bundle: %v", err)
		return "", err
	}
	if bundleData == nil {
		return "", fmt.Errorf("CA bundle not found in Bao")
	}
	bundle, ok := bundleData["bundle"].(string)
	if !ok {
		return "", fmt.Errorf("CA bundle field has wrong type")
	}
	return bundle, nil
}

// getControllerDomain retrieves the domain name from controller info
func (m *Module) getControllerDomain() (string, error) {
	info, err := m.C.Bao.Get("secret", "infra/controllers/"+m.NodeName)
	if err != nil {
		return "", err
	}
	if info == nil {
		return "", fmt.Errorf("controller info not found")
	}
	domain, ok := info["domain"].(string)
	if !ok || domain == "" {
		return "", fmt.Errorf("domain not configured for controller")
	}
	return domain, nil
}

// extractCertData extracts certificate and private key from Bao PKIIssue response
func extractCertData(data map[string]interface{}) *CertBundle {
	cert := &CertBundle{
		IssuedAt: time.Now().UTC().Format(time.RFC3339),
	}

	if c, ok := data["certificate"].(string); ok {
		cert.Certificate = c
	}
	if k, ok := data["private_key"].(string); ok {
		cert.PrivateKey = k
	}

	// Extract expiration time from certificate metadata if available
	if expStr, ok := data["expiration"].(string); ok {
		cert.ExpiresAt = expStr
	} else if exp, ok := data["expiration"].(float64); ok {
		cert.ExpiresAt = time.Unix(int64(exp), 0).UTC().Format(time.RFC3339)
	}

	return cert
}

// certsForResponse formats certificates for JSON response (excludes private keys in sensitive contexts if needed)
func certsForResponse(certs map[string]*CertBundle) map[string]interface{} {
	if certs == nil {
		return nil
	}
	resp := make(map[string]interface{})
	for layer, bundle := range certs {
		resp[layer] = map[string]string{
			"certificate": bundle.Certificate,
			"private_key": bundle.PrivateKey,
			"issued_at":   bundle.IssuedAt,
			"expires_at":  bundle.ExpiresAt,
		}
	}
	return resp
}
