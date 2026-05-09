package service

import (
	"encoding/json"
	"fmt"
)

// --- Service CRUD (extended) ---

func (z *ZitiService) GetService(token, id string) (map[string]interface{}, error) {
	return z.getEntity(token, "services", id)
}

func (z *ZitiService) UpdateService(token, id string, data map[string]interface{}) error {
	return z.patchEntity(token, "services", id, data)
}

// --- Config CRUD (extended) ---

func (z *ZitiService) GetConfig(token, id string) (map[string]interface{}, error) {
	return z.getEntity(token, "configs", id)
}

func (z *ZitiService) UpdateConfig(token, id string, data interface{}) error {
	payload, _ := json.Marshal(map[string]interface{}{"data": data})
	_, err := z.apiCall(token, "PATCH", "/edge/management/v1/configs/"+id, payload)
	return err
}

func (z *ZitiService) ListConfigs(token string) ([]map[string]interface{}, error) {
	return z.listEntities(token, "configs", 100)
}

// --- ServicePolicy CRUD (extended) ---

func (z *ZitiService) GetServicePolicy(token, id string) (map[string]interface{}, error) {
	return z.getEntity(token, "service-policies", id)
}

func (z *ZitiService) UpdateServicePolicy(token, id string, data map[string]interface{}) error {
	return z.patchEntity(token, "service-policies", id, data)
}

func (z *ZitiService) ListServicePolicies(token string) ([]map[string]interface{}, error) {
	return z.listEntities(token, "service-policies", 100)
}

// --- Edge Router CRUD ---

func (z *ZitiService) CreateEdgeRouter(token, name string, attrs []string, tunnelerEnabled bool) (string, error) {
	payload, _ := json.Marshal(map[string]interface{}{
		"name":              name,
		"roleAttributes":    attrs,
		"isTunnelerEnabled": tunnelerEnabled,
	})
	resp, err := z.apiCall(token, "POST", "/edge/management/v1/edge-routers", payload)
	if err != nil {
		return "", err
	}
	return z.extractID(resp), nil
}

func (z *ZitiService) GetEdgeRouter(token, id string) (map[string]interface{}, error) {
	return z.getEntity(token, "edge-routers", id)
}

func (z *ZitiService) UpdateEdgeRouter(token, id string, attrs []string) error {
	payload, _ := json.Marshal(map[string]interface{}{"roleAttributes": attrs})
	_, err := z.apiCall(token, "PATCH", "/edge/management/v1/edge-routers/"+id, payload)
	return err
}

func (z *ZitiService) DeleteEdgeRouter(token, id string) error {
	_, err := z.apiCall(token, "DELETE", "/edge/management/v1/edge-routers/"+id, nil)
	return err
}

// --- Edge Router Policy CRUD ---

func (z *ZitiService) CreateEdgeRouterPolicy(token, name, semantic string, identityRoles, routerRoles []string) error {
	payload, _ := json.Marshal(map[string]interface{}{
		"name":            name,
		"semantic":        semantic,
		"identityRoles":   identityRoles,
		"edgeRouterRoles": routerRoles,
	})
	_, err := z.apiCall(token, "POST", "/edge/management/v1/edge-router-policies", payload)
	return err
}

func (z *ZitiService) UpdateEdgeRouterPolicy(token, id string, data map[string]interface{}) error {
	return z.patchEntity(token, "edge-router-policies", id, data)
}

func (z *ZitiService) DeleteEdgeRouterPolicy(token, id string) error {
	_, err := z.apiCall(token, "DELETE", "/edge/management/v1/edge-router-policies/"+id, nil)
	return err
}

func (z *ZitiService) ListEdgeRouterPolicies(token string) ([]map[string]interface{}, error) {
	return z.listEntities(token, "edge-router-policies", 100)
}

// --- Service Edge Router Policy CRUD ---

func (z *ZitiService) CreateServiceEdgeRouterPolicy(token, name, semantic string, serviceRoles, routerRoles []string) error {
	payload, _ := json.Marshal(map[string]interface{}{
		"name":            name,
		"semantic":        semantic,
		"serviceRoles":    serviceRoles,
		"edgeRouterRoles": routerRoles,
	})
	_, err := z.apiCall(token, "POST", "/edge/management/v1/service-edge-router-policies", payload)
	return err
}

