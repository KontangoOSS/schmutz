package service

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ZitiService handles all interactions with the Ziti management API.
type ZitiService struct {
	Addr         string // e.g., "localhost:1280" or "ziti-mgmt.tango:443"
	OIDCAddr     string // public controller for OIDC token exchange e.g. "ctrl-1.konoss.org:443"
	User         string
	Pass         string
	RefreshToken string // OIDC refresh token — required for pre11 external connections
	// Transport routes all management API calls through the Ziti overlay when set.
	// This removes the need for port 1280 to be publicly accessible.
	Transport *ZitiTransport

	// cached token state — avoids a round-trip auth call on every management API request
	cacheMu      sync.Mutex
	cachedToken  string
	cacheExpires time.Time
}

func (z *ZitiService) client() *http.Client {
	if z.Transport != nil {
		return z.Transport.HTTPClient()
	}
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
}

// Authenticate returns a cached management session token, refreshing it when
// it has less than 2 minutes of TTL remaining. Tokens are valid for 30 minutes
// in Ziti's default configuration.
func (z *ZitiService) Authenticate() (string, error) {
	z.cacheMu.Lock()
	if z.cachedToken != "" && time.Now().Before(z.cacheExpires) {
		t := z.cachedToken
		z.cacheMu.Unlock()
		return t, nil
	}
	z.cacheMu.Unlock()

	var (
		token string
		err   error
	)
	if z.RefreshToken != "" {
		token, err = z.authenticateOIDC()
	} else {
		token, err = z.authenticatePassword()
	}
	if err != nil {
		return "", err
	}

	z.cacheMu.Lock()
	z.cachedToken = token
	z.cacheExpires = time.Now().Add(28 * time.Minute) // Ziti tokens live 30m; refresh 2m early
	z.cacheMu.Unlock()

	return token, nil
}

// InvalidateToken clears the cached token, forcing a fresh auth on the next call.
// Call this when an API request returns 401.
func (z *ZitiService) InvalidateToken() {
	z.cacheMu.Lock()
	z.cachedToken = ""
	z.cacheExpires = time.Time{}
	z.cacheMu.Unlock()
}

// authenticateOIDC exchanges a refresh token for a Bearer token via the OIDC endpoint.
// Required for pre11 controllers when connecting externally.
// Uses OIDCAddr if set (public controller), otherwise falls back to Addr.
func (z *ZitiService) authenticateOIDC() (string, error) {
	oidcHost := z.OIDCAddr
	if oidcHost == "" {
		oidcHost = z.Addr
	}
	body := fmt.Sprintf("grant_type=refresh_token&refresh_token=%s&client_id=native", z.RefreshToken)
	// OIDC token exchange always uses plain TLS, not the overlay transport
	plainClient := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	req, _ := http.NewRequest("POST",
		"https://"+oidcHost+"/oidc/oauth/token",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := plainClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("oidc refresh: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("oidc refresh %d: %s", resp.StatusCode, b)
	}

	var result struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || result.AccessToken == "" {
		return "", fmt.Errorf("oidc: empty access token")
	}
	return result.AccessToken, nil
}

// authenticatePassword uses the legacy password auth method — only works on localhost:1280.
func (z *ZitiService) authenticatePassword() (string, error) {
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, z.User, z.Pass)
	req, _ := http.NewRequest("POST",
		"https://"+z.Addr+"/edge/management/v1/authenticate?method=password",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := z.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("auth %d: %s", resp.StatusCode, b)
	}

	var result struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	return result.Data.Token, nil
}

// CreateIdentity creates a Ziti identity with the given attributes and appData.
func (z *ZitiService) CreateIdentity(name string, attrs []string, appData map[string]interface{}) (id, jwt string, err error) {
	token, err := z.Authenticate()
	if err != nil {
		return "", "", fmt.Errorf("ziti auth: %w", err)
	}

	payload := map[string]interface{}{
		"name":           name,
		"type":           "Default",
		"isAdmin":        false,
		"enrollment":     map[string]interface{}{"ott": true},
		"roleAttributes": attrs,
		"appData":        appData,
	}
	body, _ := json.Marshal(payload)

	createResp, err := z.apiCall(token, "POST", "/edge/management/v1/identities", body)
	if err != nil {
		return "", "", err
	}

	var createResult struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	json.Unmarshal(createResp, &createResult)
	id = createResult.Data.ID

	// Fetch JWT
	detailResp, err := z.apiCall(token, "GET", "/edge/management/v1/identities/"+id, nil)
	if err != nil {
		return id, "", err
	}

	var detail struct {
		Data struct {
			Enrollment struct {
				OTT struct {
					JWT string `json:"jwt"`
				} `json:"ott"`
			} `json:"enrollment"`
		} `json:"data"`
	}
	json.Unmarshal(detailResp, &detail)
	jwt = detail.Data.Enrollment.OTT.JWT

	return id, jwt, nil
}

