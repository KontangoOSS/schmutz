package sandbox

import (
	"fmt"
	"strings"
)

// CompileShell turns a workflow into a bash script where each step:
//  1. Fetches its own creds from Bao into /run/creds.env
//  2. Reads context from previous steps via /run/context.json
//  3. Runs the plugin container with creds + context + inputs mounted
//  4. Merges its /run/output.json into the context for the next step
func CompileShell(r *Registry, w *Workflow) (string, error) {
	var b strings.Builder

	b.WriteString("#!/usr/bin/env bash\n")
	b.WriteString("set -euo pipefail\n\n")
	b.WriteString(fmt.Sprintf("# Workflow: %s\n", w.Name))
	if w.Description != "" {
		b.WriteString(fmt.Sprintf("# %s\n", w.Description))
	}
	b.WriteString("#\n")
	b.WriteString("# Contract per step:\n")
	b.WriteString("#   IN:  /run/creds.env    — fresh from Bao (step-scoped secret path)\n")
	b.WriteString("#   IN:  /run/context.json — accumulated outputs from previous steps\n")
	b.WriteString("#   OUT: /run/output.json  — this step's output → merged into context\n")
	b.WriteString("\n")

	// Working directory for the run
	b.WriteString("WORKDIR=$(mktemp -d)\n")
	b.WriteString("trap 'rm -rf \"$WORKDIR\"' EXIT\n")
	b.WriteString("echo '{}' > \"$WORKDIR/context.json\"\n\n")

	for i, step := range w.Steps {
		plugin, ok := r.Get(step.Plugin)
		if !ok {
			return "", fmt.Errorf("step %q: unknown plugin %q", step.ID, step.Plugin)
		}

		b.WriteString(fmt.Sprintf("# --- Step %d: %s [%s.%s] ---\n", i+1, step.ID, step.Plugin, step.Action))
		b.WriteString(fmt.Sprintf("echo \">> Step %d/%d: %s (%s.%s)\"\n", i+1, len(w.Steps), step.ID, step.Plugin, step.Action))

		// 1. Fetch creds fresh from Bao
		if step.CredsPath != "" {
			b.WriteString(fmt.Sprintf("bao kv get -format=json %q | jq -r '.data.data | to_entries[] | \"export PLUGIN_\" + (.key | ascii_upcase) + \"=\\\"\" + .value + \"\\\"\"' > \"$WORKDIR/creds.env\"\n", step.CredsPath))
		} else {
			b.WriteString("echo '# no creds_path for this step' > \"$WORKDIR/creds.env\"\n")
		}

		// 2. Run the plugin container
		b.WriteString("docker run --rm \\\n")
		b.WriteString("  -v \"$WORKDIR/creds.env:/run/creds.env:ro\" \\\n")
		b.WriteString("  -v \"$WORKDIR/context.json:/run/context.json:ro\" \\\n")
		b.WriteString("  -v \"$WORKDIR/output.json:/run/output.json\" \\\n")
		b.WriteString(fmt.Sprintf("  -e PLUGIN_ACTION=%q \\\n", step.Action))

		// Static inputs as env vars, resolving context refs
		for k, v := range step.Inputs {
			envKey := "PLUGIN_" + strings.ToUpper(k)
			if strings.HasPrefix(v, "{{") && strings.HasSuffix(v, "}}") {
				// Context ref: {{ step-id.key }} → read from context.json at runtime
				ref := strings.TrimSpace(v[2 : len(v)-2])
				parts := strings.SplitN(ref, ".", 2)
				if len(parts) == 2 {
					b.WriteString(fmt.Sprintf("  -e %s=\"$(jq -r '.[\"%s\"][\"%s\"] // empty' \"$WORKDIR/context.json\")\" \\\n", envKey, parts[0], parts[1]))
				}
			} else {
				b.WriteString(fmt.Sprintf("  -e %s=%q \\\n", envKey, v))
			}
		}

		b.WriteString(fmt.Sprintf("  %s\n\n", plugin.Image))

		// 3. Merge output into context
		b.WriteString(fmt.Sprintf("# Merge step output into context under key \"%s\"\n", step.ID))
		b.WriteString(fmt.Sprintf("if [ -f \"$WORKDIR/output.json\" ]; then\n"))
		b.WriteString(fmt.Sprintf("  jq --argjson step \"$(cat \"$WORKDIR/output.json\")\" '. + {\"%s\": $step}' \"$WORKDIR/context.json\" > \"$WORKDIR/context.tmp\"\n", step.ID))
		b.WriteString("  mv \"$WORKDIR/context.tmp\" \"$WORKDIR/context.json\"\n")
		b.WriteString("  rm -f \"$WORKDIR/output.json\"\n")
		b.WriteString("fi\n\n")
	}

	b.WriteString("echo \">> Workflow complete. Final context:\"\n")
	b.WriteString("cat \"$WORKDIR/context.json\"\n")

	return b.String(), nil
}

