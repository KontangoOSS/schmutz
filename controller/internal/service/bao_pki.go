package service

import "strings"

// --- PKI extended operations ---

func (s *StoreService) PKIListRoles(mount string) ([]string, error) {
	secret, err := s.client.Logical().List(mount + "/roles")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return extractKeys(secret.Data), nil
}

func (s *StoreService) PKIReadRole(mount, name string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read(mount + "/roles/" + name)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) PKICreateRole(mount, name string, data map[string]interface{}) error {
	_, err := s.client.Logical().Write(mount+"/roles/"+name, data)
	return err
}

func (s *StoreService) PKIDeleteRole(mount, name string) error {
	_, err := s.client.Logical().Delete(mount + "/roles/" + name)
	return err
}

func (s *StoreService) PKIReadCert(mount, serial string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read(mount + "/cert/" + serial)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) PKIReadCA(mount string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read(mount + "/cert/ca")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) PKIReadCRL(mount string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read(mount + "/cert/crl")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) PKIGenerateRoot(mount, commonName, issuerName string, ttl string, keyType string) (map[string]interface{}, error) {
	data := map[string]interface{}{
		"common_name": commonName,
		"ttl":         ttl,
	}
	if issuerName != "" {
		data["issuer_name"] = issuerName
	}
	if keyType != "" {
		data["key_type"] = keyType
	}
	secret, err := s.client.Logical().Write(mount+"/root/generate/internal", data)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) PKIGenerateIntermediate(mount, commonName string, keyType string) (map[string]interface{}, error) {
	data := map[string]interface{}{
		"common_name": commonName,
	}
	if keyType != "" {
		data["key_type"] = keyType
	}
	secret, err := s.client.Logical().Write(mount+"/intermediate/generate/internal", data)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) PKISignIntermediate(mount, csr, commonName, ttl string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Write(mount+"/root/sign-intermediate", map[string]interface{}{
		"csr":         csr,
		"common_name": commonName,
		"ttl":         ttl,
	})
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) PKISetSignedIntermediate(mount, certificate string) error {
	_, err := s.client.Logical().Write(mount+"/intermediate/set-signed", map[string]interface{}{
		"certificate": certificate,
	})
	return err
}

func (s *StoreService) PKISign(mount, role, csr, commonName, ttl string, altNames []string) (map[string]interface{}, error) {
	data := map[string]interface{}{
		"csr":         csr,
		"common_name": commonName,
		"ttl":         ttl,
	}
	if len(altNames) > 0 {
		data["alt_names"] = strings.Join(altNames, ",")
	}
	secret, err := s.client.Logical().Write(mount+"/sign/"+role, data)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) PKITidy(mount string, tidyCertStore, tidyRevokedCerts bool, safetyBuffer string) error {
	_, err := s.client.Logical().Write(mount+"/tidy", map[string]interface{}{
		"tidy_cert_store":    tidyCertStore,
		"tidy_revoked_certs": tidyRevokedCerts,
		"safety_buffer":      safetyBuffer,
	})
	return err
}

func (s *StoreService) PKIConfigURLs(mount string, issuingCertificates, crlDistributionPoints, ocspServers []string) error {
	_, err := s.client.Logical().Write(mount+"/config/urls", map[string]interface{}{
		"issuing_certificates":    strings.Join(issuingCertificates, ","),
		"crl_distribution_points": strings.Join(crlDistributionPoints, ","),
		"ocsp_servers":            strings.Join(ocspServers, ","),
	})
	return err
}

func (s *StoreService) PKIReadURLs(mount string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read(mount + "/config/urls")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) PKIListIssuers(mount string) ([]string, error) {
	secret, err := s.client.Logical().List(mount + "/issuers")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return extractKeys(secret.Data), nil
}

func (s *StoreService) PKIReadIssuer(mount, issuerRef string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read(mount + "/issuer/" + issuerRef)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) PKIGenerateCRL(mount string) error {
	_, err := s.client.Logical().Read(mount + "/crl/rotate")
	return err
}

// PKIIssue issues a certificate using a PKI role, generating the key+cert internally
func (s *StoreService) PKIIssue(mount, role, commonName, ttl string) (map[string]interface{}, error) {
	data := map[string]interface{}{
		"common_name": commonName,
		"ttl":         ttl,
	}
	secret, err := s.client.Logical().Write(mount+"/issue/"+role, data)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}
