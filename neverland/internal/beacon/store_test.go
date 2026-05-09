package beacon_test

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ory/dockertest/v3"

	"git.konoss.org/kore/schmutz/neverland/internal/beacon"
)

//go:embed testdata/0001_beacon_log.sql
var migrationFS embed.FS

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	pool, err := dockertest.NewPool("")
	if err != nil {
		log.Fatalf("dockertest pool: %v", err)
	}
	res, err := pool.Run("postgres", "16-alpine", []string{
		"POSTGRES_PASSWORD=test", "POSTGRES_DB=test", "POSTGRES_USER=test",
	})
	if err != nil {
		log.Fatalf("dockertest run: %v", err)
	}
	defer pool.Purge(res)

	dsn := fmt.Sprintf("postgres://test:test@localhost:%s/test?sslmode=disable", res.GetPort("5432/tcp"))

	if err := pool.Retry(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		p, e := pgxpool.New(ctx, dsn)
		if e != nil {
			return e
		}
		defer p.Close()
		return p.Ping(ctx)
	}); err != nil {
		log.Fatalf("dockertest connect: %v", err)
	}

	pp, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	testPool = pp

	// Apply migration
	sqlBytes, err := migrationFS.ReadFile("testdata/0001_beacon_log.sql")
	if err != nil {
		log.Fatalf("read embedded migration: %v", err)
	}
	if _, err := pp.Exec(context.Background(), string(sqlBytes)); err != nil {
		log.Fatalf("apply migration: %v", err)
	}

	code := m.Run()
	pp.Close()
	os.Exit(code)
}

func TestStore_Insert(t *testing.T) {
	store := beacon.NewStore(testPool)
	row := beacon.Row{
		SessionID: uuid.New(),
		Level:     "ipxe",
		MAC:       "bc:24:11:aa:bb:cc",
		IP:        "192.0.2.1",
		Payload:   json.RawMessage(`{"arch":"amd64"}`),
	}
	if err := store.Insert(context.Background(), row); err != nil {
		t.Fatalf("insert: %v", err)
	}
}

func TestStore_MarkClaimed(t *testing.T) {
	store := beacon.NewStore(testPool)
	sid := uuid.New()
	row := beacon.Row{
		SessionID: sid,
		Level:     "hook",
		MAC:       "bc:24:11:aa:bb:cd",
		Payload:   json.RawMessage(`{}`),
	}
	if err := store.Insert(context.Background(), row); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := store.MarkClaimed(context.Background(), sid, "dillon@kontango.us"); err != nil {
		t.Fatalf("mark: %v", err)
	}
	var claimedBy string
	if err := testPool.QueryRow(context.Background(),
		"SELECT claimed_by FROM beacon_log WHERE session_id=$1 LIMIT 1", sid).
		Scan(&claimedBy); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claimedBy != "dillon@kontango.us" {
		t.Fatalf("expected dillon@kontango.us, got %q", claimedBy)
	}
}

func TestStore_MarkClaimed_UnknownSession(t *testing.T) {
	store := beacon.NewStore(testPool)
	err := store.MarkClaimed(context.Background(), uuid.New(), "noone@example.com")
	if !errors.Is(err, beacon.ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}