// ReissueOTT issues a new enrollment OTT for an existing identity by ID.
// Used for re-enrollment (dine-in) — same identity, fresh cert, same policies.
// Uses POST /edge/management/v1/enrollments with identityId (Ziti v2 API).
func (z *ZitiService) ReissueOTT(token, identityID string) (jwt string, err error) {
	// Create new enrollment for existing identity
	expires := time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)
	body, _ := json.Marshal(map[string]interface{}{
		"method":     "ott",
		"identityId": identityID,
		"expiresAt":  expires,
	})
	resp, err := z.apiCall(token, "POST", "/edge/management/v1/enrollments", body)
	if err != nil {
		return "", fmt.Errorf("reissue OTT: create enrollment: %w", err)
	}

	// Get the enrollment ID from the create response
	var createResult struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if jsonErr := json.Unmarshal(resp, &createResult); jsonErr != nil {
		return "", fmt.Errorf("reissue OTT: parse create response: %w", jsonErr)
	}
	if createResult.Data.ID == "" {
		return "", fmt.Errorf("reissue OTT: no enrollment ID in response")
	}

	// Fetch the enrollment to get the JWT
	detail, err := z.apiCall(token, "GET", "/edge/management/v1/enrollments/"+createResult.Data.ID, nil)
	if err != nil {
		return "", fmt.Errorf("reissue OTT: fetch enrollment: %w", err)
	}

	var detailResult struct {
		Data struct {
			JWT string `json:"jwt"`
		} `json:"data"`
	}
	if jsonErr := json.Unmarshal(detail, &detailResult); jsonErr != nil {
		return "", fmt.Errorf("reissue OTT: parse detail: %w", jsonErr)
	}
	if detailResult.Data.JWT == "" {
		return "", fmt.Errorf("reissue OTT: no JWT in enrollment detail")
	}
	return detailResult.Data.JWT, nil
}

// CreateService creates a Ziti service with configs and policies.
func (z *ZitiService) CreateService(token, name string, configIDs []string, attrs []string) (string, error) {
	payload, _ := json.Marshal(map[string]interface{}{
		"name": name, "encryptionRequired": true,
		"configs": configIDs, "roleAttributes": attrs,
	})
	resp, err := z.apiCall(token, "POST", "/edge/management/v1/services", payload)
	if err != nil {
		return "", err
	}
	return z.extractID(resp), nil
}

// CreateConfig creates a Ziti config (intercept.v1, host.v2, etc).
func (z *ZitiService) CreateConfig(token, name, configTypeID string, data interface{}) (string, error) {
	payload, _ := json.Marshal(map[string]interface{}{
		"name": name, "configTypeId": configTypeID, "data": data,
	})
	resp, err := z.apiCall(token, "POST", "/edge/management/v1/configs", payload)
	if err != nil {
		return "", err
	}
	return z.extractID(resp), nil
}

// CreateServicePolicy creates a bind or dial policy.
func (z *ZitiService) CreateServicePolicy(token, name, policyType, semantic string, identityRoles, serviceRoles []string) error {
	payload, _ := json.Marshal(map[string]interface{}{
		"name": name, "type": policyType, "semantic": semantic,
		"identityRoles": identityRoles, "serviceRoles": serviceRoles,
	})
	_, err := z.apiCall(token, "POST", "/edge/management/v1/service-policies", payload)
	return err
}

// GetConfigTypeID looks up a config type by name.
func (z *ZitiService) GetConfigTypeID(token, typeName string) string {
	resp, err := z.apiCall(token, "GET", "/edge/management/v1/config-types?filter=name%3D%22"+typeName+"%22", nil)
	if err != nil {
		return ""
	}
	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	json.Unmarshal(resp, &result)
	if len(result.Data) > 0 {
		return result.Data[0].ID
	}
	return ""
}

// GetServiceIDByName looks up a service by name. Returns "" if not found.
func (z *ZitiService) GetServiceIDByName(token, name string) string {
	resp, err := z.apiCall(token, "GET", "/edge/management/v1/services?filter=name%3D%22"+name+"%22", nil)
	if err != nil {
		return ""
	}
	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	json.Unmarshal(resp, &result)
	if len(result.Data) > 0 {
		return result.Data[0].ID
	}
	return ""
}

