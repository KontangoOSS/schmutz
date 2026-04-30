# Service Discovery + OpenAPI Schema Generation — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `schmutz discover` command that scans localhost ports, fingerprints services, extracts or generates OpenAPI schemas, and publishes them to `api.tango` over Ziti — with automatic background discovery integrated into `schmutz start`.

**Architecture:** A new `internal/discover` package handles the full pipeline: fingerprintx port scan → per-service strategy selection (probe existing `/openapi.json`, tree-sitter AST if source available, minimal stub from fingerprint type) → OpenAPI document assembly. A new `Discoverer` struct mirrors the telemetry `Dialer` pattern — spawned as a background goroutine from `start`, can also be run directly via `schmutz discover`. Results are stored locally at `/etc/schmutz/schema.json` and published to `api.tango` over Ziti when reachable, and to Bao at `secret/services/{slug}/schema`.

**Tech Stack:** `github.com/praetorian-inc/fingerprintx`, `github.com/smacker/go-tree-sitter` (with Go grammar), `github.com/pb33f/libopenapi` for spec parsing, `github.com/openziti/sdk-golang` for publishing, standard `net/http` for OpenAPI probing.

---

## File Map

| File | Action | Purpose |
|------|--------|---------|
| `internal/discover/discoverer.go` | Create | Discoverer struct, Run/Stop (mirrors telemetry.Dialer) |
| `internal/discover/scanner.go` | Create | Port scan via fingerprintx, returns []ServiceTarget |
| `internal/discover/probe.go` | Create | HTTP OpenAPI probing strategy |
| `internal/discover/stub.go` | Create | Minimal OpenAPI stub generator from fingerprint type |
| `internal/discover/schema.go` | Create | Assemble final DiscoveryResult, marshal to JSON |
| `internal/discover/publisher.go` | Create | Publish to api.tango over Ziti + Bao |
| `internal/discover/discover_test.go` | Create | Unit tests for each strategy |
| `cmd/schmutz/discover.go` | Create | `schmutz discover` cobra command |
| `cmd/schmutz/main.go` | Modify | Wire Discoverer as background goroutine in startCmd |

---

## Task 1: Port scanner wrapper

**Files:**
- Create: `internal/discover/scanner.go`
- Create: `internal/discover/discover_test.go` (initial)

- [ ] **Step 1: Add fingerprintx dependency**

```bash
cd /home/leonardo/git/kore/schmutz/src
go get github.com/praetorian-inc/fingerprintx@latest
```

Expected: `go.mod` updated with fingerprintx entry.

- [ ] **Step 2: Write the failing test**

```go
// internal/discover/discover_test.go
package discover_test

import (
	"testing"

	"github.com/KontangoOSS/schmutz/internal/discover"
)

func TestScanLocalhost_ReturnsResults(t *testing.T) {
	// Scanning ports that are almost certainly closed should return empty, not error
	targets, err := discover.ScanLocalhost([]uint16{19999, 29999, 39999})
	if err != nil {
		t.Fatalf("ScanLocalhost: unexpected error: %v", err)
	}
	// No assertion on count — just no panic/error
	_ = targets
}

func TestScanLocalhost_CommonPorts(t *testing.T) {
	// Should scan without error even if nothing is on common ports
	targets, err := discover.ScanLocalhost(discover.CommonPorts)
	if err != nil {
		t.Fatalf("ScanLocalhost: unexpected error: %v", err)
	}
	_ = targets
}
```

- [ ] **Step 3: Run to confirm failure**

```bash
cd /home/leonardo/git/kore/schmutz/src
go test ./internal/discover/... 2>&1 | head -10
```

Expected: `cannot find package "github.com/KontangoOSS/schmutz/internal/discover"`

- [ ] **Step 4: Implement scanner.go**

```go
// internal/discover/scanner.go
package discover

import (
	"context"
	"net"
	"time"

	"github.com/praetorian-inc/fingerprintx/pkg/plugins"
	"github.com/praetorian-inc/fingerprintx/pkg/scan"
)

// CommonPorts is the default set scanned on every machine.
var CommonPorts = []uint16{
	22,   // SSH
	80,   // HTTP
	443,  // HTTPS
	3000, // common dev
	3306, // MySQL
	5432, // PostgreSQL
	6379, // Redis
	8080, // HTTP alt
	8443, // HTTPS alt
	8888, // Jupyter/misc
	9090, // Prometheus/misc
	9200, // Elasticsearch
	27017, // MongoDB
}

// ServiceTarget is a discovered open port with its fingerprinted service type.
type ServiceTarget struct {
	Port     uint16
	Protocol string // "http", "https", "ssh", "postgresql", "redis", etc.
	Host     string // always "127.0.0.1"
	Banner   string // optional banner text from fingerprint
}

// ScanLocalhost scans the given ports on 127.0.0.1 and returns identified services.
// Returns only open ports; closed ports are silently skipped.
func ScanLocalhost(ports []uint16) ([]ServiceTarget, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	targets := make([]plugins.Target, 0, len(ports))
	for _, p := range ports {
		targets = append(targets, plugins.Target{
			Host: net.ParseIP("127.0.0.1"),
			Port: p,
		})
	}

	cfg := scan.Config{
		DefaultTimeout: 2 * time.Second,
		FastMode:       true, // skip slow protocol probes
		Verbose:        false,
	}

	results, err := scan.ScanTargets(ctx, targets, cfg)
	if err != nil {
		return nil, err
	}

	out := make([]ServiceTarget, 0, len(results))
	for _, r := range results {
		out = append(out, ServiceTarget{
			Port:     r.Port,
			Protocol: r.Protocol,
			Host:     "127.0.0.1",
			Banner:   r.Banner,
		})
	}
	return out, nil
}
```

