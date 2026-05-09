package gateway

import (
	"context"
	"fmt"
)

// SpecPlugin resolves an OpenAPI spec for a named well-known application.
// Implementations know the app-specific probe path, required headers, and
// whether the spec is fetched locally (localhost:port) or from an upstream URL.
type SpecPlugin interface {
	// FetchSpec returns raw OpenAPI JSON/YAML bytes for the service.
	// port is the local port the app is listening on (may be ignored by
	// upstream plugins that fetch from a canonical public URL).
	FetchSpec(ctx context.Context, port uint16) ([]byte, error)
}

// pluginRegistry maps plugin name → constructor.
var pluginRegistry = map[string]func() SpecPlugin{
	"forgejo":    func() SpecPlugin { return &forgejoPlugin{} },
	"grafana":    func() SpecPlugin { return &grafanaPlugin{} },
	"zitadel":    func() SpecPlugin { return &zitadelPlugin{} },
	"woodpecker": func() SpecPlugin { return &woodpeckerPlugin{} },
	"inventree":  func() SpecPlugin { return &inventreePlugin{} },
	"konmail":    func() SpecPlugin { return &konmailPlugin{} },
}

// LookupPlugin returns the SpecPlugin for the given name, or nil if unknown.
func LookupPlugin(name string) SpecPlugin {
	ctor, ok := pluginRegistry[name]
	if !ok {
		return nil
	}
	return ctor()
}

// --- forgejo ---

type forgejoPlugin struct{}

// FetchSpec fetches the Forgejo Swagger 2.0 spec from the well-known path.
// Forgejo serves a full 300+ path spec at /swagger.v1.json.
func (p *forgejoPlugin) FetchSpec(ctx context.Context, port uint16) ([]byte, error) {
	return fetchSpecBytes(ctx, fmt.Sprintf("http://127.0.0.1:%d/swagger.v1.json", port))
}

// --- grafana ---

type grafanaPlugin struct{}

// FetchSpec fetches the Grafana OpenAPI spec.
// Grafana serves its spec at /api/swagger.json and requires at minimum viewer auth.
// Operators with auth-gated Grafana should use api.services[].spec with a
// token URL instead of this plugin.
func (p *grafanaPlugin) FetchSpec(ctx context.Context, port uint16) ([]byte, error) {
	url := fmt.Sprintf("http://127.0.0.1:%d/api/swagger.json", port)
	req, err := newGetRequest(ctx, url)
	if err != nil {
		return nil, err
	}
	// base64("viewer:viewer") — works for internal Grafana instances that allow
	// basic auth with the default viewer credentials.
	req.Header.Set("Authorization", "Basic dmlld2VyOnZpZXdlcg==")
	b, status, contentType, err := doHTTPWithType(req)
	if err != nil {
		return nil, err
	}
	if status == 401 || status == 403 {
		return nil, fmt.Errorf("grafana: spec requires auth (status %d); set api.services[].spec with a token URL", status)
	}
	if status != 200 {
		return nil, fmt.Errorf("grafana: spec returned %d", status)
	}
	if isHTML(contentType) {
		return nil, fmt.Errorf("grafana: html response (not a spec)")
	}
	return b, nil
}

// --- zitadel ---

type zitadelPlugin struct{}

// FetchSpec fetches the Zitadel management API OpenAPI spec from the local instance.
func (p *zitadelPlugin) FetchSpec(ctx context.Context, port uint16) ([]byte, error) {
	paths := []string{
		"/management/v1/openapi/v2/swagger.json",
		"/openapi/v2/swagger.json",
		"/grpc-gateway/management/v1/swagger.json",
	}
	for _, path := range paths {
		b, err := fetchSpecBytes(ctx, fmt.Sprintf("http://127.0.0.1:%d%s", port, path))
		if err == nil && len(b) > 0 {
			return b, nil
		}
	}
	return nil, fmt.Errorf("zitadel: spec not found on localhost:%d (tried %d paths)", port, len(paths))
}

// --- woodpecker ---

type woodpeckerPlugin struct{}

// FetchSpec fetches the Woodpecker CI OpenAPI spec.
func (p *woodpeckerPlugin) FetchSpec(ctx context.Context, port uint16) ([]byte, error) {
	paths := []string{
		"/api/swagger.json",
		"/swagger.json",
		"/woodpecker.json",
	}
	for _, path := range paths {
		b, err := fetchSpecBytes(ctx, fmt.Sprintf("http://127.0.0.1:%d%s", port, path))
		if err == nil && len(b) > 0 {
			return b, nil
		}
	}
	return nil, fmt.Errorf("woodpecker: spec not found on localhost:%d", port)
}

// --- inventree ---

type inventreePlugin struct{}

// FetchSpec fetches the InvenTree OpenAPI spec.
// InvenTree (Django REST Framework) exposes the schema at /api/schema/?format=json.
func (p *inventreePlugin) FetchSpec(ctx context.Context, port uint16) ([]byte, error) {
	paths := []string{
		"/api/schema/?format=json",
		"/api/schema/",
		"/api/openapi.json",
	}
	for _, path := range paths {
		b, err := fetchSpecBytes(ctx, fmt.Sprintf("http://127.0.0.1:%d%s", port, path))
		if err == nil && len(b) > 0 {
			return b, nil
		}
	}
	return nil, fmt.Errorf("inventree: spec not found on localhost:%d", port)
}

// --- konmail ---

type konmailPlugin struct{}

// FetchSpec fetches the konmail API spec.
// konmail exposes its spec at /v1/openapi.json.
func (p *konmailPlugin) FetchSpec(ctx context.Context, port uint16) ([]byte, error) {
	paths := []string{
		"/v1/openapi.json",
		"/openapi.json",
		"/docs/openapi.json",
	}
	for _, path := range paths {
		b, err := fetchSpecBytes(ctx, fmt.Sprintf("http://127.0.0.1:%d%s", port, path))
		if err == nil && len(b) > 0 {
			return b, nil
		}
	}
	return nil, fmt.Errorf("konmail: spec not found on localhost:%d", port)
}

// isHTML returns true if the content-type indicates an HTML response.
func isHTML(ct string) bool {
	return len(ct) >= 9 && ct[:9] == "text/html"
}
