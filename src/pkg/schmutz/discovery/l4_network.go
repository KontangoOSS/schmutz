package discovery

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// L4 — outbound network probes. These reveal the device's existence to
// third parties (the IP echoer, the geolocation provider). Off by default;
// the operator must explicitly enable L4 in the probe config.

// L4Config controls which network probes to run and where.
type L4Config struct {
	// PublicIPEndpoints — first one to succeed wins. Empty = use defaults.
	PublicIPEndpoints []string

	// GeolocationEndpoint — one URL returning {country,region,city,lat,lon,asn,org}-ish JSON.
	// Empty = use ipapi.co default.
	GeolocationEndpoint string

	// Timeout per request. Zero = 5s.
	Timeout time.Duration
}

// DefaultL4Endpoints returns the built-in defaults — public IP echoers and a
// geolocation provider.
func DefaultL4Endpoints() L4Config {
	return L4Config{
		PublicIPEndpoints:   []string{"https://api.ipify.org", "https://ifconfig.me/ip", "https://icanhazip.com"},
		GeolocationEndpoint: "https://ipapi.co/json/",
		Timeout:             5 * time.Second,
	}
}

// collectL4 hits the public-IP echo and geolocation endpoints. All errors are
// recorded but non-fatal — the rest of the result is still useful.
func collectL4(ctx context.Context, r *Result, cfg L4Config) {
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}
	if len(cfg.PublicIPEndpoints) == 0 {
		cfg.PublicIPEndpoints = DefaultL4Endpoints().PublicIPEndpoints
	}
	if cfg.GeolocationEndpoint == "" {
		cfg.GeolocationEndpoint = DefaultL4Endpoints().GeolocationEndpoint
	}

	publicIP := fetchPublicIP(ctx, cfg)
	if publicIP != "" {
		r.Set("public_ip", publicIP, LayerL4)
	}
	if geo := fetchGeolocation(ctx, cfg, publicIP); geo != nil {
		r.Set("geolocation", geo, LayerL4)
	}
}

func fetchPublicIP(ctx context.Context, cfg L4Config) string {
	client := &http.Client{Timeout: cfg.Timeout}
	for _, endpoint := range cfg.PublicIPEndpoints {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "schmutz-discovery/1")
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
		resp.Body.Close()
		if err != nil {
			continue
		}
		ip := strings.TrimSpace(string(body))
		if ip != "" && len(ip) <= 45 { // max IPv6 length
			return ip
		}
	}
	return ""
}

// Geolocation is a flexible struct — we don't pin to a single provider's schema.
type Geolocation struct {
	IP          string  `json:"ip,omitempty"`
	Country     string  `json:"country,omitempty"`
	CountryCode string  `json:"country_code,omitempty"`
	Region      string  `json:"region,omitempty"`
	RegionCode  string  `json:"region_code,omitempty"`
	City        string  `json:"city,omitempty"`
	Postal      string  `json:"postal,omitempty"`
	Latitude    float64 `json:"latitude,omitempty"`
	Longitude   float64 `json:"longitude,omitempty"`
	Timezone    string  `json:"timezone,omitempty"`
	ASN         string  `json:"asn,omitempty"`
	Org         string  `json:"org,omitempty"`
}

func fetchGeolocation(ctx context.Context, cfg L4Config, ipHint string) *Geolocation {
	client := &http.Client{Timeout: cfg.Timeout}
	url := cfg.GeolocationEndpoint
	if !strings.Contains(url, "{ip}") && ipHint != "" {
		// ipapi.co accepts /ip/json/
		if strings.HasSuffix(url, "/json/") {
			url = strings.TrimSuffix(url, "/json/") + "/" + ipHint + "/json/"
		}
	} else {
		url = strings.ReplaceAll(url, "{ip}", ipHint)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "schmutz-discovery/1")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if err != nil {
		return nil
	}
	g := &Geolocation{}
	if err := json.Unmarshal(body, g); err != nil {
		return nil
	}
	if g.IP == "" && g.Country == "" && g.City == "" {
		return nil
	}
	return g
}
