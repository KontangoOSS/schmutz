// Package bff provides the BFF (Backend-for-Frontend) framework for Browzer apps.
//
// It handles the repeating concerns: Ziti session token caching, JSON
// response encoding, error formatting, and providing a clean handler
// signature so app code doesn't deal with boilerplate.
package bff

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"git.konoss.org/kore/schmutz/dashboard/pkg/browzer/ziti"
	"github.com/openziti/edge-api/rest_management_api_client"
)

// Handler is the signature for BFF route handlers.
// The token is a valid zt-session token, cached and refreshed automatically.
type Handler func(token string, w http.ResponseWriter, r *http.Request)

// Router is the BFF router. It wraps http.ServeMux and adds Ziti
// session management so handlers receive a ready-to-use token.
type Router struct {
	Ziti *ziti.Client
	Mux  *http.ServeMux

	mu       sync.Mutex
	token    string
	tokenAt  time.Time
	tokenTTL time.Duration
}

// New creates a BFF router with the given Ziti client.
func New(client *ziti.Client) *Router {
	return &Router{
		Ziti:     client,
		Mux:      http.NewServeMux(),
		tokenTTL: 20 * time.Minute, // Ziti cert sessions last 30m — refresh 10m early
	}
}

// Token returns a cached Ziti session token, refreshing when stale.
func (rt *Router) Token() (string, error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	if rt.token != "" && time.Since(rt.tokenAt) < rt.tokenTTL {
		return rt.token, nil
	}

	tok, err := rt.Ziti.Authenticate()
	if err != nil {
		return "", err
	}
	rt.token = tok
	rt.tokenAt = time.Now()
	return tok, nil
}

// MgmtClient returns a typed SDK management client using the cached token.
func (rt *Router) MgmtClient() (*rest_management_api_client.ZitiEdgeManagement, error) {
	tok, err := rt.Token()
	if err != nil {
		return nil, err
	}
	return rt.Ziti.MgmtClient(tok)
}

// InvalidateToken clears the cached token, forcing re-auth on next call.
func (rt *Router) InvalidateToken() {
	rt.mu.Lock()
	rt.token = ""
	rt.mu.Unlock()
}

// Handle registers a BFF handler at the given pattern.
// The handler receives a valid Ziti session token automatically.
// If the handler writes a 401, the token is invalidated and the request retried once.
func (rt *Router) Handle(pattern string, h Handler) {
	rt.Mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		tok, err := rt.Token()
		if err != nil {
			WriteError(w, http.StatusBadGateway, "ziti auth failed", err)
			return
		}
		// Use a response recorder to detect 401 and retry with fresh token
		rec := &statusRecorder{ResponseWriter: w}
		h(tok, rec, r)
		if rec.status == http.StatusUnauthorized {
			rt.InvalidateToken()
			tok2, err2 := rt.Token()
			if err2 != nil {
				return
			}
			h(tok2, w, r)
		}
	})
}

// statusRecorder wraps ResponseWriter to capture the status code.
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	if code != http.StatusUnauthorized {
		r.ResponseWriter.WriteHeader(code)
		r.wrote = true
	}
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == http.StatusUnauthorized {
		return len(b), nil // discard — will be rewritten on retry
	}
	return r.ResponseWriter.Write(b)
}

// HandleRaw registers a standard http.HandlerFunc (no token needed).
func (rt *Router) HandleRaw(pattern string, h http.HandlerFunc) {
	rt.Mux.HandleFunc(pattern, h)
}

// -- JSON response helpers ---------------------------------------------------

// WriteJSON encodes v as JSON and writes it to w.
func WriteJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// WriteRaw writes raw JSON bytes to w.
func WriteRaw(w http.ResponseWriter, data json.RawMessage) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// WriteError logs the error and writes a JSON error response.
func WriteError(w http.ResponseWriter, status int, msg string, err error) {
	log.Printf("ERROR: %s: %v", msg, err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
