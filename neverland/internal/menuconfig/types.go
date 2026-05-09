package menuconfig

import "time"

// ThemeColors holds iPXE colour --rgb hex values.
type ThemeColors struct {
	Background  string `json:"background"`
	Foreground  string `json:"foreground"`
	HighlightBg string `json:"highlight_bg"`
	HighlightFg string `json:"highlight_fg"`
	GapText     string `json:"gap_text"`
}

// ThemeConfig is the parsed shape of theme.json.
type ThemeConfig struct {
	Title          string      `json:"title"`
	Tagline        string      `json:"tagline"`
	LogoASCII      []string    `json:"logo_ascii"`
	LogoPNGURL     string      `json:"logo_png_url"`
	Colors         ThemeColors `json:"colors"`
	TimeoutSeconds int         `json:"timeout_seconds"`
	DefaultEntry   string      `json:"default_entry"`
}

// EntryConfig is one row in entries.json.
type EntryConfig struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Key      string   `json:"key"`
	Type     string   `json:"type"`      // "hook", "url", "chain"
	Variant  string   `json:"variant"`   // hook only — blank means use ${buildarch}
	URL      string   `json:"url"`       // url type only
	ChainURL string   `json:"chain_url"` // chain type only
	Arch     []string `json:"arch"`
	Enabled  bool     `json:"enabled"`
}

// EntriesConfig is the parsed shape of entries.json.
type EntriesConfig struct {
	Entries []EntryConfig `json:"entries"`
}

// ThemeResponse is returned by GET /api/v1/menu/theme.
type ThemeResponse struct {
	ThemeConfig
	Source   string    `json:"source"`    // "git" or "fallback"
	CachedAt time.Time `json:"cached_at"` // zero if fallback
}

// EntriesResponse is returned by GET /api/v1/menu/entries.
type EntriesResponse struct {
	Entries  []EntryConfig `json:"entries"`
	Source   string        `json:"source"`
	CachedAt time.Time     `json:"cached_at"`
}
