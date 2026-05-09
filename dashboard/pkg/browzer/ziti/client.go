package ziti

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/openziti/edge-api/rest_management_api_client"
	"github.com/openziti/edge-api/rest_util"
)

// Client holds connection config for the Ziti management API.
// Auth priority: cert (isAdmin identity) > OIDC refresh token > password.
type Client struct {
	Addr         string
	RefreshToken string
	User         string
	Pass         string
	// CertPEM and KeyPEM enable cert-based auth against the management API.
	// The identity must have isAdmin=true in Ziti.
	CertPEM string
	KeyPEM  string
	UseCert bool // true when cert auth is active — changes header from Bearer to zt-session

	// cached session token (zt-session expires in 30m, cache for 20m)
	cacheMu     sync.Mutex
	cachedToken string
	cacheExpiry time.Time
}

func (c *Client) httpClient() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
}

func (c *Client) certClient() (*http.Client, error) {
	cert, err := tls.X509KeyPair([]byte(c.CertPEM), []byte(c.KeyPEM))
	if err != nil {
		return nil, fmt.Errorf("load cert: %w", err)
	}
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
				Certificates:       []tls.Certificate{cert},
			},
		},
	}, nil
}

// Authenticate returns a management API session token.
// Auth priority: cert (isAdmin identity) > OIDC refresh token > password.
// Tokens are cached for 20 minutes (session TTL is 30m).
func (c *Client) Authenticate() (string, error) {
	c.cacheMu.Lock()
	if c.cachedToken != "" && time.Now().Before(c.cacheExpiry) {
		tok := c.cachedToken
		c.cacheMu.Unlock()
		return tok, nil
	}
	c.cacheMu.Unlock()

	var tok string
	var err error

	switch {
	case c.CertPEM != "" && c.KeyPEM != "":
		c.UseCert = true
		tok, err = c.authenticateCert()
	case c.RefreshToken != "":
		c.UseCert = false
		tok, err = c.authenticateOIDC()
	case c.User != "" && c.Pass != "":
		c.UseCert = true // password auth also returns a zt-session token
		tok, err = c.authenticatePassword()
	default:
		return "", fmt.Errorf("no auth credentials configured (need cert, OIDC refresh token, or username+password)")
	}

	if err != nil {
		return "", err
	}
	c.cacheMu.Lock()
	c.cachedToken = tok
	c.cacheExpiry = time.Now().Add(20 * time.Minute)
	c.cacheMu.Unlock()
	return tok, nil
}

