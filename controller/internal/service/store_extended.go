package service

import (
	"fmt"
	"path"
)

// --- System operations ---

// Health returns Bao health status.
func (s *StoreService) Health() (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read("sys/health")
	if err != nil {
		// Health endpoint returns non-200 for sealed/standby — parse anyway
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

// SealStatus returns the seal status.
func (s *StoreService) SealStatus() (map[string]interface{}, error) {
	resp, err := s.client.Sys().SealStatus()
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"sealed":       resp.Sealed,
		"initialized":  resp.Initialized,
		"cluster_name": resp.ClusterName,
		"version":      resp.Version,
		"n":            resp.N,
		"t":            resp.T,
	}, nil
}

// Leader returns the current leader status.
func (s *StoreService) Leader() (map[string]interface{}, error) {
	resp, err := s.client.Sys().Leader()
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"is_self":        resp.IsSelf,
		"leader_address": resp.LeaderAddress,
	}, nil
}

// --- Policy management ---

// CreatePolicy creates a Bao ACL policy.
func (s *StoreService) CreatePolicy(name, rules string) error {
	return s.client.Sys().PutPolicy(name, rules)
}

// GetPolicy reads a Bao ACL policy.
func (s *StoreService) GetPolicy(name string) (string, error) {
	return s.client.Sys().GetPolicy(name)
}

// DeletePolicy removes a Bao ACL policy.
func (s *StoreService) DeletePolicy(name string) error {
	return s.client.Sys().DeletePolicy(name)
}

// ListPolicies lists all Bao ACL policies.
func (s *StoreService) ListPolicies() ([]string, error) {
	return s.client.Sys().ListPolicies()
}

// --- Token management ---

// CreateToken creates a new Bao token with given policies and TTL.
func (s *StoreService) CreateToken(policies []string, ttl string, metadata map[string]string) (string, error) {
	opts := &tokenCreateRequest{
		Policies: policies,
		TTL:      ttl,
		Metadata: metadata,
	}
	secret, err := s.client.Logical().Write("auth/token/create", map[string]interface{}{
		"policies": opts.Policies,
		"ttl":      opts.TTL,
		"metadata": opts.Metadata,
	})
	if err != nil {
		return "", err
	}
	if secret == nil || secret.Auth == nil {
		return "", fmt.Errorf("no token in response")
	}
	return secret.Auth.ClientToken, nil
}

type tokenCreateRequest struct {
	Policies []string
	TTL      string
	Metadata map[string]string
}

// AppRoleLogin authenticates with role_id + secret_id and returns entity info.
func (s *StoreService) AppRoleLogin(roleID, secretID string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Write("auth/approle/login", map[string]interface{}{
		"role_id":   roleID,
		"secret_id": secretID,
	})
	if err != nil {
		return nil, fmt.Errorf("approle login: %w", err)
	}
	if secret == nil || secret.Auth == nil {
		return nil, fmt.Errorf("approle login: no auth response")
	}

	result := map[string]interface{}{
		"entity_id": secret.Auth.EntityID,
		"token":     secret.Auth.ClientToken,
		"policies":  secret.Auth.TokenPolicies,
	}
	// Include metadata from the entity
	if secret.Auth.Metadata != nil {
		for k, v := range secret.Auth.Metadata {
			result[k] = v
		}
	}
	return result, nil
}

// RevokeToken revokes a token.
func (s *StoreService) RevokeToken(token string) error {
	_, err := s.client.Logical().Write("auth/token/revoke", map[string]interface{}{
		"token": token,
	})
	return err
}

// LookupToken returns info about a token.
func (s *StoreService) LookupToken(token string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Write("auth/token/lookup", map[string]interface{}{
		"token": token,
	})
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

// --- AppRole auth ---

// CreateAppRole creates an AppRole with policies.
func (s *StoreService) CreateAppRole(name string, policies []string, tokenTTL string) error {
	_, err := s.client.Logical().Write("auth/approle/role/"+name, map[string]interface{}{
		"policies":  policies,
		"token_ttl": tokenTTL,
	})
	return err
}

// GetAppRoleID returns the role-id for an AppRole.
func (s *StoreService) GetAppRoleID(name string) (string, error) {
	secret, err := s.client.Logical().Read("auth/approle/role/" + name + "/role-id")
	if err != nil {
		return "", err
	}
	if secret == nil || secret.Data == nil {
		return "", nil
	}
	roleID, _ := secret.Data["role_id"].(string)
	return roleID, nil
}

// CreateAppRoleSecretID generates a new secret-id for an AppRole.
func (s *StoreService) CreateAppRoleSecretID(name string, metadata map[string]string) (string, error) {
	data := map[string]interface{}{}
	if len(metadata) > 0 {
		data["metadata"] = metadata
	}
	secret, err := s.client.Logical().Write("auth/approle/role/"+name+"/secret-id", data)
	if err != nil {
		return "", err
	}
	if secret == nil || secret.Data == nil {
		return "", nil
	}
	secretID, _ := secret.Data["secret_id"].(string)
	return secretID, nil
}

// DeleteAppRole deletes an AppRole.
func (s *StoreService) DeleteAppRole(name string) error {
	_, err := s.client.Logical().Delete("auth/approle/role/" + name)
	return err
}

// ListAppRoles lists all AppRoles.
func (s *StoreService) ListAppRoles() ([]string, error) {
	secret, err := s.client.Logical().List("auth/approle/role")
	if err != nil {
		return nil, err
	}
	if secret == nil || secret.Data == nil {
		return nil, nil
	}
	return extractKeys(secret.Data), nil
}

// --- PKI ---

// IssueCert requests a certificate from the PKI engine.
func (s *StoreService) IssueCert(mount, role, commonName string, ttl string, altNames []string) (map[string]interface{}, error) {
	data := map[string]interface{}{
		"common_name": commonName,
		"ttl":         ttl,
	}
	if len(altNames) > 0 {
		data["alt_names"] = joinStrings(altNames, ",")
	}
	secret, err := s.client.Logical().Write(path.Join(mount, "issue", role), data)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

// ListCerts lists certificate serial numbers from the PKI engine.
func (s *StoreService) ListCerts(mount string) ([]string, error) {
	secret, err := s.client.Logical().List(path.Join(mount, "certs"))
	if err != nil {
		return nil, err
	}
	if secret == nil || secret.Data == nil {
		return nil, nil
	}
	return extractKeys(secret.Data), nil
}

// RevokeCert revokes a certificate by serial number.
func (s *StoreService) RevokeCert(mount, serial string) error {
	_, err := s.client.Logical().Write(path.Join(mount, "revoke"), map[string]interface{}{
		"serial_number": serial,
	})
	return err
}

// --- OIDC auth config ---

// GetOIDCConfig reads the OIDC auth method configuration.
func (s *StoreService) GetOIDCConfig() (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read("auth/oidc/config")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

// --- Helpers ---

func extractKeys(data map[string]interface{}) []string {
	keysRaw, _ := data["keys"].([]interface{})
	keys := make([]string, len(keysRaw))
	for i, k := range keysRaw {
		keys[i], _ = k.(string)
	}
	return keys
}

func joinStrings(strs []string, sep string) string {
	result := ""
	for i, s := range strs {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}
