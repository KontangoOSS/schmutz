package audit

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

type seededKV struct{ entries map[string][]byte }

func (s *seededKV) WriteJSON(ctx context.Context, p string, b []byte) error {
	s.entries[p] = b
	return nil
}
func (s *seededKV) ReadJSON(ctx context.Context, p string) ([]byte, error) {
	if b, ok := s.entries[p]; ok {
		return b, nil
	}
	return nil, ErrAuditNotFound
}
func (s *seededKV) ListKeys(ctx context.Context, p string) ([]string, error) {
	var out []string
	for k := range s.entries {
		if len(k) > len(p) && k[:len(p)] == p {
			out = append(out, k[len(p):])
		}
	}
	return out, nil
}
func (s *seededKV) DeleteKey(ctx context.Context, p string) error { delete(s.entries, p); return nil }

func seedEvents(t *testing.T, kv *seededKV, evs ...Event) {
	t.Helper()
	l := NewBaoLogger(kv)
	for _, ev := range evs {
		if err := l.Record(context.Background(), ev); err != nil {
			t.Fatal(err)
		}
	}
}

func TestQuery_FilterByActor(t *testing.T) {
	kv := &seededKV{entries: map[string][]byte{}}
	may := time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)
	seedEvents(t, kv,
		Event{Timestamp: may, Actor: "dillon-laptop", Action: ActionTokenIssue, Result: ResultOK},
		Event{Timestamp: may.Add(time.Minute), Actor: "alice", Action: ActionTokenIssue, Result: ResultOK},
		Event{Timestamp: may.Add(2 * time.Minute), Actor: "dillon-laptop", Action: ActionIdentityApprove, Result: ResultOK},
	)
	q := NewQuerier(kv)
	got, err := q.Query(context.Background(), Filter{Actor: "dillon-laptop", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2", len(got))
	}
	for _, ev := range got {
		if ev.Actor != "dillon-laptop" {
			t.Errorf("filter leaked actor %q", ev.Actor)
		}
	}
}

func TestQuery_SortDescAndLimit(t *testing.T) {
	kv := &seededKV{entries: map[string][]byte{}}
	base := time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		seedEvents(t, kv, Event{Timestamp: base.Add(time.Duration(i) * time.Minute), Actor: "x", Action: ActionTokenIssue, Result: ResultOK})
	}
	q := NewQuerier(kv)
	got, err := q.Query(context.Background(), Filter{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d, want 3", len(got))
	}
	if !got[0].Timestamp.After(got[1].Timestamp) || !got[1].Timestamp.After(got[2].Timestamp) {
		t.Errorf("expected descending timestamps, got %v %v %v", got[0].Timestamp, got[1].Timestamp, got[2].Timestamp)
	}
}

func TestQuery_LimitDefaultAndCap(t *testing.T) {
	kv := &seededKV{entries: map[string][]byte{}}
	q := NewQuerier(kv)
	f := Filter{}
	if got := f.normalizedLimit(); got != 100 {
		t.Errorf("default limit = %d, want 100", got)
	}
	f.Limit = 5000
	if got := f.normalizedLimit(); got != 1000 {
		t.Errorf("cap = %d, want 1000", got)
	}
	_ = q
}

func TestQuery_FilterBySinceUntil(t *testing.T) {
	kv := &seededKV{entries: map[string][]byte{}}
	base := time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC)
	seedEvents(t, kv,
		Event{Timestamp: base, Actor: "x", Action: ActionTokenIssue, Result: ResultOK},
		Event{Timestamp: base.Add(time.Hour), Actor: "x", Action: ActionTokenIssue, Result: ResultOK},
		Event{Timestamp: base.Add(2 * time.Hour), Actor: "x", Action: ActionTokenIssue, Result: ResultOK},
	)
	q := NewQuerier(kv)
	got, err := q.Query(context.Background(), Filter{
		Since: base.Add(30 * time.Minute),
		Until: base.Add(90 * time.Minute),
		Limit: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d, want 1; events: %v", len(got), eventsTimes(got))
	}
}

func eventsTimes(evs []Event) []time.Time {
	out := make([]time.Time, len(evs))
	for i, e := range evs {
		out[i] = e.Timestamp
	}
	return out
}

// silence unused import
var _ = json.Marshal
