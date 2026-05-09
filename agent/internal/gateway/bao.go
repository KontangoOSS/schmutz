package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// BaoWriteConfig carries credentials and path info for Bao writes.
type BaoWriteConfig struct {
	BaoAddr    string // e.g. https://secrets.kontango.net
	BaoToken   string // scoped token from /run/bao-token
	Tenant     string
	App        string
	Deployment string
}

// WriteSpecs writes each entry's OpenAPI spec to two Bao paths:
//
//	{tenant}/secret/data/apps/{app}/{dep}/api-spec  (tenant copy)
//	public/secret/data/applications/{app}/api-spec  (canonical, any agent can read)
//
// The public namespace write is non-fatal — logged and skipped on failure.
func WriteSpecs(ctx context.Context, cfg BaoWriteConfig, entries []ServiceEntry) error {
	if cfg.BaoAddr == "" || cfg.BaoToken == "" {
		return nil
	}

	for _, e := range entries {
		if len(e.SpecJSON) == 0 {
			continue
		}
		payload := map[string]any{
			"data": map[string]any{
				"spec":         string(e.SpecJSON),
				"spec_source":  e.SpecSource,
				"service_name": e.Name,
				"port":         e.Port,
				"updated_at":   time.Now().UTC().Format(time.RFC3339),
			},
		}
		b, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("gateway bao: marshal spec for %s: %w", e.Name, err)
		}

		// Tenant-scoped write
		tenantPath := fmt.Sprintf("%s/v1/secret/data/apps/%s/%s/api-spec",
			cfg.BaoAddr, cfg.App, cfg.Deployment)
		if err := baoKVWrite(ctx, cfg.BaoToken, cfg.Tenant+"/", tenantPath, b); err != nil {
			return fmt.Errorf("gateway bao: write tenant spec for %s: %w", e.Name, err)
		}

		// Public namespace write — non-fatal
		publicPath := fmt.Sprintf("%s/v1/secret/data/applications/%s/api-spec",
			cfg.BaoAddr, cfg.App)
		if err := baoKVWrite(ctx, cfg.BaoToken, "public/", publicPath, b); err != nil {
			log.Printf("gateway bao: public namespace write for %s: %v (non-fatal)", e.Name, err)
		}
	}
	return nil
}

// baoKVWrite performs a KV v2 POST with the given namespace header.
func baoKVWrite(ctx context.Context, token, namespace, url string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("X-Vault-Token", token)
	req.Header.Set("Content-Type", "application/json")
	if namespace != "" {
		req.Header.Set("X-Vault-Namespace", namespace)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("bao write %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("bao write %s: status %d", url, resp.StatusCode)
	}
	return nil
}
