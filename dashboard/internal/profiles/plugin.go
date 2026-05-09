// Package profiles provides the profile builder — create, read, update,
// delete identity profiles stored in Ziti's appData.
//
// Profiles are stored on a well-known identity called "fabric-profiles".
// Each profile is a key in that identity's appData map.
package profiles

import (
	"encoding/json"
	"fmt"
	"net/http"

	"git.konoss.org/kore/schmutz/dashboard/pkg/browzer/bff"
	"git.konoss.org/kore/schmutz/dashboard/pkg/browzer/ziti"
)

const profilesIdentity = "fabric-profiles"

// Profile is the definition of what an identity gets when assigned this profile.
type Profile struct {
	Name                     string   `json:"name"`
	Description              string   `json:"description"`
	IdentityRole             string   `json:"identity_role"`
	EdgeRouterRoles          []string `json:"edge_router_roles"`
	ServiceBindRoles         []string `json:"service_bind_roles"`
	ServiceDialRoles         []string `json:"service_dial_roles"`
	DefaultHostingCost       int      `json:"default_hosting_cost"`
	DefaultHostingPrecedence string   `json:"default_hosting_precedence"`
	AuthPolicy               string   `json:"auth_policy"`
	TerminatorStrategy       string   `json:"terminator_strategy"`
	PromotesTo               string   `json:"promotes_to,omitempty"`
	HeartbeatEnabled         bool     `json:"heartbeat_enabled"`
	HeartbeatFallback        string   `json:"heartbeat_fallback,omitempty"`
}

type Plugin struct{}

func (p *Plugin) Name() string { return "profiles" }
func (p *Plugin) Description() string {
	return "Profile builder — create and manage identity templates"
}

