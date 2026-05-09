package menuconfig

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// GitFetcherConfig holds connection settings for the git host.
type GitFetcherConfig struct {
	// APIBase is the root of the contents API.
	// Forgejo: "https://git.konoss.org/api/v1"
	// GitHub:  "https://api.github.com"
	APIBase string
	Owner   string
	Repo    string
	Ref     string
	Token   string
}

// GitFetcher fetches theme.json and entries.json from a git host.
type GitFetcher struct {
	cfg    GitFetcherConfig
	client *http.Client
}

// NewGitFetcher constructs a GitFetcher with a 10-second HTTP timeout.
func NewGitFetcher(cfg GitFetcherConfig) *GitFetcher {
	return &GitFetcher{
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// FetchTheme retrieves and parses theme.json.
func (f *GitFetcher) FetchTheme(ctx context.Context) (*ThemeConfig, error) {
	var cfg ThemeConfig
	if err := f.fetch(ctx, "theme.json", &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// FetchEntries retrieves and parses entries.json.
func (f *GitFetcher) FetchEntries(ctx context.Context) (*EntriesConfig, error) {
	var cfg EntriesConfig
	if err := f.fetch(ctx, "entries.json", &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// fetch calls the contents API for a single file, base64-decodes the response,
// and JSON-unmarshals into v.
func (f *GitFetcher) fetch(ctx context.Context, filename string, v interface{}) error {
	base := strings.TrimRight(f.cfg.APIBase, "/")
	url := fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s",
		base, f.cfg.Owner, f.cfg.Repo, filename, f.cfg.Ref)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if f.cfg.Token != "" {
		req.Header.Set("Authorization", "token "+f.cfg.Token)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", filename, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("fetch %s: HTTP %d: %s", filename, resp.StatusCode, body)
	}

	var envelope struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("decode envelope for %s: %w", filename, err)
	}

	raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(envelope.Content, "\n", ""))
	if err != nil {
		return fmt.Errorf("base64 decode %s: %w", filename, err)
	}

	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("json unmarshal %s: %w", filename, err)
	}
	return nil
}