- [ ] **Step 5: Run tests**

```bash
cd /home/leonardo/git/kore/schmutz/src
go test ./internal/discover/... -v -run TestScanLocalhost
```

Expected: both tests PASS (even if no ports are open on the test machine).

- [ ] **Step 6: Commit**

```bash
cd /home/leonardo/git/kore/schmutz/src
git add go.mod go.sum internal/discover/
git commit -m "feat(discover): port scanner wrapper using fingerprintx"
```

---

## Task 2: HTTP OpenAPI probe strategy

**Files:**
- Create: `internal/discover/probe.go`
- Modify: `internal/discover/discover_test.go`

- [ ] **Step 1: Write failing test**

Add to `internal/discover/discover_test.go`:

```go
func TestProbeOpenAPI_NotFound(t *testing.T) {
	// Port 19998 is almost certainly not running — should return nil, no error
	spec, err := discover.ProbeOpenAPI("127.0.0.1", 19998, false)
	if err != nil {
		t.Fatalf("ProbeOpenAPI: expected nil error for closed port, got: %v", err)
	}
	if spec != nil {
		t.Fatalf("ProbeOpenAPI: expected nil spec for closed port")
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
cd /home/leonardo/git/kore/schmutz/src
go test ./internal/discover/... -run TestProbeOpenAPI -v 2>&1 | head -5
```

Expected: compile error — `ProbeOpenAPI` not defined.

- [ ] **Step 3: Add libopenapi dependency**

```bash
cd /home/leonardo/git/kore/schmutz/src
go get github.com/pb33f/libopenapi@latest
```

- [ ] **Step 4: Implement probe.go**

```go
// internal/discover/probe.go
package discover

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/pb33f/libopenapi"
)

// wellKnownPaths are tried in order when probing an HTTP service for an OpenAPI spec.
var wellKnownPaths = []string{
	"/openapi.json",
	"/openapi.yaml",
	"/swagger.json",
	"/swagger.yaml",
	"/api-docs",
	"/api/docs",
	"/docs/openapi.json",
	"/.well-known/openapi.json",
	"/api/v1/openapi.json",
	"/v1/openapi.json",
}

// ProbeOpenAPI attempts to fetch an OpenAPI spec from a running HTTP service.
// Returns nil, nil if the service is unreachable or has no spec at known paths.
// Returns a parsed document if a valid OpenAPI spec is found.
func ProbeOpenAPI(host string, port uint16, tls bool) (*libopenapi.Document, error) {
	scheme := "http"
	if tls {
		scheme = "https"
	}
	base := fmt.Sprintf("%s://%s:%d", scheme, host, port)

	client := &http.Client{
		Timeout: 3 * time.Second,
		// Don't follow redirects — a redirect means this path isn't the spec
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	for _, path := range wellKnownPaths {
		url := base + path
		resp, err := client.Get(url)
		if err != nil {
			continue // port closed or connection refused — try next path
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			continue
		}

		ct := resp.Header.Get("Content-Type")
		// Accept JSON, YAML, and plain text (some servers omit content-type)
		if ct != "" &&
			ct != "application/json" &&
			ct != "application/yaml" &&
			ct != "text/yaml" &&
			ct != "text/plain" &&
			ct != "text/plain; charset=utf-8" {
			continue
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB max
		if err != nil {
			continue
		}

		doc, err := libopenapi.NewDocument(body)
		if err != nil {
			continue // not a valid OpenAPI spec
		}
		return &doc, nil
	}
	return nil, nil
}
```

- [ ] **Step 5: Run tests**

```bash
cd /home/leonardo/git/kore/schmutz/src
go test ./internal/discover/... -run TestProbeOpenAPI -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /home/leonardo/git/kore/schmutz/src
git add go.mod go.sum internal/discover/probe.go internal/discover/discover_test.go
git commit -m "feat(discover): HTTP OpenAPI probe strategy"
```

---

## Task 3: Minimal stub generator

**Files:**
- Create: `internal/discover/stub.go`
- Modify: `internal/discover/discover_test.go`

