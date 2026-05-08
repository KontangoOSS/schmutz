package gateway

import (
	"context"
	"log"
	"os"
	"strings"
	"time"
)

// Gateway orchestrates discovery, spec writes, and the HTTP server.
type Gateway struct {
	cfg     *RuntimeConfig
	server  *Server
	entries []ServiceEntry
}

// New builds a Gateway from a RuntimeConfig.
func New(cfg *RuntimeConfig) *Gateway {
	return &Gateway{cfg: cfg}
}

// Run performs initial discovery, publishes specs, starts a background
// refresh loop, and returns the HTTP server handler.
// The caller binds the server to a Ziti service (or localhost for testing).
func (g *Gateway) Run(ctx context.Context) (*Server, error) {
	if err := g.refresh(ctx); err != nil {
		log.Printf("gateway: initial discovery failed: %v", err)
	}
	go g.refreshLoop(ctx)

	info := HostInfo{
		ZitiIdentity:   g.cfg.Agent.ZitiIdentity,
		App:            g.cfg.Agent.App,
		Deployment:     g.cfg.Agent.Deployment,
		Tenant:         g.cfg.Agent.Tenant,
		GatewayVersion: "0.1.0",
	}
	g.server = NewServer(info, g.entries)
	return g.server, nil
}

func (g *Gateway) refresh(ctx context.Context) error {
	baoToken := g.readBaoToken()
	discCfg := DiscoverConfig{
		AgentJSON: AgentInfo{
			Tenant:     g.cfg.Agent.Tenant,
			App:        g.cfg.Agent.App,
			Deployment: g.cfg.Agent.Deployment,
			BaoAddr:    g.cfg.Agent.BaoAddr,
			BaoToken:   baoToken,
		},
		GatewayConfig: g.cfg.Gateway,
		ForgejoURL:    g.cfg.ForgejoURL,
		ForgejoToken:  g.cfg.ForgejoToken,
		ForgejoOrg:    g.cfg.ForgejoOrg,
	}

	entries, err := Discover(ctx, discCfg)
	if err != nil {
		return err
	}
	g.entries = entries

	if baoToken != "" {
		baoCfg := BaoWriteConfig{
			BaoAddr:    g.cfg.Agent.BaoAddr,
			BaoToken:   baoToken,
			Tenant:     g.cfg.Agent.Tenant,
			App:        g.cfg.Agent.App,
			Deployment: g.cfg.Agent.Deployment,
		}
		if err := WriteSpecs(ctx, baoCfg, entries); err != nil {
			log.Printf("gateway: bao write: %v", err)
		}
	}

	for _, e := range entries {
		if len(e.SpecJSON) == 0 {
			continue
		}
		fCfg := ForgejoWriteConfig{
			ForgejoURL:   g.cfg.ForgejoURL,
			ForgejoToken: g.cfg.ForgejoToken,
			ForgejoOrg:   g.cfg.ForgejoOrg,
			App:          g.cfg.Agent.App,
		}
		if err := WriteSpecToForgejo(ctx, fCfg, e.SpecJSON); err != nil {
			log.Printf("gateway: forgejo write for %s: %v", e.Name, err)
		}
		break
	}

	if g.server != nil {
		info := HostInfo{
			ZitiIdentity: g.cfg.Agent.ZitiIdentity,
			App:          g.cfg.Agent.App,
			Deployment:   g.cfg.Agent.Deployment,
			Tenant:       g.cfg.Agent.Tenant,
		}
		g.server = NewServer(info, entries)
	}

	log.Printf("gateway: discovered %d services", len(entries))
	return nil
}

func (g *Gateway) refreshLoop(ctx context.Context) {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := g.refresh(ctx); err != nil {
				log.Printf("gateway: refresh: %v", err)
			}
		}
	}
}

func (g *Gateway) readBaoToken() string {
	path := g.cfg.BaoTokenPath
	if path == "" {
		path = "/run/bao-token"
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
