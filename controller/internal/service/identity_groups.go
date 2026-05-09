package service

// --- Group Aliases (external identity provider mapping) ---

func (i *IdentityService) CreateGroupAlias(groupID, aliasName, mountAccessor string, metadata map[string]string) (string, error) {
	data := map[string]interface{}{
		"canonical_id":   groupID,
		"name":           aliasName,
		"mount_accessor": mountAccessor,
	}
	if metadata != nil {
		data["custom_metadata"] = metadata
	}
	secret, err := i.client.Logical().Write("identity/group-alias", data)
	if err != nil {
		return "", err
	}
	if secret == nil {
		return "", nil
	}
	if id, ok := secret.Data["id"].(string); ok {
		return id, nil
	}
	return "", nil
}

func (i *IdentityService) GetGroupAlias(id string) (map[string]interface{}, error) {
	secret, err := i.client.Logical().Read("identity/group-alias/id/" + id)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (i *IdentityService) UpdateGroupAlias(id string, data map[string]interface{}) error {
	_, err := i.client.Logical().Write("identity/group-alias/id/"+id, data)
	return err
}

func (i *IdentityService) DeleteGroupAlias(id string) error {
	_, err := i.client.Logical().Delete("identity/group-alias/id/" + id)
	return err
}

func (i *IdentityService) ListGroupAliases() ([]string, error) {
	secret, err := i.client.Logical().List("identity/group-alias/id")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	keys, ok := secret.Data["keys"].([]interface{})
	if !ok {
		return nil, nil
	}
	var result []string
	for _, k := range keys {
		if s, ok := k.(string); ok {
			result = append(result, s)
		}
	}
	return result, nil
}

// --- OIDC Provider ---

func (i *IdentityService) OIDCCreateKey(name string, data map[string]interface{}) error {
	_, err := i.client.Logical().Write("identity/oidc/key/"+name, data)
	return err
}

func (i *IdentityService) OIDCReadKey(name string) (map[string]interface{}, error) {
	secret, err := i.client.Logical().Read("identity/oidc/key/" + name)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (i *IdentityService) OIDCDeleteKey(name string) error {
	_, err := i.client.Logical().Delete("identity/oidc/key/" + name)
	return err
}

func (i *IdentityService) OIDCListKeys() ([]string, error) {
	secret, err := i.client.Logical().List("identity/oidc/key")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	keys, ok := secret.Data["keys"].([]interface{})
	if !ok {
		return nil, nil
	}
	var result []string
	for _, k := range keys {
		if s, ok := k.(string); ok {
			result = append(result, s)
		}
	}
	return result, nil
}

func (i *IdentityService) OIDCRotateKey(name string) error {
	_, err := i.client.Logical().Write("identity/oidc/key/"+name+"/rotate", nil)
	return err
}

func (i *IdentityService) OIDCCreateRole(name string, data map[string]interface{}) error {
	_, err := i.client.Logical().Write("identity/oidc/role/"+name, data)
	return err
}

func (i *IdentityService) OIDCReadRole(name string) (map[string]interface{}, error) {
	secret, err := i.client.Logical().Read("identity/oidc/role/" + name)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (i *IdentityService) OIDCDeleteRole(name string) error {
	_, err := i.client.Logical().Delete("identity/oidc/role/" + name)
	return err
}

func (i *IdentityService) OIDCListRoles() ([]string, error) {
	secret, err := i.client.Logical().List("identity/oidc/role")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	keys, ok := secret.Data["keys"].([]interface{})
	if !ok {
		return nil, nil
	}
	var result []string
	for _, k := range keys {
		if s, ok := k.(string); ok {
			result = append(result, s)
		}
	}
	return result, nil
}

func (i *IdentityService) OIDCReadConfig() (map[string]interface{}, error) {
	secret, err := i.client.Logical().Read("identity/oidc/config")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (i *IdentityService) OIDCWriteConfig(issuer string) error {
	_, err := i.client.Logical().Write("identity/oidc/config", map[string]interface{}{
		"issuer": issuer,
	})
	return err
}

// --- Entity by ID listing ---

func (i *IdentityService) ListEntitiesByID() ([]string, error) {
	secret, err := i.client.Logical().List("identity/entity/id")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	keys, ok := secret.Data["keys"].([]interface{})
	if !ok {
		return nil, nil
	}
	var result []string
	for _, k := range keys {
		if s, ok := k.(string); ok {
			result = append(result, s)
		}
	}
	return result, nil
}

// --- Group by ID ---

func (i *IdentityService) GetGroupByID(id string) (map[string]interface{}, error) {
	secret, err := i.client.Logical().Read("identity/group/id/" + id)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

func (i *IdentityService) ListGroupsByID() ([]string, error) {
	secret, err := i.client.Logical().List("identity/group/id")
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	keys, ok := secret.Data["keys"].([]interface{})
	if !ok {
		return nil, nil
	}
	var result []string
	for _, k := range keys {
		if s, ok := k.(string); ok {
			result = append(result, s)
		}
	}
	return result, nil
}
