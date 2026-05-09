package service

// --- Rekey / Rotate ---

func (s *StoreService) RekeyInit(shares, threshold int) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Write("sys/rekey/init", map[string]interface{}{
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

func (s *StoreService) RekeyUpdate(key, nonce string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Write("sys/rekey/update", map[string]interface{}{
		"key":   key,
		"nonce": nonce,
	})
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) RekeyCancel() error {
	_, err := s.client.Logical().Delete("sys/rekey/init")
	return err
}

func (s *StoreService) RekeyStatus() (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read("sys/rekey/init")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) RotateEncryptionKey() error {
	_, err := s.client.Logical().Write("sys/rotate", nil)
	return err
}

// --- Cubbyhole ---

func (s *StoreService) CubbyRead(path string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read("cubbyhole/" + path)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) CubbyWrite(path string, data map[string]interface{}) error {
	_, err := s.client.Logical().Write("cubbyhole/"+path, data)
	return err
}

func (s *StoreService) CubbyDelete(path string) error {
	_, err := s.client.Logical().Delete("cubbyhole/" + path)
	return err
}

func (s *StoreService) CubbyList(path string) ([]string, error) {
	secret, err := s.client.Logical().List("cubbyhole/" + path)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return extractKeys(secret.Data), nil
}

// --- Database secrets engine ---

func (s *StoreService) DatabaseConfigConnection(mount, name string, data map[string]interface{}) error {
	_, err := s.client.Logical().Write(mount+"/config/"+name, data)
	return err
}

func (s *StoreService) DatabaseReadConnection(mount, name string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read(mount + "/config/" + name)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) DatabaseDeleteConnection(mount, name string) error {
	_, err := s.client.Logical().Delete(mount + "/config/" + name)
	return err
}

func (s *StoreService) DatabaseListConnections(mount string) ([]string, error) {
	secret, err := s.client.Logical().List(mount + "/config")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return extractKeys(secret.Data), nil
}

func (s *StoreService) DatabaseCreateRole(mount, name string, data map[string]interface{}) error {
	_, err := s.client.Logical().Write(mount+"/roles/"+name, data)
	return err
}

func (s *StoreService) DatabaseReadRole(mount, name string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read(mount + "/roles/" + name)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) DatabaseDeleteRole(mount, name string) error {
	_, err := s.client.Logical().Delete(mount + "/roles/" + name)
	return err
}

func (s *StoreService) DatabaseListRoles(mount string) ([]string, error) {
	secret, err := s.client.Logical().List(mount + "/roles")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return extractKeys(secret.Data), nil
}

func (s *StoreService) DatabaseCreateStaticRole(mount, name string, data map[string]interface{}) error {
	_, err := s.client.Logical().Write(mount+"/static-roles/"+name, data)
	return err
}

func (s *StoreService) DatabaseReadStaticRole(mount, name string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read(mount + "/static-roles/" + name)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) DatabaseDeleteStaticRole(mount, name string) error {
	_, err := s.client.Logical().Delete(mount + "/static-roles/" + name)
	return err
}

func (s *StoreService) DatabaseListStaticRoles(mount string) ([]string, error) {
	secret, err := s.client.Logical().List(mount + "/static-roles")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return extractKeys(secret.Data), nil
}

func (s *StoreService) DatabaseGenerateCredentials(mount, role string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read(mount + "/creds/" + role)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) DatabaseGetStaticCredentials(mount, role string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read(mount + "/static-creds/" + role)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) DatabaseRotateRootCredentials(mount, name string) error {
	_, err := s.client.Logical().Write(mount+"/rotate-root/"+name, nil)
	return err
}

func (s *StoreService) DatabaseRotateStaticRole(mount, name string) error {
	_, err := s.client.Logical().Write(mount+"/rotate-role/"+name, nil)
	return err
}

// --- LDAP auth ---

func (s *StoreService) LDAPConfigWrite(data map[string]interface{}) error {
	_, err := s.client.Logical().Write("auth/ldap/config", data)
	return err
}

func (s *StoreService) LDAPConfigRead() (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read("auth/ldap/config")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) LDAPCreateGroup(name string, policies []string) error {
	_, err := s.client.Logical().Write("auth/ldap/groups/"+name, map[string]interface{}{
		"policies": policies,
	})
	return err
}

func (s *StoreService) LDAPReadGroup(name string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read("auth/ldap/groups/" + name)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) LDAPDeleteGroup(name string) error {
	_, err := s.client.Logical().Delete("auth/ldap/groups/" + name)
	return err
}

func (s *StoreService) LDAPListGroups() ([]string, error) {
	secret, err := s.client.Logical().List("auth/ldap/groups")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return extractKeys(secret.Data), nil
}

func (s *StoreService) LDAPCreateUser(name string, policies, groups []string) error {
	_, err := s.client.Logical().Write("auth/ldap/users/"+name, map[string]interface{}{
		"policies": policies,
		"groups":   groups,
	})
	return err
}

func (s *StoreService) LDAPDeleteUser(name string) error {
	_, err := s.client.Logical().Delete("auth/ldap/users/" + name)
	return err
}

func (s *StoreService) LDAPListUsers() ([]string, error) {
	secret, err := s.client.Logical().List("auth/ldap/users")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return extractKeys(secret.Data), nil
}

func (s *StoreService) LDAPLogin(username, password string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Write("auth/ldap/login/"+username, map[string]interface{}{
		"password": password,
	})
	if err != nil {
		return nil, err
	}
	if secret == nil || secret.Auth == nil {
		return nil, nil
	}
	return map[string]interface{}{
		"client_token": secret.Auth.ClientToken,
		"policies":     secret.Auth.Policies,
		"metadata":     secret.Auth.Metadata,
	}, nil
}

// --- TLS Certificate auth ---

func (s *StoreService) CertAuthCreateRole(name string, data map[string]interface{}) error {
	_, err := s.client.Logical().Write("auth/cert/certs/"+name, data)
	return err
}

func (s *StoreService) CertAuthReadRole(name string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read("auth/cert/certs/" + name)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) CertAuthDeleteRole(name string) error {
	_, err := s.client.Logical().Delete("auth/cert/certs/" + name)
	return err
}

func (s *StoreService) CertAuthListRoles() ([]string, error) {
	secret, err := s.client.Logical().List("auth/cert/certs")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return extractKeys(secret.Data), nil
}

// --- Kubernetes auth ---

func (s *StoreService) K8sAuthConfigWrite(data map[string]interface{}) error {
	_, err := s.client.Logical().Write("auth/kubernetes/config", data)
	return err
}

func (s *StoreService) K8sAuthConfigRead() (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read("auth/kubernetes/config")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) K8sAuthCreateRole(name string, data map[string]interface{}) error {
	_, err := s.client.Logical().Write("auth/kubernetes/role/"+name, data)
	return err
}

func (s *StoreService) K8sAuthReadRole(name string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read("auth/kubernetes/role/" + name)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) K8sAuthDeleteRole(name string) error {
	_, err := s.client.Logical().Delete("auth/kubernetes/role/" + name)
	return err
}

func (s *StoreService) K8sAuthListRoles() ([]string, error) {
	secret, err := s.client.Logical().List("auth/kubernetes/role")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return extractKeys(secret.Data), nil
}

func (s *StoreService) K8sAuthLogin(role, jwt string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Write("auth/kubernetes/login", map[string]interface{}{
		"role": role,
		"jwt":  jwt,
	})
	if err != nil {
		return nil, err
	}
	if secret == nil || secret.Auth == nil {
		return nil, nil
	}
	return map[string]interface{}{
		"client_token": secret.Auth.ClientToken,
		"policies":     secret.Auth.Policies,
		"metadata":     secret.Auth.Metadata,
	}, nil
}

// --- JWT/OIDC auth method (roles) ---

func (s *StoreService) JWTAuthCreateRole(name string, data map[string]interface{}) error {
	_, err := s.client.Logical().Write("auth/jwt/role/"+name, data)
	return err
}

func (s *StoreService) JWTAuthReadRole(name string) (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read("auth/jwt/role/" + name)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (s *StoreService) JWTAuthDeleteRole(name string) error {
	_, err := s.client.Logical().Delete("auth/jwt/role/" + name)
	return err
}

func (s *StoreService) JWTAuthListRoles() ([]string, error) {
	secret, err := s.client.Logical().List("auth/jwt/role")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return extractKeys(secret.Data), nil
}

func (s *StoreService) JWTAuthConfigWrite(data map[string]interface{}) error {
	_, err := s.client.Logical().Write("auth/jwt/config", data)
	return err
}

func (s *StoreService) JWTAuthConfigRead() (map[string]interface{}, error) {
	secret, err := s.client.Logical().Read("auth/jwt/config")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}