// GetIdentityByName looks up an identity and returns its attributes and appData.
func (z *ZitiService) GetIdentityByName(token, name string) (id string, attrs []string, appData map[string]interface{}, err error) {
	resp, err := z.apiCall(token, "GET", "/edge/management/v1/identities?filter=name%3D%22"+name+"%22", nil)
	if err != nil {
		return "", nil, nil, err
	}
	var result struct {
		Data []struct {
			ID             string                 `json:"id"`
			RoleAttributes []string               `json:"roleAttributes"`
			AppData        map[string]interface{} `json:"appData"`
		} `json:"data"`
	}
	json.Unmarshal(resp, &result)
	if len(result.Data) == 0 {
		return "", nil, nil, fmt.Errorf("identity %q not found", name)
	}
	d := result.Data[0]
	return d.ID, d.RoleAttributes, d.AppData, nil
}

// GetIdentityAppData fetches appData for a known Ziti ID (no name lookup).
func (z *ZitiService) GetIdentityAppData(token, zitiID string) (map[string]interface{}, error) {
	resp, err := z.apiCall(token, "GET", "/edge/management/v1/identities/"+zitiID, nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data struct {
			AppData map[string]interface{} `json:"appData"`
		} `json:"data"`
	}
	json.Unmarshal(resp, &result)
	return result.Data.AppData, nil
}

// GetIdentityPolicies returns edge router policies for an identity.
func (z *ZitiService) GetIdentityPolicies(token, zitiID string) (routerRoles []string, err error) {
	resp, err := z.apiCall(token, "GET", "/edge/management/v1/identities/"+zitiID+"/edge-router-policies", nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data []struct {
			EdgeRouterRoles []string `json:"edgeRouterRoles"`
		} `json:"data"`
	}
	json.Unmarshal(resp, &result)
	for _, p := range result.Data {
		routerRoles = append(routerRoles, p.EdgeRouterRoles...)
	}
	return routerRoles, nil
}

func (z *ZitiService) apiCall(token, method, path string, body []byte) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = strings.NewReader(string(body))
	}
	req, _ := http.NewRequest(method, "https://"+z.Addr+path, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	// OIDC tokens use Bearer auth; legacy password tokens use zt-session.
	if z.RefreshToken != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	} else {
		req.Header.Set("zt-session", token)
	}

	resp, err := z.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return nil, fmt.Errorf("ziti %s %s: %d %s", method, path, resp.StatusCode, string(respBody)[:min(len(respBody), 200)])
	}
	return respBody, nil
}

// --- Identity lifecycle ---

// UpdateIdentity changes role attributes (promotion/demotion).
func (z *ZitiService) UpdateIdentity(token, id string, attrs []string) error {
	payload, _ := json.Marshal(map[string]interface{}{"roleAttributes": attrs})
	_, err := z.apiCall(token, "PATCH", "/edge/management/v1/identities/"+id, payload)
	return err
}

// UpdateIdentityAddAttr adds a single attribute to an identity without removing existing ones.
func (z *ZitiService) UpdateIdentityAddAttr(token, id, attr string) error {
	// Fetch current attributes
	resp, err := z.apiCall(token, "GET", "/edge/management/v1/identities/"+id, nil)
	if err != nil {
		return err
	}
	var detail struct {
		Data struct {
			RoleAttributes []string `json:"roleAttributes"`
		} `json:"data"`
	}
	json.Unmarshal(resp, &detail)

	// Check if already present
	for _, a := range detail.Data.RoleAttributes {
		if a == attr {
			return nil
		}
	}

	attrs := append(detail.Data.RoleAttributes, attr)
	return z.UpdateIdentity(token, id, attrs)
}

// UpdateIdentityRemoveAttr removes a single attribute from an identity without touching others.
func (z *ZitiService) UpdateIdentityRemoveAttr(token, id, attr string) error {
	// Fetch current attributes
	resp, err := z.apiCall(token, "GET", "/edge/management/v1/identities/"+id, nil)
	if err != nil {
		return err
	}
	var detail struct {
		Data struct {
			RoleAttributes []string `json:"roleAttributes"`
		} `json:"data"`
	}
	json.Unmarshal(resp, &detail)

	// Build new list without the target attr
	var attrs []string
	for _, a := range detail.Data.RoleAttributes {
		if a != attr {
			attrs = append(attrs, a)
		}
	}
	return z.UpdateIdentity(token, id, attrs)
}

// UpdateIdentityAppData updates metadata on an identity.
func (z *ZitiService) UpdateIdentityAppData(token, id string, appData map[string]interface{}) error {
	payload, _ := json.Marshal(map[string]interface{}{"appData": appData})
	_, err := z.apiCall(token, "PATCH", "/edge/management/v1/identities/"+id, payload)
	return err
}

// RenameIdentity updates the name field of a Ziti identity.
func (z *ZitiService) RenameIdentity(token, id, name string) error {
	payload, _ := json.Marshal(map[string]interface{}{"name": name})
	_, err := z.apiCall(token, "PATCH", "/edge/management/v1/identities/"+id, payload)
	return err
}

