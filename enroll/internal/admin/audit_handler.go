package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"git.konoss.org/kore/schmutz/enroll/internal/audit"
)

// auditQuerier is the minimum interface AuditHandler needs.
// *audit.Querier satisfies this directly; tests pass a fixedQuerier.
type auditQuerier interface {
	Query(ctx context.Context, f audit.Filter) ([]audit.Event, error)
}

type AuditHandler struct {
	q auditQuerier
}

func NewAuditHandler(q auditQuerier) *AuditHandler {
	return &AuditHandler{q: q}
}

// NewAuditHandlerFromQuerier is a production-wiring helper. Identical to
// NewAuditHandler, but accepts the concrete *audit.Querier for clarity.
func NewAuditHandlerFromQuerier(q *audit.Querier) *AuditHandler {
	return &AuditHandler{q: q}
}

func (h *AuditHandler) Query() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f := audit.Filter{
			Actor:    r.URL.Query().Get("actor"),
			Action:   r.URL.Query().Get("action"),
			Resource: r.URL.Query().Get("resource"),
		}
		if s := r.URL.Query().Get("since"); s != "" {
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				f.Since = t
			}
		}
		if s := r.URL.Query().Get("until"); s != "" {
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				f.Until = t
			}
		}
		if s := r.URL.Query().Get("limit"); s != "" {
			if n, err := strconv.Atoi(s); err == nil {
				f.Limit = n
			}
		}
		evs, err := h.q.Query(r.Context(), f)
		if err != nil {
			writeErr(w, http.StatusServiceUnavailable, "audit query failed: "+err.Error())
			return
		}
		if evs == nil {
			evs = []audit.Event{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"events": evs})
	})
}