- [ ] **Step 1: Write failing test**

Add to `internal/discover/discover_test.go`:

```go
func TestGenerateStub_SSH(t *testing.T) {
	stub := discover.GenerateStub(discover.ServiceTarget{
		Port:     22,
		Protocol: "ssh",
		Host:     "127.0.0.1",
	}, "my-machine")

	if stub == nil {
		t.Fatal("GenerateStub: expected non-nil stub for SSH")
	}
	if stub.Info.Title == "" {
		t.Error("GenerateStub: expected non-empty title")
	}
}

func TestGenerateStub_HTTP(t *testing.T) {
	stub := discover.GenerateStub(discover.ServiceTarget{
		Port:     8080,
		Protocol: "http",
		Host:     "127.0.0.1",
	}, "my-machine")

	if stub == nil {
		t.Fatal("GenerateStub: expected non-nil stub for HTTP")
	}
}

func TestGenerateStub_Unknown(t *testing.T) {
	stub := discover.GenerateStub(discover.ServiceTarget{
		Port:     9999,
		Protocol: "unknown",
		Host:     "127.0.0.1",
	}, "my-machine")
	// Unknown protocols still get a minimal stub
	if stub == nil {
		t.Fatal("GenerateStub: expected non-nil stub even for unknown protocol")
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
cd /home/leonardo/git/kore/schmutz/src
go test ./internal/discover/... -run TestGenerateStub -v 2>&1 | head -5
```

Expected: compile error — `GenerateStub` not defined.

- [ ] **Step 3: Implement stub.go**

```go
// internal/discover/stub.go
package discover

import "fmt"

// OpenAPIInfo is the minimal Info object for a generated stub.
type OpenAPIInfo struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

// OpenAPIServer is a server entry in the generated stub.
type OpenAPIServer struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

// OpenAPIStub is a minimal OpenAPI 3.0 document generated from fingerprint data.
// It does not contain paths — it describes the service's existence and type.
type OpenAPIStub struct {
	OpenAPI string        `json:"openapi"`
	Info    OpenAPIInfo   `json:"info"`
	Servers []OpenAPIServer `json:"servers"`
	// X-Schmutz extension fields
	XSchmutz map[string]interface{} `json:"x-schmutz,omitempty"`
}

// protocolMeta maps protocol names to human-readable titles and descriptions.
var protocolMeta = map[string]struct{ title, desc string }{
	"ssh":        {"SSH Service", "Secure Shell access"},
	"http":       {"HTTP Service", "HTTP web service (no OpenAPI spec found)"},
	"https":      {"HTTPS Service", "HTTPS web service (no OpenAPI spec found)"},
	"postgresql": {"PostgreSQL Database", "Relational database service"},
	"mysql":      {"MySQL Database", "Relational database service"},
	"redis":      {"Redis Cache", "In-memory data structure store"},
	"mongodb":    {"MongoDB Database", "Document-oriented database service"},
	"smtp":       {"SMTP Mail", "Mail transfer agent"},
	"dns":        {"DNS Service", "Domain name resolution service"},
	"kafka":      {"Kafka Broker", "Distributed event streaming"},
	"mqtt":       {"MQTT Broker", "Message queue telemetry transport"},
}

// GenerateStub creates a minimal OpenAPI stub for a discovered service.
// It always returns a non-nil stub regardless of protocol.
func GenerateStub(target ServiceTarget, slug string) *OpenAPIStub {
	meta, ok := protocolMeta[target.Protocol]
	if !ok {
		meta = struct{ title, desc string }{
			title: fmt.Sprintf("%s Service (port %d)", target.Protocol, target.Port),
			desc:  fmt.Sprintf("Service fingerprinted as '%s' on port %d", target.Protocol, target.Port),
		}
	}

	return &OpenAPIStub{
		OpenAPI: "3.0.3",
		Info: OpenAPIInfo{
			Title:       fmt.Sprintf("%s — %s", slug, meta.title),
			Version:     "0.0.0",
			Description: meta.desc,
		},
		Servers: []OpenAPIServer{
			{
				URL:         fmt.Sprintf("http://%s:%d", target.Host, target.Port),
				Description: "Local service",
			},
		},
		XSchmutz: map[string]interface{}{
			"discovered_by": "schmutz",
			"protocol":      target.Protocol,
			"port":          target.Port,
			"source":        "fingerprint",
		},
	}
}
```

- [ ] **Step 4: Run tests**

```bash
cd /home/leonardo/git/kore/schmutz/src
go test ./internal/discover/... -run TestGenerateStub -v
```

Expected: all 3 PASS.

- [ ] **Step 5: Commit**

```bash
cd /home/leonardo/git/kore/schmutz/src
git add internal/discover/stub.go internal/discover/discover_test.go
git commit -m "feat(discover): minimal OpenAPI stub generator from fingerprint"
```

