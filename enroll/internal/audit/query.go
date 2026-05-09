package audit

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"
)

var ErrAuditNotFound = errors.New("audit entry not found")

type Filter struct {
	Actor    string
	Action   string
	Resource string
	// Since: zero means current month only; set explicitly to scan further back.
	Since    time.Time
	Until    time.Time
	Limit    int
}

func (f Filter) normalizedLimit() int {
	switch {
	case f.Limit <= 0:
		return 100
	case f.Limit > 1000:
		return 1000
	default:
		return f.Limit
	}
}

type Querier struct {
	kv KV
}

func NewQuerier(kv KV) *Querier { return &Querier{kv: kv} }

// Query returns matching events sorted by timestamp desc, capped at Filter.Limit.
// It scans monthly partitions covering [Since, Until]. If Since is zero, it scans
// the current month only (callers must set Since explicitly to scan further back).
func (q *Querier) Query(ctx context.Context, f Filter) ([]Event, error) {
	limit := f.normalizedLimit()
	months := q.monthsInRange(f.Since, f.Until)

	var events []Event
	for _, m := range months {
		prefix := "schmutz/audit/" + m + "/"
		keys, err := q.kv.ListKeys(ctx, prefix)
		if err != nil {
			return nil, err
		}
		for _, k := range keys {
			body, err := q.kv.ReadJSON(ctx, prefix+k)
			if err != nil {
				if errors.Is(err, ErrAuditNotFound) {
					continue
				}
				return nil, err
			}
			var ev Event
			if err := json.Unmarshal(body, &ev); err != nil {
				continue
			}
			if !filterMatches(ev, f) {
				continue
			}
			events = append(events, ev)
		}
	}

	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.After(events[j].Timestamp)
	})
	if len(events) > limit {
		events = events[:limit]
	}
	return events, nil
}

func filterMatches(ev Event, f Filter) bool {
	if f.Actor != "" && ev.Actor != f.Actor {
		return false
	}
	if f.Action != "" && ev.Action != f.Action {
		return false
	}
	if f.Resource != "" && ev.Resource != f.Resource {
		return false
	}
	if !f.Since.IsZero() && ev.Timestamp.Before(f.Since) {
		return false
	}
	if !f.Until.IsZero() && ev.Timestamp.After(f.Until) {
		return false
	}
	return true
}

func (q *Querier) monthsInRange(since, until time.Time) []string {
	if until.IsZero() {
		until = time.Now().UTC()
	}
	if since.IsZero() {
		since = time.Date(until.Year(), until.Month(), 1, 0, 0, 0, 0, time.UTC)
	}
	var out []string
	cur := time.Date(since.Year(), since.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(until.Year(), until.Month(), 1, 0, 0, 0, 0, time.UTC)
	for !cur.After(end) {
		out = append(out, cur.Format("2006-01"))
		cur = cur.AddDate(0, 1, 0)
	}
	return out
}
