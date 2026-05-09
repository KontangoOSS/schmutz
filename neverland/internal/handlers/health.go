package handlers

import (
	"net/http"

	"git.konoss.org/kore/schmutz/neverland/internal/respond"
)

func Health(w http.ResponseWriter, r *http.Request) {
	respond.JSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "neverland"})
}
