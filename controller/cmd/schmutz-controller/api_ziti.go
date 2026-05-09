package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

// --- Ziti Identity handlers ---

func (a *API) ZitiIdentities(w http.ResponseWriter, r *http.Request) {
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	switch r.Method {
	case http.MethodGet:
		data, err := a.ziti.ListIdentities(token, 200)
		jsonOrErr(w, data, err)
	case http.MethodPost:
		var req struct {
			Name       string                 `json:"name"`
			Attributes []string               `json:"attributes"`
			AppData    map[string]interface{} `json:"app_data"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		id, jwt, err := a.ziti.CreateIdentity(req.Name, req.Attributes, req.AppData)
		jsonOrErr(w, map[string]string{"id": id, "jwt": jwt}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) ZitiIdentity(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/ziti/identities/")
	if id == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	token := a.zitiToken(w)
	if token == "" {
		return
	}

	// Check for sub-resources
	if strings.HasSuffix(id, "/policies") {
		zitiID := strings.TrimSuffix(id, "/policies")
		routers, err := a.ziti.GetIdentityPolicies(token, zitiID)
		jsonOrErr(w, map[string]interface{}{"routers": routers}, err)
		return
	}
	if strings.HasSuffix(id, "/service-configs") {
		zitiID := strings.TrimSuffix(id, "/service-configs")
		var req struct {
			ServiceConfigs []map[string]string `json:"service_configs"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.ziti.UpdateIdentityConfigs(token, zitiID, req.ServiceConfigs)
		jsonOrErr(w, map[string]string{"status": "updated"}, err)
		return
	}
	if strings.HasSuffix(id, "/re-enroll") {
		zitiID := strings.TrimSuffix(id, "/re-enroll")
		resp, err := a.ziti.ReEnrollIdentity(token, zitiID)
		jsonOrErr(w, map[string]string{"result": resp}, err)
		return
	}
	if strings.HasSuffix(id, "/policy-advice") {
		zitiID := strings.TrimSuffix(id, "/policy-advice")
		data, err := a.ziti.PolicyAdvisorIdentity(token, zitiID)
		jsonOrErr(w, data, err)
		return
	}

	switch r.Method {
	case http.MethodGet:
		zitiID, attrs, appData, err := a.ziti.GetIdentityByName(token, id)
		jsonOrErr(w, map[string]interface{}{"id": zitiID, "attributes": attrs, "app_data": appData}, err)
	case http.MethodPatch:
		var req struct {
			Attributes []string               `json:"attributes"`
			AppData    map[string]interface{} `json:"app_data"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		if req.Attributes != nil {
			if err := a.ziti.UpdateIdentity(token, id, req.Attributes); err != nil {
				errJSON(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		if req.AppData != nil {
			if err := a.ziti.UpdateIdentityAppData(token, id, req.AppData); err != nil {
				errJSON(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		okJSON(w, "updated")
	case http.MethodDelete:
		err := a.ziti.DeleteIdentity(token, id)
		jsonOrErr(w, map[string]string{"status": "deleted"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Ziti Service handlers ---

func (a *API) ZitiServices(w http.ResponseWriter, r *http.Request) {
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	switch r.Method {
	case http.MethodGet:
		data, err := a.ziti.ListServices(token)
		jsonOrErr(w, data, err)
	case http.MethodPost:
		var req struct {
			Name       string   `json:"name"`
			ConfigIDs  []string `json:"config_ids"`
			Attributes []string `json:"attributes"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		id, err := a.ziti.CreateService(token, req.Name, req.ConfigIDs, req.Attributes)
		jsonOrErr(w, map[string]string{"id": id}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) ZitiService(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/ziti/services/")
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	switch r.Method {
	case http.MethodGet:
		data, err := a.ziti.GetService(token, id)
		jsonOrErr(w, data, err)
	case http.MethodPatch:
		var req map[string]interface{}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.ziti.UpdateService(token, id, req)
		jsonOrErr(w, map[string]string{"status": "updated"}, err)
	case http.MethodDelete:
		err := a.ziti.DeleteService(token, id)
		jsonOrErr(w, map[string]string{"status": "deleted"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Ziti Config handlers ---

func (a *API) ZitiConfigs(w http.ResponseWriter, r *http.Request) {
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	switch r.Method {
	case http.MethodGet:
		data, err := a.ziti.ListConfigs(token)
		jsonOrErr(w, data, err)
	case http.MethodPost:
		var req struct {
			Name         string      `json:"name"`
			ConfigTypeID string      `json:"config_type_id"`
			Data         interface{} `json:"data"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		id, err := a.ziti.CreateConfig(token, req.Name, req.ConfigTypeID, req.Data)
		jsonOrErr(w, map[string]string{"id": id}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) ZitiConfig(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/ziti/configs/")
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	switch r.Method {
	case http.MethodGet:
		data, err := a.ziti.GetConfig(token, id)
		jsonOrErr(w, data, err)
	case http.MethodPatch:
		var req interface{}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.ziti.UpdateConfig(token, id, req)
		jsonOrErr(w, map[string]string{"status": "updated"}, err)
	case http.MethodDelete:
		err := a.ziti.DeleteConfig(token, id)
		jsonOrErr(w, map[string]string{"status": "deleted"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) ZitiConfigType(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/ziti/config-types/")
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	id := a.ziti.GetConfigTypeID(token, name)
	json.NewEncoder(w).Encode(map[string]string{"id": id, "name": name})
}

// --- Ziti Service Policy handlers ---

func (a *API) ZitiServicePolicies(w http.ResponseWriter, r *http.Request) {
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	switch r.Method {
	case http.MethodGet:
		data, err := a.ziti.ListServicePolicies(token)
		jsonOrErr(w, data, err)
	case http.MethodPost:
		var req struct {
			Name          string   `json:"name"`
			Type          string   `json:"type"`
			Semantic      string   `json:"semantic"`
			IdentityRoles []string `json:"identity_roles"`
			ServiceRoles  []string `json:"service_roles"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.ziti.CreateServicePolicy(token, req.Name, req.Type, req.Semantic, req.IdentityRoles, req.ServiceRoles)
		jsonOrErr(w, map[string]string{"status": "created"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) ZitiServicePolicy(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/ziti/service-policies/")
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	switch r.Method {
	case http.MethodGet:
		data, err := a.ziti.GetServicePolicy(token, id)
		jsonOrErr(w, data, err)
	case http.MethodPatch:
		var req map[string]interface{}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.ziti.UpdateServicePolicy(token, id, req)
		jsonOrErr(w, map[string]string{"status": "updated"}, err)
	case http.MethodDelete:
		err := a.ziti.DeleteServicePolicy(token, id)
		jsonOrErr(w, map[string]string{"status": "deleted"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Ziti Router handlers ---

func (a *API) ZitiRouters(w http.ResponseWriter, r *http.Request) {
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	switch r.Method {
	case http.MethodGet:
		data, err := a.ziti.ListRouters(token)
		jsonOrErr(w, data, err)
	case http.MethodPost:
		var req struct {
			Name            string   `json:"name"`
			Attributes      []string `json:"attributes"`
			TunnelerEnabled bool     `json:"tunneler_enabled"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		id, err := a.ziti.CreateEdgeRouter(token, req.Name, req.Attributes, req.TunnelerEnabled)
		jsonOrErr(w, map[string]string{"id": id}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) ZitiRouter(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/ziti/routers/")
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	switch r.Method {
	case http.MethodGet:
		data, err := a.ziti.GetEdgeRouter(token, id)
		jsonOrErr(w, data, err)
	case http.MethodPatch:
		var req struct {
			Attributes []string `json:"attributes"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.ziti.UpdateEdgeRouter(token, id, req.Attributes)
		jsonOrErr(w, map[string]string{"status": "updated"}, err)
	case http.MethodDelete:
		err := a.ziti.DeleteEdgeRouter(token, id)
		jsonOrErr(w, map[string]string{"status": "deleted"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Ziti Edge Router Policies ---

func (a *API) ZitiRouterPolicies(w http.ResponseWriter, r *http.Request) {
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	switch r.Method {
	case http.MethodGet:
		data, err := a.ziti.ListEdgeRouterPolicies(token)
		jsonOrErr(w, data, err)
	case http.MethodPost:
		var req struct {
			Name          string   `json:"name"`
			Semantic      string   `json:"semantic"`
			IdentityRoles []string `json:"identity_roles"`
			RouterRoles   []string `json:"router_roles"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.ziti.CreateEdgeRouterPolicy(token, req.Name, req.Semantic, req.IdentityRoles, req.RouterRoles)
		jsonOrErr(w, map[string]string{"status": "created"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) ZitiRouterPolicy(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/ziti/router-policies/")
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	switch r.Method {
	case http.MethodPatch:
		var req map[string]interface{}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.ziti.UpdateEdgeRouterPolicy(token, id, req)
		jsonOrErr(w, map[string]string{"status": "updated"}, err)
	case http.MethodDelete:
		err := a.ziti.DeleteEdgeRouterPolicy(token, id)
		jsonOrErr(w, map[string]string{"status": "deleted"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Ziti Service Edge Router Policies ---

func (a *API) ZitiServiceRouterPolicies(w http.ResponseWriter, r *http.Request) {
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	switch r.Method {
	case http.MethodGet:
		data, err := a.ziti.ListServiceEdgeRouterPolicies(token)
		jsonOrErr(w, data, err)
	case http.MethodPost:
		var req struct {
			Name         string   `json:"name"`
			Semantic     string   `json:"semantic"`
			ServiceRoles []string `json:"service_roles"`
			RouterRoles  []string `json:"router_roles"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.ziti.CreateServiceEdgeRouterPolicy(token, req.Name, req.Semantic, req.ServiceRoles, req.RouterRoles)
		jsonOrErr(w, map[string]string{"status": "created"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) ZitiServiceRouterPolicy(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/ziti/service-router-policies/")
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	err := a.ziti.DeleteServiceEdgeRouterPolicy(token, id)
	jsonOrErr(w, map[string]string{"status": "deleted"}, err)
}

// --- Ziti read-only ---

func (a *API) ZitiEnrollments(w http.ResponseWriter, r *http.Request) {
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	data, err := a.ziti.ListEnrollments(token)
	jsonOrErr(w, data, err)
}

func (a *API) ZitiEnrollment(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/ziti/enrollments/")
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	err := a.ziti.DeleteEnrollment(token, id)
	jsonOrErr(w, map[string]string{"status": "deleted"}, err)
}

func (a *API) ZitiSessions(w http.ResponseWriter, r *http.Request) {
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	data, err := a.ziti.ListSessions(token)
	jsonOrErr(w, data, err)
}

func (a *API) ZitiSession(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/ziti/sessions/")
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	err := a.ziti.DeleteSession(token, id)
	jsonOrErr(w, map[string]string{"status": "deleted"}, err)
}

func (a *API) ZitiTerminators(w http.ResponseWriter, r *http.Request) {
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	switch r.Method {
	case http.MethodGet:
		filter := r.URL.Query().Get("filter")
		data, err := a.ziti.ListTerminators(token, filter)
		jsonOrErr(w, data, err)
	case http.MethodPost:
		var req struct {
			ServiceID  string `json:"service_id"`
			RouterID   string `json:"router_id"`
			Binding    string `json:"binding"`
			Address    string `json:"address"`
			Cost       int32  `json:"cost"`
			Precedence string `json:"precedence"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		id, err := a.ziti.CreateTerminator(token, req.ServiceID, req.RouterID, req.Binding, req.Address, req.Cost, req.Precedence)
		jsonOrErr(w, map[string]string{"id": id}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) ZitiCAs(w http.ResponseWriter, r *http.Request) {
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	data, err := a.ziti.ListCAs(token)
	jsonOrErr(w, data, err)
}

func (a *API) ZitiRoleAttributes(w http.ResponseWriter, r *http.Request) {
	entityType := strings.TrimPrefix(r.URL.Path, "/api/ziti/role-attributes/")
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	data, err := a.ziti.ListRoleAttributes(token, entityType)
	jsonOrErr(w, data, err)
}

func (a *API) ZitiVersion(w http.ResponseWriter, r *http.Request) {
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	data, err := a.ziti.GetVersion(token)
	jsonOrErr(w, data, err)
}

func (a *API) ZitiFind(w http.ResponseWriter, r *http.Request) {
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/api/ziti/find/"), "/", 2)
	if len(parts) != 2 {
		http.Error(w, "usage: /api/ziti/find/{type}/{name}", http.StatusBadRequest)
		return
	}
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	id, err := a.ziti.FindByName(token, parts[0], parts[1])
	jsonOrErr(w, map[string]string{"id": id}, err)
}

// --- Terminator CRUD (traffic shaping) ---

func (a *API) ZitiTerminator(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/ziti/terminators/")
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	switch r.Method {
	case http.MethodGet:
		data, err := a.ziti.GetTerminator(token, id)
		jsonOrErr(w, data, err)
	case http.MethodPatch:
		var req struct {
			Cost       *int32 `json:"cost"`
			Precedence string `json:"precedence"`
			Address    string `json:"address"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.ziti.UpdateTerminator(token, id, req.Cost, req.Precedence, req.Address)
		jsonOrErr(w, map[string]string{"status": "updated"}, err)
	case http.MethodDelete:
		err := a.ziti.DeleteTerminator(token, id)
		jsonOrErr(w, map[string]string{"status": "deleted"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Summary ---

func (a *API) ZitiSummary(w http.ResponseWriter, r *http.Request) {
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	data, err := a.ziti.Summary(token)
	jsonOrErr(w, data, err)
}

// --- Auth Policies ---

func (a *API) ZitiAuthPolicies(w http.ResponseWriter, r *http.Request) {
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	switch r.Method {
	case http.MethodGet:
		data, err := a.ziti.ListAuthPolicies(token)
		jsonOrErr(w, data, err)
	case http.MethodPost:
		var req map[string]interface{}
		if !parseJSON(r, w, &req) {
			return
		}
		id, err := a.ziti.CreateAuthPolicy(token, req)
		jsonOrErr(w, map[string]string{"id": id}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) ZitiAuthPolicy(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/ziti/auth-policies/")
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	switch r.Method {
	case http.MethodGet:
		data, err := a.ziti.GetAuthPolicy(token, id)
		jsonOrErr(w, data, err)
	case http.MethodPatch:
		var req map[string]interface{}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.ziti.UpdateAuthPolicy(token, id, req)
		jsonOrErr(w, map[string]string{"status": "updated"}, err)
	case http.MethodDelete:
		err := a.ziti.DeleteAuthPolicy(token, id)
		jsonOrErr(w, map[string]string{"status": "deleted"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Authenticators ---

func (a *API) ZitiAuthenticators(w http.ResponseWriter, r *http.Request) {
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	switch r.Method {
	case http.MethodGet:
		data, err := a.ziti.ListAuthenticators(token)
		jsonOrErr(w, data, err)
	case http.MethodPost:
		var req map[string]interface{}
		if !parseJSON(r, w, &req) {
			return
		}
		id, err := a.ziti.CreateAuthenticator(token, req)
		jsonOrErr(w, map[string]string{"id": id}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) ZitiAuthenticator(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/ziti/authenticators/")
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	switch r.Method {
	case http.MethodPatch:
		var req map[string]interface{}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.ziti.UpdateAuthenticator(token, id, req)
		jsonOrErr(w, map[string]string{"status": "updated"}, err)
	case http.MethodDelete:
		err := a.ziti.DeleteAuthenticator(token, id)
		jsonOrErr(w, map[string]string{"status": "deleted"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Config Types ---

func (a *API) ZitiConfigTypes(w http.ResponseWriter, r *http.Request) {
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	switch r.Method {
	case http.MethodGet:
		data, err := a.ziti.ListConfigTypes(token)
		jsonOrErr(w, data, err)
	case http.MethodPost:
		var req map[string]interface{}
		if !parseJSON(r, w, &req) {
			return
		}
		id, err := a.ziti.CreateConfigType(token, req)
		jsonOrErr(w, map[string]string{"id": id}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- External JWT Signers ---

func (a *API) ZitiExtJWTSigners(w http.ResponseWriter, r *http.Request) {
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	switch r.Method {
	case http.MethodGet:
		data, err := a.ziti.ListExtJWTSigners(token)
		jsonOrErr(w, data, err)
	case http.MethodPost:
		var req map[string]interface{}
		if !parseJSON(r, w, &req) {
			return
		}
		id, err := a.ziti.CreateExtJWTSigner(token, req)
		jsonOrErr(w, map[string]string{"id": id}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) ZitiExtJWTSigner(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/ziti/ext-jwt-signers/")
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	switch r.Method {
	case http.MethodPatch:
		var req map[string]interface{}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.ziti.UpdateExtJWTSigner(token, id, req)
		jsonOrErr(w, map[string]string{"status": "updated"}, err)
	case http.MethodDelete:
		err := a.ziti.DeleteExtJWTSigner(token, id)
		jsonOrErr(w, map[string]string{"status": "deleted"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- Transit Routers ---

func (a *API) ZitiTransitRouters(w http.ResponseWriter, r *http.Request) {
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	switch r.Method {
	case http.MethodGet:
		data, err := a.ziti.ListTransitRouters(token)
		jsonOrErr(w, data, err)
	case http.MethodPost:
		var req struct {
			Name string `json:"name"`
			Cost uint16 `json:"cost"`
		}
		if !parseJSON(r, w, &req) {
			return
		}
		id, err := a.ziti.CreateTransitRouter(token, req.Name, req.Cost)
		jsonOrErr(w, map[string]string{"id": id}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) ZitiTransitRouter(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/ziti/transit-routers/")
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	err := a.ziti.DeleteTransitRouter(token, id)
	jsonOrErr(w, map[string]string{"status": "deleted"}, err)
}

// --- Posture Checks ---

func (a *API) ZitiPostureChecks(w http.ResponseWriter, r *http.Request) {
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	switch r.Method {
	case http.MethodGet:
		data, err := a.ziti.ListPostureChecks(token)
		jsonOrErr(w, data, err)
	case http.MethodPost:
		var req map[string]interface{}
		if !parseJSON(r, w, &req) {
			return
		}
		id, err := a.ziti.CreatePostureCheck(token, req)
		jsonOrErr(w, map[string]string{"id": id}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) ZitiPostureCheck(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/ziti/posture-checks/")
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	switch r.Method {
	case http.MethodGet:
		data, err := a.ziti.GetPostureCheck(token, id)
		jsonOrErr(w, data, err)
	case http.MethodPatch:
		var req map[string]interface{}
		if !parseJSON(r, w, &req) {
			return
		}
		err := a.ziti.UpdatePostureCheck(token, id, req)
		jsonOrErr(w, map[string]string{"status": "updated"}, err)
	case http.MethodDelete:
		err := a.ziti.DeletePostureCheck(token, id)
		jsonOrErr(w, map[string]string{"status": "deleted"}, err)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) ZitiPostureCheckTypes(w http.ResponseWriter, r *http.Request) {
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	data, err := a.ziti.ListPostureCheckTypes(token)
	jsonOrErr(w, data, err)
}

// --- API Sessions ---

func (a *API) ZitiAPISessions(w http.ResponseWriter, r *http.Request) {
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	data, err := a.ziti.ListAPISessions(token)
	jsonOrErr(w, data, err)
}

func (a *API) ZitiAPISession(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/ziti/api-sessions/")
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	err := a.ziti.DeleteAPISession(token, id)
	jsonOrErr(w, map[string]string{"status": "deleted"}, err)
}

// --- Policy Advisor ---

func (a *API) ZitiPolicyAdvisorIdentity(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/ziti/policy-advisor/identity/")
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	data, err := a.ziti.PolicyAdvisorIdentity(token, id)
	jsonOrErr(w, data, err)
}

func (a *API) ZitiPolicyAdvisorService(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/ziti/policy-advisor/service/")
	token := a.zitiToken(w)
	if token == "" {
		return
	}
	data, err := a.ziti.PolicyAdvisorService(token, id)
	jsonOrErr(w, data, err)
}
