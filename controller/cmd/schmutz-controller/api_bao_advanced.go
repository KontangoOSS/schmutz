package main

import (
	"net/http"
	"strings"
)

// --- Rekey / Rotate ---

func (a *API) BaoRekeyInit(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data, err := a.store.RekeyStatus()
		jsonOrErr(w, data, err)
	case http.MethodPost:
		var req struct {
			Shares    int `json:"shares"`
			Threshold int `json:"threshold"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		data, err := a.store.RekeyInit(req.Shares, req.Threshold)
		jsonOrErr(w, data, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) BaoRekeyUpdate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key   string `json:"key"`
		Nonce string `json:"nonce"`
	}
	if !parseJSON(r, w, &req) {
		return
	}
	data, err := a.store.RekeyUpdate(req.Key, req.Nonce)
	jsonOrErr(w, data, err)
}

func (a *API) BaoRekeyCancel(w http.ResponseWriter, r *http.Request) {
	err := a.store.RekeyCancel()
	jsonOrErr(w, map[string]string{"status": "cancelled"}, err)
}

func (a *API) BaoRekeyStatus(w http.ResponseWriter, r *http.Request) {
	data, err := a.store.RekeyStatus()
	jsonOrErr(w, data, err)
}

func (a *API) BaoRotate(w http.ResponseWriter, r *http.Request) {
	err := a.store.RotateEncryptionKey()
	jsonOrErr(w, map[string]string{"status": "rotated"}, err)
}

// --- Cubbyhole ---

func (a *API) BaoCubbyhole(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/bao/cubbyhole/")
	switch r.Method {
	case http.MethodGet:
		if strings.HasSuffix(path, "/") || path == "" {
			data, err := a.store.CubbyList(path)
			jsonOrErr(w, map[string]interface{}{"keys": data}, err)
		} else {
			data, err := a.store.CubbyRead(path)
			jsonOrErr(w, data, err)
		}
	case http.MethodPut, http.MethodPost:
		var req map[string]interface{}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.store.CubbyWrite(path, req)
		jsonOrErr(w, map[string]string{"status": "written"}, err)
	case http.MethodDelete:
		err := a.store.CubbyDelete(path)
		jsonOrErr(w, map[string]string{"status": "deleted"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Database secrets engine ---

func (a *API) BaoDatabase(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/bao/database/")
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 {
		errJSON(w, "usage: /api/bao/database/{mount}/{action}[/{name}]", http.StatusBadRequest)
		return
	}
	mount, action := parts[0], parts[1]
	name := ""
	if len(parts) == 3 {
		name = parts[2]
	}

	switch action {
	case "config":
		if name == "" {
			data, err := a.store.DatabaseListConnections(mount)
			jsonOrErr(w, map[string]interface{}{"connections": data}, err)
			return
		}
		switch r.Method {
		case http.MethodGet:
			data, err := a.store.DatabaseReadConnection(mount, name)
			jsonOrErr(w, data, err)
		case http.MethodPost:
			var req map[string]interface{}
			if !parseJSON(r, w, &req) {
				return
			}
			err := a.store.DatabaseConfigConnection(mount, name, req)
			jsonOrErr(w, map[string]string{"status": "configured"}, err)
		case http.MethodDelete:
			err := a.store.DatabaseDeleteConnection(mount, name)
			jsonOrErr(w, map[string]string{"status": "deleted"}, err)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case "roles":
		if name == "" {
			data, err := a.store.DatabaseListRoles(mount)
			jsonOrErr(w, map[string]interface{}{"roles": data}, err)
			return
		}
		switch r.Method {
		case http.MethodGet:
			data, err := a.store.DatabaseReadRole(mount, name)
			jsonOrErr(w, data, err)
		case http.MethodPost:
			var req map[string]interface{}
			if !parseJSON(r, w, &req) {
				return
			}
			err := a.store.DatabaseCreateRole(mount, name, req)
			jsonOrErr(w, map[string]string{"status": "created"}, err)
		case http.MethodDelete:
			err := a.store.DatabaseDeleteRole(mount, name)
			jsonOrErr(w, map[string]string{"status": "deleted"}, err)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case "static-roles":
		if name == "" {
			data, err := a.store.DatabaseListStaticRoles(mount)
			jsonOrErr(w, map[string]interface{}{"roles": data}, err)
			return
		}
		switch r.Method {
		case http.MethodGet:
			data, err := a.store.DatabaseReadStaticRole(mount, name)
			jsonOrErr(w, data, err)
		case http.MethodPost:
			var req map[string]interface{}
			if !parseJSON(r, w, &req) {
				return
			}
			err := a.store.DatabaseCreateStaticRole(mount, name, req)
			jsonOrErr(w, map[string]string{"status": "created"}, err)
		case http.MethodDelete:
			err := a.store.DatabaseDeleteStaticRole(mount, name)
			jsonOrErr(w, map[string]string{"status": "deleted"}, err)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case "creds":
		data, err := a.store.DatabaseGenerateCredentials(mount, name)
		jsonOrErr(w, data, err)
	case "static-creds":
		data, err := a.store.DatabaseGetStaticCredentials(mount, name)
		jsonOrErr(w, data, err)
	case "rotate-root":
		err := a.store.DatabaseRotateRootCredentials(mount, name)
		jsonOrErr(w, map[string]string{"status": "rotated"}, err)
	case "rotate-role":
		err := a.store.DatabaseRotateStaticRole(mount, name)
		jsonOrErr(w, map[string]string{"status": "rotated"}, err)
	default:
		errJSON(w, "unknown database action: "+action, http.StatusBadRequest)
	}
}

// --- LDAP auth ---

func (a *API) BaoLDAPConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data, err := a.store.LDAPConfigRead()
		jsonOrErr(w, data, err)
	case http.MethodPost:
		var req map[string]interface{}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.store.LDAPConfigWrite(req)
		jsonOrErr(w, map[string]string{"status": "configured"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) BaoLDAPGroups(w http.ResponseWriter, r *http.Request) {
	data, err := a.store.LDAPListGroups()
	jsonOrErr(w, map[string]interface{}{"groups": data}, err)
}

func (a *API) BaoLDAPGroup(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/bao/ldap/groups/")
	switch r.Method {
	case http.MethodGet:
		data, err := a.store.LDAPReadGroup(name)
		jsonOrErr(w, data, err)
	case http.MethodPost:
		var req struct {
			Policies []string `json:"policies"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.store.LDAPCreateGroup(name, req.Policies)
		jsonOrErr(w, map[string]string{"status": "created"}, err)
	case http.MethodDelete:
		err := a.store.LDAPDeleteGroup(name)
		jsonOrErr(w, map[string]string{"status": "deleted"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) BaoLDAPUsers(w http.ResponseWriter, r *http.Request) {
	data, err := a.store.LDAPListUsers()
	jsonOrErr(w, map[string]interface{}{"users": data}, err)
}

func (a *API) BaoLDAPUser(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/bao/ldap/users/")
	switch r.Method {
	case http.MethodPost:
		var req struct {
			Policies []string `json:"policies"`
			Groups   []string `json:"groups"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.store.LDAPCreateUser(name, req.Policies, req.Groups)
		jsonOrErr(w, map[string]string{"status": "created"}, err)
	case http.MethodDelete:
		err := a.store.LDAPDeleteUser(name)
		jsonOrErr(w, map[string]string{"status": "deleted"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) BaoLDAPLogin(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimPrefix(r.URL.Path, "/api/bao/ldap/login/")
	var req struct {
		Password string `json:"password"`
	}
	if !parseJSON(r, w, &req) {
		return
	}
	data, err := a.store.LDAPLogin(username, req.Password)
	jsonOrErr(w, data, err)
}

// --- Cert auth ---

func (a *API) BaoCertRoles(w http.ResponseWriter, r *http.Request) {
	data, err := a.store.CertAuthListRoles()
	jsonOrErr(w, map[string]interface{}{"roles": data}, err)
}

func (a *API) BaoCertRole(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/bao/cert/roles/")
	switch r.Method {
	case http.MethodGet:
		data, err := a.store.CertAuthReadRole(name)
		jsonOrErr(w, data, err)
	case http.MethodPost:
		var req map[string]interface{}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.store.CertAuthCreateRole(name, req)
		jsonOrErr(w, map[string]string{"status": "created"}, err)
	case http.MethodDelete:
		err := a.store.CertAuthDeleteRole(name)
		jsonOrErr(w, map[string]string{"status": "deleted"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Kubernetes auth ---

func (a *API) BaoK8sConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data, err := a.store.K8sAuthConfigRead()
		jsonOrErr(w, data, err)
	case http.MethodPost:
		var req map[string]interface{}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.store.K8sAuthConfigWrite(req)
		jsonOrErr(w, map[string]string{"status": "configured"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) BaoK8sRoles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data, err := a.store.K8sAuthListRoles()
		jsonOrErr(w, map[string]interface{}{"roles": data}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) BaoK8sRole(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/bao/k8s/roles/")
	switch r.Method {
	case http.MethodGet:
		data, err := a.store.K8sAuthReadRole(name)
		jsonOrErr(w, data, err)
	case http.MethodPost:
		var req map[string]interface{}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.store.K8sAuthCreateRole(name, req)
		jsonOrErr(w, map[string]string{"status": "created"}, err)
	case http.MethodDelete:
		err := a.store.K8sAuthDeleteRole(name)
		jsonOrErr(w, map[string]string{"status": "deleted"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) BaoK8sLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Role string `json:"role"`
		JWT  string `json:"jwt"`
	}
	if !parseJSON(r, w, &req) {
		return
	}
	data, err := a.store.K8sAuthLogin(req.Role, req.JWT)
	jsonOrErr(w, data, err)
}

// --- JWT/OIDC auth ---

func (a *API) BaoJWTConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data, err := a.store.JWTAuthConfigRead()
		jsonOrErr(w, data, err)
	case http.MethodPost:
		var req map[string]interface{}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.store.JWTAuthConfigWrite(req)
		jsonOrErr(w, map[string]string{"status": "configured"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) BaoJWTRoles(w http.ResponseWriter, r *http.Request) {
	data, err := a.store.JWTAuthListRoles()
	jsonOrErr(w, map[string]interface{}{"roles": data}, err)
}

func (a *API) BaoJWTRole(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/bao/jwt/roles/")
	switch r.Method {
	case http.MethodGet:
		data, err := a.store.JWTAuthReadRole(name)
		jsonOrErr(w, data, err)
	case http.MethodPost:
		var req map[string]interface{}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.store.JWTAuthCreateRole(name, req)
		jsonOrErr(w, map[string]string{"status": "created"}, err)
	case http.MethodDelete:
		err := a.store.JWTAuthDeleteRole(name)
		jsonOrErr(w, map[string]string{"status": "deleted"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
