package ziti

type EnrollmentRequest struct {
	IdentityName   string
	RoleAttributes []string
	Tags           map[string]string
}

type EnrollmentResult struct {
	IdentityID   string
	IdentityName string
	JWT          string
	CABundle     string
}

type Identity struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	IsAdmin        bool              `json:"isAdmin"`
	RoleAttributes []string          `json:"roleAttributes"`
	Tags           map[string]string `json:"tags"`
}

type IdentityFilter struct {
	NameContains string
	HasRole      string
	HasTagKey    string
	HasTagValue  string
}

type UpdateIdentityRequest struct {
	// RoleAttributes is always sent on PATCH; pass the current value to
	// preserve when updating only Tags, or Ziti will clear it.
	RoleAttributes []string
	Tags           map[string]string
}
