package main

import (
	"net/http"
	"strings"
)

// --- Bao KV ---

func (a *API) BaoKV(w http.ResponseWriter, r *http.Request) {
	// /api/bao/kv/{mount}/{path...}
	// Also handles /api/bao/kv/{mount}/list/{path...} for LIST
	rest := strings.TrimPrefix(r.URL.Path, "/api/bao/kv/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) < 2 {
		errJSON(w, "usage: /api/bao/kv/{mount}/{path}", http.StatusBadRequest)
		return
	}
	mount := parts[0]
	path := parts[1]

	// Handle list: /api/bao/kv/{mount}/list/{path}
	if strings.HasPrefix(path, "list/") {
		listPath := strings.TrimPrefix(path, "list/")
		data, err := a.store.List(mount, listPath)
		jsonOrErr(w, map[string]interface{}{"keys": data}, err)
		return
	}

	switch r.Method {
	case http.MethodGet:
		data, err := a.store.Get(mount, path)
		jsonOrErr(w, data, err)
	case http.MethodPut, http.MethodPost:
		var req map[string]interface{}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.store.Put(mount, path, req)
		jsonOrErr(w, map[string]string{"status": "written"}, err)
	case http.MethodDelete:
		err := a.store.Delete(mount, path)
		jsonOrErr(w, map[string]string{"status": "deleted"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Bao system ---

func (a *API) BaoHealth(w http.ResponseWriter, r *http.Request) {
	data, err := a.store.Health()
	jsonOrErr(w, data, err)
}

func (a *API) BaoSealStatus(w http.ResponseWriter, r *http.Request) {
	data, err := a.store.SealStatus()
	jsonOrErr(w, data, err)
}

func (a *API) BaoLeader(w http.ResponseWriter, r *http.Request) {
	data, err := a.store.Leader()
	jsonOrErr(w, data, err)
}

// --- Bao policies ---

func (a *API) BaoPolicies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data, err := a.store.ListPolicies()
		jsonOrErr(w, map[string]interface{}{"policies": data}, err)
	case http.MethodPost:
		var req struct {
			Name  string `json:"name"`
			Rules string `json:"rules"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.store.CreatePolicy(req.Name, req.Rules)
		jsonOrErr(w, map[string]string{"status": "created"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) BaoPolicy(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/bao/policies/")
	switch r.Method {
	case http.MethodGet:
		rules, err := a.store.GetPolicy(name)
		jsonOrErr(w, map[string]string{"name": name, "rules": rules}, err)
	case http.MethodDelete:
		err := a.store.DeletePolicy(name)
		jsonOrErr(w, map[string]string{"status": "deleted"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Bao tokens ---

func (a *API) BaoTokenCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Policies []string          `json:"policies"`
		TTL      string            `json:"ttl"`
		Metadata map[string]string `json:"metadata"`
	}
	if !parseJSON(r, w, &req) {
		return
	}
	token, err := a.store.CreateToken(req.Policies, req.TTL, req.Metadata)
	jsonOrErr(w, map[string]string{"token": token}, err)
}

func (a *API) BaoTokenLookup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if !parseJSON(r, w, &req) {
		return
	}
	data, err := a.store.LookupToken(req.Token)
	jsonOrErr(w, data, err)
}

func (a *API) BaoTokenRevoke(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if !parseJSON(r, w, &req) {
		return
	}
	err := a.store.RevokeToken(req.Token)
	jsonOrErr(w, map[string]string{"status": "revoked"}, err)
}

// --- Bao AppRole ---

func (a *API) BaoAppRoles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data, err := a.store.ListAppRoles()
		jsonOrErr(w, map[string]interface{}{"roles": data}, err)
	case http.MethodPost:
		var req struct {
			Name     string   `json:"name"`
			Policies []string `json:"policies"`
			TokenTTL string   `json:"token_ttl"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.store.CreateAppRole(req.Name, req.Policies, req.TokenTTL)
		jsonOrErr(w, map[string]string{"status": "created"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) BaoAppRoleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RoleID   string `json:"role_id"`
		SecretID string `json:"secret_id"`
	}
	if !parseJSON(r, w, &req) {
		return
	}
	data, err := a.store.AppRoleLogin(req.RoleID, req.SecretID)
	jsonOrErr(w, data, err)
}

func (a *API) BaoAppRole(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/bao/approles/")

	// Sub-resources
	if strings.HasSuffix(name, "/role-id") {
		roleName := strings.TrimSuffix(name, "/role-id")
		id, err := a.store.GetAppRoleID(roleName)
		jsonOrErr(w, map[string]string{"role_id": id}, err)
		return
	}
	if strings.HasSuffix(name, "/secret-id") {
		roleName := strings.TrimSuffix(name, "/secret-id")
		var req struct {
			Metadata map[string]string `json:"metadata"`
		}
		parseJSON(r, w, &req)
		id, err := a.store.CreateAppRoleSecretID(roleName, req.Metadata)
		jsonOrErr(w, map[string]string{"secret_id": id}, err)
		return
	}

	switch r.Method {
	case http.MethodDelete:
		err := a.store.DeleteAppRole(name)
		jsonOrErr(w, map[string]string{"status": "deleted"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Bao PKI ---

func (a *API) BaoPKI(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/bao/pki/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) < 2 {
		errJSON(w, "usage: /api/bao/pki/{mount}/{action}", http.StatusBadRequest)
		return
	}
	mount := parts[0]
	action := parts[1]

	switch action {
	case "issue":
		var req struct {
			Role       string   `json:"role"`
			CommonName string   `json:"common_name"`
			TTL        string   `json:"ttl"`
			AltNames   []string `json:"alt_names"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		data, err := a.store.IssueCert(mount, req.Role, req.CommonName, req.TTL, req.AltNames)
		jsonOrErr(w, data, err)
	case "certs":
		data, err := a.store.ListCerts(mount)
		jsonOrErr(w, map[string]interface{}{"certs": data}, err)
	case "revoke":
		var req struct {
			Serial string `json:"serial"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.store.RevokeCert(mount, req.Serial)
		jsonOrErr(w, map[string]string{"status": "revoked"}, err)
	default:
		errJSON(w, "unknown action: "+action, http.StatusBadRequest)
	}
}

// --- Bao OIDC ---

func (a *API) BaoOIDCConfig(w http.ResponseWriter, r *http.Request) {
	data, err := a.store.GetOIDCConfig()
	jsonOrErr(w, data, err)
}
