package bao

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type Client interface {
	GetToken(ctx context.Context, token string) (*TokenRecord, error)
	UpdateToken(ctx context.Context, token string, rec *TokenRecord) error
	ListTokens(ctx context.Context, includeAll bool) ([]TokenListEntry, error)
	DeleteToken(ctx context.Context, token string) error

	// AdminKV is embedded so admin handlers can use generic KV operations
	// without a separate client.
	AdminKV
}

type httpClient struct {
	addr      string
	authToken string
	mount     string
	prefix    string
	hc        *http.Client
}

func NewHTTP(addr, authToken, mount, prefix string, skipTLSVerify bool) Client {
	return &httpClient{
		addr:      addr,
		authToken: authToken,
		mount:     mount,
		prefix:    prefix,
		hc: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: skipTLSVerify},
			},
		},
	}
}

func (c *httpClient) tokenURL(token string) string {
	return fmt.Sprintf("%s/v1/%s/data/%s/%s", c.addr, c.mount, c.prefix, url.PathEscape(token))
}

// do is the shared HTTP plumbing used by token + admin KV operations.
// It returns (status, body, err). Status >= 400 is NOT treated as an error;
// the caller decides how to interpret it.
func (c *httpClient) do(ctx context.Context, method, fullURL string, body []byte) (int, []byte, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, rdr)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("X-Vault-Token", c.authToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("bao request failed: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b, nil
}

type kvV2Response struct {
	Data struct {
		Data     json.RawMessage `json:"data"`
		Metadata struct {
			Version int `json:"version"`
		} `json:"metadata"`
	} `json:"data"`
}

func (c *httpClient) GetToken(ctx context.Context, token string) (*TokenRecord, error) {
	status, body, err := c.do(ctx, "GET", c.tokenURL(token), nil)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("bao returned %d: %s", status, body)
	}
	var kv kvV2Response
	if err := json.Unmarshal(body, &kv); err != nil {
		return nil, fmt.Errorf("decode kv response: %w", err)
	}
	var rec TokenRecord
	if err := json.Unmarshal(kv.Data.Data, &rec); err != nil {
		return nil, fmt.Errorf("decode token record: %w", err)
	}
	return &rec, nil
}

type kvV2Write struct {
	Data interface{} `json:"data"`
}

func (c *httpClient) UpdateToken(ctx context.Context, token string, rec *TokenRecord) error {
	body, err := json.Marshal(kvV2Write{Data: rec})
	if err != nil {
		return err
	}
	status, b, err := c.do(ctx, "POST", c.tokenURL(token), body)
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusNoContent {
		return fmt.Errorf("bao returned %d: %s", status, b)
	}
	return nil
}

var ErrNotFound = errors.New("token not found")

// Compile-time check: httpClient implements the full Client interface
// (including the embedded AdminKV).
var _ Client = (*httpClient)(nil)
