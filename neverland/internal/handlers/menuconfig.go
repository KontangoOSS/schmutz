package handlers

import (
	"context"
	"net/http"

	"git.konoss.org/kore/schmutz/neverland/internal/menuconfig"
	"git.konoss.org/kore/schmutz/neverland/internal/respond"
)

// MenuCache is the interface MenuConfigCache satisfies.
type MenuCache interface {
	Theme(ctx context.Context) (*menuconfig.ThemeResponse, error)
	Entries(ctx context.Context) (*menuconfig.EntriesResponse, error)
}

// MenuConfigHandler exposes the theme and entries cache as HTTP JSON endpoints.
type MenuConfigHandler struct {
	cache MenuCache
}

// NewMenuConfigHandler constructs a MenuConfigHandler.
func NewMenuConfigHandler(cache MenuCache) *MenuConfigHandler {
	return &MenuConfigHandler{cache: cache}
}

// GetTheme handles GET /api/v1/menu/theme.
func (h *MenuConfigHandler) GetTheme(w http.ResponseWriter, r *http.Request) {
	theme, err := h.cache.Theme(r.Context())
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "failed to load theme")
		return
	}
	respond.JSON(w, http.StatusOK, theme)
}

// GetEntries handles GET /api/v1/menu/entries.
// When ?arch= is provided, filters to enabled entries valid for that arch
// (after alias resolution). Without ?arch=, all entries are returned.
func (h *MenuConfigHandler) GetEntries(w http.ResponseWriter, r *http.Request) {
	entries, err := h.cache.Entries(r.Context())
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "failed to load entries")
		return
	}

	arch := r.URL.Query().Get("arch")
	if arch == "" {
		respond.JSON(w, http.StatusOK, entries)
		return
	}

	resolved := resolveArchAlias(arch)

	filtered := make([]menuconfig.EntryConfig, 0, len(entries.Entries))
	for _, e := range entries.Entries {
		if !e.Enabled {
			continue
		}
		for _, a := range e.Arch {
			if a == resolved {
				filtered = append(filtered, e)
				break
			}
		}
	}

	respond.JSON(w, http.StatusOK, &menuconfig.EntriesResponse{
		Entries:  filtered,
		Source:   entries.Source,
		CachedAt: entries.CachedAt,
	})
}

// archAliasMap maps iPXE buildarch values to canonical arch names.
var archAliasMap = map[string]string{
	"i386":  "x86_64",
	"amd64": "x86_64",
	"arm64": "aarch64",
}

func resolveArchAlias(arch string) string {
	if a, ok := archAliasMap[arch]; ok {
		return a
	}
	return arch
}