func (z *ZitiService) DeleteServiceEdgeRouterPolicy(token, id string) error {
	_, err := z.apiCall(token, "DELETE", "/edge/management/v1/service-edge-router-policies/"+id, nil)
	return err
}

func (z *ZitiService) ListServiceEdgeRouterPolicies(token string) ([]map[string]interface{}, error) {
	return z.listEntities(token, "service-edge-router-policies", 100)
}

// --- Enrollment management ---

func (z *ZitiService) ListEnrollments(token string) ([]map[string]interface{}, error) {
	return z.listEntities(token, "enrollments", 100)
}

func (z *ZitiService) DeleteEnrollment(token, id string) error {
	_, err := z.apiCall(token, "DELETE", "/edge/management/v1/enrollments/"+id, nil)
	return err
}

// --- Sessions ---

func (z *ZitiService) ListSessions(token string) ([]map[string]interface{}, error) {
	return z.listEntities(token, "sessions", 100)
}

func (z *ZitiService) DeleteSession(token, id string) error {
	_, err := z.apiCall(token, "DELETE", "/edge/management/v1/sessions/"+id, nil)
	return err
}

// --- Posture Checks ---

func (z *ZitiService) ListPostureChecks(token string) ([]map[string]interface{}, error) {
	return z.listEntities(token, "posture-checks", 100)
}

// --- Certificate Authorities ---

func (z *ZitiService) ListCAs(token string) ([]map[string]interface{}, error) {
	return z.listEntities(token, "cas", 100)
}

// --- Role Attributes ---

func (z *ZitiService) ListRoleAttributes(token, entityType string) ([]string, error) {
	resp, err := z.apiCall(token, "GET", "/edge/management/v1/"+entityType+"-role-attributes", nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data []string `json:"data"`
	}
	json.Unmarshal(resp, &result)
	return result.Data, nil
}

// --- Informational ---

func (z *ZitiService) GetVersion(token string) (map[string]interface{}, error) {
	return z.getEntity(token, "../edge/client/v1/version", "")
}

// --- Terminator CRUD (traffic shaping) ---

func (z *ZitiService) CreateTerminator(token, serviceID, routerID, binding, address string, cost int32, precedence string) (string, error) {
	payload, _ := json.Marshal(map[string]interface{}{
		"service":    serviceID,
		"router":     routerID,
		"binding":    binding,
		"address":    address,
		"cost":       cost,
		"precedence": precedence,
	})
	resp, err := z.apiCall(token, "POST", "/edge/management/v1/terminators", payload)
	if err != nil {
		return "", err
	}
	return z.extractID(resp), nil
}

func (z *ZitiService) GetTerminator(token, id string) (map[string]interface{}, error) {
	return z.getEntity(token, "terminators", id)
}

func (z *ZitiService) UpdateTerminator(token, id string, cost *int32, precedence, address string) error {
	data := map[string]interface{}{}
	if cost != nil {
		data["cost"] = *cost
	}
	if precedence != "" {
		data["precedence"] = precedence
	}
	if address != "" {
		data["address"] = address
	}
	return z.patchEntity(token, "terminators", id, data)
}

func (z *ZitiService) DeleteTerminator(token, id string) error {
	_, err := z.apiCall(token, "DELETE", "/edge/management/v1/terminators/"+id, nil)
	return err
}

// --- Edge Router cost ---

func (z *ZitiService) UpdateEdgeRouterFull(token, id string, attrs []string, cost *uint16, tunnelerEnabled *bool) error {
	data := map[string]interface{}{}
	if attrs != nil {
		data["roleAttributes"] = attrs
	}
	if cost != nil {
		data["cost"] = *cost
	}
	if tunnelerEnabled != nil {
		data["isTunnelerEnabled"] = *tunnelerEnabled
	}
	return z.patchEntity(token, "edge-routers", id, data)
}

func (z *ZitiService) CreateEdgeRouterFull(token, name string, attrs []string, cost uint16, tunnelerEnabled bool) (string, error) {
	payload, _ := json.Marshal(map[string]interface{}{
		"name":              name,
		"roleAttributes":    attrs,
		"cost":              cost,
		"isTunnelerEnabled": tunnelerEnabled,
	})
	resp, err := z.apiCall(token, "POST", "/edge/management/v1/edge-routers", payload)
	if err != nil {
		return "", err
	}
	return z.extractID(resp), nil
}

