package enrollments

import (
	"git.konoss.org/kore/schmutz/dashboard/internal/memory"
	"git.konoss.org/kore/schmutz/dashboard/internal/store"
	"git.konoss.org/kore/schmutz/dashboard/pkg/browzer/bff"
	"git.konoss.org/kore/schmutz/dashboard/pkg/browzer/ziti"
)

type Plugin struct {
	Store         *store.Store
	Memory        *memory.Layer
	ControllerURL string
	Ziti          *ziti.Client // for querying quarantine identities directly
	ZitiAddr      string       // e.g. "ctrl-1.konoss.org:1280" — for direct password auth
	ZitiUser      string
	ZitiPass      string
}

func (p *Plugin) Name() string { return "enrollments" }
func (p *Plugin) Description() string {
	return "Enrollment approval queue — pending, approve, deny"
}

func (p *Plugin) Register(r *bff.Router) {
	r.HandleRaw("GET /api/enrollments", p.listEnrollments)
	r.HandleRaw("GET /api/enrollments/live", p.liveTelemetry)
	r.HandleRaw("POST /api/ops/approve", p.approve)
	r.HandleRaw("POST /api/ops/deny", p.deny)
	r.HandleRaw("POST /api/ops/flush", p.flush)
}
