package main

import (
	"net/http"
	"strings"
)

// --- CA CRUD ---

func (a *API) ZitiCA(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/ziti/cas/")
	token := a.zitiToken(w)
	if token == "" {
		return
	}

	if strings.HasSuffix(id, "/verify") {
		caID := strings.TrimSuffix(id, "/verify")
		var req struct {
			Certificate string `json:"certificate"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.ziti.VerifyCA(token, caID, req.Certificate)
		jsonOrErr(w, map[string]string{"status": "verified"}, err)
		return
	}

	switch r.Method {
	case http.MethodGet:
		data, err := a.ziti.GetCA(token, id)
		jsonOrErr(w, data, err)
	case http.MethodPost:
		var req map[string]interface{}
		if !parseJSON(r, w, &req) {
			return
		}
		newID, err := a.ziti.CreateCA(token, req)
		jsonOrErr(w, map[string]string{"id": newID}, err)
	case http.MethodPatch:
		var req map[string]interface{}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.ziti.UpdateCA(token, id, req)
		jsonOrErr(w, map[string]string{"status": "updated"}, err)
	case http.MethodDelete:
		err := a.ziti.DeleteCA(token, id)
		jsonOrErr(w, map[string]string{"status": "deleted"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Database management ---

func (a *API) ZitiDBCheck(w http.ResponseWriter, r *http.Request) {
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	data, err := a.ziti.DatabaseCheckIntegrity(token)
	jsonOrErr(w, data, err)
}

func (a *API) ZitiDBFix(w http.ResponseWriter, r *http.Request) {
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	data, err := a.ziti.DatabaseFixIntegrity(token)
	jsonOrErr(w, data, err)
}

func (a *API) ZitiDBSnapshot(w http.ResponseWriter, r *http.Request) {
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	data, err := a.ziti.DatabaseSnapshot(token)
	if err != nil {
		errJSON(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=ziti-snapshot.db")
	w.Write(data)
}

// --- Network JWTs ---

func (a *API) ZitiNetworkJWTs(w http.ResponseWriter, r *http.Request) {
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	data, err := a.ziti.ListNetworkJWTs(token)
	jsonOrErr(w, data, err)
}

// --- Validation ---

func (a *API) ZitiValidateIdentity(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/ziti/validate/identity/")
	token := a.zitiToken(w)
	if token == "" {
		return
	}

	resource := r.URL.Query().Get("resource")
	switch resource {
	case "edge-routers":
		data, err := a.ziti.ValidateIdentityEdgeRouters(token, id)
		jsonOrErr(w, data, err)
	default: // services
		data, err := a.ziti.ValidateIdentityServices(token, id)
		jsonOrErr(w, data, err)
	}
}

func (a *API) ZitiValidateService(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/ziti/validate/service/")
	token := a.zitiToken(w)
	if token == "" {
		return
	}

	resource := r.URL.Query().Get("resource")
	switch resource {
	case "edge-routers":
		data, err := a.ziti.ValidateServiceEdgeRouters(token, id)
		jsonOrErr(w, data, err)
	default: // terminators
		data, err := a.ziti.ValidateServiceTerminators(token, id)
		jsonOrErr(w, data, err)
	}
}

func (a *API) ZitiValidateRouter(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/ziti/validate/router/")
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	data, err := a.ziti.ValidateRouterIdentities(token, id)
	jsonOrErr(w, data, err)
}
