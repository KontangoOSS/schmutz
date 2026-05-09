package service

import (
	"fmt"
)

// --- Seal / Unseal / Init ---

func (s *StoreService) Seal() error {
	_, err := s.client.Logical().Write("sys/seal", nil)
	return err
}

func (s *StoreService) Unseal(key string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Write("sys/unseal", map[string]interface{}{"key": key})
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) Init(shares, threshold int) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Write("sys/init", map[string]interface{}{
		"secret_shares":    shares,
		"secret_threshold": threshold,
	})
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) StepDown() error {
	_, err := s.client.Logical().Write("sys/step-down", nil)
	return err
}

func (s *StoreService) KeyStatus() (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read("sys/key-status")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

// --- Mount management ---

func (s *StoreService) ListMounts() (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read("sys/mounts")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) MountSecretEngine(path, engineType, description string, options map[string]interface{}) error {
	data := map[string]interface{}{
		"type":        engineType,
		"description": description,
	}
	if options != nil {
		data["options"] = options
	}
	_, err := s.client.Logical().Write("sys/mounts/"+path, data)
	return err
}

func (s *StoreService) UnmountSecretEngine(path string) error {
	_, err := s.client.Logical().Delete("sys/mounts/" + path)
	return err
}

func (s *StoreService) TuneMount(path string, config map[string]interface{}) error {
	_, err := s.client.Logical().Write("sys/mounts/"+path+"/tune", config)
	return err
}

func (s *StoreService) ReadMountConfig(path string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read("sys/mounts/" + path + "/tune")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

// --- Auth method management ---

func (s *StoreService) ListAuthMethods() (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read("sys/auth")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) EnableAuthMethod(path, methodType, description string) error {
	_, err := s.client.Logical().Write("sys/auth/"+path, map[string]interface{}{
		"type":        methodType,
		"description": description,
	})
	return err
}

func (s *StoreService) DisableAuthMethod(path string) error {
	_, err := s.client.Logical().Delete("sys/auth/" + path)
	return err
}

func (s *StoreService) TuneAuthMethod(path string, config map[string]interface{}) error {
	_, err := s.client.Logical().Write("sys/auth/"+path+"/tune", config)
	return err
}

// --- Audit ---

func (s *StoreService) ListAuditDevices() (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read("sys/audit")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) EnableAuditDevice(path, deviceType string, options map[string]interface{}) error {
	data := map[string]interface{}{
		"type":    deviceType,
		"options": options,
	}
	_, err := s.client.Logical().Write("sys/audit/"+path, data)
	return err
}

func (s *StoreService) DisableAuditDevice(path string) error {
	_, err := s.client.Logical().Delete("sys/audit/" + path)
	return err
}

// --- Leases ---

func (s *StoreService) RenewLease(leaseID string, increment int) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Write("sys/leases/renew", map[string]interface{}{
		"lease_id":  leaseID,
		"increment": increment,
	})
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) RevokeLease(leaseID string) error {
	_, err := s.client.Logical().Write("sys/leases/revoke", map[string]interface{}{
		"lease_id": leaseID,
	})
	return err
}

func (s *StoreService) RevokeLeasePrefix(prefix string) error {
	_, err := s.client.Logical().Write("sys/leases/revoke-prefix/"+prefix, nil)
	return err
}

func (s *StoreService) LookupLease(leaseID string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Write("sys/leases/lookup", map[string]interface{}{
		"lease_id": leaseID,
	})
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) ListLeases(prefix string) ([]string, error) {
	secret, err := s.client.Logical().List("sys/leases/lookup/" + prefix)
	if err != nil {
		return nil, err
	}
	return extractKeys(secret.Data), nil
}

// --- Capabilities ---

func (s *StoreService) CheckCapabilities(token, path string) ([]string, error) {
	secret, err := s.client.Logical().Write("sys/capabilities", map[string]interface{}{
		"token": token,
		"path":  path,
	})
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	if caps, ok := secret.Data["capabilities"].([]interface{}); ok {
		var result []string
		for _, c := range caps {
			if s, ok := c.(string); ok {
				result = append(result, s)
			}
		}
		return result, nil
	}
	return nil, nil
}

func (s *StoreService) CheckCapabilitiesSelf(path string) ([]string, error) {
	secret, err := s.client.Logical().Write("sys/capabilities-self", map[string]interface{}{
		"path": path,
	})
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	if caps, ok := secret.Data["capabilities"].([]interface{}); ok {
		var result []string
		for _, c := range caps {
			if s, ok := c.(string); ok {
				result = append(result, s)
			}
		}
		return result, nil
	}
	return nil, nil
}

// --- Plugins ---

func (s *StoreService) ListPlugins() (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read("sys/plugins/catalog")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) ReloadPlugin(plugin string) error {
	_, err := s.client.Logical().Write("sys/plugins/reload/backend", map[string]interface{}{
		"plugin": plugin,
	})
	return err
}

// --- Metrics ---

func (s *StoreService) SysMetrics(format string) (map[string]interface{}, error) {
	p := "sys/metrics"
	if format != "" {
		p = fmt.Sprintf("sys/metrics?format=%s", format)
	}
	secret, err := s.client.Logical().Read(p)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

// --- Wrapping ---

func (s *StoreService) Wrap(data map[string]interface{}, ttl string) (string, error) {
	s.client.SetWrappingLookupFunc(func(_, _ string) string { return ttl })
	defer s.client.SetWrappingLookupFunc(nil)
	secret, err := s.client.Logical().Write("sys/wrapping/wrap", data)
	if err != nil {
		return "", err
	}
	if secret == nil {
		return "", nil
	}
	return secret.WrapInfo.Token, nil
}

func (s *StoreService) Unwrap(token string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Write("sys/wrapping/unwrap", map[string]interface{}{
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

func (s *StoreService) WrapLookup(token string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Write("sys/wrapping/lookup", map[string]interface{}{
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

// --- Tools ---

func (s *StoreService) ToolsRandom(bytes int, format string) (string, error) {
	data := map[string]interface{}{"bytes": bytes}
	if format != "" {
		data["format"] = format
	}
	secret, err := s.client.Logical().Write("sys/tools/random", data)
	if err != nil {
		return "", err
	}
	if secret == nil {
		return "", nil
	}
	if v, ok := secret.Data["random_bytes"].(string); ok {
		return v, nil
	}
	return "", nil
}

func (s *StoreService) ToolsHash(input, algorithm, format string) (string, error) {
	data := map[string]interface{}{"input": input}
	if algorithm != "" {
		data["algorithm"] = algorithm
	}
	if format != "" {
		data["format"] = format
	}
	secret, err := s.client.Logical().Write("sys/tools/hash", data)
	if err != nil {
		return "", err
	}
	if secret == nil {
		return "", nil
	}
	if v, ok := secret.Data["sum"].(string); ok {
		return v, nil
	}
	return "", nil
}
