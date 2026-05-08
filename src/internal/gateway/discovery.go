package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/KontangoOSS/schmutz/internal/discover"
	"github.com/KontangoOSS/schmutz/internal/shared"
)

// AgentInfo carries identity fields from /etc/schmutz/agent.json.
type AgentInfo struct {
	Tenant     string
	App        string
	Deployment string
	BaoAddr    string
	BaoToken   string
}

// DiscoverConfig is the input to Discover.
type DiscoverConfig struct {
	AgentJSON        AgentInfo
	GatewayConfig    *shared.GatewayConfig
	ForgejoURL       string
	ForgejoToken     string
	ForgejoOrg       string
	SkipBaoWrite     bool
	SkipForgejoWrite bool
}

// wellKnownSpecPaths are the paths probed on each service port.
var wellKnownSpecPaths = []string{
	"/openapi.json", "/openapi.yaml", "/swagger.json", "/swagger.yaml",
	"/api-docs", "/api/docs", "/docs/openapi.json",
	"/.well-known/openapi.json", "/api/v1/openapi.json", "/v1/openapi.json",
}

// Discover scans declared services, resolves their OpenAPI specs, and returns entries.
// It does NOT write to Bao or Forgejo — callers handle that separately.
func Discover(ctx context.Context, cfg DiscoverConfig) ([]ServiceEntry, error) {
	if cfg.GatewayConfig == nil || !cfg.GatewayConfig.Enabled {
		return nil, nil
	}

	entries := make([]ServiceEntry, 0, len(cfg.GatewayConfig.Services))

	for _, svc := range cfg.GatewayConfig.Services {
		entry := ServiceEntry{
			Name:         svc.Name,
			Port:         svc.Port,
			Protocol:     "http",
			DiscoveredAt: time.Now().UTC(),
		}

		// 1. Try the explicit spec hint URL first.
		if svc.Spec != "" {
			if spec, err := fetchSpecBytes(ctx, svc.Spec); err == nil {
				entry.SpecJSON = spec
				entry.SpecSource = "openapi"
				entry.EndpointCount = countPaths(spec)
				entries = append(entries, entry)
				continue
			}
		}

		// 2. Probe well-known OpenAPI paths on the port.
		if spec, err := probeSpecOnPort(ctx, svc.Port); err == nil {
			entry.SpecJSON = spec
			entry.SpecSource = "openapi"
			entry.EndpointCount = countPaths(spec)
			entries = append(entries, entry)
			continue
		}

		// 3. Fall back to stub generator.
		target := discover.ServiceTarget{
			Port:     svc.Port,
			Protocol: "http",
			Host:     fmt.Sprintf("127.0.0.1:%d", svc.Port),
		}
		stub := discover.GenerateStub(target, cfg.AgentJSON.App+"-"+svc.Name)
		b, err := json.Marshal(stub)
		if err != nil {
			log.Printf("gateway: stub marshal for %s: %v", svc.Name, err)
			continue
		}
		entry.SpecJSON = b
		entry.SpecSource = "stub"
		entries = append(entries, entry)
	}

	return entries, nil
}

// probeSpecOnPort tries well-known OpenAPI paths on localhost:port.
func probeSpecOnPort(ctx context.Context, port uint16) ([]byte, error) {
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	for _, path := range wellKnownSpecPaths {
		spec, err := fetchSpecBytes(ctx, base+path)
		if err == nil && len(spec) > 0 {
			return spec, nil
		}
	}
	return nil, fmt.Errorf("no spec found on port %d", port)
}

// fetchSpecBytes fetches raw bytes from a URL, returning error if status != 200.
func fetchSpecBytes(ctx context.Context, url string) ([]byte, error) {
	req, err := newGetRequest(ctx, url)
	if err != nil {
		return nil, err
	}
	b, status, err := doHTTP(req)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("status %d from %s", status, url)
	}
	if len(b) == 0 {
		return nil, fmt.Errorf("empty response from %s", url)
	}
	return b, nil
}

// countPaths returns the number of path items in a raw OpenAPI JSON doc.
func countPaths(spec []byte) int {
	var doc struct {
		Paths map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(spec, &doc); err != nil {
		return 0
	}
	return len(doc.Paths)
}
