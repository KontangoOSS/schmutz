package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"git.konoss.org/kore/schmutz/neverland/internal/handlers"
	"git.konoss.org/kore/schmutz/neverland/internal/menuconfig"
)

// fakeMenuCache satisfies handlers.MenuCache for unit tests.
type fakeMenuCache struct {
	theme   *menuconfig.ThemeResponse
	entries *menuconfig.EntriesResponse
}

func (f *fakeMenuCache) Theme(_ context.Context) (*menuconfig.ThemeResponse, error) {
	return f.theme, nil
}
func (f *fakeMenuCache) Entries(_ context.Context) (*menuconfig.EntriesResponse, error) {
	return f.entries, nil
}

func TestThemeHandler_ReturnsJSON(t *testing.T) {
	cache := &fakeMenuCache{
		theme: &menuconfig.ThemeResponse{
			ThemeConfig: menuconfig.ThemeConfig{Title: "Test Boot", TimeoutSeconds: 30},
			Source:      "git",
			CachedAt:    time.Now(),
		},
	}
	h := handlers.NewMenuConfigHandler(cache)

	req := httptest.NewRequest("GET", "/api/v1/menu/theme", nil)
	w := httptest.NewRecorder()
	h.GetTheme(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp menuconfig.ThemeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Title != "Test Boot" {
		t.Errorf("title: got %q, want %q", resp.Title, "Test Boot")
	}
	if resp.Source != "git" {
		t.Errorf("source: got %q, want %q", resp.Source, "git")
	}
	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("content-type: got %q, want application/json", ct)
	}
}

func TestEntriesHandler_ReturnsAll(t *testing.T) {
	cache := &fakeMenuCache{
		entries: &menuconfig.EntriesResponse{
			Entries: []menuconfig.EntryConfig{
				{ID: "hookos", Arch: []string{"x86_64", "aarch64"}, Enabled: true, Key: "1", Type: "hook"},
				{ID: "ubuntu", Arch: []string{"x86_64"}, Enabled: false, Key: "2", Type: "url"},
			},
			Source: "git",
		},
	}
	h := handlers.NewMenuConfigHandler(cache)

	req := httptest.NewRequest("GET", "/api/v1/menu/entries", nil)
	w := httptest.NewRecorder()
	h.GetEntries(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp menuconfig.EntriesResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Entries) != 2 {
		t.Fatalf("expected 2 entries unfiltered, got %d", len(resp.Entries))
	}
}

func TestEntriesHandler_FiltersByArch(t *testing.T) {
	cache := &fakeMenuCache{
		entries: &menuconfig.EntriesResponse{
			Entries: []menuconfig.EntryConfig{
				{ID: "x86only", Arch: []string{"x86_64"}, Enabled: true, Key: "1", Type: "hook"},
				{ID: "both", Arch: []string{"x86_64", "aarch64"}, Enabled: true, Key: "2", Type: "hook"},
				{ID: "armonly", Arch: []string{"aarch64"}, Enabled: true, Key: "3", Type: "hook"},
			},
			Source: "git",
		},
	}
	h := handlers.NewMenuConfigHandler(cache)

	req := httptest.NewRequest("GET", "/api/v1/menu/entries?arch=x86_64", nil)
	w := httptest.NewRecorder()
	h.GetEntries(w, req)

	var resp menuconfig.EntriesResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Entries) != 2 {
		t.Fatalf("expected 2 entries for x86_64 (x86only + both), got %d", len(resp.Entries))
	}
	for _, e := range resp.Entries {
		if e.ID == "armonly" {
			t.Error("armonly should not appear in x86_64 filter")
		}
	}
}

func TestEntriesHandler_FiltersByArchAlias(t *testing.T) {
	cache := &fakeMenuCache{
		entries: &menuconfig.EntriesResponse{
			Entries: []menuconfig.EntryConfig{
				{ID: "hookos", Arch: []string{"x86_64"}, Enabled: true, Key: "1", Type: "hook"},
			},
			Source: "git",
		},
	}
	h := handlers.NewMenuConfigHandler(cache)

	for _, alias := range []string{"i386", "amd64"} {
		req := httptest.NewRequest("GET", "/api/v1/menu/entries?arch="+alias, nil)
		w := httptest.NewRecorder()
		h.GetEntries(w, req)
		var resp menuconfig.EntriesResponse
		json.Unmarshal(w.Body.Bytes(), &resp)
		if len(resp.Entries) != 1 {
			t.Errorf("arch alias %q: expected 1 entry, got %d", alias, len(resp.Entries))
		}
	}
}

func TestEntriesHandler_DisabledEntriesExcluded(t *testing.T) {
	cache := &fakeMenuCache{
		entries: &menuconfig.EntriesResponse{
			Entries: []menuconfig.EntryConfig{
				{ID: "enabled", Arch: []string{"x86_64"}, Enabled: true, Key: "1", Type: "hook"},
				{ID: "disabled", Arch: []string{"x86_64"}, Enabled: false, Key: "2", Type: "hook"},
			},
			Source: "git",
		},
	}
	h := handlers.NewMenuConfigHandler(cache)

	req := httptest.NewRequest("GET", "/api/v1/menu/entries?arch=x86_64", nil)
	w := httptest.NewRecorder()
	h.GetEntries(w, req)
	var resp menuconfig.EntriesResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Entries) != 1 {
		t.Fatalf("expected 1 (disabled excluded), got %d", len(resp.Entries))
	}
	if resp.Entries[0].ID != "enabled" {
		t.Errorf("expected enabled entry, got %q", resp.Entries[0].ID)
	}
}
