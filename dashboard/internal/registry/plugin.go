// Package registry exposes the plugin catalog and workflow compiler as BFF endpoints.
package registry

import (
	"encoding/json"
	"net/http"

	"git.konoss.org/kore/schmutz/dashboard/pkg/browzer/bff"
	"git.konoss.org/kore/schmutz/dashboard/pkg/browzer/sandbox"
)

// Plugin is the registry BFF plugin.
type Plugin struct {
	Registry *sandbox.Registry
}

func (p *Plugin) Name() string        { return "registry" }
func (p *Plugin) Description() string { return "Plugin catalog, action browser, and workflow compiler" }

func (p *Plugin) Register(router *bff.Router) {
	r := p.Registry

	// List all plugins
	router.HandleRaw("GET /api/registry/plugins", func(w http.ResponseWriter, req *http.Request) {
		bff.WriteJSON(w, r.List())
	})

	// Get a single plugin by name
	router.HandleRaw("GET /api/registry/plugins/{name}", func(w http.ResponseWriter, req *http.Request) {
		name := req.PathValue("name")
		plugin, ok := r.Get(name)
		if !ok {
			bff.WriteError(w, 404, "plugin not found: "+name, nil)
			return
		}
		bff.WriteJSON(w, plugin)
	})

	// List all actions across all plugins
	router.HandleRaw("GET /api/registry/actions", func(w http.ResponseWriter, req *http.Request) {
		bff.WriteJSON(w, r.Actions())
	})

	// Validate a workflow
	router.HandleRaw("POST /api/sandbox/validate", func(w http.ResponseWriter, req *http.Request) {
		var wf sandbox.Workflow
		if err := json.NewDecoder(req.Body).Decode(&wf); err != nil {
			bff.WriteError(w, 400, "invalid workflow JSON", err)
			return
		}
		errors := sandbox.Validate(r, &wf)
		bff.WriteJSON(w, map[string]any{
			"valid":  len(errors) == 0,
			"errors": errors,
		})
	})

	// Compile a workflow to bash script
	router.HandleRaw("POST /api/sandbox/compile/shell", func(w http.ResponseWriter, req *http.Request) {
		var wf sandbox.Workflow
		if err := json.NewDecoder(req.Body).Decode(&wf); err != nil {
			bff.WriteError(w, 400, "invalid workflow JSON", err)
			return
		}
		if errors := sandbox.Validate(r, &wf); len(errors) > 0 {
			bff.WriteJSON(w, map[string]any{"valid": false, "errors": errors})
			return
		}
		script, err := sandbox.CompileShell(r, &wf)
		if err != nil {
			bff.WriteError(w, 500, "compile failed", err)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(script))
	})

	// Compile a workflow to Woodpecker YAML
	router.HandleRaw("POST /api/sandbox/compile/woodpecker", func(w http.ResponseWriter, req *http.Request) {
		var wf sandbox.Workflow
		if err := json.NewDecoder(req.Body).Decode(&wf); err != nil {
			bff.WriteError(w, 400, "invalid workflow JSON", err)
			return
		}
		if errors := sandbox.Validate(r, &wf); len(errors) > 0 {
			bff.WriteJSON(w, map[string]any{"valid": false, "errors": errors})
			return
		}
		yaml, err := sandbox.CompileWoodpecker(r, &wf)
		if err != nil {
			bff.WriteError(w, 500, "compile failed", err)
			return
		}
		w.Header().Set("Content-Type", "text/yaml")
		w.Write([]byte(yaml))
	})
}