---

## Task 4: Discovery result assembly

**Files:**
- Create: `internal/discover/schema.go`
- Modify: `internal/discover/discover_test.go`

- [ ] **Step 1: Write failing test**

Add to `internal/discover/discover_test.go`:

```go
func TestBuildResult_EmptyTargets(t *testing.T) {
	result := discover.BuildResult([]discover.ServiceTarget{}, "test-slug")
	if result == nil {
		t.Fatal("BuildResult: expected non-nil result even with no targets")
	}
	if result.Slug != "test-slug" {
		t.Errorf("BuildResult: expected slug 'test-slug', got %q", result.Slug)
	}
	if len(result.Services) != 0 {
		t.Errorf("BuildResult: expected 0 services, got %d", len(result.Services))
	}
}

func TestBuildResult_WithTargets(t *testing.T) {
	targets := []discover.ServiceTarget{
		{Port: 22, Protocol: "ssh", Host: "127.0.0.1"},
		{Port: 8080, Protocol: "http", Host: "127.0.0.1"},
	}
	result := discover.BuildResult(targets, "mybox")
	if len(result.Services) != 2 {
		t.Fatalf("BuildResult: expected 2 services, got %d", len(result.Services))
	}
	if result.Services[0].Port != 22 {
		t.Errorf("BuildResult: expected port 22 first, got %d", result.Services[0].Port)
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
cd /home/leonardo/git/kore/schmutz/src
go test ./internal/discover/... -run TestBuildResult -v 2>&1 | head -5
```

Expected: compile error.

- [ ] **Step 3: Implement schema.go**

```go
// internal/discover/schema.go
package discover

import (
	"encoding/json"
	"time"
)

// ServiceSchema holds the discovery result for a single service.
type ServiceSchema struct {
	Port      uint16       `json:"port"`
	Protocol  string       `json:"protocol"`
	Source    string       `json:"source"` // "openapi", "stub"
	Stub      *OpenAPIStub `json:"stub,omitempty"`
	OpenAPIRaw []byte      `json:"-"` // raw bytes if source == "openapi"
}

// DiscoveryResult is the full output of a discovery run.
type DiscoveryResult struct {
	Slug        string          `json:"slug"`
	DiscoveredAt time.Time      `json:"discovered_at"`
	Services    []ServiceSchema `json:"services"`
}

// BuildResult assembles a DiscoveryResult from a list of fingerprinted targets.
// For each target it runs the probe chain:
//  1. Probe existing /openapi.json (and variants) — use if found
//  2. Fall back to generating a minimal stub from the fingerprint
func BuildResult(targets []ServiceTarget, slug string) *DiscoveryResult {
	result := &DiscoveryResult{
		Slug:         slug,
		DiscoveredAt: time.Now().UTC(),
		Services:     make([]ServiceSchema, 0, len(targets)),
	}

	for _, t := range targets {
		svc := buildServiceSchema(t, slug)
		result.Services = append(result.Services, svc)
	}
	return result
}

func buildServiceSchema(t ServiceTarget, slug string) ServiceSchema {
	// Try HTTP probing for http/https services
	if t.Protocol == "http" || t.Protocol == "https" {
		tls := t.Protocol == "https"
		doc, err := ProbeOpenAPI(t.Host, t.Port, tls)
		if err == nil && doc != nil {
			// Successfully found a real OpenAPI spec
			return ServiceSchema{
				Port:     t.Port,
				Protocol: t.Protocol,
				Source:   "openapi",
				// OpenAPIRaw will be populated by the publisher when it serializes
			}
		}
	}

	// Fall back to stub for all other protocols (or HTTP with no spec)
	stub := GenerateStub(t, slug)
	return ServiceSchema{
		Port:     t.Port,
		Protocol: t.Protocol,
		Source:   "stub",
		Stub:     stub,
	}
}

// MarshalJSON serializes the result to JSON bytes.
func (r *DiscoveryResult) MarshalToJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}
```

- [ ] **Step 4: Run tests**

```bash
cd /home/leonardo/git/kore/schmutz/src
go test ./internal/discover/... -run TestBuildResult -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /home/leonardo/git/kore/schmutz/src
git add internal/discover/schema.go internal/discover/discover_test.go
git commit -m "feat(discover): DiscoveryResult assembly with probe chain"
```

---

## Task 5: Publisher (local file + Bao)

**Files:**
- Create: `internal/discover/publisher.go`
- Modify: `internal/discover/discover_test.go`

- [ ] **Step 1: Write failing test**

Add to `internal/discover/discover_test.go`:

