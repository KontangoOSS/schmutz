package bao

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// AdminKV is implemented by httpClient and exposes generic KV v2 operations.
// Used by audit log + break-glass identity storage.
type AdminKV interface {
	WriteJSON(ctx context.Context, path string, body []byte) error
	ReadJSON(ctx context.Context, path string) ([]byte, error)
	ListKeys(ctx context.Context, path string) ([]string, error)
	DeleteKey(ctx context.Context, path string) error
}

func (c *httpClient) dataURL(path string) string {
	return fmt.Sprintf("%s/v1/%s/data/%s", c.addr, c.mount, path)
}

func (c *httpClient) metadataURL(path string) string {
	return fmt.Sprintf("%s/v1/%s/metadata/%s", c.addr, c.mount, path)
}

func (c *httpClient) WriteJSON(ctx context.Context, path string, body []byte) error {
	wrap, err := json.Marshal(map[string]interface{}{"data": json.RawMessage(body)})
	if err != nil {
		return err
	}
	status, b, err := c.do(ctx, "POST", c.dataURL(path), wrap)
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusNoContent {
		return fmt.Errorf("bao write %s returned %d: %s", path, status, b)
	}
	return nil
}

func (c *httpClient) ReadJSON(ctx context.Context, path string) ([]byte, error) {
	status, body, err := c.do(ctx, "GET", c.dataURL(path), nil)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, fmt.Errorf("bao read %s: not found", path)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("bao read %s returned %d: %s", path, status, body)
	}
	var kv kvV2Response
	if err := json.Unmarshal(body, &kv); err != nil {
		return nil, fmt.Errorf("decode kv response: %w", err)
	}
	return kv.Data.Data, nil
}

func (c *httpClient) ListKeys(ctx context.Context, path string) ([]string, error) {
	listURL := c.metadataURL(path) + "?list=true"
	status, body, err := c.do(ctx, "GET", listURL, nil)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, nil
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("bao list %s returned %d: %s", path, status, body)
	}
	var resp struct {
		Data struct {
			Keys []string `json:"keys"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode list response: %w", err)
	}
	return resp.Data.Keys, nil
}

func (c *httpClient) DeleteKey(ctx context.Context, path string) error {
	status, b, err := c.do(ctx, "DELETE", c.metadataURL(path), nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusNoContent {
		return fmt.Errorf("bao delete %s returned %d: %s", path, status, b)
	}
	return nil
}

// Compile-time check: httpClient is the canonical AdminKV implementation.
var _ AdminKV = (*httpClient)(nil)