// --- Auth Policies ---

func (z *ZitiService) ListAuthPolicies(token string) ([]map[string]interface{}, error) {
	return z.listEntities(token, "auth-policies", 100)
}

func (z *ZitiService) GetAuthPolicy(token, id string) (map[string]interface{}, error) {
	return z.getEntity(token, "auth-policies", id)
}

func (z *ZitiService) CreateAuthPolicy(token string, data map[string]interface{}) (string, error) {
	payload, _ := json.Marshal(data)
	resp, err := z.apiCall(token, "POST", "/edge/management/v1/auth-policies", payload)
	if err != nil {
		return "", err
	}
	return z.extractID(resp), nil
}

func (z *ZitiService) UpdateAuthPolicy(token, id string, data map[string]interface{}) error {
	return z.patchEntity(token, "auth-policies", id, data)
}

func (z *ZitiService) DeleteAuthPolicy(token, id string) error {
	_, err := z.apiCall(token, "DELETE", "/edge/management/v1/auth-policies/"+id, nil)
	return err
}

// --- Authenticators ---

func (z *ZitiService) ListAuthenticators(token string) ([]map[string]interface{}, error) {
	return z.listEntities(token, "authenticators", 100)
}

func (z *ZitiService) CreateAuthenticator(token string, data map[string]interface{}) (string, error) {
	payload, _ := json.Marshal(data)
	resp, err := z.apiCall(token, "POST", "/edge/management/v1/authenticators", payload)
	if err != nil {
		return "", err
	}
	return z.extractID(resp), nil
}

func (z *ZitiService) UpdateAuthenticator(token, id string, data map[string]interface{}) error {
	return z.patchEntity(token, "authenticators", id, data)
}

func (z *ZitiService) DeleteAuthenticator(token, id string) error {
	_, err := z.apiCall(token, "DELETE", "/edge/management/v1/authenticators/"+id, nil)
	return err
}

// --- Config Types ---

func (z *ZitiService) ListConfigTypes(token string) ([]map[string]interface{}, error) {
	return z.listEntities(token, "config-types", 100)
}

func (z *ZitiService) GetConfigType(token, id string) (map[string]interface{}, error) {
	return z.getEntity(token, "config-types", id)
}

func (z *ZitiService) CreateConfigType(token string, data map[string]interface{}) (string, error) {
	payload, _ := json.Marshal(data)
	resp, err := z.apiCall(token, "POST", "/edge/management/v1/config-types", payload)
	if err != nil {
		return "", err
	}
	return z.extractID(resp), nil
}

func (z *ZitiService) UpdateConfigType(token, id string, data map[string]interface{}) error {
	return z.patchEntity(token, "config-types", id, data)
}

// --- External JWT Signers ---

func (z *ZitiService) ListExtJWTSigners(token string) ([]map[string]interface{}, error) {
	return z.listEntities(token, "ext-jwt-signers", 100)
}

func (z *ZitiService) CreateExtJWTSigner(token string, data map[string]interface{}) (string, error) {
	payload, _ := json.Marshal(data)
	resp, err := z.apiCall(token, "POST", "/edge/management/v1/ext-jwt-signers", payload)
	if err != nil {
		return "", err
	}
	return z.extractID(resp), nil
}

func (z *ZitiService) UpdateExtJWTSigner(token, id string, data map[string]interface{}) error {
	return z.patchEntity(token, "ext-jwt-signers", id, data)
}

func (z *ZitiService) DeleteExtJWTSigner(token, id string) error {
	_, err := z.apiCall(token, "DELETE", "/edge/management/v1/ext-jwt-signers/"+id, nil)
	return err
}

// --- Transit Routers ---

func (z *ZitiService) ListTransitRouters(token string) ([]map[string]interface{}, error) {
	return z.listEntities(token, "transit-routers", 100)
}

func (z *ZitiService) CreateTransitRouter(token, name string, cost uint16) (string, error) {
	payload, _ := json.Marshal(map[string]interface{}{
		"name": name,
		"cost": cost,
	})
	resp, err := z.apiCall(token, "POST", "/edge/management/v1/transit-routers", payload)
	if err != nil {
		return "", err
	}
	return z.extractID(resp), nil
}