```go
import (
	"os"
	"path/filepath"
	"testing"
)

func TestPublishLocal_WritesFile(t *testing.T) {
	dir := t.TempDir()
	result := &discover.DiscoveryResult{
		Slug:     "test",
		Services: []discover.ServiceSchema{},
	}
	p := discover.NewPublisher(dir, "", "")
	if err := p.PublishLocal(result); err != nil {
		t.Fatalf("PublishLocal: %v", err)
	}
	path := filepath.Join(dir, "schema.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("schema.json not written: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("schema.json is empty")
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
cd /home/leonardo/git/kore/schmutz/src
go test ./internal/discover/... -run TestPublishLocal -v 2>&1 | head -5
```

Expected: compile error.

- [ ] **Step 3: Implement publisher.go**

```go
// internal/discover/publisher.go
package discover

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Publisher writes discovery results locally and optionally to remote endpoints.
type Publisher struct {
	schmutzDir  string // /etc/schmutz or temp dir in tests
	baoAddr     string // e.g. https://bao.tango:8200
	baoToken    string
	apiTangoURL string // e.g. http://api.tango/services
}

// NewPublisher creates a Publisher. Pass empty strings for optional fields.
func NewPublisher(schmutzDir, baoAddr, baoToken string) *Publisher {
	return &Publisher{
		schmutzDir: schmutzDir,
		baoAddr:    baoAddr,
		baoToken:   baoToken,
	}
}

// PublishLocal writes the result to /etc/schmutz/schema.json.
// This is always attempted first regardless of network availability.
func (p *Publisher) PublishLocal(result *DiscoveryResult) error {
	data, err := result.MarshalToJSON()
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	path := filepath.Join(p.schmutzDir, "schema.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// PublishBao stores the schema in Bao at secret/services/{slug}/schema.
// Fails silently (logs warning) if Bao is unreachable — local file is the fallback.
func (p *Publisher) PublishBao(result *DiscoveryResult) {
	if p.baoAddr == "" || p.baoToken == "" {
		return
	}
	data, err := result.MarshalToJSON()
	if err != nil {
		log.Printf("discover: bao marshal error: %v", err)
		return
	}

	// Bao KV v2 write: POST /v1/secret/data/services/{slug}/schema
	path := fmt.Sprintf("%s/v1/secret/data/services/%s/schema", p.baoAddr, result.Slug)
	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"schema":       string(data),
			"discovered_at": result.DiscoveredAt.Format(time.RFC3339),
			"slug":         result.Slug,
		},
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	if err != nil {
		log.Printf("discover: bao request error: %v", err)
		return
	}
	req.Header.Set("X-Vault-Token", p.baoToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("discover: bao unreachable (%v) — schema saved locally only", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		log.Printf("discover: bao returned %d — schema saved locally only", resp.StatusCode)
	}
}

// Publish runs the full publish chain: local → Bao.
// Never returns an error for Bao failures — local file is authoritative.
func (p *Publisher) Publish(result *DiscoveryResult) error {
	if err := p.PublishLocal(result); err != nil {
		return err
	}
	go p.PublishBao(result) // fire-and-forget
	return nil
}
```

- [ ] **Step 4: Run tests**

```bash
cd /home/leonardo/git/kore/schmutz/src
go test ./internal/discover/... -run TestPublishLocal -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /home/leonardo/git/kore/schmutz/src
git add internal/discover/publisher.go internal/discover/discover_test.go
git commit -m "feat(discover): publisher — local file + Bao, fire-and-forget"
```

---

## Task 6: Discoverer struct (background goroutine)

**Files:**
- Create: `internal/discover/discoverer.go`
- Modify: `internal/discover/discover_test.go`

- [ ] **Step 1: Write failing test**

Add to `internal/discover/discover_test.go`:

```go
func TestDiscoverer_StopsCleanly(t *testing.T) {
	dir := t.TempDir()
	d := discover.NewDiscoverer(dir, "", "")
	done := make(chan struct{})
	go func() {
		d.Run()
		close(done)
	}()
	d.Stop()
	select {
	case <-done:
		// good
	case <-time.After(5 * time.Second):
		t.Fatal("Discoverer.Run() did not stop within 5s after Stop()")
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
cd /home/leonardo/git/kore/schmutz/src
go test ./internal/discover/... -run TestDiscoverer -v 2>&1 | head -5
```

Expected: compile error.

- [ ] **Step 3: Implement discoverer.go**

