package dashboard

import "git.konoss.org/kore/schmutz/dashboard/pkg/browzer/bff"

// Plugin is the Ziti network dashboard plugin.
type Plugin struct{}

func (p *Plugin) Name() string { return "dashboard" }
func (p *Plugin) Description() string {
	return "Ziti network overview — services, identities, routers, policies, terminators"
}

func (p *Plugin) Register(router *bff.Router) {
	z := router.Ziti

	router.Handle("GET /api/overview", overviewHandler(z))
	router.Handle("GET /api/services", listHandler[Service](z, "/edge/management/v1/services?limit=500"))
	router.Handle("GET /api/identities", listHandler[Identity](z, "/edge/management/v1/identities?limit=500"))
	router.Handle("GET /api/identities/{name}", identityDetailHandler(z))
	router.Handle("GET /api/routers", listHandler[EdgeRouter](z, "/edge/management/v1/edge-routers?limit=100"))
	router.Handle("GET /api/policies", listHandler[ServicePolicy](z, "/edge/management/v1/service-policies?limit=500"))
	router.Handle("GET /api/terminators", listHandler[Terminator](z, "/edge/management/v1/terminators?limit=500"))
	router.HandleRaw("GET /api/version", versionHandler(z))

	// --- CRUD: services ---
	router.Handle("POST /api/services", crudCreate(router, "/edge/management/v1/services"))
	router.Handle("PATCH /api/services/{id}", crudPatch(z, "/edge/management/v1/services"))
	router.Handle("DELETE /api/services/{id}", crudDelete(z, "/edge/management/v1/services"))

	// --- CRUD: edge routers ---
	router.Handle("POST /api/routers", crudCreate(router, "/edge/management/v1/edge-routers"))
	router.Handle("PATCH /api/routers/{id}", crudPatch(z, "/edge/management/v1/edge-routers"))
	router.Handle("DELETE /api/routers/{id}", crudDelete(z, "/edge/management/v1/edge-routers"))

	// --- CRUD: service policies ---
	router.Handle("POST /api/policies", crudCreate(router, "/edge/management/v1/service-policies"))
	router.Handle("PATCH /api/policies/{id}", crudPatch(z, "/edge/management/v1/service-policies"))
	router.Handle("DELETE /api/policies/{id}", crudDelete(z, "/edge/management/v1/service-policies"))

	// --- CRUD: terminators ---
	router.Handle("POST /api/terminators", crudCreate(router, "/edge/management/v1/terminators"))
	router.Handle("PATCH /api/terminators/{id}", crudPatch(z, "/edge/management/v1/terminators"))
	router.Handle("DELETE /api/terminators/{id}", crudDelete(z, "/edge/management/v1/terminators"))

	// --- CRUD: identities ---
	router.Handle("POST /api/identities", createIdentityHandler(router))
	router.Handle("PATCH /api/identities/{id}", crudPatch(z, "/edge/management/v1/identities"))
	router.Handle("DELETE /api/identities/{id}", crudDelete(z, "/edge/management/v1/identities"))
	router.Handle("GET /api/identities/{id}/enrollment-jwt", identityEnrollmentJWTHandler(z))
}
