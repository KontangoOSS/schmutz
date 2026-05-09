package main

import (
	"encoding/json"
	"io"
	"net/http"
)

// zitiToken returns a cached Ziti management session token, refreshing when needed.
// Writes a 500 error and returns "" if auth fails.
func (a *API) zitiToken(w http.ResponseWriter) string {
	token, err := a.ziti.Authenticate()
	if err != nil {
		errJSON(w, "ziti auth failed: "+err.Error(), http.StatusInternalServerError)
		return ""
	}
	return token
}

// requireStore writes a 503 and returns false when Bao is unavailable.
// Use at the top of any handler that cannot function without the store.
func (a *API) requireStore(w http.ResponseWriter) bool {
	if a.store == nil {
		errJSON(w, "store unavailable (Bao not configured)", http.StatusServiceUnavailable)
		return false
	}
	return true
}

// requireStoreMW wraps a handler, returning 503 if Bao is not configured.
// Use this in route registration to gate entire path groups on store availability.
func (a *API) requireStoreMW(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.requireStore(w) {
			return
		}
		h(w, r)
	}
}

// parseJSON reads and decodes the request body into v.
// Returns false and writes an error if parsing fails.
func parseJSON(r *http.Request, w http.ResponseWriter, v interface{}) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		errJSON(w, "read body: "+err.Error(), http.StatusBadRequest)
		return false
	}
	if err := json.Unmarshal(body, v); err != nil {
		errJSON(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

// jsonOrErr writes data as JSON on success, or an error response on failure.
func jsonOrErr(w http.ResponseWriter, data interface{}, err error) {
	if err != nil {
		errJSON(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// errJSON writes a JSON error response.
func errJSON(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// okJSON writes a simple success response.
func okJSON(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": message})
}
