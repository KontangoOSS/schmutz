package service

import (
	"fmt"

	vault "github.com/hashicorp/vault/api"
)

// IdentityService handles Bao's native identity engine (entities, aliases, groups).
// This is separate from the KV store — it uses Bao's built-in identity management.
type IdentityService struct {
	client *vault.Client
}

// NewIdentityService creates a new identity service using the same Bao client.
func NewIdentityService(addr, token string) (*IdentityService, error) {
	config := vault.DefaultConfig()
	config.Address = addr
	client, err := vault.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("bao identity client: %w", err)
	}
	client.SetToken(token)
	return &IdentityService{client: client}, nil
}

// --- Entities (person or app — long-lived) ---

// CreateEntity creates a new Bao entity with metadata.
func (i *IdentityService) CreateEntity(name string, metadata map[string]string, policies []string) (string, error) {
	data := map[string]interface{}{
		"name":     name,
		"metadata": metadata,
	}
	if len(policies) > 0 {
		data["policies"] = policies
	}

	secret, err := i.client.Logical().Write("identity/entity", data)
	if err != nil {
		return "", fmt.Errorf("create entity: %w", err)
	}
	if secret == nil {
		// Some Bao versions return nil secret on success — read back by name
		readBack, err := i.GetEntity(name)
		if err != nil || readBack == nil {
			return "", fmt.Errorf("create entity: write succeeded but read-back failed")
		}
		return readBack.ID, nil
	}
	if secret.Data != nil {
		if id, ok := secret.Data["id"].(string); ok {
			return id, nil
		}
	}
	return "", fmt.Errorf("create entity: no ID in response")
}

// GetEntity reads an entity by name.
func (i *IdentityService) GetEntity(name string) (*Entity, error) {
	secret, err := i.client.Logical().Read("identity/entity/name/" + name)
	if err != nil {
		return nil, err
	}
	if secret == nil || secret.Data == nil {
		return nil, nil
	}
	return parseEntity(secret.Data), nil
}

// GetEntityByID reads an entity by ID.
func (i *IdentityService) GetEntityByID(id string) (*Entity, error) {
	secret, err := i.client.Logical().Read("identity/entity/id/" + id)
	if err != nil {
		return nil, err
	}
	if secret == nil || secret.Data == nil {
		return nil, nil
	}
	return parseEntity(secret.Data), nil
}

// UpdateEntityMetadata updates metadata on an existing entity.
func (i *IdentityService) UpdateEntityMetadata(id string, metadata map[string]string) error {
	_, err := i.client.Logical().Write("identity/entity", map[string]interface{}{
		"id":       id,
		"metadata": metadata,
	})
	return err
}

// ListEntities lists all entity names.
func (i *IdentityService) ListEntities() ([]string, error) {
	secret, err := i.client.Logical().List("identity/entity/name")
	if err != nil {
		return nil, err
	}
	if secret == nil || secret.Data == nil {
		return nil, nil
	}
	keysRaw, _ := secret.Data["keys"].([]interface{})
	keys := make([]string, len(keysRaw))
	for idx, k := range keysRaw {
		keys[idx], _ = k.(string)
	}
	return keys, nil
}

// DeleteEntity removes an entity by ID.
func (i *IdentityService) DeleteEntity(id string) error {
	_, err := i.client.Logical().Delete("identity/entity/id/" + id)
	return err
}

// --- Aliases (device/login → entity mapping) ---

// CreateAlias links a Ziti device identity to a Bao entity.
func (i *IdentityService) CreateAlias(entityID, aliasName, mountAccessor string, metadata map[string]string) (string, error) {
	data := map[string]interface{}{
		"name":           aliasName,
		"canonical_id":   entityID,
		"mount_accessor": mountAccessor,
	}
	if len(metadata) > 0 {
		data["custom_metadata"] = metadata
	}

	secret, err := i.client.Logical().Write("identity/entity-alias", data)
	if err != nil {
		return "", fmt.Errorf("create alias: %w", err)
	}
	if secret == nil || secret.Data == nil {
		return "", fmt.Errorf("create alias: no response")
	}
	id, _ := secret.Data["id"].(string)
	return id, nil
}

// ListAliases lists all aliases for an entity.
func (i *IdentityService) ListAliases() ([]string, error) {
	secret, err := i.client.Logical().List("identity/entity-alias/id")
	if err != nil {
		return nil, err
	}
	if secret == nil || secret.Data == nil {
		return nil, nil
	}
	keysRaw, _ := secret.Data["keys"].([]interface{})
	keys := make([]string, len(keysRaw))
	for idx, k := range keysRaw {
		keys[idx], _ = k.(string)
	}
	return keys, nil
}

