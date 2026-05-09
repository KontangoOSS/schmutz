package main

import (
	"net/http"
	"strings"
)

// --- Group Aliases ---

func (a *API) IdentityGroupAliases(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data, err := a.identity.ListGroupAliases()
		jsonOrErr(w, map[string]interface{}{"group_aliases": data}, err)
	case http.MethodPost:
		var req struct {
			GroupID       string            `json:"group_id"`
			Name          string            `json:"name"`
			MountAccessor string            `json:"mount_accessor"`
			Metadata      map[string]string `json:"metadata"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		id, err := a.identity.CreateGroupAlias(req.GroupID, req.Name, req.MountAccessor, req.Metadata)
		jsonOrErr(w, map[string]string{"id": id}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) IdentityGroupAlias(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/identity/group-aliases/")
	switch r.Method {
	case http.MethodGet:
		data, err := a.identity.GetGroupAlias(id)
		jsonOrErr(w, data, err)
	case http.MethodPatch:
		var req map[string]interface{}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.identity.UpdateGroupAlias(id, req)
		jsonOrErr(w, map[string]string{"status": "updated"}, err)
	case http.MethodDelete:
		err := a.identity.DeleteGroupAlias(id)
		jsonOrErr(w, map[string]string{"status": "deleted"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Entity/Group listing by ID ---

func (a *API) IdentityEntitiesByID(w http.ResponseWriter, r *http.Request) {
	data, err := a.identity.ListEntitiesByID()
	jsonOrErr(w, map[string]interface{}{"entity_ids": data}, err)
}

func (a *API) IdentityGroupsByID(w http.ResponseWriter, r *http.Request) {
	data, err := a.identity.ListGroupsByID()
	jsonOrErr(w, map[string]interface{}{"group_ids": data}, err)
}

// --- OIDC Provider (identity engine) ---

func (a *API) IdentityOIDCConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data, err := a.identity.OIDCReadConfig()
		jsonOrErr(w, data, err)
	case http.MethodPost:
		var req struct {
			Issuer string `json:"issuer"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.identity.OIDCWriteConfig(req.Issuer)
		jsonOrErr(w, map[string]string{"status": "configured"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) IdentityOIDCKeys(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data, err := a.identity.OIDCListKeys()
		jsonOrErr(w, map[string]interface{}{"keys": data}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) IdentityOIDCKey(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/identity/oidc/keys/")
	if strings.HasSuffix(name, "/rotate") {
		keyName := strings.TrimSuffix(name, "/rotate")
		err := a.identity.OIDCRotateKey(keyName)
		jsonOrErr(w, map[string]string{"status": "rotated"}, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		data, err := a.identity.OIDCReadKey(name)
		jsonOrErr(w, data, err)
	case http.MethodPost:
		var req map[string]interface{}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.identity.OIDCCreateKey(name, req)
		jsonOrErr(w, map[string]string{"status": "created"}, err)
	case http.MethodDelete:
		err := a.identity.OIDCDeleteKey(name)
		jsonOrErr(w, map[string]string{"status": "deleted"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) IdentityOIDCRoles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data, err := a.identity.OIDCListRoles()
		jsonOrErr(w, map[string]interface{}{"roles": data}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) IdentityOIDCRole(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/identity/oidc/roles/")
	switch r.Method {
	case http.MethodGet:
		data, err := a.identity.OIDCReadRole(name)
		jsonOrErr(w, data, err)
	case http.MethodPost:
		var req map[string]interface{}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.identity.OIDCCreateRole(name, req)
		jsonOrErr(w, map[string]string{"status": "created"}, err)
	case http.MethodDelete:
		err := a.identity.OIDCDeleteRole(name)
		jsonOrErr(w, map[string]string{"status": "deleted"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