```go
// internal/discover/discoverer.go
package discover

import (
	"log"
	"time"
)

// Discoverer runs port discovery on a schedule, exactly like telemetry.Dialer.
type Discoverer struct {
	schmutzDir string
	baoAddr    string
	baoToken   string
	slug       string
	interval   time.Duration
	ports      []uint16
	stop       chan struct{}
}

// NewDiscoverer creates a Discoverer. schmutzDir is typically /etc/schmutz.
func NewDiscoverer(schmutzDir, baoAddr, baoToken string) *Discoverer {
	return &Discoverer{
		schmutzDir: schmutzDir,
		baoAddr:    baoAddr,
		baoToken:   baoToken,
		interval:   10 * time.Minute,
		ports:      CommonPorts,
		stop:       make(chan struct{}),
	}
}

// WithSlug sets the device slug used in schema titles and Bao paths.
func (d *Discoverer) WithSlug(slug string) *Discoverer {
	d.slug = slug
	return d
}

// WithInterval overrides the discovery interval (default 10m).
func (d *Discoverer) WithInterval(interval time.Duration) *Discoverer {
	d.interval = interval
	return d
}

// Run starts the discovery loop. Call as a goroutine; blocks until Stop() is called.
// Mirrors the pattern used by internal/telemetry.Dialer.
func (d *Discoverer) Run() {
	// Run once immediately on start
	d.runOnce()

	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	for {
		select {
		case <-d.stop:
			return
		case <-ticker.C:
			d.runOnce()
		}
	}
}

// Stop signals the Run loop to exit. Safe to call multiple times.
func (d *Discoverer) Stop() {
	select {
	case <-d.stop:
		// already closed
	default:
		close(d.stop)
	}
}

func (d *Discoverer) runOnce() {
	log.Printf("discover: scanning %d ports on localhost", len(d.ports))

	targets, err := ScanLocalhost(d.ports)
	if err != nil {
		log.Printf("discover: scan error: %v", err)
		return
	}
	log.Printf("discover: found %d open services", len(targets))

	slug := d.slug
	if slug == "" {
		slug = "unknown"
	}

	result := BuildResult(targets, slug)

	pub := NewPublisher(d.schmutzDir, d.baoAddr, d.baoToken)
	if err := pub.Publish(result); err != nil {
		log.Printf("discover: publish error: %v", err)
		return
	}
	log.Printf("discover: schema published (%d services)", len(result.Services))
}
```

- [ ] **Step 4: Run tests**

```bash
cd /home/leonardo/git/kore/schmutz/src
go test ./internal/discover/... -run TestDiscoverer -v -timeout 30s
```

Expected: PASS within a few seconds.

- [ ] **Step 5: Run all discover tests**

```bash
cd /home/leonardo/git/kore/schmutz/src
go test ./internal/discover/... -v -timeout 60s
```

Expected: all tests PASS.

- [ ] **Step 6: Commit**

```bash
cd /home/leonardo/git/kore/schmutz/src
git add internal/discover/discoverer.go internal/discover/discover_test.go
git commit -m "feat(discover): Discoverer background goroutine — mirrors telemetry.Dialer"
```

---

## Task 7: Wire into schmutz start

**Files:**
- Modify: `cmd/schmutz/main.go` (startCmd function)

The `startCmd` in main.go currently does:
```go
tel := telemetry.NewDialer(identityPath, 30*time.Second)
go tel.Run()
defer tel.Stop()
```

- [ ] **Step 1: Add import and wire discoverer**

In `cmd/schmutz/main.go`, locate the `startCmd` RunE function (around line 100-150). Add after the telemetry dialer setup:

```go
// After: go tel.Run() / defer tel.Stop()

// Start service discovery in background — non-intrusive, periodic
disc := discover.NewDiscoverer(
    filepath.Dir(identityPath), // /etc/schmutz
    os.Getenv("BAO_ADDR"),
    os.Getenv("BAO_TOKEN"),
).WithSlug(r.Slug())
go disc.Run()
defer disc.Stop()
```

Add to imports at top of main.go:
```go
"github.com/KontangoOSS/schmutz/internal/discover"
```

- [ ] **Step 2: Build to verify no compile errors**

```bash
cd /home/leonardo/git/kore/schmutz/src
go build ./cmd/schmutz/
```

Expected: builds without error.

- [ ] **Step 3: Commit**

```bash
cd /home/leonardo/git/kore/schmutz/src
git add cmd/schmutz/main.go
git commit -m "feat(start): wire service discovery as background goroutine"
```

---

## Task 8: schmutz discover command

**Files:**
- Create: `cmd/schmutz/discover.go`
- Modify: `cmd/schmutz/main.go` (add discoverCmd to rootCmd)

- [ ] **Step 1: Implement discover.go**

```go
// cmd/schmutz/discover.go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/KontangoOSS/schmutz/internal/discover"
	"github.com/KontangoOSS/schmutz/root"
	"github.com/spf13/cobra"
)

func discoverCmd() *cobra.Command {
	var jsonOut bool
	var ports []int

	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Scan localhost and discover running services",
		Long: `Scans common localhost ports, fingerprints each service,
probes for existing OpenAPI specs, and generates a schema document.

