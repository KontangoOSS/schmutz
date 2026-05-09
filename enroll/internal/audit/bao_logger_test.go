package audit

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeKV struct {
	mu     sync.Mutex
	writes map[string][]byte
}

func newFakeKV() *fakeKV { return &fakeKV{writes: map[string][]byte{}} }

func (f *fakeKV) WriteJSON(ctx context.Context, path string, body []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes[path] = body
	return nil
}
func (f *fakeKV) ListKeys(ctx context.Context, path string) ([]string, error) { return nil, nil }
func (f *fakeKV) ReadJSON(ctx context.Context, path string) ([]byte, error)   { return nil, nil }
func (f *fakeKV) DeleteKey(ctx context.Context, path string) error            { return nil }

func TestBaoLogger_Record_PathPartition(t *testing.T) {
	kv := newFakeKV()
	l := NewBaoLogger(kv)
	ts := time.Date(2026, 5, 3, 14, 22, 11, 123_000_000, time.UTC)
	ev := Event{
		Timestamp: ts,
		Actor:     "dillon-laptop",
		Action:    ActionIdentityApprove,
		Resource:  "machine-a583fdac",
		Result:    ResultOK,
	}
	if err := l.Record(context.Background(), ev); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if len(kv.writes) != 1 {
		t.Fatalf("expected 1 write, got %d", len(kv.writes))
	}
	var path string
	for p := range kv.writes {
		path = p
	}
	if !strings.HasPrefix(path, "schmutz/audit/2026-05/") {
		t.Errorf("path = %q, want prefix schmutz/audit/2026-05/", path)
	}
	var body Event
	if err := json.Unmarshal(kv.writes[path], &body); err != nil {
		t.Fatalf("unmarshal stored event: %v", err)
	}
	if body.Actor != "dillon-laptop" {
		t.Errorf("stored actor = %q", body.Actor)
	}
}

func TestBaoLogger_Record_FillsTimestampIfZero(t *testing.T) {
	kv := newFakeKV()
	l := NewBaoLogger(kv)
	ev := Event{Action: ActionTokenIssue, Result: ResultOK}
	before := time.Now().UTC().Truncate(time.Millisecond)
	if err := l.Record(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	after := time.Now().UTC().Truncate(time.Millisecond).Add(time.Millisecond)
	if len(kv.writes) != 1 {
		t.Fatal("no write")
	}
	for _, body := range kv.writes {
		var stored Event
		json.Unmarshal(body, &stored)
		if stored.Timestamp.Before(before) || stored.Timestamp.After(after) {
			t.Errorf("timestamp = %v not in [%v, %v]", stored.Timestamp, before, after)
		}
	}
}
