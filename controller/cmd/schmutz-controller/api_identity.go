package main

import (
	"net/http"
	"strings"
)

// --- Identity Entities ---

func (a *API) IdentityEntities(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data, err := a.identity.ListEntities()
		jsonOrErr(w, map[string]interface{}{"entities": data}, err)
	case http.MethodPost:
		var req struct {
			Name     string            `json:"name"`
			Metadata map[string]string `json:"metadata"`
			Policies []string          `json:"policies"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		id, err := a.identity.CreateEntity(req.Name, req.Metadata, req.Policies)
		jsonOrErr(w, map[string]string{"id": id}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) IdentityEntity(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/identity/entities/")

	// by-name sub-resource
	if strings.HasPrefix(id, "by-name/") {
		name := strings.TrimPrefix(id, "by-name/")
		entity, err := a.identity.GetEntity(name)
		jsonOrErr(w, entity, err)
		return
	}

	switch r.Method {
	case http.MethodGet:
		entity, err := a.identity.GetEntityByID(id)
		jsonOrErr(w, entity, err)
	case http.MethodPatch:
		var req struct {
			Metadata map[string]string `json:"metadata"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.identity.UpdateEntityMetadata(id, req.Metadata)
		jsonOrErr(w, map[string]string{"status": "updated"}, err)
	case http.MethodDelete:
		err := a.identity.DeleteEntity(id)
		jsonOrErr(w, map[string]string{"status": "deleted"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) IdentityMerge(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ToEntityID    string   `json:"to_entity_id"`
		FromEntityIDs []string `json:"from_entity_ids"`
	}
	if !parseJSON(r, w, &req) {
		return
	}
	err := a.identity.MergeEntities(req.ToEntityID, req.FromEntityIDs)
	jsonOrErr(w, map[string]string{"status": "merged"}, err)
}

// --- Identity Aliases ---

func (a *API) IdentityAliases(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data, err := a.identity.ListAliases()
		jsonOrErr(w, map[string]interface{}{"aliases": data}, err)
	case http.MethodPost:
		var req struct {
			EntityID      string            `json:"entity_id"`
			Name          string            `json:"name"`
			MountAccessor string            `json:"mount_accessor"`
			Metadata      map[string]string `json:"metadata"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		id, err := a.identity.CreateAlias(req.EntityID, req.Name, req.MountAccessor, req.Metadata)
		jsonOrErr(w, map[string]string{"id": id}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) IdentityAlias(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/identity/aliases/")
	switch r.Method {
	case http.MethodGet:
		data, err := a.identity.GetAlias(id)
		jsonOrErr(w, data, err)
	case http.MethodPatch:
		var req struct {
			Metadata map[string]string `json:"metadata"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.identity.UpdateAlias(id, req.Metadata)
		jsonOrErr(w, map[string]string{"status": "updated"}, err)
	case http.MethodDelete:
		err := a.identity.DeleteAlias(id)
		jsonOrErr(w, map[string]string{"status": "deleted"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Identity Groups ---

func (a *API) IdentityGroups(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data, err := a.identity.ListGroups()
		jsonOrErr(w, map[string]interface{}{"groups": data}, err)
	case http.MethodPost:
		var req struct {
			Name            string            `json:"name"`
			MemberEntityIDs []string          `json:"member_entity_ids"`
			Policies        []string          `json:"policies"`
			Metadata        map[string]string `json:"metadata"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		id, err := a.identity.CreateGroup(req.Name, req.MemberEntityIDs, req.Policies, req.Metadata)
		jsonOrErr(w, map[string]string{"id": id}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) IdentityGroup(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/identity/groups/")

	// Member sub-resource: /api/identity/groups/{name}/members
	if strings.HasSuffix(name, "/members") {
		groupName := strings.TrimSuffix(name, "/members")
		var req struct {
			EntityID string `json:"entity_id"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		switch r.Method {
		case http.MethodPost:
			err := a.identity.AddGroupMember(groupName, req.EntityID)
			jsonOrErr(w, map[string]string{"status": "added"}, err)
		case http.MethodDelete:
			err := a.identity.RemoveGroupMember(groupName, req.EntityID)
			jsonOrErr(w, map[string]string{"status": "removed"}, err)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	switch r.Method {
	case http.MethodGet:
		data, err := a.identity.GetGroup(name)
		jsonOrErr(w, data, err)
	case http.MethodPatch:
		var req struct {
			MemberEntityIDs []string `json:"member_entity_ids"`
			Policies        []string `json:"policies"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		// Need group ID — get it first
		group, err := a.identity.GetGroup(name)
		if err != nil {
			errJSON(w, err.Error(), http.StatusInternalServerError)
			return
		}
		groupID, _ := group["id"].(string)
		err = a.identity.UpdateGroup(groupID, req.MemberEntityIDs, req.Policies)
		jsonOrErr(w, map[string]string{"status": "updated"}, err)
	case http.MethodDelete:
		group, err := a.identity.GetGroup(name)
		if err != nil {
			errJSON(w, err.Error(), http.StatusInternalServerError)
			return
		}
		groupID, _ := group["id"].(string)
		err = a.identity.DeleteGroup(groupID)
		jsonOrErr(w, map[string]string{"status": "deleted"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) IdentityAuthAccessor(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/identity/auth-accessor/")
	accessor, err := a.identity.GetAuthAccessor(path)
	jsonOrErr(w, map[string]string{"accessor": accessor, "path": path}, err)
}