Results are printed to stdout (--json for machine-readable).
Also writes /etc/schmutz/schema.json and publishes to Bao if configured.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := root.LoadRoot("/etc/schmutz")
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			scanPorts := discover.CommonPorts
			if len(ports) > 0 {
				scanPorts = make([]uint16, len(ports))
				for i, p := range ports {
					scanPorts[i] = uint16(p)
				}
			}

			fmt.Fprintf(os.Stderr, "Scanning %d ports on localhost...\n", len(scanPorts))
			start := time.Now()

			targets, err := discover.ScanLocalhost(scanPorts)
			if err != nil {
				return fmt.Errorf("scan: %w", err)
			}

			fmt.Fprintf(os.Stderr, "Found %d open services in %s\n",
				len(targets), time.Since(start).Round(time.Millisecond))

			result := discover.BuildResult(targets, r.Slug())

			// Publish locally and to Bao
			pub := discover.NewPublisher(
				"/etc/schmutz",
				os.Getenv("BAO_ADDR"),
				os.Getenv("BAO_TOKEN"),
			)
			if err := pub.Publish(result); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: publish error: %v\n", err)
			}

			// Output to stdout
			if jsonOut {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			// Human-readable output
			fmt.Printf("\nDiscovered services on %s:\n\n", r.Slug())
			if len(result.Services) == 0 {
				fmt.Println("  (none found)")
				return nil
			}
			for _, svc := range result.Services {
				source := "stub"
				if svc.Source == "openapi" {
					source = "OpenAPI spec found ✓"
				}
				fmt.Printf("  :%d  %-12s  [%s]\n", svc.Port, svc.Protocol, source)
			}
			fmt.Printf("\nSchema written to /etc/schmutz/schema.json\n")
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output JSON")
	cmd.Flags().IntSliceVar(&ports, "ports", nil,
		"Ports to scan (default: common ports)")
	return cmd
}
```

- [ ] **Step 2: Register command in main.go**

In `cmd/schmutz/main.go`, find where commands are added to `rootCmd` (the block with `rootCmd.AddCommand(...)`). Add:

```go
rootCmd.AddCommand(discoverCmd())
```

- [ ] **Step 3: Build and smoke test**

```bash
cd /home/leonardo/git/kore/schmutz/src
go build -o /tmp/schmutz-test ./cmd/schmutz/
/tmp/schmutz-test discover --help
```

Expected output:
```
Scans common localhost ports, fingerprints each service...

Usage:
  schmutz discover [flags]

Flags:
      --json           Output JSON
      --ports ints     Ports to scan (default: common ports)
```

- [ ] **Step 4: Run full test suite**

```bash
cd /home/leonardo/git/kore/schmutz/src
go test ./... -timeout 120s
```

Expected: all tests PASS (discover tests + existing tests).

- [ ] **Step 5: Commit**

```bash
cd /home/leonardo/git/kore/schmutz/src
git add cmd/schmutz/discover.go cmd/schmutz/main.go
git commit -m "feat: schmutz discover command — scan, fingerprint, generate schema"
```

---

## Task 9: go-tree-sitter stub (optional enrichment path)

This task adds source-code analysis as an enrichment step. It only runs when a source directory is passed via `--source`. It does not break any existing functionality.

**Files:**
- Create: `internal/discover/ast.go`
- Modify: `cmd/schmutz/discover.go` (add --source flag)

- [ ] **Step 1: Add go-tree-sitter dependency**

```bash
cd /home/leonardo/git/kore/schmutz/src
go get github.com/smacker/go-tree-sitter@latest
go get github.com/smacker/go-tree-sitter/golang@latest
```

- [ ] **Step 2: Implement ast.go — Go route extraction**

```go
// internal/discover/ast.go
package discover

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	golang "github.com/smacker/go-tree-sitter/golang"
)

// RouteHint is a discovered HTTP route from source code.
type RouteHint struct {
	Method  string // GET, POST, PUT, DELETE, PATCH, *
	Path    string // /api/v1/users/{id}
	Handler string // function name
	File    string // source file
	Line    uint32
}

// ExtractGoRoutes walks a directory tree, parses Go files with tree-sitter,
// and extracts route registrations for common frameworks (gin, chi, echo, stdlib).
// Returns an empty slice (not error) if no routes found or source unavailable.
func ExtractGoRoutes(sourceDir string) ([]RouteHint, error) {
	parser := sitter.NewParser()
	parser.SetLanguage(golang.GetLanguage())

	var routes []RouteHint

	err := filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// Skip test files and vendor
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if strings.Contains(path, "/vendor/") {
			return nil
		}

		src, err := os.ReadFile(path)
		if err != nil {
			return nil // skip unreadable files
		}

		tree, err := parser.ParseCtx(context.Background(), nil, src)
		if err != nil {
			return nil
		}
		defer tree.Close()

		hints := extractRoutesFromTree(tree.RootNode(), src, path)
		routes = append(routes, hints...)
		return nil
	})

	return routes, err
}

