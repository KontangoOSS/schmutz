package bao

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
)

func newTestServer(t *testing.T) (*httptest.Server, map[string]string) {
	t.Helper()
	store := map[string]string{}
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/secret/data/", func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/v1/secret/data/")
		switch r.Method {
		case "GET":
			body, ok := store[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Write([]byte(`{"data":{"data":` + body + `,"metadata":{"version":1}}}`))
		case "POST":
			var wrap struct {
				Data json.RawMessage `json:"data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&wrap); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			store[key] = string(wrap.Data)
			w.WriteHeader(http.StatusNoContent)
		case "DELETE":
			delete(store, key)
			w.WriteHeader(http.StatusNoContent)
		}
	})
	mux.HandleFunc("/v1/secret/metadata/", func(w http.ResponseWriter, r *http.Request) {
		// LIST
		if r.Method == "LIST" || (r.Method == "GET" && r.URL.Query().Get("list") == "true") {
			prefix := strings.TrimPrefix(r.URL.Path, "/v1/secret/metadata/")
			var keys []string
			for k := range store {
				if strings.HasPrefix(k, prefix) {
					rest := strings.TrimPrefix(k, prefix)
					if i := strings.Index(rest, "/"); i >= 0 {
						rest = rest[:i+1]
					}
					if rest != "" {
						keys = append(keys, rest)
					}
				}
			}
			sort.Strings(keys)
			out := map[string]map[string]interface{}{
				"data": {"keys": uniq(keys)},
			}
			b, _ := json.Marshal(out)
			w.Write(b)
			return
		}
		// DELETE: hard-delete all versions (KV v2 metadata endpoint)
		if r.Method == "DELETE" {
			key := strings.TrimPrefix(r.URL.Path, "/v1/secret/metadata/")
			delete(store, key)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, store
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func TestAdminKV_WriteReadRoundTrip(t *testing.T) {
	srv, _ := newTestServer(t)
	c := NewHTTP(srv.URL, "tok", "secret", "enroll-tokens", false).(*httpClient)

	ctx := context.Background()
	if err := c.WriteJSON(ctx, "schmutz/foo/bar", []byte(`{"hello":"world"}`)); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	got, err := c.ReadJSON(ctx, "schmutz/foo/bar")
	if err != nil {
		t.Fatalf("ReadJSON: %v", err)
	}
	if string(got) != `{"hello":"world"}` {
		t.Errorf("read = %s", got)
	}
}

func TestAdminKV_ReadMissingReturnsErr(t *testing.T) {
	srv, _ := newTestServer(t)
	c := NewHTTP(srv.URL, "tok", "secret", "enroll-tokens", false).(*httpClient)
	_, err := c.ReadJSON(context.Background(), "missing/key")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestAdminKV_ListKeys(t *testing.T) {
	srv, _ := newTestServer(t)
	c := NewHTTP(srv.URL, "tok", "secret", "enroll-tokens", false).(*httpClient)
	ctx := context.Background()
	c.WriteJSON(ctx, "schmutz/audit/2026-05/1-aaaa", []byte(`{}`))
	c.WriteJSON(ctx, "schmutz/audit/2026-05/2-bbbb", []byte(`{}`))
	c.WriteJSON(ctx, "schmutz/audit/2026-04/1-cccc", []byte(`{}`))

	got, err := c.ListKeys(ctx, "schmutz/audit/2026-05/")
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d keys, want 2: %v", len(got), got)
	}
}

func TestAdminKV_DeleteKey(t *testing.T) {
	srv, _ := newTestServer(t)
	c := NewHTTP(srv.URL, "tok", "secret", "enroll-tokens", false).(*httpClient)
	ctx := context.Background()
	c.WriteJSON(ctx, "schmutz/x", []byte(`{}`))
	if err := c.DeleteKey(ctx, "schmutz/x"); err != nil {
		t.Fatalf("DeleteKey: %v", err)
	}
	if _, err := c.ReadJSON(ctx, "schmutz/x"); err == nil {
		t.Error("expected ReadJSON to fail after delete")
	}
}
