package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/KontangoOSS/schmutz/enroll/internal/bao"
	"github.com/KontangoOSS/schmutz/enroll/internal/identity"
)

// healthZitiClient is the minimum surface MetaHandler needs from the Ziti client.
type healthZitiClient interface {
	Health(ctx context.Context) error
}

type MetaHandler struct {
	b bao.Client
	z healthZitiClient
}

func NewMetaHandler(b bao.Client, z healthZitiClient) *MetaHandler {
	return &MetaHandler{b: b, z: z}
}

func (h *MetaHandler) Healthz() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		_, err := h.b.GetToken(ctx, "__healthz__")
		if err != nil && !errors.Is(err, bao.ErrNotFound) {
			writeErr(w, http.StatusServiceUnavailable, "bao unhealthy: "+err.Error())
			return
		}
		if err := h.z.Health(ctx); err != nil {
			writeErr(w, http.StatusServiceUnavailable, "ziti unhealthy: "+err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
}

func (h *MetaHandler) Whoami() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, ok := identity.FromContext(r.Context())
		if !ok {
			writeErr(w, http.StatusInternalServerError, "caller identity missing")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"name":            c.Name,
			"role_attributes": c.RoleAttributes,
		})
	})
}