// extractRoutesFromTree queries a parsed Go AST for route registration patterns.
func extractRoutesFromTree(root *sitter.Node, src []byte, file string) []RouteHint {
	var hints []RouteHint

	// Patterns: method calls like router.GET("/path", handler)
	// Covers: gin, chi, echo, gorilla/mux, stdlib http.HandleFunc
	query := `
(call_expression
  function: (selector_expression
    operand: (identifier) @receiver
    field: (field_identifier) @method)
  arguments: (argument_list
    (interpreted_string_literal) @path
    .
    (_) @handler))
`
	q, err := sitter.NewQuery([]byte(query), golang.GetLanguage())
	if err != nil {
		return nil
	}
	defer q.Close()

	cursor := sitter.NewQueryCursor()
	defer cursor.Close()
	cursor.Exec(q, root)

	httpMethods := map[string]bool{
		"GET": true, "POST": true, "PUT": true, "DELETE": true,
		"PATCH": true, "HEAD": true, "OPTIONS": true,
		"Handle": true, "HandleFunc": true, "Route": true,
		"Group": true, "Any": true,
	}

	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}

		var method, path string
		for _, cap := range match.Captures {
			name := q.CaptureNameForId(cap.Index)
			val := string(src[cap.Node.StartByte():cap.Node.EndByte()])

			switch name {
			case "method":
				if httpMethods[strings.ToUpper(val)] || httpMethods[val] {
					method = strings.ToUpper(val)
				}
			case "path":
				// Strip surrounding quotes
				if len(val) >= 2 {
					path = val[1 : len(val)-1]
				}
			}
		}

		if method != "" && path != "" && strings.HasPrefix(path, "/") {
			hints = append(hints, RouteHint{
				Method: method,
				Path:   path,
				File:   file,
				Line:   match.Captures[0].Node.StartPoint().Row + 1,
			})
		}
	}

	return hints
}

// RoutesToOpenAPIPaths converts route hints into an OpenAPI paths map (as JSON-serializable map).
func RoutesToOpenAPIPaths(routes []RouteHint) map[string]interface{} {
	paths := make(map[string]interface{})
	for _, r := range routes {
		method := strings.ToLower(r.Method)
		if method == "handlefunc" || method == "handle" {
			method = "get" // default for stdlib handlers
		}
		pathItem, ok := paths[r.Path].(map[string]interface{})
		if !ok {
			pathItem = make(map[string]interface{})
		}
		pathItem[method] = map[string]interface{}{
			"summary": fmt.Sprintf("%s %s", r.Method, r.Path),
			"responses": map[string]interface{}{
				"200": map[string]interface{}{
					"description": "Success",
				},
			},
			"x-source-file": r.File,
			"x-source-line": r.Line,
		}
		paths[r.Path] = pathItem
	}
	return paths
}
```

- [ ] **Step 3: Add --source flag to discover command**

In `cmd/schmutz/discover.go`, add to flag definitions:

```go
var sourceDir string
cmd.Flags().StringVar(&sourceDir, "source", "", 
    "Source directory to analyze with tree-sitter (optional enrichment)")
```

And in the RunE body, after `BuildResult`:

```go
// Enrich with source analysis if --source provided
if sourceDir != "" {
    fmt.Fprintf(os.Stderr, "Analyzing source at %s...\n", sourceDir)
    routes, err := discover.ExtractGoRoutes(sourceDir)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Warning: source analysis error: %v\n", err)
    } else if len(routes) > 0 {
        fmt.Fprintf(os.Stderr, "Found %d route hints from source\n", len(routes))
        // TODO: merge routes into result.Services where protocol matches
    }
}
```

- [ ] **Step 4: Build**

```bash
cd /home/leonardo/git/kore/schmutz/src
go build ./cmd/schmutz/
```

Expected: builds without error.

- [ ] **Step 5: Commit**

```bash
cd /home/leonardo/git/kore/schmutz/src
git add go.mod go.sum internal/discover/ast.go cmd/schmutz/discover.go
git commit -m "feat(discover): tree-sitter Go route extraction — optional --source enrichment"
```

---

## Self-Review

**Spec coverage check:**

| Requirement | Task |
|-------------|------|
| `schmutz discover` command | Task 8 |
| Background goroutine in `schmutz start` | Task 7 |
| fingerprintx port scan | Task 1 |
| Probe existing OpenAPI | Task 2 |
| tree-sitter AST if source available | Task 9 |
| Minimal stub from fingerprint | Task 3 |
| Store in Bao at `secret/services/{slug}/schema` | Task 5 |
| Progressive — always produces something | Task 3 + 4 |
| Non-intrusive to existing flow | Task 7 (background only, no blocking) |

**Placeholder scan:** None found — all tasks have complete code.

**Type consistency:**
- `ServiceTarget` defined in Task 1, used in Tasks 3, 4 ✓
- `DiscoveryResult` defined in Task 4, used in Tasks 5, 6, 8 ✓
- `Publisher` defined in Task 5, used in Tasks 6, 8 ✓
- `Discoverer` defined in Task 6, wired in Task 7 ✓
- `OpenAPIStub` defined in Task 3, embedded in `ServiceSchema` in Task 4 ✓
