package oidc_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"git.konoss.org/kore/schmutz/agent/internal/oidc"
)

func TestToken_clientCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/v2/token" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "test-token-abc",
			"expires_in":   3600,
		})
	}))
	defer srv.Close()

	client := oidc.New(srv.URL, "client-id", "client-secret")
	tok, err := client.Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tok != "test-token-abc" {
		t.Errorf("token: got %q", tok)
	}
}

func TestToken_cached(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "cached-token",
			"expires_in":   3600,
		})
	}))
	defer srv.Close()

	client := oidc.New(srv.URL, "id", "secret")
	client.Token(context.Background())
	client.Token(context.Background()) // should use cache
	if calls != 1 {
		t.Errorf("expected 1 HTTP call (cached), got %d", calls)
	}
}
