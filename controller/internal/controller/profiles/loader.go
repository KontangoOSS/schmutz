package profiles

import (
	"log"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Registry holds loaded profiles keyed by name.
type Registry struct {
	profiles map[string]*Profile
}

// LoadProfiles reads all *.yaml files from dir and returns a Registry.
// Missing or unreadable dir returns an empty registry (not an error).
// Malformed YAML files are skipped with a warning.
func LoadProfiles(dir string) (*Registry, error) {
	reg := &Registry{profiles: make(map[string]*Profile)}

	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("profiles: directory %q not found — no profiles loaded", dir)
		return reg, nil
	}

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			log.Printf("profiles: skipping %s: %v", e.Name(), err)
			continue
		}
		var p Profile
		if err := yaml.Unmarshal(data, &p); err != nil {
			log.Printf("profiles: skipping %s: invalid YAML: %v", e.Name(), err)
			continue
		}
		if p.Name == "" {
			log.Printf("profiles: skipping %s: missing name field", e.Name())
			continue
		}
		reg.profiles[p.Name] = &p
		log.Printf("profiles: loaded %q (%d attrs, %d extra services)", p.Name, len(p.Attributes), len(p.ExtraServices))
	}

	return reg, nil
}

// Get returns the named profile, falling back to "base" if not found.
// Returns nil if neither the named profile nor "base" exists.
func (r *Registry) Get(name string) *Profile {
	if p, ok := r.profiles[name]; ok {
		return p
	}
	if p, ok := r.profiles["base"]; ok {
		return p
	}
	return nil
}
