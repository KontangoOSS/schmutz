package main

import (
	"net/http"
	"strings"
)

// --- KV metadata/versions/config ---

func (a *API) BaoKVMetadata(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/bao/kv-metadata/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) < 2 {
		errJSON(w, "usage: /api/bao/kv-metadata/{mount}/{path}", http.StatusBadRequest)
		return
	}
	mount, path := parts[0], parts[1]
	switch r.Method {
	case http.MethodGet:
		data, err := a.store.GetMetadata(mount, path)
		jsonOrErr(w, data, err)
	case http.MethodPut, http.MethodPost:
		var req map[string]interface{}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.store.PutMetadata(mount, path, req)
		jsonOrErr(w, map[string]string{"status": "written"}, err)
	case http.MethodDelete:
		err := a.store.DeleteMetadata(mount, path)
		jsonOrErr(w, map[string]string{"status": "deleted"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) BaoKVVersions(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/bao/kv-versions/")
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 3 {
		errJSON(w, "usage: /api/bao/kv-versions/{mount}/{action}/{path}", http.StatusBadRequest)
		return
	}
	mount, action, path := parts[0], parts[1], parts[2]
	var req struct {
		Versions []int `json:"versions"`
	}
	if !parseJSON(r, w, &req) {
		return
	}
	var err error
	switch action {
	case "delete":
		err = a.store.DeleteVersions(mount, path, req.Versions)
	case "undelete":
		err = a.store.UndeleteVersions(mount, path, req.Versions)
	case "destroy":
		err = a.store.DestroyVersions(mount, path, req.Versions)
	default:
		errJSON(w, "unknown action: "+action, http.StatusBadRequest)
		return
	}
	jsonOrErr(w, map[string]string{"status": action + "d"}, err)
}

func (a *API) BaoKVConfig(w http.ResponseWriter, r *http.Request) {
	mount := strings.TrimPrefix(r.URL.Path, "/api/bao/kv-config/")
	switch r.Method {
	case http.MethodGet:
		data, err := a.store.GetKVConfig(mount)
		jsonOrErr(w, data, err)
	case http.MethodPut, http.MethodPost:
		var req struct {
			MaxVersions        int    `json:"max_versions"`
			CASRequired        bool   `json:"cas_required"`
			DeleteVersionAfter string `json:"delete_version_after"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.store.PutKVConfig(mount, req.MaxVersions, req.CASRequired, req.DeleteVersionAfter)
		jsonOrErr(w, map[string]string{"status": "configured"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- System operations ---

func (a *API) BaoSeal(w http.ResponseWriter, r *http.Request) {
	err := a.store.Seal()
	jsonOrErr(w, map[string]string{"status": "sealed"}, err)
}

func (a *API) BaoUnseal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key string `json:"key"`
	}
	if !parseJSON(r, w, &req) {
		return
	}
	data, err := a.store.Unseal(req.Key)
	jsonOrErr(w, data, err)
}

func (a *API) BaoInit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Shares    int `json:"shares"`
		Threshold int `json:"threshold"`
	}
	if !parseJSON(r, w, &req) {
		return
	}
	data, err := a.store.Init(req.Shares, req.Threshold)
	jsonOrErr(w, data, err)
}

func (a *API) BaoStepDown(w http.ResponseWriter, r *http.Request) {
	err := a.store.StepDown()
	jsonOrErr(w, map[string]string{"status": "stepped_down"}, err)
}

func (a *API) BaoKeyStatus(w http.ResponseWriter, r *http.Request) {
	data, err := a.store.KeyStatus()
	jsonOrErr(w, data, err)
}

// --- Mounts ---

func (a *API) BaoMounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data, err := a.store.ListMounts()
		jsonOrErr(w, data, err)
	case http.MethodPost:
		var req struct {
			Path        string                 `json:"path"`
			Type        string                 `json:"type"`
			Description string                 `json:"description"`
			Options     map[string]interface{} `json:"options"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.store.MountSecretEngine(req.Path, req.Type, req.Description, req.Options)
		jsonOrErr(w, map[string]string{"status": "mounted"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) BaoMount(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/bao/mounts/")
	if strings.HasSuffix(path, "/tune") {
		mountPath := strings.TrimSuffix(path, "/tune")
		switch r.Method {
		case http.MethodGet:
			data, err := a.store.ReadMountConfig(mountPath)
			jsonOrErr(w, data, err)
		case http.MethodPost:
			var req map[string]interface{}
			if !parseJSON(r, w, &req) {
				return
			}
			err := a.store.TuneMount(mountPath, req)
			jsonOrErr(w, map[string]string{"status": "tuned"}, err)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}
	switch r.Method {
	case http.MethodDelete:
		err := a.store.UnmountSecretEngine(path)
		jsonOrErr(w, map[string]string{"status": "unmounted"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Auth methods ---

func (a *API) BaoAuthMethods(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data, err := a.store.ListAuthMethods()
		jsonOrErr(w, data, err)
	case http.MethodPost:
		var req struct {
			Path        string `json:"path"`
			Type        string `json:"type"`
			Description string `json:"description"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.store.EnableAuthMethod(req.Path, req.Type, req.Description)
		jsonOrErr(w, map[string]string{"status": "enabled"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) BaoAuthMethod(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/bao/auth-methods/")
	if strings.HasSuffix(path, "/tune") {
		authPath := strings.TrimSuffix(path, "/tune")
		var req map[string]interface{}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.store.TuneAuthMethod(authPath, req)
		jsonOrErr(w, map[string]string{"status": "tuned"}, err)
		return
	}
	err := a.store.DisableAuthMethod(path)
	jsonOrErr(w, map[string]string{"status": "disabled"}, err)
}

// --- Audit ---

func (a *API) BaoAuditDevices(w http.ResponseWriter, r *http.Request) {
	data, err := a.store.ListAuditDevices()
	jsonOrErr(w, data, err)
}

func (a *API) BaoAuditDevice(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/bao/audit/")
	switch r.Method {
	case http.MethodPost:
		var req struct {
			Type    string                 `json:"type"`
			Options map[string]interface{} `json:"options"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.store.EnableAuditDevice(path, req.Type, req.Options)
		jsonOrErr(w, map[string]string{"status": "enabled"}, err)
	case http.MethodDelete:
		err := a.store.DisableAuditDevice(path)
		jsonOrErr(w, map[string]string{"status": "disabled"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Leases ---

func (a *API) BaoLeaseLookup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LeaseID string `json:"lease_id"`
	}
	if !parseJSON(r, w, &req) {
		return
	}
	data, err := a.store.LookupLease(req.LeaseID)
	jsonOrErr(w, data, err)
}

func (a *API) BaoLeaseRenew(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LeaseID   string `json:"lease_id"`
		Increment int    `json:"increment"`
	}
	if !parseJSON(r, w, &req) {
		return
	}
	data, err := a.store.RenewLease(req.LeaseID, req.Increment)
	jsonOrErr(w, data, err)
}

func (a *API) BaoLeaseRevoke(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LeaseID string `json:"lease_id"`
	}
	if !parseJSON(r, w, &req) {
		return
	}
	err := a.store.RevokeLease(req.LeaseID)
	jsonOrErr(w, map[string]string{"status": "revoked"}, err)
}

func (a *API) BaoLeaseList(w http.ResponseWriter, r *http.Request) {
	prefix := strings.TrimPrefix(r.URL.Path, "/api/bao/leases/list/")
	data, err := a.store.ListLeases(prefix)
	jsonOrErr(w, map[string]interface{}{"leases": data}, err)
}

// --- Capabilities ---

func (a *API) BaoCapabilities(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
		Path  string `json:"path"`
	}
	if !parseJSON(r, w, &req) {
		return
	}
	caps, err := a.store.CheckCapabilities(req.Token, req.Path)
	jsonOrErr(w, map[string]interface{}{"capabilities": caps}, err)
}

func (a *API) BaoCapabilitiesSelf(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !parseJSON(r, w, &req) {
		return
	}
	caps, err := a.store.CheckCapabilitiesSelf(req.Path)
	jsonOrErr(w, map[string]interface{}{"capabilities": caps}, err)
}

// --- Plugins ---

func (a *API) BaoPlugins(w http.ResponseWriter, r *http.Request) {
	data, err := a.store.ListPlugins()
	jsonOrErr(w, data, err)
}

func (a *API) BaoPluginReload(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Plugin string `json:"plugin"`
	}
	if !parseJSON(r, w, &req) {
		return
	}
	err := a.store.ReloadPlugin(req.Plugin)
	jsonOrErr(w, map[string]string{"status": "reloaded"}, err)
}

// --- Wrapping ---

func (a *API) BaoWrap(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Data map[string]interface{} `json:"data"`
		TTL  string                 `json:"ttl"`
	}
	if !parseJSON(r, w, &req) {
		return
	}
	token, err := a.store.Wrap(req.Data, req.TTL)
	jsonOrErr(w, map[string]string{"wrap_token": token}, err)
}

func (a *API) BaoUnwrap(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if !parseJSON(r, w, &req) {
		return
	}
	data, err := a.store.Unwrap(req.Token)
	jsonOrErr(w, data, err)
}

func (a *API) BaoWrapLookup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if !parseJSON(r, w, &req) {
		return
	}
	data, err := a.store.WrapLookup(req.Token)
	jsonOrErr(w, data, err)
}

// --- Tools ---

func (a *API) BaoToolsRandom(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Bytes  int    `json:"bytes"`
		Format string `json:"format"`
	}
	if !parseJSON(r, w, &req) {
		return
	}
	data, err := a.store.ToolsRandom(req.Bytes, req.Format)
	jsonOrErr(w, map[string]string{"random_bytes": data}, err)
}

func (a *API) BaoToolsHash(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Input     string `json:"input"`
		Algorithm string `json:"algorithm"`
		Format    string `json:"format"`
	}
	if !parseJSON(r, w, &req) {
		return
	}
	sum, err := a.store.ToolsHash(req.Input, req.Algorithm, req.Format)
	jsonOrErr(w, map[string]string{"sum": sum}, err)
}

func (a *API) BaoSysMetrics(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	data, err := a.store.SysMetrics(format)
	jsonOrErr(w, data, err)
}

// --- Extended token operations ---

func (a *API) BaoTokenLookupSelf(w http.ResponseWriter, r *http.Request) {
	data, err := a.store.LookupTokenSelf()
	jsonOrErr(w, data, err)
}

func (a *API) BaoTokenRevokeSelf(w http.ResponseWriter, r *http.Request) {
	err := a.store.RevokeTokenSelf()
	jsonOrErr(w, map[string]string{"status": "revoked"}, err)
}

func (a *API) BaoTokenRenew(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token     string `json:"token"`
		Increment string `json:"increment"`
	}
	if !parseJSON(r, w, &req) {
		return
	}
	data, err := a.store.RenewToken(req.Token, req.Increment)
	jsonOrErr(w, data, err)
}

func (a *API) BaoTokenRenewSelf(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Increment string `json:"increment"`
	}
	parseJSON(r, w, &req)
	data, err := a.store.RenewTokenSelf(req.Increment)
	jsonOrErr(w, data, err)
}

func (a *API) BaoTokenOrphan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Policies []string          `json:"policies"`
		TTL      string            `json:"ttl"`
		Metadata map[string]string `json:"metadata"`
	}
	if !parseJSON(r, w, &req) {
		return
	}
	token, err := a.store.CreateOrphanToken(req.Policies, req.TTL, req.Metadata)
	jsonOrErr(w, map[string]string{"token": token}, err)
}

func (a *API) BaoTokenAccessors(w http.ResponseWriter, r *http.Request) {
	data, err := a.store.ListTokenAccessors()
	jsonOrErr(w, map[string]interface{}{"accessors": data}, err)
}

func (a *API) BaoTokenAccessor(w http.ResponseWriter, r *http.Request) {
	accessor := strings.TrimPrefix(r.URL.Path, "/api/bao/tokens/accessor/")
	switch r.Method {
	case http.MethodGet:
		data, err := a.store.LookupTokenAccessor(accessor)
		jsonOrErr(w, data, err)
	case http.MethodDelete:
		err := a.store.RevokeTokenAccessor(accessor)
		jsonOrErr(w, map[string]string{"status": "revoked"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) BaoTokenTidy(w http.ResponseWriter, r *http.Request) {
	err := a.store.TidyTokens()
	jsonOrErr(w, map[string]string{"status": "tidy_started"}, err)
}

// --- Userpass ---

func (a *API) BaoUserpassUsers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data, err := a.store.UserpassListUsers()
		jsonOrErr(w, map[string]interface{}{"users": data}, err)
	case http.MethodPost:
		var req struct {
			Username string   `json:"username"`
			Password string   `json:"password"`
			Policies []string `json:"policies"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.store.UserpassCreateUser(req.Username, req.Password, req.Policies)
		jsonOrErr(w, map[string]string{"status": "created"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) BaoUserpassUser(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimPrefix(r.URL.Path, "/api/bao/userpass/")
	if strings.HasSuffix(username, "/password") {
		name := strings.TrimSuffix(username, "/password")
		var req struct {
			Password string `json:"password"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.store.UserpassUpdatePassword(name, req.Password)
		jsonOrErr(w, map[string]string{"status": "updated"}, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		data, err := a.store.UserpassReadUser(username)
		jsonOrErr(w, data, err)
	case http.MethodDelete:
		err := a.store.UserpassDeleteUser(username)
		jsonOrErr(w, map[string]string{"status": "deleted"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) BaoUserpassLogin(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimPrefix(r.URL.Path, "/api/bao/userpass/login/")
	var req struct {
		Password string `json:"password"`
	}
	if !parseJSON(r, w, &req) {
		return
	}
	data, err := a.store.UserpassLogin(username, req.Password)
	jsonOrErr(w, data, err)
}

// --- Transit ---

func (a *API) BaoTransit(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/bao/transit/")
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 {
		errJSON(w, "usage: /api/bao/transit/{mount}/{action}[/{key}]", http.StatusBadRequest)
		return
	}
	mount := parts[0]
	action := parts[1]
	keyName := ""
	if len(parts) == 3 {
		keyName = parts[2]
	}

	switch action {
	case "keys":
		if keyName == "" {
			// List keys
			data, err := a.store.TransitListKeys(mount)
			jsonOrErr(w, map[string]interface{}{"keys": data}, err)
			return
		}
		switch r.Method {
		case http.MethodGet:
			data, err := a.store.TransitReadKey(mount, keyName)
			jsonOrErr(w, data, err)
		case http.MethodPost:
			var req struct {
				Type       string `json:"type"`
				Derived    bool   `json:"derived"`
				Exportable bool   `json:"exportable"`
			}
			if !parseJSON(r, w, &req) {
				return
			}
			err := a.store.TransitCreateKey(mount, keyName, req.Type, req.Derived, req.Exportable)
			jsonOrErr(w, map[string]string{"status": "created"}, err)
		case http.MethodDelete:
			err := a.store.TransitDeleteKey(mount, keyName)
			jsonOrErr(w, map[string]string{"status": "deleted"}, err)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case "encrypt":
		var req struct {
			Plaintext string `json:"plaintext"`
			Context   []byte `json:"context"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		ct, err := a.store.TransitEncrypt(mount, keyName, req.Plaintext, req.Context)
		jsonOrErr(w, map[string]string{"ciphertext": ct}, err)
	case "decrypt":
		var req struct {
			Ciphertext string `json:"ciphertext"`
			Context    []byte `json:"context"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		pt, err := a.store.TransitDecrypt(mount, keyName, req.Ciphertext, req.Context)
		jsonOrErr(w, map[string]string{"plaintext": pt}, err)
	case "sign":
		var req struct {
			Input         string `json:"input"`
			HashAlgorithm string `json:"hash_algorithm"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		sig, err := a.store.TransitSign(mount, keyName, req.Input, req.HashAlgorithm)
		jsonOrErr(w, map[string]string{"signature": sig}, err)
	case "verify":
		var req struct {
			Input         string `json:"input"`
			Signature     string `json:"signature"`
			HashAlgorithm string `json:"hash_algorithm"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		valid, err := a.store.TransitVerify(mount, keyName, req.Input, req.Signature, req.HashAlgorithm)
		jsonOrErr(w, map[string]interface{}{"valid": valid}, err)
	case "hmac":
		var req struct {
			Input     string `json:"input"`
			Algorithm string `json:"algorithm"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		hmac, err := a.store.TransitHMAC(mount, keyName, req.Input, req.Algorithm)
		jsonOrErr(w, map[string]string{"hmac": hmac}, err)
	case "datakey":
		keyType := "plaintext" // default: returns both plaintext + ciphertext
		if q := r.URL.Query().Get("type"); q != "" {
			keyType = q
		}
		data, err := a.store.TransitGenerateDataKey(mount, keyName, keyType)
		jsonOrErr(w, data, err)
	case "rewrap":
		var req struct {
			Ciphertext string `json:"ciphertext"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		ct, err := a.store.TransitRewrap(mount, keyName, req.Ciphertext)
		jsonOrErr(w, map[string]string{"ciphertext": ct}, err)
	case "rotate":
		err := a.store.TransitRotateKey(mount, keyName)
		jsonOrErr(w, map[string]string{"status": "rotated"}, err)
	case "random":
		var req struct {
			Bytes  int    `json:"bytes"`
			Format string `json:"format"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		data, err := a.store.TransitRandom(mount, req.Bytes, req.Format)
		jsonOrErr(w, map[string]string{"random_bytes": data}, err)
	case "hash":
		var req struct {
			Input     string `json:"input"`
			Algorithm string `json:"algorithm"`
			Format    string `json:"format"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		sum, err := a.store.TransitHash(mount, req.Input, req.Algorithm, req.Format)
		jsonOrErr(w, map[string]string{"sum": sum}, err)
	default:
		errJSON(w, "unknown transit action: "+action, http.StatusBadRequest)
	}
}

// --- TOTP ---

func (a *API) BaoTOTP(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/bao/totp/")
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 {
		errJSON(w, "usage: /api/bao/totp/{mount}/{action}[/{key}]", http.StatusBadRequest)
		return
	}
	mount, action := parts[0], parts[1]
	keyName := ""
	if len(parts) == 3 {
		keyName = parts[2]
	}

	switch action {
	case "keys":
		if keyName == "" {
			data, err := a.store.TOTPListKeys(mount)
			jsonOrErr(w, map[string]interface{}{"keys": data}, err)
			return
		}
		switch r.Method {
		case http.MethodGet:
			data, err := a.store.TOTPReadKey(mount, keyName)
			jsonOrErr(w, data, err)
		case http.MethodPost:
			var req map[string]interface{}
			if !parseJSON(r, w, &req) {
				return
			}
			err := a.store.TOTPCreateKey(mount, keyName, req)
			jsonOrErr(w, map[string]string{"status": "created"}, err)
		case http.MethodDelete:
			err := a.store.TOTPDeleteKey(mount, keyName)
			jsonOrErr(w, map[string]string{"status": "deleted"}, err)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case "code":
		switch r.Method {
		case http.MethodGet:
			code, err := a.store.TOTPGenerateCode(mount, keyName)
			jsonOrErr(w, map[string]string{"code": code}, err)
		case http.MethodPost:
			var req struct {
				Code string `json:"code"`
			}
			if !parseJSON(r, w, &req) {
				return
			}
			valid, err := a.store.TOTPValidateCode(mount, keyName, req.Code)
			jsonOrErr(w, map[string]interface{}{"valid": valid}, err)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	default:
		errJSON(w, "unknown totp action: "+action, http.StatusBadRequest)
	}
}

// --- SSH ---

func (a *API) BaoSSH(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/bao/ssh/")
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 {
		errJSON(w, "usage: /api/bao/ssh/{mount}/{action}[/{name}]", http.StatusBadRequest)
		return
	}
	mount, action := parts[0], parts[1]
	name := ""
	if len(parts) == 3 {
		name = parts[2]
	}

	switch action {
	case "sign":
		var req struct {
			PublicKey       string `json:"public_key"`
			CertType        string `json:"cert_type"`
			ValidPrincipals string `json:"valid_principals"`
			TTL             string `json:"ttl"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		signed, err := a.store.SSHSignKey(mount, name, req.PublicKey, req.CertType, req.ValidPrincipals, req.TTL)
		jsonOrErr(w, map[string]string{"signed_key": signed}, err)
	case "roles":
		if name == "" {
			data, err := a.store.SSHListRoles(mount)
			jsonOrErr(w, map[string]interface{}{"roles": data}, err)
			return
		}
		switch r.Method {
		case http.MethodGet:
			data, err := a.store.SSHReadRole(mount, name)
			jsonOrErr(w, data, err)
		case http.MethodPost:
			var req map[string]interface{}
			if !parseJSON(r, w, &req) {
				return
			}
			err := a.store.SSHCreateRole(mount, name, req)
			jsonOrErr(w, map[string]string{"status": "created"}, err)
		case http.MethodDelete:
			err := a.store.SSHDeleteRole(mount, name)
			jsonOrErr(w, map[string]string{"status": "deleted"}, err)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case "config":
		if name == "ca" {
			switch r.Method {
			case http.MethodGet:
				data, err := a.store.SSHReadCA(mount)
				jsonOrErr(w, data, err)
			case http.MethodPost:
				var req struct {
					PrivateKey string `json:"private_key"`
					PublicKey  string `json:"public_key"`
				}
				if !parseJSON(r, w, &req) {
					return
				}
				err := a.store.SSHConfigCA(mount, req.PrivateKey, req.PublicKey)
				jsonOrErr(w, map[string]string{"status": "configured"}, err)
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		}
	default:
		errJSON(w, "unknown ssh action: "+action, http.StatusBadRequest)
	}
}