func (c *Client) authenticateOIDC() (string, error) {
	vals := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {c.RefreshToken},
		"client_id":     {"native"},
	}
	req, err := http.NewRequest("POST", "https://"+c.Addr+"/oidc/oauth/token", strings.NewReader(vals.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("oidc refresh failed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("oidc refresh failed (status %d): %s", resp.StatusCode, string(data))
	}
	var result struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(data, &result); err != nil || result.AccessToken == "" {
		return "", fmt.Errorf("oidc returned empty access token")
	}
	return result.AccessToken, nil
}

func (c *Client) authenticatePassword() (string, error) {
	body, _ := json.Marshal(map[string]string{"username": c.User, "password": c.Pass})
	req, err := http.NewRequest("POST",
		"https://"+c.Addr+"/edge/management/v1/authenticate?method=password",
		strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("password auth failed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("password auth failed (status %d): %s", resp.StatusCode, string(data))
	}
	var result struct {
		Data struct{ Token string `json:"token"` } `json:"data"`
	}
	if err := json.Unmarshal(data, &result); err != nil || result.Data.Token == "" {
		return "", fmt.Errorf("password auth returned empty token")
	}
	return result.Data.Token, nil
}

// InvalidateCache clears the cached cert auth token, forcing re-auth on next call.
func (c *Client) InvalidateCache() {
	c.cacheMu.Lock()
	c.cachedToken = ""
	c.cacheMu.Unlock()
}

// authenticateCert uses the enrolled identity cert to get a management API session token.
// The identity must have isAdmin=true. No expiry — cert is valid for the identity's lifetime.
func (c *Client) authenticateCert() (string, error) {
	client, err := c.certClient()
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest("POST",
		"https://"+c.Addr+"/edge/management/v1/authenticate?method=cert",
		strings.NewReader("{}"))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("cert auth failed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("cert auth failed (status %d): %s", resp.StatusCode, string(data))
	}
	var result struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &result); err != nil || result.Data.Token == "" {
		return "", fmt.Errorf("cert auth returned empty token")
	}
	return result.Data.Token, nil
}

// MgmtClient returns a typed SDK management client authenticated with the given token.
func (c *Client) MgmtClient(token string) (*rest_management_api_client.ZitiEdgeManagement, error) {
	apiURL := "https://" + c.Addr
	httpClient := c.httpClient()
	return rest_util.NewEdgeManagementClientWithToken(httpClient, apiURL, token)
}

// GetUnauthenticated does a GET without auth (for /version etc).
func (c *Client) GetUnauthenticated(path string) (json.RawMessage, error) {
	req, err := http.NewRequest("GET", "https://"+c.Addr+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// setAuthHeader sets the correct auth header based on auth mode.
func (c *Client) setAuthHeader(req *http.Request, token string) {
	if c.UseCert {
		req.Header.Set("zt-session", token)
	} else {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

// doWithRetry executes an http.Request with the given token.
// On 401 it clears the cached token in the BFF and returns the error —
// the caller (BFF.Handle) will retry with a fresh token.
func (c *Client) doWithRetry(httpClient *http.Client, req *http.Request, token string) ([]byte, int, error) {
	c.setAuthHeader(req, token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}

func (c *Client) Get(token, path string) (json.RawMessage, error) {
	req, err := http.NewRequest("GET", "https://"+c.Addr+path, nil)
	if err != nil {
		return nil, err
	}
	data, status, err := c.doWithRetry(c.httpClient(), req, token)
	if err != nil {
		return nil, err
	}
	// On 401 invalidate cache and retry once with fresh token
	if status == 401 {
		c.InvalidateCache()
		freshTok, authErr := c.Authenticate()
		if authErr == nil {
			req2, _ := http.NewRequest("GET", "https://"+c.Addr+path, nil)
			data, status, err = c.doWithRetry(c.httpClient(), req2, freshTok)
			if err != nil {
				return nil, err
			}
		}
	}
	if status != 200 {
		trunc := string(data)
		if len(trunc) > 200 {
			trunc = trunc[:200]
		}
		return nil, fmt.Errorf("GET %s: %d %s", path, status, trunc)
	}
	return data, nil
}

func (c *Client) Post(token, path string, body []byte) (json.RawMessage, error) {
	req, err := http.NewRequest("POST", "https://"+c.Addr+path, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.UseCert { req.Header.Set("zt-session", token) } else { req.Header.Set("Authorization", "Bearer "+token) }
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		trunc := string(data)
		if len(trunc) > 200 {
			trunc = trunc[:200]
		}
		return nil, fmt.Errorf("POST %s: %d %s", path, resp.StatusCode, trunc)
	}
	return data, nil
}

func (c *Client) Patch(token, path string, body []byte) (json.RawMessage, error) {
	req, err := http.NewRequest("PATCH", "https://"+c.Addr+path, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.UseCert { req.Header.Set("zt-session", token) } else { req.Header.Set("Authorization", "Bearer "+token) }
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		trunc := string(data)
		if len(trunc) > 200 {
			trunc = trunc[:200]
		}
		return nil, fmt.Errorf("PATCH %s: %d %s", path, resp.StatusCode, trunc)
	}
	return data, nil
}

func (c *Client) Delete(token, path string) error {
	req, err := http.NewRequest("DELETE", "https://"+c.Addr+path, nil)
	if err != nil {
		return err
	}
	if c.UseCert { req.Header.Set("zt-session", token) } else { req.Header.Set("Authorization", "Bearer "+token) }
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		trunc := string(data)
		if len(trunc) > 200 {
			trunc = trunc[:200]
		}
		return fmt.Errorf("DELETE %s: %d %s", path, resp.StatusCode, trunc)
	}
	return nil
}

// DecodeList unwraps a Ziti {"data": [...]} envelope.
func DecodeList[T any](raw json.RawMessage) ([]T, error) {
	var envelope struct {
		Data []T `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	return envelope.Data, nil
}

// DecodeOne unwraps a Ziti {"data": {...}} envelope.
func DecodeOne[T any](raw json.RawMessage) (*T, error) {
	var envelope struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	return &envelope.Data, nil
}
