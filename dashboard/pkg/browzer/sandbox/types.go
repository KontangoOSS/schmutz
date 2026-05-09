// Package sandbox provides the plugin registry and workflow compiler.
//
// A plugin is a config file (plugin.json). It declares actions and
// their input schemas. The sandbox loads these, lets users wire
// actions into workflows, and compiles workflows into executable scripts.
package sandbox

import "encoding/json"

// PluginManifest is the parsed plugin.json — the complete definition
// of what a plugin can do.
type PluginManifest struct {
	Name        string         `json:"name"`
	Version     string         `json:"version"`
	Description string         `json:"description"`
	Author      string         `json:"author"`
	Repo        string         `json:"repo"`
	Image       string         `json:"image"`
	Tags        []string       `json:"tags"`
	Settings    PluginSettings `json:"settings"`
	Actions     []PluginAction `json:"actions"`
}

// UnmarshalJSON handles both action formats:
//   - flat array: [{"name":"health","description":"..."}]
//   - grouped object: {"containers":["lxc-list","lxc-get",...]}
func (m *PluginManifest) UnmarshalJSON(data []byte) error {
	type Alias PluginManifest
	var raw struct {
		Alias
		RawActions json.RawMessage `json:"actions"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	*m = PluginManifest(raw.Alias)

	if len(raw.RawActions) == 0 {
		return nil
	}

	// Try flat array first
	var flat []PluginAction
	if err := json.Unmarshal(raw.RawActions, &flat); err == nil {
		m.Actions = flat
		return nil
	}

	// Try grouped object: {"category": ["action1", "action2"]}
	var grouped map[string]json.RawMessage
	if err := json.Unmarshal(raw.RawActions, &grouped); err != nil {
		return err
	}

	for category, val := range grouped {
		var names []string
		if err := json.Unmarshal(val, &names); err == nil {
			for _, name := range names {
				m.Actions = append(m.Actions, PluginAction{
					Name:        name,
					Description: category + ": " + name,
					Category:    category,
				})
			}
			continue
		}

		var objects []PluginAction
		if err := json.Unmarshal(val, &objects); err == nil {
			for i := range objects {
				objects[i].Category = category
				m.Actions = append(m.Actions, objects[i])
			}
		}
	}

	return nil
}

type PluginSettings struct {
	Required map[string]SettingDef `json:"required"`
	Optional map[string]SettingDef `json:"optional"`
}

type SettingDef struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Default     any    `json:"default,omitempty"`
	Secret      bool   `json:"secret,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

type PluginAction struct {
	Name          string                `json:"name"`
	Description   string                `json:"description"`
	Category      string                `json:"category,omitempty"`
	ExtraSettings map[string]SettingDef `json:"extra_settings,omitempty"`
}

// -- Workflow types ----------------------------------------------------------
//
// Every step in a workflow follows the same contract:
//
//	IN:  /run/creds.env    — secrets fetched fresh from Bao for THIS plugin
//	IN:  /run/context.json — accumulated outputs from all completed previous steps
//	IN:  PLUGIN_* env vars — static params + resolved context refs
//	OUT: /run/output.json  — this step's output, merged into context for the next step
//
// Credentials are NEVER passed between steps. Each step fetches its own
// creds from Bao using a secret path declared in the workflow step.
// The context file is the chain state — it grows as steps complete.

// Workflow is a named sequence of steps that wire plugin actions together.
type Workflow struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Steps       []WorkflowStep `json:"steps"`
}

// WorkflowStep is a single action call in a workflow.
type WorkflowStep struct {
	ID         string            `json:"id"`
	Plugin     string            `json:"plugin"`
	Action     string            `json:"action"`
	CredsPath  string            `json:"creds_path"`           // Bao secret path for this step's creds
	Inputs     map[string]string `json:"inputs"`               // static values or context refs "{{ step-id.key }}"
	DependsOn  []string          `json:"depends_on,omitempty"`
}