func (z *ZitiService) DeleteTransitRouter(token, id string) error {
	_, err := z.apiCall(token, "DELETE", "/edge/management/v1/transit-routers/"+id, nil)
	return err
}

// --- Posture Check CRUD ---

func (z *ZitiService) CreatePostureCheck(token string, data map[string]interface{}) (string, error) {
	payload, _ := json.Marshal(data)
	resp, err := z.apiCall(token, "POST", "/edge/management/v1/posture-checks", payload)
	if err != nil {
		return "", err
	}
	return z.extractID(resp), nil
}

func (z *ZitiService) GetPostureCheck(token, id string) (map[string]interface{}, error) {
	return z.getEntity(token, "posture-checks", id)
}

func (z *ZitiService) UpdatePostureCheck(token, id string, data map[string]interface{}) error {
	return z.patchEntity(token, "posture-checks", id, data)
}

func (z *ZitiService) DeletePostureCheck(token, id string) error {
	_, err := z.apiCall(token, "DELETE", "/edge/management/v1/posture-checks/"+id, nil)
	return err
}

func (z *ZitiService) ListPostureCheckTypes(token string) ([]map[string]interface{}, error) {
	return z.listEntities(token, "posture-check-types", 100)
}

// --- Identity Configs ---

func (z *ZitiService) UpdateIdentityConfigs(token, identityID string, serviceConfigs []map[string]string) error {
	payload, _ := json.Marshal(map[string]interface{}{"serviceConfigs": serviceConfigs})
	_, err := z.apiCall(token, "PUT", "/edge/management/v1/identities/"+identityID+"/service-configs", payload)
	return err
}

// --- Re-enrollment ---

func (z *ZitiService) ReEnrollIdentity(token, identityID string) (string, error) {
	resp, err := z.apiCall(token, "POST", "/edge/management/v1/identities/"+identityID+"/re-enroll", nil)
	if err != nil {
		return "", err
	}
	return string(resp), nil
}

// --- Summary ---

func (z *ZitiService) Summary(token string) (map[string]interface{}, error) {
	return z.getEntity(token, "summary", "")
}

// --- Policy Advisor ---

func (z *ZitiService) PolicyAdvisorIdentity(token, identityID string) ([]map[string]interface{}, error) {
	resp, err := z.apiCall(token, "GET", "/edge/management/v1/identities/"+identityID+"/policy-advice/services", nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data []map[string]interface{} `json:"data"`
	}
	json.Unmarshal(resp, &result)
	return result.Data, nil
}

func (z *ZitiService) PolicyAdvisorService(token, serviceID string) ([]map[string]interface{}, error) {
	resp, err := z.apiCall(token, "GET", "/edge/management/v1/services/"+serviceID+"/policy-advice/identities", nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data []map[string]interface{} `json:"data"`
	}
	json.Unmarshal(resp, &result)
	return result.Data, nil
}

// --- API Sessions ---

func (z *ZitiService) ListAPISessions(token string) ([]map[string]interface{}, error) {
	return z.listEntities(token, "api-sessions", 100)
}

func (z *ZitiService) DeleteAPISession(token, id string) error {
	_, err := z.apiCall(token, "DELETE", "/edge/management/v1/api-sessions/"+id, nil)
	return err
}

// --- Certificate Authorities CRUD ---

func (z *ZitiService) CreateCA(token string, data map[string]interface{}) (string, error) {
	payload, _ := json.Marshal(data)
	resp, err := z.apiCall(token, "POST", "/edge/management/v1/cas", payload)
	if err != nil {
		return "", err
	}
	return z.extractID(resp), nil
}

func (z *ZitiService) GetCA(token, id string) (map[string]interface{}, error) {
	return z.getEntity(token, "cas", id)
}

func (z *ZitiService) UpdateCA(token, id string, data map[string]interface{}) error {
	return z.patchEntity(token, "cas", id, data)
}

func (z *ZitiService) DeleteCA(token, id string) error {
	_, err := z.apiCall(token, "DELETE", "/edge/management/v1/cas/"+id, nil)
	return err
}

func (z *ZitiService) VerifyCA(token, id, certificate string) error {
	payload, _ := json.Marshal(map[string]interface{}{"certificate": certificate})
	_, err := z.apiCall(token, "POST", "/edge/management/v1/cas/"+id+"/verify", payload)
	return err
}

// --- Database management ---