func (p *Plugin) Register(router *bff.Router) {
	z := router.Ziti

	// List all profiles
	router.Handle("GET /api/v1/profiles", func(token string, w http.ResponseWriter, r *http.Request) {
		profiles, err := loadProfiles(z, token)
		if err != nil {
			bff.WriteError(w, 502, "load profiles failed", err)
			return
		}
		list := make([]Profile, 0, len(profiles))
		for _, p := range profiles {
			list = append(list, p)
		}
		bff.WriteJSON(w, list)
	})

	// Get a single profile
	router.Handle("GET /api/v1/profiles/{name}", func(token string, w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		profiles, err := loadProfiles(z, token)
		if err != nil {
			bff.WriteError(w, 502, "load profiles failed", err)
			return
		}
		prof, ok := profiles[name]
		if !ok {
			bff.WriteError(w, 404, "profile not found: "+name, nil)
			return
		}
		bff.WriteJSON(w, prof)
	})

	// Create or update a profile
	router.Handle("PUT /api/v1/profiles/{name}", func(token string, w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		var prof Profile
		if err := json.NewDecoder(r.Body).Decode(&prof); err != nil {
			bff.WriteError(w, 400, "invalid JSON", err)
			return
		}
		prof.Name = name

		profiles, err := loadProfiles(z, token)
		if err != nil {
			bff.WriteError(w, 502, "load profiles failed", err)
			return
		}
		profiles[name] = prof

		if err := saveProfiles(z, token, profiles); err != nil {
			bff.WriteError(w, 502, "save profiles failed", err)
			return
		}
		bff.WriteJSON(w, prof)
	})

	// Delete a profile
	router.Handle("DELETE /api/v1/profiles/{name}", func(token string, w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		profiles, err := loadProfiles(z, token)
		if err != nil {
			bff.WriteError(w, 502, "load profiles failed", err)
			return
		}
		delete(profiles, name)

		if err := saveProfiles(z, token, profiles); err != nil {
			bff.WriteError(w, 502, "save profiles failed", err)
			return
		}
		w.WriteHeader(204)
	})

	// Get all role descriptions
	router.Handle("GET /api/v1/roles", func(token string, w http.ResponseWriter, r *http.Request) {
		descs, err := loadRoleDescriptions(z, token)
		if err != nil {
			bff.WriteError(w, 502, "load role descriptions failed", err)
			return
		}
		bff.WriteJSON(w, descs)
	})

	// Set a role description
	router.Handle("PUT /api/v1/roles/{type}/{name}", func(token string, w http.ResponseWriter, r *http.Request) {
		rType := r.PathValue("type") // "identity", "service", or "router"
		rName := r.PathValue("name")
		var body struct {
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			bff.WriteError(w, 400, "invalid JSON", err)
			return
		}

		descs, err := loadRoleDescriptions(z, token)
		if err != nil {
			descs = map[string]map[string]string{}
		}
		if descs[rType] == nil {
			descs[rType] = map[string]string{}
		}
		descs[rType][rName] = body.Description

		if err := saveRoleDescriptions(z, token, descs); err != nil {
			bff.WriteError(w, 502, "save failed", err)
			return
		}
		bff.WriteJSON(w, map[string]string{"type": rType, "name": rName, "description": body.Description})
	})

	// List building blocks (roles available for composing profiles)
	router.Handle("GET /api/v1/profiles/building-blocks", func(token string, w http.ResponseWriter, r *http.Request) {
		blocks, err := loadBuildingBlocks(z, token)
		if err != nil {
			bff.WriteError(w, 502, "load building blocks failed", err)
			return
		}
		bff.WriteJSON(w, blocks)
	})

	// Apply a profile to an identity
	router.Handle("POST /api/v1/profiles/{name}/apply/{identity}", func(token string, w http.ResponseWriter, r *http.Request) {
		profileName := r.PathValue("name")
		identityName := r.PathValue("identity")

		profiles, err := loadProfiles(z, token)
		if err != nil {
			bff.WriteError(w, 502, "load profiles failed", err)
			return
		}
		prof, ok := profiles[profileName]
		if !ok {
			bff.WriteError(w, 404, "profile not found: "+profileName, nil)
			return
		}

		// Find the identity
		identityID, err := findByName(z, token, "identities", identityName)
		if err != nil {
			bff.WriteError(w, 404, "identity not found: "+identityName, err)
			return
		}

		// Apply: set role to the profile's identity_role + store profile name in appData
		patch := map[string]any{
			"roleAttributes": []string{prof.IdentityRole},
			"appData": map[string]any{
				"fabric": map[string]any{
					"profile": profileName,
				},
			},
			"defaultHostingCost":       prof.DefaultHostingCost,
			"defaultHostingPrecedence": prof.DefaultHostingPrecedence,
		}

		_, err = z.Patch(token, "/edge/management/v1/identities/"+identityID, mustJSON(patch))
		if err != nil {
			bff.WriteError(w, 502, "apply profile failed", err)
			return
		}

		bff.WriteJSON(w, map[string]any{
			"identity": identityName,
			"profile":  profileName,
			"role":     prof.IdentityRole,
			"applied":  true,
		})
	})
}

// -- Internal helpers --------------------------------------------------------