// --- Groups (admin, engineering, etc.) ---

// CreateGroup creates a Bao group with member entities.
func (i *IdentityService) CreateGroup(name string, memberEntityIDs []string, policies []string, metadata map[string]string) (string, error) {
	data := map[string]interface{}{
		"name":              name,
		"member_entity_ids": memberEntityIDs,
	}
	if len(policies) > 0 {
		data["policies"] = policies
	}
	if len(metadata) > 0 {
		data["metadata"] = metadata
	}

	secret, err := i.client.Logical().Write("identity/group", data)
	if err != nil {
		return "", fmt.Errorf("create group: %w", err)
	}
	if secret == nil || secret.Data == nil {
		return "", fmt.Errorf("create group: no response")
	}
	id, _ := secret.Data["id"].(string)
	return id, nil
}

// GetGroup reads a group by name.
func (i *IdentityService) GetGroup(name string) (map[string]interface{}, error) {
	secret, err := i.client.Logical().Read("identity/group/name/" + name)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, nil
	}
	return secret.Data, nil
}

// ListGroups lists all group names.
func (i *IdentityService) ListGroups() ([]string, error) {
	secret, err := i.client.Logical().List("identity/group/name")
	if err != nil {
		return nil, err
	}
	if secret == nil || secret.Data == nil {
		return nil, nil
	}
	keysRaw, _ := secret.Data["keys"].([]interface{})
	keys := make([]string, len(keysRaw))
	for idx, k := range keysRaw {
		keys[idx], _ = k.(string)
	}
	return keys, nil
}

// --- Auth method accessors ---

// GetAuthAccessor returns the mount accessor for an auth method (needed for alias creation).
func (i *IdentityService) GetAuthAccessor(path string) (string, error) {
	secret, err := i.client.Logical().Read("sys/auth")
	if err != nil {
		return "", err
	}
	if secret == nil || secret.Data == nil {
		return "", fmt.Errorf("no auth methods")
	}
	if mount, ok := secret.Data[path].(map[string]interface{}); ok {
		accessor, _ := mount["accessor"].(string)
		return accessor, nil
	}
	return "", fmt.Errorf("auth method %s not found", path)
}

// --- Types ---

type Entity struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Metadata map[string]string `json:"metadata"`
	Policies []string          `json:"policies"`
	Aliases  []EntityAlias     `json:"aliases"`
	GroupIDs []string          `json:"group_ids"`
	Disabled bool              `json:"disabled"`
}

type EntityAlias struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	MountAccessor string            `json:"mount_accessor"`
	MountType     string            `json:"mount_type"`
	Metadata      map[string]string `json:"custom_metadata"`
}

func parseEntity(data map[string]interface{}) *Entity {
	e := &Entity{}
	e.ID, _ = data["id"].(string)
	e.Name, _ = data["name"].(string)
	e.Disabled, _ = data["disabled"].(bool)

	if md, ok := data["metadata"].(map[string]interface{}); ok {
		e.Metadata = make(map[string]string)
		for k, v := range md {
			e.Metadata[k], _ = v.(string)
		}
	}

	if policies, ok := data["policies"].([]interface{}); ok {
		for _, p := range policies {
			if s, ok := p.(string); ok {
				e.Policies = append(e.Policies, s)
			}
		}
	}

	if aliases, ok := data["aliases"].([]interface{}); ok {
		for _, a := range aliases {
			if am, ok := a.(map[string]interface{}); ok {
				alias := EntityAlias{}
				alias.ID, _ = am["id"].(string)
				alias.Name, _ = am["name"].(string)
				alias.MountAccessor, _ = am["mount_accessor"].(string)
				alias.MountType, _ = am["mount_type"].(string)
				if cm, ok := am["custom_metadata"].(map[string]interface{}); ok {
					alias.Metadata = make(map[string]string)
					for k, v := range cm {
						alias.Metadata[k], _ = v.(string)
					}
				}
				e.Aliases = append(e.Aliases, alias)
			}
		}
	}

	if groups, ok := data["group_ids"].([]interface{}); ok {
		for _, g := range groups {
			if s, ok := g.(string); ok {
				e.GroupIDs = append(e.GroupIDs, s)
			}
		}
	}

	return e
}
