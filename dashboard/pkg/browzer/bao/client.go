// Package bao provides a client for the OpenBao/Vault KV v2 API.
package bao

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

type Client struct {
	Addr  string // e.g. "https://bao.tango:8200"
	Token string
}

func New(addr, token string) *Client {
	return &Client{Addr: addr, Token: token}
}

func (c *Client) httpClient() *http.Client {
	skipVerify := os.Getenv("BAO_SKIP_VERIFY") == "1" || os.Getenv("VAULT_SKIP_VERIFY") == "1"
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: skipVerify},
		},
	}
}

// List returns keys under a KV v2 path.
func (c *Client) List(path string) ([]string, error) {
	req, err := http.NewRequest("LIST", fmt.Sprintf("%s/v1/secret/metadata/%s", c.Addr, path), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Vault-Token", c.Token)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, nil
	}

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("LIST %s: %d", path, resp.StatusCode)
	}

	var env struct {
		Data struct {
			Keys []string `json:"keys"`
		} `json:"data"`
	}
	json.Unmarshal(body, &env)
	return env.Data.Keys, nil
}

// Get reads a KV v2 secret, returning the data map.
func (c *Client) Get(path string) (map[string]any, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/v1/secret/data/%s", c.Addr, path), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Vault-Token", c.Token)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, nil
	}

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GET %s: %d", path, resp.StatusCode)
	}

	var env struct {
		Data struct {
			Data map[string]any `json:"data"`
		} `json:"data"`
	}
	json.Unmarshal(body, &env)
	return env.Data.Data, nil
}

// GetFromMount reads a KV v2 secret from a specific named mount.
func (c *Client) GetFromMount(mount, path string) (map[string]any, error) {
	url := fmt.Sprintf("%s/v1/%s/data/%s", c.Addr, mount, path)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Vault-Token", c.Token)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, nil
	}
	var result struct {
		Data struct {
			Data map[string]any `json:"data"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Data.Data, nil
}

// GetMetadataOnly reads a KV v2 secret but only returns key names, not values.
func (c *Client) GetKeys(path string) ([]string, error) {
	data, err := c.Get(path)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	return keys, nil
}
