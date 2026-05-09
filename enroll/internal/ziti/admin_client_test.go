package ziti

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeZiti is an httptest stand-in for the Ziti management API.
type fakeZiti struct {
	mu         sync.Mutex
	identities map[string]Identity // by name
	deleted    []string
	updates    map[string]UpdateIdentityRequest
}

func newFakeZiti() *fakeZiti {
	return &fakeZiti{identities: map[string]Identity{}, updates: map[string]UpdateIdentityRequest{}}
}

func (f *fakeZiti) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/edge/management/v1/authenticate", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":{"token":"sess"}}`))
	})
	mux.HandleFunc("/edge/management/v1/identities", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Method == "GET" {
			var arr []Identity
			for _, id := range f.identities {
				arr = append(arr, id)
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"data": arr})
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})
	mux.HandleFunc("/edge/management/v1/identities/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		name := strings.TrimPrefix(r.URL.Path, "/edge/management/v1/identities/")
		switch r.Method {
		case "GET":
			id, ok := f.identities[name]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			json.NewEncoder(w).Encode(map[string]Identity{"data": id})
		case "PATCH":
			var body UpdateIdentityRequest
			json.NewDecoder(r.Body).Decode(&body)
			f.updates[name] = body
			id := f.identities[name]
			id.RoleAttributes = body.RoleAttributes
			if body.Tags != nil {
				id.Tags = body.Tags
			}
			f.identities[name] = id
			w.WriteHeader(http.StatusOK)
		case "DELETE":
			delete(f.identities, name)
			f.deleted = append(f.deleted, name)
			w.WriteHeader(http.StatusOK)
		}
	})
	return mux
}

func TestListIdentities_FilterByRole(t *testing.T) {
	f := newFakeZiti()
	f.identities["alice"] = Identity{ID: "a", Name: "alice", RoleAttributes: []string{"admins"}}
	f.identities["bob"] = Identity{ID: "b", Name: "bob", RoleAttributes: []string{"test"}}
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)

	c := NewHTTP(srv.URL, "u", "p", true)
	got, err := c.ListIdentities(context.Background(), IdentityFilter{HasRole: "admins"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "alice" {
		t.Errorf("got %+v", got)
	}
}

func TestGetIdentity_NotFound(t *testing.T) {
	f := newFakeZiti()
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	c := NewHTTP(srv.URL, "u", "p", true)
	if _, err := c.GetIdentity(context.Background(), "nope"); err == nil {
		t.Error("expected error for missing identity")
	}
}

func TestUpdateIdentity_PatchesRoleAttrs(t *testing.T) {
	f := newFakeZiti()
	f.identities["m1"] = Identity{ID: "m1", Name: "m1", RoleAttributes: []string{"quarantine"}}
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	c := NewHTTP(srv.URL, "u", "p", true)

	err := c.UpdateIdentity(context.Background(), "m1", UpdateIdentityRequest{
		RoleAttributes: []string{"test"},
		Tags:           map[string]string{"approved-by": "alice"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.updates["m1"].RoleAttributes; len(got) != 1 || got[0] != "test" {
		t.Errorf("update payload roles = %v", got)
	}
}
