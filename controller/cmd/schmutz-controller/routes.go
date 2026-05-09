package main

import (
	"net/http"

	"git.konoss.org/kore/schmutz/controller/internal/service"
)

// routes registers all API endpoints on the given mux.
//
// Enrollment is handled by schmutz-enroll (enroll-server), not this binary.
// /api/v1/enroll and /api/enroll are served on enroll.kontango.net → port 8765.
// This controller handles management plane APIs only.
func routes(mux *http.ServeMux, api *API, ziti *service.ZitiService) {

	// --- Landing pages ---
	mux.HandleFunc("/", api.LandingPage)
	mux.HandleFunc("/guide", api.GuidePage)
	mux.HandleFunc("/css/", api.ServeCSS)
	mux.HandleFunc("/js/", api.ServeJS)

	// --- System ---
	mux.HandleFunc("/api/health", api.Health)
	mux.HandleFunc("/api/health/device", api.HealthDevice)

	// --- Honeypot integration (called from 404rd on localhost) ---
	mux.HandleFunc("/api/auth/check", api.AuthCheck)
	mux.HandleFunc("/api/honeypot/push", api.HoneypotPush)
	mux.HandleFunc("/api/honeypot/approve/", api.HoneypotApprove)
	mux.HandleFunc("/api/whoami", api.Whoami)
	mux.HandleFunc("/api/controllers", api.Controllers)

	// --- Catalog ---
	mux.HandleFunc("/api/os", api.OSCatalog)
	mux.HandleFunc("/api/catalog/applications", api.CatalogApplications)
	mux.HandleFunc("/api/catalog/applications/", api.CatalogApplication)

	// --- Pulse KV state ---
	mux.HandleFunc("/api/pulse/live", api.PulseState)
	mux.HandleFunc("/api/pulse/", api.PulseMachine)

	// --- Agent ---
	mux.HandleFunc("/api/heartbeat", api.Heartbeat)
	mux.HandleFunc("/api/machines/live", api.LiveMachines)
	mux.HandleFunc("/api/machines/claim/", api.ClaimMachine)
	mux.HandleFunc("/api/machines/push/", api.PushInstruction)

	// --- Download (redirect to GitHub) ---
	mux.HandleFunc("/download/", api.Download)

	// --- Ziti ---
	mux.HandleFunc("/api/ziti/identities", api.ZitiIdentities)
	mux.HandleFunc("/api/ziti/identities/", api.ZitiIdentity)
	mux.HandleFunc("/api/ziti/services", api.ZitiServices)
	mux.HandleFunc("/api/ziti/services/", api.ZitiService)
	mux.HandleFunc("/api/ziti/configs", api.ZitiConfigs)
	mux.HandleFunc("/api/ziti/configs/", api.ZitiConfig)
	mux.HandleFunc("/api/ziti/service-policies", api.ZitiServicePolicies)
	mux.HandleFunc("/api/ziti/service-policies/", api.ZitiServicePolicy)
	mux.HandleFunc("/api/ziti/routers", api.ZitiRouters)
	mux.HandleFunc("/api/ziti/routers/", api.ZitiRouter)
	mux.HandleFunc("/api/ziti/router-policies", api.ZitiRouterPolicies)
	mux.HandleFunc("/api/ziti/router-policies/", api.ZitiRouterPolicy)
	mux.HandleFunc("/api/ziti/service-router-policies", api.ZitiServiceRouterPolicies)
	mux.HandleFunc("/api/ziti/service-router-policies/", api.ZitiServiceRouterPolicy)
	mux.HandleFunc("/api/ziti/enrollments", api.ZitiEnrollments)
	mux.HandleFunc("/api/ziti/enrollments/", api.ZitiEnrollment)
	mux.HandleFunc("/api/ziti/sessions", api.ZitiSessions)
	mux.HandleFunc("/api/ziti/sessions/", api.ZitiSession)
	mux.HandleFunc("/api/ziti/terminators", api.ZitiTerminators)
	mux.HandleFunc("/api/ziti/terminators/", api.ZitiTerminator)
	mux.HandleFunc("/api/ziti/cas", api.ZitiCAs)
	mux.HandleFunc("/api/ziti/cas/", api.ZitiCA)
	mux.HandleFunc("/api/ziti/database/check", api.ZitiDBCheck)
	mux.HandleFunc("/api/ziti/database/fix", api.ZitiDBFix)
	mux.HandleFunc("/api/ziti/database/snapshot", api.ZitiDBSnapshot)
	mux.HandleFunc("/api/ziti/network-jwts", api.ZitiNetworkJWTs)
	mux.HandleFunc("/api/ziti/validate/identity/", api.ZitiValidateIdentity)
	mux.HandleFunc("/api/ziti/validate/service/", api.ZitiValidateService)
	mux.HandleFunc("/api/ziti/validate/router/", api.ZitiValidateRouter)
	mux.HandleFunc("/api/ziti/role-attributes/", api.ZitiRoleAttributes)
	mux.HandleFunc("/api/ziti/version", api.ZitiVersion)
	mux.HandleFunc("/api/ziti/summary", api.ZitiSummary)
	mux.HandleFunc("/api/ziti/find/", api.ZitiFind)
	mux.HandleFunc("/api/ziti/auth-policies", api.ZitiAuthPolicies)
	mux.HandleFunc("/api/ziti/auth-policies/", api.ZitiAuthPolicy)
	mux.HandleFunc("/api/ziti/authenticators", api.ZitiAuthenticators)
	mux.HandleFunc("/api/ziti/authenticators/", api.ZitiAuthenticator)
	mux.HandleFunc("/api/ziti/config-types", api.ZitiConfigTypes)
	mux.HandleFunc("/api/ziti/config-types/", api.ZitiConfigType)
	mux.HandleFunc("/api/ziti/ext-jwt-signers", api.ZitiExtJWTSigners)
	mux.HandleFunc("/api/ziti/ext-jwt-signers/", api.ZitiExtJWTSigner)
	mux.HandleFunc("/api/ziti/transit-routers", api.ZitiTransitRouters)
	mux.HandleFunc("/api/ziti/transit-routers/", api.ZitiTransitRouter)
	mux.HandleFunc("/api/ziti/posture-checks", api.ZitiPostureChecks)
	mux.HandleFunc("/api/ziti/posture-checks/", api.ZitiPostureCheck)
	mux.HandleFunc("/api/ziti/posture-check-types", api.ZitiPostureCheckTypes)
	mux.HandleFunc("/api/ziti/api-sessions", api.ZitiAPISessions)
	mux.HandleFunc("/api/ziti/api-sessions/", api.ZitiAPISession)
	mux.HandleFunc("/api/ziti/policy-advisor/identity/", api.ZitiPolicyAdvisorIdentity)
	mux.HandleFunc("/api/ziti/policy-advisor/service/", api.ZitiPolicyAdvisorService)

	// --- Bao KV ---
	// All /api/bao/* and /api/identity/* routes require Bao to be configured.
	// requireStoreMW returns 503 immediately if a.store == nil.
	mux.HandleFunc("/api/bao/kv/", api.requireStoreMW(api.BaoKV))
	mux.HandleFunc("/api/bao/kv-metadata/", api.requireStoreMW(api.BaoKVMetadata))
	mux.HandleFunc("/api/bao/kv-versions/", api.requireStoreMW(api.BaoKVVersions))
	mux.HandleFunc("/api/bao/kv-config/", api.requireStoreMW(api.BaoKVConfig))

	// --- Bao system ---
	mux.HandleFunc("/api/bao/health", api.requireStoreMW(api.BaoHealth))
	mux.HandleFunc("/api/bao/seal-status", api.requireStoreMW(api.BaoSealStatus))
	mux.HandleFunc("/api/bao/seal", api.requireStoreMW(api.BaoSeal))
	mux.HandleFunc("/api/bao/unseal", api.requireStoreMW(api.BaoUnseal))
	mux.HandleFunc("/api/bao/init", api.requireStoreMW(api.BaoInit))
	mux.HandleFunc("/api/bao/leader", api.requireStoreMW(api.BaoLeader))
	mux.HandleFunc("/api/bao/step-down", api.requireStoreMW(api.BaoStepDown))
	mux.HandleFunc("/api/bao/key-status", api.requireStoreMW(api.BaoKeyStatus))

	// --- Bao mounts ---
	mux.HandleFunc("/api/bao/mounts", api.requireStoreMW(api.BaoMounts))
	mux.HandleFunc("/api/bao/mounts/", api.requireStoreMW(api.BaoMount))

	// --- Bao auth methods ---
	mux.HandleFunc("/api/bao/auth-methods", api.requireStoreMW(api.BaoAuthMethods))
	mux.HandleFunc("/api/bao/auth-methods/", api.requireStoreMW(api.BaoAuthMethod))

	// --- Bao audit ---
	mux.HandleFunc("/api/bao/audit", api.requireStoreMW(api.BaoAuditDevices))
	mux.HandleFunc("/api/bao/audit/", api.requireStoreMW(api.BaoAuditDevice))

	// --- Bao leases ---
	mux.HandleFunc("/api/bao/leases/lookup", api.requireStoreMW(api.BaoLeaseLookup))
	mux.HandleFunc("/api/bao/leases/renew", api.requireStoreMW(api.BaoLeaseRenew))
	mux.HandleFunc("/api/bao/leases/revoke", api.requireStoreMW(api.BaoLeaseRevoke))
	mux.HandleFunc("/api/bao/leases/list/", api.requireStoreMW(api.BaoLeaseList))

	// --- Bao capabilities ---
	mux.HandleFunc("/api/bao/capabilities", api.requireStoreMW(api.BaoCapabilities))
	mux.HandleFunc("/api/bao/capabilities-self", api.requireStoreMW(api.BaoCapabilitiesSelf))

	// --- Bao plugins ---
	mux.HandleFunc("/api/bao/plugins", api.requireStoreMW(api.BaoPlugins))
	mux.HandleFunc("/api/bao/plugins/reload", api.requireStoreMW(api.BaoPluginReload))

	// --- Bao wrapping ---
	mux.HandleFunc("/api/bao/wrap", api.requireStoreMW(api.BaoWrap))
	mux.HandleFunc("/api/bao/unwrap", api.requireStoreMW(api.BaoUnwrap))
	mux.HandleFunc("/api/bao/wrap-lookup", api.requireStoreMW(api.BaoWrapLookup))

	// --- Bao tools ---
	mux.HandleFunc("/api/bao/tools/random", api.requireStoreMW(api.BaoToolsRandom))
	mux.HandleFunc("/api/bao/tools/hash", api.requireStoreMW(api.BaoToolsHash))

	// --- Bao metrics ---
	mux.HandleFunc("/api/bao/sys-metrics", api.requireStoreMW(api.BaoSysMetrics))

	// --- Bao policies ---
	mux.HandleFunc("/api/bao/policies", api.requireStoreMW(api.BaoPolicies))
	mux.HandleFunc("/api/bao/policies/", api.requireStoreMW(api.BaoPolicy))

	// --- Bao tokens ---
	mux.HandleFunc("/api/bao/tokens", api.requireStoreMW(api.BaoTokenCreate))
	mux.HandleFunc("/api/bao/tokens/lookup", api.requireStoreMW(api.BaoTokenLookup))
	mux.HandleFunc("/api/bao/tokens/lookup-self", api.requireStoreMW(api.BaoTokenLookupSelf))
	mux.HandleFunc("/api/bao/tokens/revoke", api.requireStoreMW(api.BaoTokenRevoke))
	mux.HandleFunc("/api/bao/tokens/revoke-self", api.requireStoreMW(api.BaoTokenRevokeSelf))
	mux.HandleFunc("/api/bao/tokens/renew", api.requireStoreMW(api.BaoTokenRenew))
	mux.HandleFunc("/api/bao/tokens/renew-self", api.requireStoreMW(api.BaoTokenRenewSelf))
	mux.HandleFunc("/api/bao/tokens/orphan", api.requireStoreMW(api.BaoTokenOrphan))
	mux.HandleFunc("/api/bao/tokens/accessors", api.requireStoreMW(api.BaoTokenAccessors))
	mux.HandleFunc("/api/bao/tokens/accessor/", api.requireStoreMW(api.BaoTokenAccessor))
	mux.HandleFunc("/api/bao/tokens/tidy", api.requireStoreMW(api.BaoTokenTidy))

	// --- Bao AppRole ---
	mux.HandleFunc("/api/bao/approles", api.requireStoreMW(api.BaoAppRoles))
	mux.HandleFunc("/api/bao/approles/login", api.requireStoreMW(api.BaoAppRoleLogin))
	mux.HandleFunc("/api/bao/approles/", api.requireStoreMW(api.BaoAppRole))

	// --- Bao userpass ---
	mux.HandleFunc("/api/bao/userpass", api.requireStoreMW(api.BaoUserpassUsers))
	mux.HandleFunc("/api/bao/userpass/login/", api.requireStoreMW(api.BaoUserpassLogin))
	mux.HandleFunc("/api/bao/userpass/", api.requireStoreMW(api.BaoUserpassUser))

	// --- Bao PKI ---
	mux.HandleFunc("/api/bao/pki/", api.requireStoreMW(api.BaoPKI))

	// --- Bao transit ---
	mux.HandleFunc("/api/bao/transit/", api.requireStoreMW(api.BaoTransit))

	// --- Bao TOTP ---
	mux.HandleFunc("/api/bao/totp/", api.requireStoreMW(api.BaoTOTP))

	// --- Bao SSH ---
	mux.HandleFunc("/api/bao/ssh/", api.requireStoreMW(api.BaoSSH))

	// --- Bao rekey/rotate ---
	mux.HandleFunc("/api/bao/rekey/init", api.requireStoreMW(api.BaoRekeyInit))
	mux.HandleFunc("/api/bao/rekey/update", api.requireStoreMW(api.BaoRekeyUpdate))
	mux.HandleFunc("/api/bao/rekey/cancel", api.requireStoreMW(api.BaoRekeyCancel))
	mux.HandleFunc("/api/bao/rekey/status", api.requireStoreMW(api.BaoRekeyStatus))
	mux.HandleFunc("/api/bao/rotate", api.requireStoreMW(api.BaoRotate))

	// --- Bao cubbyhole ---
	mux.HandleFunc("/api/bao/cubbyhole/", api.requireStoreMW(api.BaoCubbyhole))

	// --- Bao database ---
	mux.HandleFunc("/api/bao/database/", api.requireStoreMW(api.BaoDatabase))

	// --- Bao LDAP auth ---
	mux.HandleFunc("/api/bao/ldap/config", api.requireStoreMW(api.BaoLDAPConfig))
	mux.HandleFunc("/api/bao/ldap/groups", api.requireStoreMW(api.BaoLDAPGroups))
	mux.HandleFunc("/api/bao/ldap/groups/", api.requireStoreMW(api.BaoLDAPGroup))
	mux.HandleFunc("/api/bao/ldap/users", api.requireStoreMW(api.BaoLDAPUsers))
	mux.HandleFunc("/api/bao/ldap/users/", api.requireStoreMW(api.BaoLDAPUser))
	mux.HandleFunc("/api/bao/ldap/login/", api.requireStoreMW(api.BaoLDAPLogin))

	// --- Bao cert auth ---
	mux.HandleFunc("/api/bao/cert/roles", api.requireStoreMW(api.BaoCertRoles))
	mux.HandleFunc("/api/bao/cert/roles/", api.requireStoreMW(api.BaoCertRole))

	// --- Bao kubernetes auth ---
	mux.HandleFunc("/api/bao/k8s/config", api.BaoK8sConfig)
	mux.HandleFunc("/api/bao/k8s/roles", api.BaoK8sRoles)
	mux.HandleFunc("/api/bao/k8s/roles/", api.BaoK8sRole)
	mux.HandleFunc("/api/bao/k8s/login", api.BaoK8sLogin)

	// --- Bao JWT/OIDC auth ---
	mux.HandleFunc("/api/bao/jwt/config", api.requireStoreMW(api.BaoJWTConfig))
	mux.HandleFunc("/api/bao/jwt/roles", api.requireStoreMW(api.BaoJWTRoles))
	mux.HandleFunc("/api/bao/jwt/roles/", api.requireStoreMW(api.BaoJWTRole))

	// --- Bao OIDC (auth method) ---
	mux.HandleFunc("/api/bao/oidc/config", api.requireStoreMW(api.BaoOIDCConfig))

	// --- Identity engine ---
	mux.HandleFunc("/api/identity/entities", api.requireStoreMW(api.IdentityEntities))
	mux.HandleFunc("/api/identity/entities/merge", api.requireStoreMW(api.IdentityMerge))
	mux.HandleFunc("/api/identity/entities/by-id", api.requireStoreMW(api.IdentityEntitiesByID))
	mux.HandleFunc("/api/identity/entities/", api.requireStoreMW(api.IdentityEntity))
	mux.HandleFunc("/api/identity/aliases", api.requireStoreMW(api.IdentityAliases))
	mux.HandleFunc("/api/identity/aliases/", api.requireStoreMW(api.IdentityAlias))
	mux.HandleFunc("/api/identity/groups", api.requireStoreMW(api.IdentityGroups))
	mux.HandleFunc("/api/identity/groups/by-id", api.requireStoreMW(api.IdentityGroupsByID))
	mux.HandleFunc("/api/identity/groups/", api.requireStoreMW(api.IdentityGroup))
	mux.HandleFunc("/api/identity/group-aliases", api.requireStoreMW(api.IdentityGroupAliases))
	mux.HandleFunc("/api/identity/group-aliases/", api.requireStoreMW(api.IdentityGroupAlias))
	mux.HandleFunc("/api/identity/auth-accessor/", api.requireStoreMW(api.IdentityAuthAccessor))
	mux.HandleFunc("/api/identity/oidc/config", api.requireStoreMW(api.IdentityOIDCConfig))
	mux.HandleFunc("/api/identity/oidc/keys", api.requireStoreMW(api.IdentityOIDCKeys))
	mux.HandleFunc("/api/identity/oidc/keys/", api.requireStoreMW(api.IdentityOIDCKey))
	mux.HandleFunc("/api/identity/oidc/roles", api.requireStoreMW(api.IdentityOIDCRoles))
	mux.HandleFunc("/api/identity/oidc/roles/", api.requireStoreMW(api.IdentityOIDCRole))

	// --- Machines ---
	mux.HandleFunc("/api/machines/", api.Machine)

	// --- Profiles ---
	mux.HandleFunc("/api/profiles", api.Profiles)
	mux.HandleFunc("/api/profiles/", api.Profile)

	// --- Known machines ---
	mux.HandleFunc("/api/known/", api.KnownMachine)

	// --- Security ---
	mux.HandleFunc("/api/security/bans/", api.SecurityBan)
	mux.HandleFunc("/api/security/check-duplicate", api.SecurityCheckDuplicate)
	mux.HandleFunc("/api/security/validate-fingerprint", api.SecurityValidateFingerprint)
	mux.HandleFunc("/api/security/audit", api.SecurityAudit)

	// --- Discovery ---
	mux.HandleFunc("/api/discovery/match", api.DiscoveryMatch)
	mux.HandleFunc("/api/discovery/auto-claim", api.DiscoveryAutoClaim)

	// --- Debug ---
	mux.HandleFunc("/api/debug/events/", api.DebugEvents)

	// --- Ops ---
	mux.HandleFunc("/api/ops/approve", api.Approve)
	mux.HandleFunc("/api/ops/deny", api.Deny)

	// --- Metrics ---
	mux.HandleFunc("/api/metrics/status", api.MetricsStatus)
	mux.HandleFunc("/api/metrics/ziti", api.MetricsZiti)
	mux.HandleFunc("/api/metrics/bao", api.MetricsBao)
	mux.HandleFunc("/api/metrics/machine", api.MetricsMachine)
	mux.HandleFunc("/api/metrics/latency", api.MetricsLatency)
}
