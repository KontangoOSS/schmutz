package ziti

// Standard Ziti resource types.
// These match the JSON returned by the management API.

type Service struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	RoleAttributes []string `json:"roleAttributes"`
	Permissions    []string `json:"permissions"`
}

type IdentityType struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Identity struct {
	ID                      string       `json:"id"`
	Name                    string       `json:"name"`
	Type                    IdentityType `json:"type"`
	RoleAttributes          []string     `json:"roleAttributes"`
	HasAPISession           bool         `json:"hasApiSession"`
	HasEdgeRouterConnection bool         `json:"hasEdgeRouterConnection"`
}

type EdgeRouter struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	IsOnline       bool     `json:"isOnline"`
	RoleAttributes []string `json:"roleAttributes"`
	Hostname       string   `json:"hostname"`
}

type ServicePolicy struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Type          string   `json:"type"`
	Semantic      string   `json:"semantic"`
	IdentityRoles []string `json:"identityRoles"`
	ServiceRoles  []string `json:"serviceRoles"`
}

type EntityRef struct {
	Entity string `json:"entity"`
	Name   string `json:"name"`
	ID     string `json:"id"`
}

type Terminator struct {
	ID      string    `json:"id"`
	Service EntityRef `json:"service"`
	Router  EntityRef `json:"router"`
	Binding string    `json:"binding"`
	Address string    `json:"address"`
	HostID  string    `json:"hostId"`
}

type Summary struct {
	Services    int `json:"services"`
	Identities  int `json:"identities"`
	EdgeRouters int `json:"edgeRouters"`
	Terminators int `json:"terminators"`
	APISessions int `json:"apiSessions"`
	Sessions    int `json:"sessions"`
}
