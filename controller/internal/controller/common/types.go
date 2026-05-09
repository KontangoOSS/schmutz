package common

// IdentityInfo is the unified view of an identity across Bao and Ziti.
type IdentityInfo struct {
	Name   string `json:"name"`
	Exists bool   `json:"exists"`

	// Bao identity engine
	InBao         bool              `json:"in_bao"`
	BaoEntityID   string            `json:"bao_entity_id,omitempty"`
	BaoEntityName string            `json:"bao_entity_name,omitempty"`
	BaoMetadata   map[string]string `json:"bao_metadata,omitempty"`
	BaoPolicies   []string          `json:"bao_policies,omitempty"`

	// Ziti overlay
	InZiti         bool                   `json:"in_ziti"`
	ZitiID         string                 `json:"ziti_id,omitempty"`
	ZitiAttributes []string               `json:"ziti_attributes,omitempty"`
	ZitiAppData    map[string]interface{} `json:"ziti_app_data,omitempty"`

	// Machine registration
	IsRegistered bool   `json:"is_registered"`
	MachineID    string `json:"machine_id,omitempty"`
	Nickname     string `json:"nickname,omitempty"`

	// Known machine record
	IsKnown      bool                   `json:"is_known"`
	KnownMachine map[string]interface{} `json:"known_machine,omitempty"`

	// Profile
	ProfileName string                 `json:"profile_name,omitempty"`
	Profile     map[string]interface{} `json:"profile,omitempty"`
}

// ProfileInfo is a profile with resolved references.
type ProfileInfo struct {
	Name        string                 `json:"name"`
	Data        map[string]interface{} `json:"data"`
	Attributes  []string               `json:"attributes,omitempty"`
	Application map[string]interface{} `json:"application,omitempty"`
	Build       map[string]interface{} `json:"build,omitempty"`
	Sizing      map[string]interface{} `json:"sizing,omitempty"`
}

// OSInfo is a resolved OS catalog entry.
type OSInfo struct {
	Ref       string `json:"ref"`
	Family    string `json:"family"`
	Distro    string `json:"distro"`
	Version   string `json:"version"`
	Codename  string `json:"codename,omitempty"`
	Supported bool   `json:"supported"`
}

// BanInfo is the result of a ban check.
type BanInfo struct {
	Banned      bool     `json:"banned"`
	Identifiers []string `json:"identifiers,omitempty"`
}
