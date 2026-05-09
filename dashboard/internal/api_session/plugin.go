package api_session

import (
	"net/http"

	"git.konoss.org/kore/schmutz/dashboard/pkg/browzer/bff"
	"git.konoss.org/kore/schmutz/dashboard/pkg/browzer/ziti"
)

type Plugin struct{}

func (p *Plugin) Name() string        { return "api-session" }
func (p *Plugin) Description() string { return "List and delete API sessions and sessions" }

func (p *Plugin) Register(router *bff.Router) {
	z := router.Ziti
	router.Handle("GET /api/api-sessions", listAll(z, "/edge/management/v1/api-sessions"))
	router.Handle("DELETE /api/api-sessions/{id}", del(z, "/edge/management/v1/api-sessions"))
	router.Handle("GET /api/sessions", listAll(z, "/edge/management/v1/sessions"))
	router.Handle("DELETE /api/sessions/{id}", del(z, "/edge/management/v1/sessions"))
}

func listAll(z *ziti.Client, base string) bff.Handler {
	return func(token string, w http.ResponseWriter, r *http.Request) {
		raw, err := z.Get(token, base+"?limit=500")
		if err != nil {
			bff.WriteError(w, 502, "list failed", err)
			return
		}
		bff.WriteRaw(w, raw)
	}
}

func del(z *ziti.Client, base string) bff.Handler {
	return func(token string, w http.ResponseWriter, r *http.Request) {
		if err := z.Delete(token, base+"/"+r.PathValue("id")); err != nil {
			bff.WriteError(w, 502, "delete failed", err)
			return
		}
		w.WriteHeader(204)
	}
}
