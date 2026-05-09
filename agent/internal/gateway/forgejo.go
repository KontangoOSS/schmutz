package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// ForgejoWriteConfig carries credentials for Forgejo API calls.
type ForgejoWriteConfig struct {
	ForgejoURL   string
	ForgejoToken string
	ForgejoOrg   string // default "public"
	App          string
}

// WriteSpecToForgejo commits api.json to public/<app>/api.json in Forgejo
// if the file does not already exist. Developer-committed specs take precedence.
func WriteSpecToForgejo(ctx context.Context, cfg ForgejoWriteConfig, spec []byte) error {
	if cfg.ForgejoURL == "" || cfg.ForgejoToken == "" {
		return nil
	}
	org := cfg.ForgejoOrg
	if org == "" {
		org = "public"
	}
	apiPath := fmt.Sprintf("%s/api/v1/repos/%s/%s/contents/api.json",
		cfg.ForgejoURL,
		url.PathEscape(org),
		url.PathEscape(cfg.App),
	)

	// Check if file exists
	getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, apiPath, nil)
	if err != nil {
		return fmt.Errorf("forgejo: new request: %w", err)
	}
	getReq.Header.Set("Authorization", "token "+cfg.ForgejoToken)
	resp, err := httpClient.Do(getReq)
	if err != nil {
		return fmt.Errorf("forgejo: check api.json: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil // exists — developer spec takes precedence
	}
	if resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("forgejo: check api.json returned %d", resp.StatusCode)
	}

	// Create the file
	body := map[string]string{
		"message": "feat: add discovered api.json (schmutz gateway)",
		"content": base64.StdEncoding.EncodeToString(spec),
		"branch":  "main",
	}
	b, _ := json.Marshal(body)
	postReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiPath, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("forgejo: new post request: %w", err)
	}
	postReq.Header.Set("Authorization", "token "+cfg.ForgejoToken)
	postReq.Header.Set("Content-Type", "application/json")
	postResp, err := httpClient.Do(postReq)
	if err != nil {
		return fmt.Errorf("forgejo: create api.json: %w", err)
	}
	postResp.Body.Close()
	if postResp.StatusCode != http.StatusCreated && postResp.StatusCode != http.StatusOK {
		return fmt.Errorf("forgejo: create api.json returned %d", postResp.StatusCode)
	}
	return nil
}
