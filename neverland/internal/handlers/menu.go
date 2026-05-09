package handlers

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"text/template"

	"git.konoss.org/kore/schmutz/neverland/internal/menuconfig"
)

// MenuHandler serves the iPXE chainload menu, dynamically composed from the
// MenuCache (git-backed theme + entries) with a hardcoded fallback.
type MenuHandler struct {
	bootBaseURL string
	cache       MenuCache // nil → always use fallback
}

// NewMenuHandler constructs a MenuHandler.
// If bootBaseURL is empty, the request's Host header is used.
// If cache is nil, the hardcoded fallback is always used.
func NewMenuHandler(bootBaseURL string, cache MenuCache) *MenuHandler {
	return &MenuHandler{
		bootBaseURL: strings.TrimRight(bootBaseURL, "/"),
		cache:       cache,
	}
}

// renderedEntry is one entry pre-computed for the template.
type renderedEntry struct {
	Key        string
	Label      string
	BootScript string // iPXE lines that run when user selects this entry
}

// Handle is GET /api/v1/menu.ipxe.
func (h *MenuHandler) Handle(w http.ResponseWriter, r *http.Request) {
	base := h.bootBaseURL
	if base == "" {
		scheme := "https"
		if proto := r.Header.Get("X-Forwarded-Proto"); proto == "http" {
			scheme = "http"
		} else if r.TLS != nil || proto == "https" {
			scheme = "https"
		}
		base = scheme + "://" + r.Host
	}

	arch := resolveArchAlias(r.URL.Query().Get("arch"))
	theme, entries := h.loadConfig(r, arch)

	rendered := make([]renderedEntry, 0, len(entries))
	for _, e := range entries {
		rendered = append(rendered, renderedEntry{
			Key:        e.Key,
			Label:      e.Label,
			BootScript: buildEntryScript(base, e),
		})
	}

	data := struct {
		Base      string
		Theme     menuconfig.ThemeConfig
		Entries   []renderedEntry
		TimeoutMs int
	}{
		Base:      base,
		Theme:     theme,
		Entries:   rendered,
		TimeoutMs: theme.TimeoutSeconds * 1000,
	}

	var buf bytes.Buffer
	if err := dynamicMenuTemplate.Execute(&buf, data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(buf.Bytes())
}

// loadConfig returns theme and filtered entries, falling back to hardcoded values.
func (h *MenuHandler) loadConfig(r *http.Request, arch string) (menuconfig.ThemeConfig, []menuconfig.EntryConfig) {
	theme := menuconfig.FallbackTheme()
	rawEntries := menuconfig.FallbackEntries().Entries

	if h.cache != nil {
		if t, err := h.cache.Theme(r.Context()); err == nil {
			theme = t.ThemeConfig
		}
		if e, err := h.cache.Entries(r.Context()); err == nil {
			rawEntries = e.Entries
		}
	}

	filtered := make([]menuconfig.EntryConfig, 0, len(rawEntries))
	for _, e := range rawEntries {
		if !e.Enabled {
			continue
		}
		if arch != "" {
			matched := false
			for _, a := range e.Arch {
				if a == arch {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		filtered = append(filtered, e)
	}
	return theme, filtered
}

// buildEntryScript returns the iPXE lines that boot a single entry.
func buildEntryScript(base string, e menuconfig.EntryConfig) string {
	switch e.Type {
	case "hook":
		if e.Variant == "rescue" {
			return fmt.Sprintf("chain %s/api/v1/hook/${buildarch}/hook.ipxe?rescue=1", base)
		}
		return fmt.Sprintf("chain %s/api/v1/hook/${buildarch}/hook.ipxe?session=${kontango_session_id}&claim=1", base)
	case "url":
		return fmt.Sprintf("chain %s/api/v1/artifacts/%s.raw.gz || goto menu", base, e.ID)
	case "chain":
		return fmt.Sprintf("chain --autofree %s || goto menu", e.ChainURL)
	default:
		return fmt.Sprintf("echo Unknown entry type: %s\ngoto menu", e.Type)
	}
}

var dynamicMenuTemplate = template.Must(template.New("menu").Parse(`#!ipxe

isset ${net0/ip} || dhcp net0 || goto failed

{{range .Theme.LogoASCII}}echo {{.}}
{{end}}echo {{.Theme.Tagline}}
echo

set beacon_url {{.Base}}/api/v1/beacon
imgfetch --name beacon ${beacon_url}?ipxe=1&mac=${net0/mac}&ip=${net0/ip}&arch=${buildarch}&platform=${platform}&userclass=${user-class} || goto offline_menu
imgexec beacon || goto offline_menu

iseq ${kontango_skip_claim} 1 && goto boot_anonymous || goto menu

:menu
menu {{.Theme.Title}}
item --gap
{{range .Entries}}item {{.Key}} {{.Label}}
{{end}}item l Boot from local disk
item s Drop to iPXE shell
item --gap
item --gap Network: ${net0/ip} / MAC: ${net0/mac}
item --gap Session: ${kontango_session_id}

choose --default {{.Theme.DefaultEntry}} --timeout {{.TimeoutMs}} target && goto ${target} || goto l

{{range .Entries}}:{{.Key}}
{{.BootScript}}

{{end}}:l
sanboot --no-describe || exit 1

:s
shell

:boot_anonymous
echo Recognised machine -- booting without claim screen.
chain {{.Base}}/api/v1/hook/${buildarch}/hook.ipxe?session=${kontango_session_id}&claim=0

:offline_menu
menu {{.Theme.Title}} Offline
item --gap
item r Rescue shell
item l Boot from local disk
item s iPXE shell
item retry Retry beacon
item --gap
item --gap Beacon unavailable.
item --gap Network: ${net0/ip} / MAC: ${net0/mac}

choose --default retry --timeout 30000 target && goto ${target} || goto l

:r
chain {{.Base}}/api/v1/hook/${buildarch}/hook.ipxe?rescue=1

:retry
imgfetch --name beacon ${beacon_url}?ipxe=1&mac=${net0/mac}&ip=${net0/ip}&arch=${buildarch}&platform=${platform}&userclass=${user-class} || goto offline_menu
imgexec beacon || goto offline_menu
goto menu

:failed
echo Network failed. Rebooting in 10 seconds.
sleep 10
reboot
`))