func (z *ZitiService) DatabaseCheckIntegrity(token string) (map[string]interface{}, error) {
	resp, err := z.apiCall(token, "POST", "/edge/management/v1/database/check-data-integrity", nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data map[string]interface{} `json:"data"`
	}
	json.Unmarshal(resp, &result)
	return result.Data, nil
}

func (z *ZitiService) DatabaseFixIntegrity(token string) (map[string]interface{}, error) {
	resp, err := z.apiCall(token, "POST", "/edge/management/v1/database/fix-data-integrity", nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data map[string]interface{} `json:"data"`
	}
	json.Unmarshal(resp, &result)
	return result.Data, nil
}

func (z *ZitiService) DatabaseSnapshot(token string) ([]byte, error) {
	return z.apiCall(token, "POST", "/edge/management/v1/database/snapshot", nil)
}

// --- Traceroute ---

func (z *ZitiService) Traceroute(token, serviceID string) (map[string]interface{}, error) {
	resp, err := z.apiCall(token, "GET", "/edge/management/v1/services/"+serviceID+"/terminators", nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data []map[string]interface{} `json:"data"`
	}
	json.Unmarshal(resp, &result)
	return map[string]interface{}{"terminators": result.Data}, nil
}

// --- Validation ---

func (z *ZitiService) ValidateIdentityServices(token, identityID string) ([]map[string]interface{}, error) {
	resp, err := z.apiCall(token, "GET", "/edge/management/v1/identities/"+identityID+"/services", nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data []map[string]interface{} `json:"data"`
	}
	json.Unmarshal(resp, &result)
	return result.Data, nil
}

func (z *ZitiService) ValidateIdentityEdgeRouters(token, identityID string) ([]map[string]interface{}, error) {
	resp, err := z.apiCall(token, "GET", "/edge/management/v1/identities/"+identityID+"/edge-routers", nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data []map[string]interface{} `json:"data"`
	}
	json.Unmarshal(resp, &result)
	return result.Data, nil
}

func (z *ZitiService) ValidateServiceTerminators(token, serviceID string) ([]map[string]interface{}, error) {
	resp, err := z.apiCall(token, "GET", "/edge/management/v1/services/"+serviceID+"/terminators", nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data []map[string]interface{} `json:"data"`
	}
	json.Unmarshal(resp, &result)
	return result.Data, nil
}

func (z *ZitiService) ValidateServiceEdgeRouters(token, serviceID string) ([]map[string]interface{}, error) {
	resp, err := z.apiCall(token, "GET", "/edge/management/v1/services/"+serviceID+"/edge-routers", nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data []map[string]interface{} `json:"data"`
	}
	json.Unmarshal(resp, &result)
	return result.Data, nil
}

func (z *ZitiService) ValidateRouterIdentities(token, routerID string) ([]map[string]interface{}, error) {
	resp, err := z.apiCall(token, "GET", "/edge/management/v1/edge-routers/"+routerID+"/identities", nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data []map[string]interface{} `json:"data"`
	}
	json.Unmarshal(resp, &result)
	return result.Data, nil
}

// --- Network JWTs ---

func (z *ZitiService) ListNetworkJWTs(token string) ([]map[string]interface{}, error) {
	return z.listEntities(token, "network-jwts", 100)
}

// --- Generic helpers ---

func (z *ZitiService) getEntity(token, entityType, id string) (map[string]interface{}, error) {
	path := "/edge/management/v1/" + entityType
	if id != "" {
		path += "/" + id
	}
	resp, err := z.apiCall(token, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data map[string]interface{} `json:"data"`
	}
	json.Unmarshal(resp, &result)
	return result.Data, nil
}

func (z *ZitiService) patchEntity(token, entityType, id string, data map[string]interface{}) error {
	payload, _ := json.Marshal(data)
	_, err := z.apiCall(token, "PATCH", fmt.Sprintf("/edge/management/v1/%s/%s", entityType, id), payload)
	return err
}

func (z *ZitiService) listEntities(token, entityType string, limit int) ([]map[string]interface{}, error) {
	resp, err := z.apiCall(token, "GET", fmt.Sprintf("/edge/management/v1/%s?limit=%d", entityType, limit), nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data []map[string]interface{} `json:"data"`
	}
	json.Unmarshal(resp, &result)
	return result.Data, nil
}
