// Package api defines the service interfaces that the schmutz-controller
// HTTP layer depends on. Any concrete implementation that satisfies these
// interfaces can be wired into the API struct — production services, test
// doubles, or future alternate implementations.
//
// Boundaries:
//
//	BaoStore    — KV reads and writes against OpenBao. Used by all /api/bao/* routes.
//	ZitiAdmin   — Ziti management API. Used by all /api/ziti/* routes.
//	IdentityMgr — Bao identity subsystem (entities, groups, OIDC). Used by /api/identity/* routes.
//	MachineStore — Enrollment state, pulse records, machine inventory. Used by /api/machines, /api/pulse, /api/ops routes.
//	SecuritySvc — Fingerprint validation, ban/unban, audit. Used by /api/security/* routes.
//	DiscoverySvc — Service discovery against live machines. Used by /api/discovery/* routes.
//	MetricsSvc  — Cluster metrics (Ziti, Bao, latency). Used by /api/metrics/* routes.
//
// Design rules:
//   - All methods return (result, error). No panics.
//   - Interfaces expose only what the HTTP layer actually needs. Internal
//     implementation helpers are not part of the interface.
//   - String parameters are plain values, not wrapped types — keeps call
//     sites readable and avoids import chains into internal packages.
package api

import "git.konoss.org/kore/schmutz/controller/internal/service"

// Entity is re-exported here for callers that depend on the api package
// without importing service directly.
type Entity = service.Entity

// BaoStore is the read/write interface for OpenBao KV secrets and advanced
// operations. Corresponds to service.StoreService in the concrete layer.
type BaoStore interface {
	// Basic KV
	Get(mount, path string) (map[string]interface{}, error)
	Put(mount, path string, data map[string]interface{}) error
	Delete(mount, path string) error
	List(mount, path string) ([]string, error)
	Patch(mount, path string, data map[string]interface{}) error

	// KV metadata
	GetMetadata(mount, secretPath string) (map[string]interface{}, error)
	PutMetadata(mount, secretPath string, data map[string]interface{}) error
	DeleteMetadata(mount, secretPath string) error
	GetSecretVersion(mount, secretPath string, version int) (map[string]interface{}, error)
	DeleteVersions(mount, secretPath string, versions []int) error
	UndeleteVersions(mount, secretPath string, versions []int) error
	DestroyVersions(mount, secretPath string, versions []int) error
	GetKVConfig(mount string) (map[string]interface{}, error)
	PutKVConfig(mount string, maxVersions int, casRequired bool, deleteVersionAfter string) error

	// Audit & system
	ListAuditDevices() (map[string]interface{}, error)
	EnableAuditDevice(path, deviceType string, options map[string]interface{}) error
	DisableAuditDevice(path string) error
	WriteAuditLog(machineID, event string, details map[string]interface{}) error

	// Leases
	LookupLease(leaseID string) (map[string]interface{}, error)
	RenewLease(leaseID string, increment int) (map[string]interface{}, error)
	RevokeLease(leaseID string) error
	ListLeases(prefix string) ([]string, error)

	// Token operations
	CreateToken(policies []string, ttl string, metadata map[string]string) (string, error)
	LookupToken(token string) (map[string]interface{}, error)
	RevokeToken(token string) error
	ListAccessors() ([]string, error)

	// Wrapping
	Wrap(data map[string]interface{}, ttl string) (map[string]interface{}, error)
	Unwrap(token string) (map[string]interface{}, error)
	LookupWrap(token string) (map[string]interface{}, error)

	// Plugins
	ListPlugins() (map[string]interface{}, error)
	GetPlugin(name, pluginType string) (map[string]interface{}, error)
}

// ZitiAdmin is the management interface for the Ziti overlay network.
// Corresponds to service.ZitiService in the concrete layer.
type ZitiAdmin interface {
	// Authentication — callers must obtain a token before calling other methods
	Login(username, password, baseURL string) (string, error)

	// Identities
	ListIdentities(token string) ([]map[string]interface{}, error)
	GetIdentity(token, id string) (map[string]interface{}, error)
	CreateIdentity(token, name string, attrs []string, appData map[string]interface{}) (id, jwt string, err error)
	UpdateIdentity(token, id string, name string, attrs []string) error
	DeleteIdentity(token, id string) error

	// Services
	ListServices(token string) ([]map[string]interface{}, error)
	GetService(token, id string) (map[string]interface{}, error)
	CreateService(token, name string, attrs []string) (string, error)
	UpdateService(token, id string, data map[string]interface{}) error
	DeleteService(token, id string) error

	// Configs
	ListConfigs(token string) ([]map[string]interface{}, error)
	GetConfig(token, id string) (map[string]interface{}, error)
	CreateConfig(token, name, configTypeID string, data interface{}) (string, error)
	UpdateConfig(token, id string, data interface{}) error
	DeleteConfig(token, id string) error

	// Service policies
	ListServicePolicies(token string) ([]map[string]interface{}, error)
	GetServicePolicy(token, id string) (map[string]interface{}, error)
	CreateServicePolicy(token, name, policyType, semantic string, serviceRoles, identityRoles []string) (string, error)
	UpdateServicePolicy(token, id string, data map[string]interface{}) error
	DeleteServicePolicy(token, id string) error

	// Edge routers
	ListEdgeRouters(token string) ([]map[string]interface{}, error)
	CreateEdgeRouter(token, name string, attrs []string, tunnelerEnabled bool) (string, error)
	GetEdgeRouter(token, id string) (map[string]interface{}, error)
	UpdateEdgeRouter(token, id string, attrs []string) error
	DeleteEdgeRouter(token, id string) error

	// Edge router policies
	ListEdgeRouterPolicies(token string) ([]map[string]interface{}, error)
	CreateEdgeRouterPolicy(token, name, semantic string, identityRoles, routerRoles []string) error
	UpdateEdgeRouterPolicy(token, id string, data map[string]interface{}) error
	DeleteEdgeRouterPolicy(token, id string) error

	// Enrollments & sessions
	ListEnrollments(token string) ([]map[string]interface{}, error)
	ListAPISessions(token string) ([]map[string]interface{}, error)
	DeleteAPISession(token, id string) error

	// Auth policies & JWT signers
	ListAuthPolicies(token string) ([]map[string]interface{}, error)
	CreateAuthPolicy(token string, data map[string]interface{}) (string, error)
	ListExtJWTSigners(token string) ([]map[string]interface{}, error)
	CreateExtJWTSigner(token string, data map[string]interface{}) (string, error)
	UpdateExtJWTSigner(token, id string, data map[string]interface{}) error
	DeleteExtJWTSigner(token, id string) error

	// Misc
	GetVersion(token string) (map[string]interface{}, error)
	GetSummary(token string) (map[string]interface{}, error)
	GetTerminators(token string) ([]map[string]interface{}, error)
	GetPostureChecks(token string) ([]map[string]interface{}, error)
	GetRoleAttributes(token, entityType string) ([]string, error)
}