// CompileWoodpecker turns a workflow into a .woodpecker.yml pipeline.
// Each step gets a secrets fetch + the actual plugin call.
func CompileWoodpecker(r *Registry, w *Workflow) (string, error) {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("# Workflow: %s\n", w.Name))
	if w.Description != "" {
		b.WriteString(fmt.Sprintf("# %s\n", w.Description))
	}
	b.WriteString("\nsteps:\n")

	for _, step := range w.Steps {
		plugin, ok := r.Get(step.Plugin)
		if !ok {
			return "", fmt.Errorf("step %q: unknown plugin %q", step.ID, step.Plugin)
		}

		// Creds fetch step (if creds_path is set)
		if step.CredsPath != "" {
			b.WriteString(fmt.Sprintf("  %s-creds:\n", step.ID))
			b.WriteString("    image: git.kontango.io/plugins/secrets\n")
			b.WriteString("    settings:\n")
			b.WriteString("      action: fetch\n")
			b.WriteString(fmt.Sprintf("      path: %s\n", step.CredsPath))
			if len(step.DependsOn) > 0 {
				b.WriteString("    depends_on:\n")
				for _, dep := range step.DependsOn {
					b.WriteString(fmt.Sprintf("      - %s\n", dep))
				}
			}
			b.WriteString("\n")
		}

		// Actual plugin step
		b.WriteString(fmt.Sprintf("  %s:\n", step.ID))
		b.WriteString(fmt.Sprintf("    image: %s\n", plugin.Image))
		b.WriteString("    settings:\n")
		b.WriteString(fmt.Sprintf("      action: %s\n", step.Action))

		for k, v := range step.Inputs {
			b.WriteString(fmt.Sprintf("      %s: %s\n", k, v))
		}

		// Depends on its own creds fetch + declared deps
		deps := make([]string, 0)
		if step.CredsPath != "" {
			deps = append(deps, step.ID+"-creds")
		}
		deps = append(deps, step.DependsOn...)
		if len(deps) > 0 {
			b.WriteString("    depends_on:\n")
			for _, dep := range deps {
				b.WriteString(fmt.Sprintf("      - %s\n", dep))
			}
		}

		b.WriteString("\n")
	}

	return b.String(), nil
}

// Validate checks that a workflow references only known plugins and actions.
func Validate(r *Registry, w *Workflow) []string {
	var errors []string

	stepIDs := make(map[string]bool)
	for _, step := range w.Steps {
		if stepIDs[step.ID] {
			errors = append(errors, fmt.Sprintf("duplicate step ID %q", step.ID))
		}
		stepIDs[step.ID] = true

		plugin, ok := r.Get(step.Plugin)
		if !ok {
			errors = append(errors, fmt.Sprintf("step %q: unknown plugin %q", step.ID, step.Plugin))
			continue
		}

		found := false
		for _, a := range plugin.Actions {
			if a.Name == step.Action {
				found = true
				break
			}
		}
		if !found {
			errors = append(errors, fmt.Sprintf("step %q: plugin %q has no action %q", step.ID, step.Plugin, step.Action))
		}

		for _, dep := range step.DependsOn {
			if !stepIDs[dep] {
				errors = append(errors, fmt.Sprintf("step %q: depends on unknown step %q", step.ID, dep))
			}
		}
	}

	return errors
}
