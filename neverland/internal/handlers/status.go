package handlers

import (
	"context"
	"net"
	"net/http"
	"runtime/debug"
	"sync"
	"time"

	tinkv1 "github.com/tinkerbell/tink/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"git.konoss.org/kore/schmutz/neverland/internal/respond"
)

// StatusHandler probes upstream tink-system services and returns a
// dashboard-friendly status snapshot. It is intentionally lenient: when a
// dependency is unreachable, the response still returns 200 with that
// component flagged unreachable. The GUI uses this to render tiles.
type StatusHandler struct {
	client    client.Client
	namespace string

	// Service probe addresses. These default to the in-cluster DNS names
	// resolvable from within the tink-system namespace.
	tinkServerAddr string // host:port — TCP probe (gRPC)
	hegelURL       string // HTTP URL (any 2xx/3xx/4xx == reachable)
	smeeAddr       string // host:port — TCP probe (DHCP/TFTP/Syslog have no HTTP)
	smeeHTTPURL    string // HTTP URL for smee's iPXE endpoint
	hookFilesURL   string // HTTP URL for the hook artifact server

	startTime time.Time
}

// NewStatusHandler constructs a StatusHandler with sensible defaults for the
// in-cluster tink-system service names.
func NewStatusHandler(c client.Client, namespace string) *StatusHandler {
	return &StatusHandler{
		client:         c,
		namespace:      namespace,
		tinkServerAddr: "tink-server:42113",
		hegelURL:       "http://hegel:50061/",
		smeeAddr:       "smee:7171",
		smeeHTTPURL:    "http://smee:7171/",
		hookFilesURL:   "http://tink-stack:8080/",
		startTime:      time.Now(),
	}
}

// componentStatus is the per-service block returned in the JSON response.
type componentStatus struct {
	Reachable bool   `json:"reachable"`
	Version   string `json:"version,omitempty"`
	URL       string `json:"url,omitempty"`
	Error     string `json:"error,omitempty"`
}

// neverlandSelf is the block describing the running neverland process.
type neverlandSelf struct {
	Version       string `json:"version"`
	UptimeSeconds int64  `json:"uptime_seconds"`
}

// counts is the block of CRD object counts. -1 means the count failed.
type counts struct {
	Hardware  int `json:"hardware"`
	Templates int `json:"templates"`
	Workflows int `json:"workflows"`
}

// statusResponse is the top-level JSON shape.
type statusResponse struct {
	Neverland  neverlandSelf   `json:"neverland"`
	TinkServer componentStatus `json:"tink_server"`
	Hegel      componentStatus `json:"hegel"`
	Smee       componentStatus `json:"smee"`
	HookFiles  componentStatus `json:"hook_files"`
	Counts     counts          `json:"counts"`
}

// Status is the GET /api/v1/status handler.
func (s *StatusHandler) Status(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	resp := statusResponse{
		Neverland: neverlandSelf{
			Version:       buildVersion(),
			UptimeSeconds: int64(time.Since(s.startTime).Seconds()),
		},
		Counts: counts{Hardware: -1, Templates: -1, Workflows: -1},
	}

	var wg sync.WaitGroup

	// Probe upstream services in parallel.
	wg.Add(4)
	go func() { defer wg.Done(); resp.TinkServer = probeTCP(ctx, s.tinkServerAddr) }()
	go func() { defer wg.Done(); resp.Hegel = probeHTTP(ctx, s.hegelURL) }()
	go func() {
		defer wg.Done()
		// Smee runs DHCP/TFTP/syslog (UDP) plus iPXE on 7171/TCP.
		// A TCP probe to 7171 is the closest thing to a health signal.
		st := probeTCP(ctx, s.smeeAddr)
		st.URL = s.smeeHTTPURL
		resp.Smee = st
	}()
	go func() { defer wg.Done(); resp.HookFiles = probeHTTP(ctx, s.hookFilesURL) }()

	// Count CRDs in parallel with the network probes.
	wg.Add(1)
	go func() {
		defer wg.Done()
		resp.Counts = s.countCRDs(ctx)
	}()

	wg.Wait()
	respond.JSON(w, http.StatusOK, resp)
}

// probeTCP attempts a short TCP dial. Used for services that don't speak HTTP/1.1
// (e.g. tink-server is gRPC, smee is DHCP+iPXE).
func probeTCP(ctx context.Context, addr string) componentStatus {
	d := net.Dialer{Timeout: 2 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return componentStatus{Reachable: false, Error: err.Error()}
	}
	_ = conn.Close()
	return componentStatus{Reachable: true, URL: "tcp://" + addr}
}

// probeHTTP issues a short GET. Any HTTP response (including 4xx) means the
// service is alive — only network/TLS errors mark it unreachable.
func probeHTTP(ctx context.Context, url string) componentStatus {
	c := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return componentStatus{Reachable: false, Error: err.Error(), URL: url}
	}
	res, err := c.Do(req)
	if err != nil {
		return componentStatus{Reachable: false, Error: err.Error(), URL: url}
	}
	defer res.Body.Close()
	st := componentStatus{Reachable: true, URL: url}
	// Many tink-system services expose a Server header useful as a version proxy.
	if v := res.Header.Get("Server"); v != "" {
		st.Version = v
	}
	return st
}

// countCRDs lists the three top-level Tinkerbell CRDs and returns counts.
// On error a count is left at -1 to indicate "unknown" rather than zero.
func (s *StatusHandler) countCRDs(ctx context.Context) counts {
	out := counts{Hardware: -1, Templates: -1, Workflows: -1}
	if s.client == nil {
		return out
	}
	var hw tinkv1.HardwareList
	if err := s.client.List(ctx, &hw, client.InNamespace(s.namespace)); err == nil {
		out.Hardware = len(hw.Items)
	}
	var tpl tinkv1.TemplateList
	if err := s.client.List(ctx, &tpl, client.InNamespace(s.namespace)); err == nil {
		out.Templates = len(tpl.Items)
	}
	var wf tinkv1.WorkflowList
	if err := s.client.List(ctx, &wf, client.InNamespace(s.namespace)); err == nil {
		out.Workflows = len(wf.Items)
	}
	return out
}

// buildVersion reports the build's main module version when available.
// Falls back to "dev" outside of a tagged module build.
func buildVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
		// Try to surface the VCS revision injected by `go build -buildvcs=true`.
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && s.Value != "" {
				if len(s.Value) > 12 {
					return s.Value[:12]
				}
				return s.Value
			}
		}
	}
	return "dev"
}
