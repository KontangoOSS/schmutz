package beacon

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrSessionNotFound is returned by MarkClaimed when no rows match the given session_id.
var ErrSessionNotFound = errors.New("beacon: session not found")

// Row is one beacon entry.
type Row struct {
	SessionID uuid.UUID
	Level     string // "ipxe" or "hook"
	MAC       string
	IP        string
	Payload   json.RawMessage
}

// Store writes and updates beacon_log rows.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore wraps an open pgxpool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Insert appends a beacon row. Always returns a new id; never updates existing rows.
func (s *Store) Insert(ctx context.Context, r Row) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO beacon_log (session_id, level, mac, ip, payload)
		VALUES ($1, $2, $3, $4, $5)
	`, r.SessionID, r.Level, r.MAC, r.IP, r.Payload)
	return err
}

// MarkClaimed sets claimed_at and claimed_by on every row for the given session.
// Returns ErrSessionNotFound if no rows matched session_id.
func (s *Store) MarkClaimed(ctx context.Context, sessionID uuid.UUID, userEmail string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE beacon_log
		   SET claimed_at = now(), claimed_by = $2
		 WHERE session_id = $1
	`, sessionID, userEmail)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrSessionNotFound
	}
	return nil
}
