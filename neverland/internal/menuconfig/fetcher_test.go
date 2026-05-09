package menuconfig_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"git.konoss.org/kore/schmutz/neverland/internal/menuconfig"
)

func serveContent(t *testing.T, v interface{}) *httptest.Server {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	resp := map[string]string{
		"content": base64.StdEncoding.EncodeToString(raw) + "\n",
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestFetcher_FetchTheme(t *testing.T) {
	want := menuconfig.ThemeConfig{
		Title:          "Test Boot",
		Tagline:        "Test tagline",
		LogoASCII:      []string{"line1", "line2"},
		LogoPNGURL:     "",
		Colors:         menuconfig.ThemeColors{Background: "0x111111"},
		TimeoutSeconds: 45,
		DefaultEntry:   "hookos",
	}
	srv := serveContent(t, want)
	defer srv.Close()

	cfg := menuconfig.GitFetcherConfig{
		APIBase: srv.URL,
		Owner:   "kore",
		Repo:    "boot-config",
		Ref:     "main",
		Token:   "test-token",
	}
	f := menuconfig.NewGitFetcher(cfg)

	got, err := f.FetchTheme(context.Background())
	if err != nil {
		t.Fatalf("FetchTheme: %v", err)
	}
	if got.Title != want.Title {
		t.Errorf("Title: got %q, want %q", got.Title, want.Title)
	}
	if got.TimeoutSeconds != want.TimeoutSeconds {
		t.Errorf("TimeoutSeconds: got %d, want %d", got.TimeoutSeconds, want.TimeoutSeconds)
	}
}

func TestFetcher_FetchEntries(t *testing.T) {
	want := menuconfig.EntriesConfig{
		Entries: []menuconfig.EntryConfig{
			{ID: "hookos", Label: "Hook OS", Key: "1", Type: "hook", Arch: []string{"x86_64"}, Enabled: true},
			{ID: "chain1", Label: "Chain target", Key: "c", Type: "chain", ChainURL: "https://example.com", Arch: []string{"x86_64"}, Enabled: true},
		},
	}
	srv := serveContent(t, want)
	defer srv.Close()

	cfg := menuconfig.GitFetcherConfig{
		APIBase: srv.URL,
		Owner:   "kore",
		Repo:    "boot-config",
		Ref:     "main",
		Token:   "test-token",
	}
	f := menuconfig.NewGitFetcher(cfg)

	got, err := f.FetchEntries(context.Background())
	if err != nil {
		t.Fatalf("FetchEntries: %v", err)
	}
	if len(got.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got.Entries))
	}
	if got.Entries[0].ID != "hookos" {
		t.Errorf("first entry ID: got %q, want %q", got.Entries[0].ID, "hookos")
	}
}

func TestFetcher_ReturnsErrorOn404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"not found"}`))
	}))
	defer srv.Close()

	f := menuconfig.NewGitFetcher(menuconfig.GitFetcherConfig{
		APIBase: srv.URL, Owner: "k", Repo: "r", Ref: "main", Token: "t",
	})

	_, err := f.FetchTheme(context.Background())
	if err == nil {
		t.Fatal("expected error on 404, got nil")
	}
}

func TestFetcher_URLConstruction(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := json.Marshal(menuconfig.ThemeConfig{Title: "t"})
		resp := map[string]string{"content": base64.StdEncoding.EncodeToString(raw) + "\n"}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	f := menuconfig.NewGitFetcher(menuconfig.GitFetcherConfig{
		APIBase: srv.URL,
		Owner:   "myorg",
		Repo:    "myrepo",
		Ref:     "main",
		Token:   "tok",
	})

	f.FetchTheme(context.Background())
	want := "/repos/myorg/myrepo/contents/theme.json"
	if gotPath != want {
		t.Errorf("URL path: got %q, want %q", gotPath, want)
	}
}
