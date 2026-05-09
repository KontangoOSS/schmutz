package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"git.konoss.org/kore/schmutz/neverland/internal/handlers"
	"git.konoss.org/kore/schmutz/neverland/internal/menuconfig"
)

func defaultFakeCache() *fakeMenuCache {
	return &fakeMenuCache{
		theme: &menuconfig.ThemeResponse{
			ThemeConfig: menuconfig.ThemeConfig{
				Title:          "Kontango Boot",
				Tagline:        "Boot anywhere.",
				LogoASCII:      []string{"  logo line 1", "  logo line 2"},
				Colors:         menuconfig.ThemeColors{Background: "0x0f4c5c", Foreground: "0xffffff", HighlightBg: "0xe6b94d", HighlightFg: "0x1f2024"},
				TimeoutSeconds: 30,
				DefaultEntry:   "hookos",
			},
			Source:   "git",
			CachedAt: time.Now(),
		},
		entries: &menuconfig.EntriesResponse{
			Entries: []menuconfig.EntryConfig{
				{ID: "hookos", Label: "Install Kontango Hook OS", Key: "1", Type: "hook", Arch: []string{"x86_64", "i386", "aarch64"}, Enabled: true},
				{ID: "rescue", Label: "Rescue shell", Key: "r", Type: "hook", Variant: "rescue", Arch: []string{"x86_64", "i386", "aarch64"}, Enabled: true},
			},
			Source: "git",
		},
	}
}

func TestMenu_RendersIPXEScript(t *testing.T) {
	h := handlers.NewMenuHandler("https://boot.kontango.net", defaultFakeCache())
	req := httptest.NewRequest("GET", "/api/v1/menu.ipxe?arch=x86_64", nil)
	w := httptest.NewRecorder()
	h.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.HasPrefix(body, "#!ipxe") {
		t.Fatalf("expected iPXE shebang, got: %.30s", body)
	}
	if !strings.Contains(body, "https://boot.kontango.net/api/v1/beacon") {
		t.Fatal("expected beacon URL")
	}
	if !strings.Contains(body, "${buildarch}") {
		t.Fatal("expected iPXE-side ${buildarch} placeholder")
	}
	if !strings.Contains(body, "menu Kontango Boot") {
		t.Fatal("expected menu title from theme")
	}
	if !strings.Contains(body, "logo line 1") {
		t.Fatal("expected logo line from theme")
	}
	if !strings.Contains(body, "Install Kontango Hook OS") {
		t.Fatal("expected entry label from entries")
	}
	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("expected text/plain, got %q", ct)
	}
}

func TestMenu_HasOfflineFallback(t *testing.T) {
	h := handlers.NewMenuHandler("https://boot.kontango.net", defaultFakeCache())
	req := httptest.NewRequest("GET", "/api/v1/menu.ipxe", nil)
	w := httptest.NewRecorder()
	h.Handle(w, req)
	body := w.Body.String()
	if !strings.Contains(body, ":offline_menu") {
		t.Fatal("offline_menu label missing")
	}
	if !strings.Contains(body, ":retry") {
		t.Fatal("offline retry label missing")
	}
}

func TestMenu_HostHeaderRespected(t *testing.T) {
	h := handlers.NewMenuHandler("", defaultFakeCache())
	req := httptest.NewRequest("GET", "/api/v1/menu.ipxe", nil)
	req.Host = "boot.example.com"
	req.TLS = nil
	w := httptest.NewRecorder()
	h.Handle(w, req)
	body := w.Body.String()
	if !strings.Contains(body, "https://boot.example.com") {
		t.Fatalf("expected host fallback, got: %s", body[:200])
	}
}

func TestMenu_ChainEntryRendered(t *testing.T) {
	cache := &fakeMenuCache{
		theme: defaultFakeCache().theme,
		entries: &menuconfig.EntriesResponse{
			Entries: []menuconfig.EntryConfig{
				{ID: "netbootxyz", Label: "netboot.xyz", Key: "n", Type: "chain",
					ChainURL: "https://boot.netboot.xyz", Arch: []string{"x86_64"}, Enabled: true},
			},
			Source: "git",
		},
	}
	h := handlers.NewMenuHandler("https://boot.kontango.net", cache)
	req := httptest.NewRequest("GET", "/api/v1/menu.ipxe?arch=x86_64", nil)
	w := httptest.NewRecorder()
	h.Handle(w, req)
	body := w.Body.String()
	if !strings.Contains(body, "chain --autofree https://boot.netboot.xyz") {
		idx := strings.Index(body, ":n\n")
		excerpt := ""
		if idx >= 0 {
			end := idx + 200
			if end > len(body) {
				end = len(body)
			}
			excerpt = body[idx:end]
		}
		t.Errorf("expected chain entry, got body excerpt:\n%s", excerpt)
	}
}

func TestMenu_URLEntryRendered(t *testing.T) {
	cache := &fakeMenuCache{
		theme: defaultFakeCache().theme,
		entries: &menuconfig.EntriesResponse{
			Entries: []menuconfig.EntryConfig{
				{ID: "ubuntu2404", Label: "Ubuntu 24.04", Key: "2", Type: "url",
					URL: "https://releases.ubuntu.com/ubuntu.iso", Arch: []string{"x86_64"}, Enabled: true},
			},
			Source: "git",
		},
	}
	h := handlers.NewMenuHandler("https://boot.kontango.net", cache)
	req := httptest.NewRequest("GET", "/api/v1/menu.ipxe?arch=x86_64", nil)
	w := httptest.NewRecorder()
	h.Handle(w, req)
	body := w.Body.String()
	if !strings.Contains(body, "ubuntu2404") {
		t.Errorf("expected url entry id in boot script, body len=%d", len(body))
	}
}

func TestMenu_FallbackUsedWhenCacheNil(t *testing.T) {
	h := handlers.NewMenuHandler("https://boot.kontango.net", nil)
	req := httptest.NewRequest("GET", "/api/v1/menu.ipxe", nil)
	w := httptest.NewRecorder()
	h.Handle(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 even with nil cache, got %d", w.Code)
	}
	if !strings.HasPrefix(w.Body.String(), "#!ipxe") {
		t.Fatal("expected valid iPXE with nil cache")
	}
}
