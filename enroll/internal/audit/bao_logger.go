package audit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// KV is the Bao surface used by the audit package (Logger and Querier).
// WriteJSON is used by baoLogger; ListKeys, ReadJSON, and DeleteKey are
// used by Querier (query.go, added in Task 3).
type KV interface {
	WriteJSON(ctx context.Context, path string, body []byte) error
	ListKeys(ctx context.Context, path string) ([]string, error)
	ReadJSON(ctx context.Context, path string) ([]byte, error)
	DeleteKey(ctx context.Context, path string) error
}

type baoLogger struct {
	kv KV
}

func NewBaoLogger(kv KV) Logger {
	return &baoLogger{kv: kv}
}

func (l *baoLogger) Record(ctx context.Context, ev Event) error {
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	body, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	path := fmt.Sprintf(
		"schmutz/audit/%s/%d-%s",
		ev.Timestamp.UTC().Format("2006-01"),
		ev.Timestamp.UTC().UnixNano(),
		shortID(),
	)
	return l.kv.WriteJSON(ctx, path, body)
}

func shortID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
