package gateway

import (
	_ "embed"
	"net/http"
	"strings"
)

//go:embed scalar_embed/scalar.html
var scalarHTML string

// ServeScalar renders the Scalar API reference UI for the named service.
func ServeScalar(w http.ResponseWriter, r *http.Request, title, specURL string) {
	page := strings.ReplaceAll(scalarHTML, "{{SPEC_URL}}", specURL)
	page = strings.ReplaceAll(page, "{{TITLE}}", title)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(page))
}