func loadProfiles(z *ziti.Client, token string) (map[string]Profile, error) {
	id, err := findByName(z, token, "identities", profilesIdentity)
	if err != nil {
		return map[string]Profile{}, nil // No profiles identity yet, return empty
	}
	raw, err := z.Get(token, "/edge/management/v1/identities/"+id)
	if err != nil {
		return nil, err
	}
	var env struct {
		Data struct {
			AppData map[string]json.RawMessage `json:"appData"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}

	profiles := map[string]Profile{}
	profilesRaw, ok := env.Data.AppData["profiles"]
	if !ok {
		return profiles, nil
	}
	json.Unmarshal(profilesRaw, &profiles)
	return profiles, nil
}

func saveProfiles(z *ziti.Client, token string, profiles map[string]Profile) error {
	id, err := findByName(z, token, "identities", profilesIdentity)
	if err != nil {
		return fmt.Errorf("fabric-profiles identity not found — create it first")
	}

	patch := map[string]any{
		"appData": map[string]any{
			"profiles": profiles,
		},
	}
	patchBytes, _ := json.Marshal(patch)

	_, err = z.Patch(token, fmt.Sprintf("/edge/management/v1/identities/%s", id), patchBytes)
	return err
}

type BuildingBlocks struct {
	IdentityRoles []string `json:"identity_roles"`
	ServiceRoles  []string `json:"service_roles"`
	RouterRoles   []string `json:"router_roles"`
	AuthPolicies  []string `json:"auth_policies"`
	Strategies    []string `json:"strategies"`
	Precedences   []string `json:"precedences"`
}

func loadBuildingBlocks(z *ziti.Client, token string) (*BuildingBlocks, error) {
	blocks := &BuildingBlocks{
		Strategies:  []string{"smartrouting", "weighted", "random", "ha"},
		Precedences: []string{"default", "required", "failed"},
	}

	// Identity roles
	raw, err := z.Get(token, "/edge/management/v1/identity-role-attributes")
	if err == nil {
		var env struct {
			Data []string `json:"data"`
		}
		json.Unmarshal(raw, &env)
		blocks.IdentityRoles = env.Data
	}

	// Service roles
	raw, err = z.Get(token, "/edge/management/v1/service-role-attributes")
	if err == nil {
		var env struct {
			Data []string `json:"data"`
		}
		json.Unmarshal(raw, &env)
		blocks.ServiceRoles = env.Data
	}

	// Router roles
	raw, err = z.Get(token, "/edge/management/v1/edge-router-role-attributes")
	if err == nil {
		var env struct {
			Data []string `json:"data"`
		}
		json.Unmarshal(raw, &env)
		blocks.RouterRoles = env.Data
	}

	// Auth policies
	raw, err = z.Get(token, "/edge/management/v1/auth-policies?limit=100")
	if err == nil {
		var env struct {
			Data []struct {
				Name string `json:"name"`
			} `json:"data"`
		}
		json.Unmarshal(raw, &env)
		for _, ap := range env.Data {
			blocks.AuthPolicies = append(blocks.AuthPolicies, ap.Name)
		}
	}

	return blocks, nil
}

func findByName(z *ziti.Client, token, resource, name string) (string, error) {
	raw, err := z.Get(token, fmt.Sprintf("/edge/management/v1/%s?filter=name%%3D%%22%s%%22", resource, name))
	if err != nil {
		return "", err
	}
	var env struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	json.Unmarshal(raw, &env)
	if len(env.Data) == 0 {
		return "", fmt.Errorf("%s %q not found", resource, name)
	}
	return env.Data[0].ID, nil
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// -- Role descriptions -------------------------------------------------------
// Stored in appData["role_descriptions"] on the fabric-profiles identity.
// Structure: {"identity": {"quarantine": "desc..."}, "service": {...}, "router": {...}}

func loadRoleDescriptions(z *ziti.Client, token string) (map[string]map[string]string, error) {
	id, err := findByName(z, token, "identities", profilesIdentity)
	if err != nil {
		return map[string]map[string]string{}, nil
	}
	raw, err := z.Get(token, "/edge/management/v1/identities/"+id)
	if err != nil {
		return nil, err
	}
	var env struct {
		Data struct {
			AppData map[string]json.RawMessage `json:"appData"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}

	descs := map[string]map[string]string{}
	if raw, ok := env.Data.AppData["role_descriptions"]; ok {
		json.Unmarshal(raw, &descs)
	}
	return descs, nil
}

func saveRoleDescriptions(z *ziti.Client, token string, descs map[string]map[string]string) error {
	id, err := findByName(z, token, "identities", profilesIdentity)
	if err != nil {
		return fmt.Errorf("fabric-profiles identity not found")
	}

	// Load current appData, merge role_descriptions, save back
	raw, err := z.Get(token, "/edge/management/v1/identities/"+id)
	if err != nil {
		return err
	}
	var env struct {
		Data struct {
			AppData map[string]json.RawMessage `json:"appData"`
		} `json:"data"`
	}
	json.Unmarshal(raw, &env)

	current := map[string]any{}
	for k, v := range env.Data.AppData {
		var val any
		json.Unmarshal(v, &val)
		current[k] = val
	}
	current["role_descriptions"] = descs

	patch := map[string]any{"appData": current}
	_, err = z.Patch(token, fmt.Sprintf("/edge/management/v1/identities/%s", id), mustJSON(patch))
	return err
}
