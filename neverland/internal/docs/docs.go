package docs

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static
var staticFS embed.FS

// Handler returns an http.Handler that serves the embedded Scalar API reference
// at / and the OpenAPI spec at /openapi.yaml.
func Handler() http.Handler {
	sub, _ := fs.Sub(staticFS, "static")
	return http.FileServer(http.FS(sub))
}
