package service

// --- Token extended operations ---

func (s *StoreService) CreateOrphanToken(policies []string, ttl string, metadata map[string]string) (string, error) {
	data := map[string]interface{}{
		"policies":  policies,
		"ttl":       ttl,
		"no_parent": true,
	}
	if metadata != nil {
		data["metadata"] = metadata
	}
	secret, err := s.client.Logical().Write("auth/token/create-orphan", data)
	if err != nil {
		return "", err
	}
	if secret == nil || secret.Auth == nil {
		return "", nil
	}
	return secret.Auth.ClientToken, nil
}

func (s *StoreService) RenewTokenSelf(increment string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Write("auth/token/renew-self", map[string]interface{}{
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

func (s *StoreService) LookupTokenSelf() (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read("auth/token/lookup-self")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) RevokeTokenSelf() error {
	_, err := s.client.Logical().Write("auth/token/revoke-self", nil)
	return err
}

func (s *StoreService) LookupTokenAccessor(accessor string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Write("auth/token/lookup-accessor", map[string]interface{}{
		"accessor": accessor,
	})
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) RevokeTokenAccessor(accessor string) error {
	_, err := s.client.Logical().Write("auth/token/revoke-accessor", map[string]interface{}{
		"accessor": accessor,
	})
	return err
}

func (s *StoreService) RenewToken(token, increment string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Write("auth/token/renew", map[string]interface{}{
		"token":     token,
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

func (s *StoreService) TidyTokens() error {
	_, err := s.client.Logical().Write("auth/token/tidy", nil)
	return err
}

func (s *StoreService) ListTokenAccessors() ([]string, error) {
	secret, err := s.client.Logical().List("auth/token/accessors")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return extractKeys(secret.Data), nil
}

// --- AppRole extended ---

func (s *StoreService) ReadAppRole(name string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read("auth/approle/role/" + name)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) UpdateAppRole(name string, data map[string]interface{}) error {
	_, err := s.client.Logical().Write("auth/approle/role/"+name, data)
	return err
}

func (s *StoreService) ListAppRoleSecretIDs(name string) ([]string, error) {
	secret, err := s.client.Logical().List("auth/approle/role/" + name + "/secret-id")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return extractKeys(secret.Data), nil
}

func (s *StoreService) LookupAppRoleSecretID(name, secretID string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Write("auth/approle/role/"+name+"/secret-id/lookup", map[string]interface{}{
		"secret_id": secretID,
	})
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) DestroyAppRoleSecretID(name, secretID string) error {
	_, err := s.client.Logical().Write("auth/approle/role/"+name+"/secret-id/destroy", map[string]interface{}{
		"secret_id": secretID,
	})
	return err
}

// --- Userpass auth ---

func (s *StoreService) UserpassCreateUser(username, password string, policies []string) error {
	_, err := s.client.Logical().Write("auth/userpass/users/"+username, map[string]interface{}{
		"password":       password,
		"token_policies": policies,
	})
	return err
}

func (s *StoreService) UserpassReadUser(username string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read("auth/userpass/users/" + username)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) UserpassDeleteUser(username string) error {
	_, err := s.client.Logical().Delete("auth/userpass/users/" + username)
	return err
}

func (s *StoreService) UserpassListUsers() ([]string, error) {
	secret, err := s.client.Logical().List("auth/userpass/users")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return extractKeys(secret.Data), nil
}

func (s *StoreService) UserpassUpdatePassword(username, password string) error {
	_, err := s.client.Logical().Write("auth/userpass/users/"+username+"/password", map[string]interface{}{
		"password": password,
	})
	return err
}

func (s *StoreService) UserpassLogin(username, password string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Write("auth/userpass/login/"+username, map[string]interface{}{
		"password": password,
	})
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	result := map[string]interface{}{}
	if secret.Auth != nil {
		result["client_token"] = secret.Auth.ClientToken
		result["policies"] = secret.Auth.Policies
		result["metadata"] = secret.Auth.Metadata
	}
	return result, nil
}

// --- TOTP ---

func (s *StoreService) TOTPCreateKey(mount, name string, data map[string]interface{}) error {
	_, err := s.client.Logical().Write(mount+"/keys/"+name, data)
	return err
}

func (s *StoreService) TOTPReadKey(mount, name string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read(mount + "/keys/" + name)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) TOTPDeleteKey(mount, name string) error {
	_, err := s.client.Logical().Delete(mount + "/keys/" + name)
	return err
}

func (s *StoreService) TOTPListKeys(mount string) ([]string, error) {
	secret, err := s.client.Logical().List(mount + "/keys")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return extractKeys(secret.Data), nil
}

func (s *StoreService) TOTPGenerateCode(mount, name string) (string, error) {
	secret, err := s.client.Logical().Read(mount + "/code/" + name)
	if err != nil {
		return "", err
	}
	if secret == nil {
		return "", nil
	}
	if v, ok := secret.Data["code"].(string); ok {
		return v, nil
	}
	return "", nil
}

func (s *StoreService) TOTPValidateCode(mount, name, code string) (bool, error) {
	secret, err := s.client.Logical().Write(mount+"/code/"+name, map[string]interface{}{
		"code": code,
	})
	if err != nil {
		return false, err
	}
	if secret == nil {
		return false, nil
	}
	if v, ok := secret.Data["valid"].(bool); ok {
		return v, nil
	}
	return false, nil
}

// --- SSH ---

func (s *StoreService) SSHSignKey(mount, role, publicKey string, certType string, validPrincipals string, ttl string) (string, error) {
	data := map[string]interface{}{
		"public_key": publicKey,
	}
	if certType != "" {
		data["cert_type"] = certType
	}
	if validPrincipals != "" {
		data["valid_principals"] = validPrincipals
	}
	if ttl != "" {
		data["ttl"] = ttl
	}
	secret, err := s.client.Logical().Write(mount+"/sign/"+role, data)
	if err != nil {
		return "", err
	}
	if secret == nil {
		return "", nil
	}
	if v, ok := secret.Data["signed_key"].(string); ok {
		return v, nil
	}
	return "", nil
}

func (s *StoreService) SSHListRoles(mount string) ([]string, error) {
	secret, err := s.client.Logical().List(mount + "/roles")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return extractKeys(secret.Data), nil
}

func (s *StoreService) SSHCreateRole(mount, name string, data map[string]interface{}) error {
	_, err := s.client.Logical().Write(mount+"/roles/"+name, data)
	return err
}

func (s *StoreService) SSHReadRole(mount, name string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read(mount + "/roles/" + name)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) SSHDeleteRole(mount, name string) error {
	_, err := s.client.Logical().Delete(mount + "/roles/" + name)
	return err
}

func (s *StoreService) SSHConfigCA(mount string, privateKey, publicKey string) error {
	data := map[string]interface{}{}
	if privateKey != "" {
		data["private_key"] = privateKey
	}
	if publicKey != "" {
		data["public_key"] = publicKey
	}
	_, err := s.client.Logical().Write(mount+"/config/ca", data)
	return err
}

func (s *StoreService) SSHReadCA(mount string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read(mount + "/config/ca")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}
