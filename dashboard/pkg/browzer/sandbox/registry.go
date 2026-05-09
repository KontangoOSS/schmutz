package sandbox

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// Registry holds the loaded plugin catalog.
type Registry struct {
	Plugins map[string]*PluginManifest // keyed by plugin name
}

// LoadRegistry reads all plugin.json files from the given directories.
// Each directory is expected to contain subdirectories with plugin.json files,
// e.g. /home/leonardo/git/plugins/plugins-dns/plugin.json
func LoadRegistry(dirs ...string) (*Registry, error) {
	r := &Registry{Plugins: make(map[string]*PluginManifest)}

	for _, dir := range dirs {
		matches, err := filepath.Glob(filepath.Join(dir, "*/plugin.json"))
		if err != nil {
			return nil, fmt.Errorf("glob %s: %w", dir, err)
		}

		for _, path := range matches {
			data, err := os.ReadFile(path)
			if err != nil {
				log.Printf("registry: skip %s: %v", path, err)
				continue
			}

			var m PluginManifest
			if err := json.Unmarshal(data, &m); err != nil {
				log.Printf("registry: skip %s: %v", path, err)
				continue
			}

			if m.Name == "" || m.Name == "my-plugin" {
				continue // skip template
			}

			r.Plugins[m.Name] = &m
			log.Printf("registry: loaded %s (%d actions)", m.Name, len(m.Actions))
		}
	}

	return r, nil
}

// Get returns a plugin by name.
func (r *Registry) Get(name string) (*PluginManifest, bool) {
	p, ok := r.Plugins[name]
	return p, ok
}

// List returns all loaded plugins.
func (r *Registry) List() []*PluginManifest {
	out := make([]*PluginManifest, 0, len(r.Plugins))
	for _, p := range r.Plugins {
		out = append(out, p)
	}
	return out
}

// Actions returns a flat list of all actions across all plugins,
// prefixed with the plugin name (e.g. "plugins-dns.create").
func (r *Registry) Actions() []ActionRef {
	var out []ActionRef
	for _, p := range r.Plugins {
		for _, a := range p.Actions {
			out = append(out, ActionRef{
				Plugin:      p.Name,
				Action:      a.Name,
				Description: a.Description,
			})
		}
	}
	return out
}

// ActionRef is a fully qualified reference to a plugin action.
type ActionRef struct {
	Plugin      string `json:"plugin"`
	Action      string `json:"action"`
	Description string `json:"description"`
}
