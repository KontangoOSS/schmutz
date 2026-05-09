package gateway

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// HostInfo carries static identity fields for the service index response.
type HostInfo struct {
	ZitiIdentity   string `json:"ziti_identity"`
	App            string `json:"app"`
	Deployment     string `json:"deployment"`
	Tenant         string `json:"tenant"`
	GatewayVersion string `json:"gateway_version"`
}

// Server is the http.Handler for the schmutz-api.{zone} endpoint.
type Server struct {
	info    HostInfo
	entries map[string]ServiceEntry
	mux     *http.ServeMux
}

// NewServer builds a Server from discovered entries.
func NewServer(info HostInfo, entries []ServiceEntry) *Server {
	if info.GatewayVersion == "" {
		info.GatewayVersion = "0.1.0"
	}
	s := &Server{
		info:    info,
		entries: make(map[string]ServiceEntry, len(entries)),
	}
	for _, e := range entries {
		s.entries[e.Name] = e
	}
	s.mux = http.NewServeMux()
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/services/", s.handleServices)
	s.mux.HandleFunc("/docs/", s.handleDocs)
	s.mux.HandleFunc("/", s.handleIndex)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
		"ts":     time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	type serviceItem struct {
		Name          string `json:"name"`
		Port          uint16 `json:"port"`
		Protocol      string `json:"protocol"`
		SpecSource    string `json:"spec_source"`
		SpecURL       string `json:"spec_url"`
		DocsURL       string `json:"docs_url"`
		EndpointCount int    `json:"endpoints,omitempty"`
	}
	items := make([]serviceItem, 0, len(s.entries))
	for _, e := range s.entries {
		items = append(items, serviceItem{
			Name:          e.Name,
			Port:          e.Port,
			Protocol:      e.Protocol,
			SpecSource:    e.SpecSource,
			SpecURL:       "/services/" + e.Name + "/spec",
			DocsURL:       "/docs/" + e.Name,
			EndpointCount: e.EndpointCount,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"host":            s.info.App + "/" + s.info.Deployment,
		"ziti_identity":   s.info.ZitiIdentity,
		"tenant":          s.info.Tenant,
		"app":             s.info.App,
		"deployment":      s.info.Deployment,
		"gateway_version": s.info.GatewayVersion,
		"services":        items,
	})
}

func (s *Server) handleServices(w http.ResponseWriter, r *http.Request) {
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/services/"), "/", 2)
	if len(parts) < 2 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	name, sub := parts[0], parts[1]
	entry, ok := s.entries[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	switch sub {
	case "spec":
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(http.StatusOK)
		w.Write(entry.SpecJSON)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleDocs(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/docs/")
	name = strings.TrimSuffix(name, "/")
	if _, ok := s.entries[name]; !ok {
		http.NotFound(w, r)
		return
	}
	ServeScalar(w, r, name, "/services/"+name+"/spec")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
