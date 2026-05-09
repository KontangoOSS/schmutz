package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/KontangoOSS/schmutz/enroll/internal/audit"
)

type fixedQuerier struct {
	events         []audit.Event
	capturedFilter audit.Filter
}

func (q *fixedQuerier) Query(ctx context.Context, f audit.Filter) ([]audit.Event, error) {
	q.capturedFilter = f
	var out []audit.Event
	for _, e := range q.events {
		if f.Actor != "" && e.Actor != f.Actor {
			continue
		}
		if f.Action != "" && e.Action != f.Action {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

func TestAuditHandler_Query(t *testing.T) {
	q := &fixedQuerier{events: []audit.Event{
		{Actor: "alice", Action: audit.ActionTokenIssue, Timestamp: time.Now()},
		{Actor: "bob", Action: audit.ActionIdentityApprove, Timestamp: time.Now()},
	}}
	h := NewAuditHandler(q)
	req := httptest.NewRequest("GET", "/api/audit?actor=alice", nil).
		WithContext(adminCtx("admin1", "admins"))
	w := httptest.NewRecorder()
	h.Query().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var resp struct {
		Events []audit.Event `json:"events"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Events) != 1 {
		t.Errorf("got %d events, want 1", len(resp.Events))
	}
}

func TestAuditHandler_LimitParam(t *testing.T) {
	q := &fixedQuerier{}
	h := NewAuditHandler(q)
	req := httptest.NewRequest("GET", "/api/audit?limit=50", nil)
	w := httptest.NewRecorder()
	h.Query().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status %d", w.Code)
	}
	if q.capturedFilter.Limit != 50 {
		t.Errorf("captured limit = %d, want 50", q.capturedFilter.Limit)
	}
}
