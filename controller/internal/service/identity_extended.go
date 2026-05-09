package service

import "fmt"

// --- Alias extended ---

// GetAlias reads an entity alias by ID.
func (i *IdentityService) GetAlias(id string) (map[string]interface{}, error) {
	secret, err := i.client.Logical().Read("identity/entity-alias/id/" + id)
	if err != nil {
		return nil, err
	}
	if secret == nil || secret.Data == nil {
		return nil, nil
	}
	return secret.Data, nil
}

// UpdateAlias updates an entity alias.
func (i *IdentityService) UpdateAlias(id string, metadata map[string]string) error {
	data := map[string]interface{}{
		"id":              id,
		"custom_metadata": metadata,
	}
	_, err := i.client.Logical().Write("identity/entity-alias/id/"+id, data)
	return err
}

// DeleteAlias removes an entity alias.
func (i *IdentityService) DeleteAlias(id string) error {
	_, err := i.client.Logical().Delete("identity/entity-alias/id/" + id)
	return err
}

// --- Group extended ---

// UpdateGroup updates a group's members and policies.
func (i *IdentityService) UpdateGroup(id string, memberEntityIDs []string, policies []string) error {
	data := map[string]interface{}{"id": id}
	if memberEntityIDs != nil {
		data["member_entity_ids"] = memberEntityIDs
	}
	if policies != nil {
		data["policies"] = policies
	}
	_, err := i.client.Logical().Write("identity/group/id/"+id, data)
	return err
}

// DeleteGroup removes a group.
func (i *IdentityService) DeleteGroup(id string) error {
	_, err := i.client.Logical().Delete("identity/group/id/" + id)
	return err
}

// AddGroupMember adds an entity to a group.
func (i *IdentityService) AddGroupMember(groupName, entityID string) error {
	group, err := i.GetGroup(groupName)
	if err != nil || group == nil {
		return fmt.Errorf("group %s not found", groupName)
	}
	gid, _ := group["id"].(string)
	members, _ := group["member_entity_ids"].([]interface{})

	// Check not already a member
	for _, m := range members {
		if s, _ := m.(string); s == entityID {
			return nil // Already a member
		}
	}
	members = append(members, entityID)

	ids := make([]string, len(members))
	for idx, m := range members {
		ids[idx], _ = m.(string)
	}
	return i.UpdateGroup(gid, ids, nil)
}

// RemoveGroupMember removes an entity from a group.
func (i *IdentityService) RemoveGroupMember(groupName, entityID string) error {
	group, err := i.GetGroup(groupName)
	if err != nil || group == nil {
		return fmt.Errorf("group %s not found", groupName)
	}
	gid, _ := group["id"].(string)
	members, _ := group["member_entity_ids"].([]interface{})

	var filtered []string
	for _, m := range members {
		if s, _ := m.(string); s != entityID {
			filtered = append(filtered, s)
		}
	}
	return i.UpdateGroup(gid, filtered, nil)
}

// --- Entity merge ---

// MergeEntities merges multiple entities into one.
func (i *IdentityService) MergeEntities(toEntityID string, fromEntityIDs []string) error {
	_, err := i.client.Logical().Write("identity/entity/merge", map[string]interface{}{
		"to_entity_id":                  toEntityID,
		"from_entity_ids":               fromEntityIDs,
		"conflicting_alias_ids_to_keep": nil,
	})
	return err
}
