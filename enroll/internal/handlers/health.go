package handlers

import (
	"errors"
	"net/http"

	"git.konoss.org/kore/schmutz/enroll/internal/bao"
	"git.konoss.org/kore/schmutz/enroll/internal/ziti"
)

type HealthHandler struct {
	bao  bao.Client
	ziti ziti.Client
}

func NewHealthHandler(b bao.Client, z ziti.Client) *HealthHandler {
	return &HealthHandler{bao: b, ziti: z}
}

func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// Bao health check: we expect ErrNotFound for the sentinel token.
	// Any other error or transport failure is unhealthy.
	// nil error (e.g. mock that returns no error) is also accepted as healthy.
	_, err := h.bao.GetToken(ctx, "__healthz__")
	if err != nil && !errors.Is(err, bao.ErrNotFound) {
		writeErr(w, http.StatusServiceUnavailable, "bao unhealthy: "+err.Error())
		return
	}
	if err := h.ziti.Health(ctx); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "ziti unhealthy: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
