package bao

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func writeTokenRecord(t *testing.T, c *httpClient, token string, rec TokenRecord) {
	t.Helper()
	body, _ := json.Marshal(rec)
	if err := c.WriteJSON(context.Background(), "enroll-tokens/"+token, body); err != nil {
		t.Fatalf("seed token: %v", err)
	}
}

func TestListTokens_FiltersExpiredAndConsumed(t *testing.T) {
	srv, _ := newTestServer(t)
	c := NewHTTP(srv.URL, "tok", "secret", "enroll-tokens", false).(*httpClient)

	now := time.Now().UTC()
	consumed := now.Add(-time.Hour)

	writeTokenRecord(t, c, "ZE-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", TokenRecord{
		Slug:           "active",
		RoleAttributes: []string{"test"},
		ExpiresAt:      now.Add(time.Hour),
		IssuedBy:       "admin",
	})
	writeTokenRecord(t, c, "ZE-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", TokenRecord{
		Slug:           "expired",
		RoleAttributes: []string{"test"},
		ExpiresAt:      now.Add(-time.Hour),
		IssuedBy:       "admin",
	})
	writeTokenRecord(t, c, "ZE-cccccccccccccccccccccccccccccccc", TokenRecord{
		Slug:           "consumed",
		RoleAttributes: []string{"test"},
		ExpiresAt:      now.Add(time.Hour),
		IssuedBy:       "admin",
		ConsumedAt:     &consumed,
		ConsumedBy:     "1.2.3.4",
	})

	got, err := c.ListTokens(context.Background(), false)
	if err != nil {
		t.Fatalf("ListTokens: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("active-only: got %d, want 1", len(got))
	}
	if got[0].Token == "" || got[0].Record.Slug != "active" {
		t.Errorf("got %+v", got[0])
	}

	gotAll, err := c.ListTokens(context.Background(), true)
	if err != nil {
		t.Fatalf("ListTokens(all): %v", err)
	}
	if len(gotAll) != 3 {
		t.Errorf("all: got %d, want 3", len(gotAll))
	}
}

func TestDeleteToken(t *testing.T) {
	srv, _ := newTestServer(t)
	c := NewHTTP(srv.URL, "tok", "secret", "enroll-tokens", false).(*httpClient)
	tok := "ZE-dddddddddddddddddddddddddddddddd"
	writeTokenRecord(t, c, tok, TokenRecord{Slug: "x", ExpiresAt: time.Now().Add(time.Hour)})
	if err := c.DeleteToken(context.Background(), tok); err != nil {
		t.Fatalf("DeleteToken: %v", err)
	}
	if _, err := c.GetToken(context.Background(), tok); err == nil {
		t.Error("expected GetToken to fail after delete")
	}
}
