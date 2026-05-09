package dashboard

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"git.konoss.org/kore/schmutz/dashboard/pkg/browzer/bff"
	"git.konoss.org/kore/schmutz/dashboard/pkg/browzer/ziti"
)

// Types used by this plugin — re-exported from the ziti package
// so plugin.go doesn't need to import ziti directly.
type (
	Service       = ziti.Service
	Identity      = ziti.Identity
	EdgeRouter    = ziti.EdgeRouter
	ServicePolicy = ziti.ServicePolicy
	Terminator    = ziti.Terminator
)

// listHandler returns a BFF handler that lists Ziti resources of type T.
func listHandler[T any](z *ziti.Client, path string) bff.Handler {
	return func(token string, w http.ResponseWriter, r *http.Request) {
		raw, err := z.Get(token, path)
		if err != nil {
			bff.WriteError(w, 502, "ziti request failed", err)
			return
		}
		items, err := ziti.DecodeList[T](raw)
		if err != nil {
			bff.WriteError(w, 500, "decode failed", err)
			return
		}
		bff.WriteJSON(w, items)
	}
}

func overviewHandler(z *ziti.Client) bff.Handler {
	return func(token string, w http.ResponseWriter, r *http.Request) {
		raw, err := z.Get(token, "/edge/management/v1/summary")
		if err != nil {
			bff.WriteError(w, 502, "summary failed", err)
			return
		}
		summary, err := ziti.DecodeOne[ziti.Summary](raw)
		if err != nil {
			bff.WriteError(w, 500, "decode summary failed", err)
			return
		}

		rawRouters, err := z.Get(token, "/edge/management/v1/edge-routers?limit=100")
		if err != nil {
			bff.WriteError(w, 502, "routers failed", err)
			return
		}
		routers, err := ziti.DecodeList[ziti.EdgeRouter](rawRouters)
		if err != nil {
			bff.WriteError(w, 500, "decode routers failed", err)
			return
		}

		online := 0
		for _, rt := range routers {
			if rt.IsOnline {
				online++
			}
		}

		bff.WriteJSON(w, map[string]any{
			"summary":       summary,
			"routersOnline": online,
			"routersTotal":  len(routers),
		})
	}
}

func identityDetailHandler(z *ziti.Client) bff.Handler {
	return func(token string, w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")

		findRaw, err := z.Get(token, fmt.Sprintf("/edge/management/v1/identities?filter=name%%3D%%22%s%%22", name))
		if err != nil {
			bff.WriteError(w, 502, "find identity failed", err)
			return
		}
		var findEnv struct {
			Data []struct{ ID string `json:"id"` } `json:"data"`
		}
		json.Unmarshal(findRaw, &findEnv)

		id := name // fallback to treating name as ID
		if len(findEnv.Data) > 0 {
			id = findEnv.Data[0].ID
		}

		raw, err := z.Get(token, "/edge/management/v1/identities/"+id)
		if err != nil {
			bff.WriteError(w, 404, "identity not found", err)
			return
		}
		bff.WriteRaw(w, raw)
	}
}

func versionHandler(z *ziti.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := z.GetUnauthenticated("/edge/client/v1/version")
		if err != nil {
			bff.WriteError(w, 502, "version failed", err)
			return
		}
		bff.WriteRaw(w, data)
	}
}

// crudCreate proxies a POST body to the Ziti management API.
func crudCreate(router *bff.Router, basePath string) bff.Handler {
	return func(token string, w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			bff.WriteError(w, 400, "read body failed", err)
			return
		}
		raw, err := router.Ziti.Post(token, basePath, body)
		if err != nil {
			bff.WriteError(w, 502, "create failed", err)
			return
		}
		bff.WriteRaw(w, raw)
	}
}

// crudPatch proxies a PATCH body to /basePath/{id}.
func crudPatch(z *ziti.Client, basePath string) bff.Handler {
	return func(token string, w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			bff.WriteError(w, 400, "read body failed", err)
			return
		}
		raw, err := z.Patch(token, basePath+"/"+id, body)
		if err != nil {
			bff.WriteError(w, 502, "patch failed", err)
			return
		}
		bff.WriteRaw(w, raw)
	}
}

// crudDelete calls DELETE /basePath/{id}.
func crudDelete(z *ziti.Client, basePath string) bff.Handler {
	return func(token string, w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if err := z.Delete(token, basePath+"/"+id); err != nil {
			bff.WriteError(w, 502, "delete failed", err)
			return
		}
		w.WriteHeader(204)
	}
}

func createIdentityHandler(router *bff.Router) bff.Handler {
	return func(token string, w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			bff.WriteError(w, 400, "read body failed", err)
			return
		}
		raw, err := router.Ziti.Post(token, "/edge/management/v1/identities", body)
		if err != nil {
			bff.WriteError(w, 502, "create identity failed", err)
			return
		}
		bff.WriteRaw(w, raw)
	}
}

func identityEnrollmentJWTHandler(z *ziti.Client) bff.Handler {
	return func(token string, w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		raw, err := z.Get(token, "/edge/management/v1/identities/"+id+"/enrollments")
		if err != nil {
			bff.WriteError(w, 502, "get enrollments failed", err)
			return
		}
		bff.WriteRaw(w, raw)
	}
}