// IdentityMgr is the interface for Bao's identity subsystem — entities,
// groups, aliases, and OIDC provider configuration.
// Corresponds to service.IdentityService in the concrete layer.
type IdentityMgr interface {
	// Entities
	ListEntitiesByName() ([]string, error)
	ListEntitiesByID() ([]string, error)
	GetEntity(name string) (*Entity, error)
	GetEntityByID(id string) (*Entity, error)
	CreateEntity(name string, metadata map[string]string, policies []string) (string, error)
	UpdateEntityMetadata(id string, metadata map[string]string) error
	MergeEntities(toEntityID string, fromEntityIDs []string) error
	DeleteEntity(id string) error

	// Aliases
	GetAlias(id string) (map[string]interface{}, error)
	ListAliases() ([]string, error)
	CreateAlias(entityID, aliasName, mountAccessor string, metadata map[string]string) (string, error)
	DeleteAlias(id string) error

	// Groups
	GetGroupByID(id string) (map[string]interface{}, error)
	ListGroupsByID() ([]string, error)
	CreateGroup(name string, memberEntityIDs []string, policies []string, metadata map[string]string) (string, error)
	UpdateGroup(id string, memberEntityIDs []string, policies []string) error

	// Group aliases
	CreateGroupAlias(groupID, aliasName, mountAccessor string, metadata map[string]string) (string, error)
	GetGroupAlias(id string) (map[string]interface{}, error)
	UpdateGroupAlias(id string, data map[string]interface{}) error
	DeleteGroupAlias(id string) error
	ListGroupAliases() ([]string, error)

	// OIDC provider
	OIDCReadConfig() (map[string]interface{}, error)
	OIDCWriteConfig(issuer string) error
	OIDCCreateKey(name string, data map[string]interface{}) error
	OIDCReadKey(name string) (map[string]interface{}, error)
	OIDCDeleteKey(name string) error
	OIDCListKeys() ([]string, error)
	OIDCRotateKey(name string) error
	OIDCCreateRole(name string, data map[string]interface{}) error
	OIDCReadRole(name string) (map[string]interface{}, error)
	OIDCDeleteRole(name string) error
	OIDCListRoles() ([]string, error)
}

// MachineStore is the enrollment and machine-state interface. Covers the
// inventory, pulse telemetry, and OTT (one-time token) lifecycle.
// Corresponds to service.StoreService's machine-specific methods +
// service.OTTStore in the concrete layer.
type MachineStore interface {
	// Machine inventory
	GetMachine(id string) (map[string]interface{}, error)
	ListMachines() ([]map[string]interface{}, error)
	PutMachine(id string, data map[string]interface{}) error

	// Pulse / heartbeat
	GetPulseState(machineID string) (map[string]interface{}, error)
	ListPulseStates() ([]map[string]interface{}, error)
	PutPulseState(machineID string, state map[string]interface{}) error

	// One-time tokens (for enrollment linking)
	IssueOTT(machineID string) (string, error)
	ConsumeOTT(token string) (string, error) // returns machineID
	RevokeOTT(token string) error
}

// SecuritySvc is the fingerprint verification and ban/audit interface.
// Corresponds to service.SecurityService in the concrete layer.
type SecuritySvc interface {
	CheckFingerprint(fp string) (bool, error)
	BanMachine(id, reason string) error
	UnbanMachine(id string) error
	IsBanned(id string) (bool, error)
	AuditLog(event string, data map[string]interface{}) error
}

// DiscoverySvc handles port-scan and service-probe based discovery of
// running services on enrolled machines.
// Corresponds to service.DiscoveryService in the concrete layer.
type DiscoverySvc interface {
	Probe(host string, ports []int) (map[string]interface{}, error)
	Match(results map[string]interface{}) (string, error) // returns app name
	AutoClaim(machineID string, results map[string]interface{}) error
}

// MetricsSvc surfaces cluster health numbers for dashboards and alerting.
// Corresponds to service.MetricsService in the concrete layer.
type MetricsSvc interface {
	ZitiMetrics(token string) (map[string]interface{}, error)
	BaoMetrics() (map[string]interface{}, error)
	MachineMetrics(machineID string) (map[string]interface{}, error)
	LatencyMetrics() (map[string]interface{}, error)
}
