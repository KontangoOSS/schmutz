package ext_jwt_signer

import (
	"io"
	"net/http"

	"git.konoss.org/kore/schmutz/dashboard/pkg/browzer/bff"
	"git.konoss.org/kore/schmutz/dashboard/pkg/browzer/ziti"
)

type Plugin struct{}

func (p *Plugin) Name() string        { return "ext-jwt-signer" }
func (p *Plugin) Description() string { return "CRUD for Ziti external JWT signers" }

func (p *Plugin) Register(router *bff.Router) {
	z := router.Ziti
	base := "/edge/management/v1/external-jwt-signers"
	router.Handle("GET /api/ext-jwt-signers", listAll(z, base))
	router.Handle("POST /api/ext-jwt-signers", create(router, base))
	router.Handle("PATCH /api/ext-jwt-signers/{id}", patch(z, base))
	router.Handle("DELETE /api/ext-jwt-signers/{id}", del(z, base))
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

func create(router *bff.Router, base string) bff.Handler {
	return func(token string, w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			bff.WriteError(w, 400, "read body failed", err)
			return
		}
		raw, err := router.Ziti.Post(token, base, body)
		if err != nil {
			bff.WriteError(w, 502, "create failed", err)
			return
		}
		bff.WriteRaw(w, raw)
	}
}

func patch(z *ziti.Client, base string) bff.Handler {
	return func(token string, w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			bff.WriteError(w, 400, "read body failed", err)
			return
		}
		raw, err := z.Patch(token, base+"/"+id, body)
		if err != nil {
			bff.WriteError(w, 502, "patch failed", err)
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
