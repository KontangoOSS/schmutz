package oidc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Client fetches and caches a Zitadel machine account access token.
// Uses the OAuth2 client credentials flow — no user interaction.
type Client struct {
	issuer       string
	clientID     string
	clientSecret string

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

// New creates a Zitadel OIDC client for a machine account.
func New(issuer, clientID, clientSecret string) *Client {
	return &Client{
		issuer:       strings.TrimRight(issuer, "/"),
		clientID:     clientID,
		clientSecret: clientSecret,
	}
}

// Token returns a valid access token, refreshing if expired.
func (c *Client) Token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.expiresAt.Add(-30 * time.Second)) {
		return c.token, nil
	}
	return c.refresh(ctx)
}

func (c *Client) refresh(ctx context.Context) (string, error) {
	tokenURL := c.issuer + "/oauth/v2/token"
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
		"scope":         {"openid"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("oidc: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("oidc: POST %s: %w", tokenURL, err)
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("oidc: decode response: %w", err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("oidc: token error: %s", result.Error)
	}

	c.token = result.AccessToken
	c.expiresAt = time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)
	return c.token, nil
}