// DeleteIdentity removes an identity.
func (z *ZitiService) DeleteIdentity(token, id string) error {
	_, err := z.apiCall(token, "DELETE", "/edge/management/v1/identities/"+id, nil)
	return err
}

// SetIdentityAuthPolicyByName finds the named auth policy and assigns it to the identity.
func (z *ZitiService) SetIdentityAuthPolicyByName(token, identityID, policyName string) error {
	policyID, err := z.FindByName(token, "auth-policies", policyName)
	if err != nil {
		return fmt.Errorf("find auth-policy %q: %w", policyName, err)
	}
	payload, _ := json.Marshal(map[string]interface{}{"authPolicyId": policyID})
	_, err = z.apiCall(token, "PATCH", "/edge/management/v1/identities/"+identityID, payload)
	return err
}

// CreateServiceSimple creates a Ziti service with no configs (SDK-embedded bind/dial only).
// Returns the new service ID, or "" on error.
func (z *ZitiService) CreateServiceSimple(token, name string) string {
	svcID, _ := z.CreateService(token, name, []string{}, []string{name})
	return svcID
}

// --- Cleanup ---

func (z *ZitiService) DeleteService(token, id string) error {
	_, err := z.apiCall(token, "DELETE", "/edge/management/v1/services/"+id, nil)
	return err
}

func (z *ZitiService) DeleteConfig(token, id string) error {
	_, err := z.apiCall(token, "DELETE", "/edge/management/v1/configs/"+id, nil)
	return err
}

func (z *ZitiService) DeleteServicePolicy(token, id string) error {
	_, err := z.apiCall(token, "DELETE", "/edge/management/v1/service-policies/"+id, nil)
	return err
}

// FindByName looks up any entity by name, returns its ID.
func (z *ZitiService) FindByName(token, entityType, name string) (string, error) {
	resp, err := z.apiCall(token, "GET",
		"/edge/management/v1/"+entityType+"?filter=name%3D%22"+name+"%22", nil)
	if err != nil {
		return "", err
	}
	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	json.Unmarshal(resp, &result)
	if len(result.Data) > 0 {
		return result.Data[0].ID, nil
	}
	return "", nil
}

// --- Listing / inspection ---

func (z *ZitiService) ListIdentities(token string, limit int) ([]IdentitySummary, error) {
	resp, err := z.apiCall(token, "GET", fmt.Sprintf("/edge/management/v1/identities?limit=%d", limit), nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data []IdentitySummary `json:"data"`
	}
	json.Unmarshal(resp, &result)
	return result.Data, nil
}

func (z *ZitiService) ListRouters(token string) ([]RouterSummary, error) {
	resp, err := z.apiCall(token, "GET", "/edge/management/v1/edge-routers?limit=100", nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data []RouterSummary `json:"data"`
	}
	json.Unmarshal(resp, &result)
	return result.Data, nil
}

func (z *ZitiService) ListServices(token string) ([]ServiceSummary, error) {
	resp, err := z.apiCall(token, "GET", "/edge/management/v1/services?limit=100", nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data []ServiceSummary `json:"data"`
	}
	json.Unmarshal(resp, &result)
	return result.Data, nil
}

func (z *ZitiService) ListTerminators(token, filter string) ([]TerminatorSummary, error) {
	query := "/edge/management/v1/terminators?limit=200"
	if filter != "" {
		query += "&filter=" + filter
	}
	resp, err := z.apiCall(token, "GET", query, nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data []TerminatorSummary `json:"data"`
	}
	json.Unmarshal(resp, &result)
	return result.Data, nil
}

// --- Types ---

type IdentitySummary struct {
	ID             string                 `json:"id"`
	Name           string                 `json:"name"`
	Type           map[string]string      `json:"type"`
	RoleAttributes []string               `json:"roleAttributes"`
	AppData        map[string]interface{} `json:"appData"`
	HasAPISession  bool                   `json:"hasApiSession"`
	HasEdgeRouter  bool                   `json:"hasEdgeRouterConnection"`
}

type RouterSummary struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	IsOnline       bool     `json:"isOnline"`
	RoleAttributes []string `json:"roleAttributes"`
}

type ServiceSummary struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	RoleAttributes []string `json:"roleAttributes"`
}

type TerminatorSummary struct {
	ID      string `json:"id"`
	Service string `json:"service"`
	Router  string `json:"router"`
	Binding string `json:"binding"`
	HostID  string `json:"hostId"`
}

// --- Internal ---

func (z *ZitiService) extractID(body []byte) string {
	var result struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	json.Unmarshal(body, &result)
	return result.Data.ID
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
