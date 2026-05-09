// Package app provides the Browzer application bootstrap.
//
// It handles config-from-env, health endpoint, SPA file serving,
// plugin registration, and startup.
package app

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"git.konoss.org/kore/schmutz/dashboard/pkg/browzer/bff"
	"git.konoss.org/kore/schmutz/dashboard/pkg/browzer/ziti"
)

// Config holds the standard configuration for any Browzer app.
type Config struct {
	ZitiAddr     string
	ZitiUser     string
	ZitiPass     string
	ZitiRefresh  string // OIDC refresh token for pre11 management API
	ListenAddr   string
	WebDir       string
}

// ConfigFromEnv reads standard Browzer app config from environment variables.
func ConfigFromEnv() Config {
	return Config{
		ZitiAddr:    envOr("ZITI_CTRL_ADDR", "ctrl.konoss.org:1280"),
		ZitiUser:    envOr("ZITI_ADMIN_USER", "admin"),
		ZitiPass:    envOr("ZITI_ADMIN_PASS", ""),
		ZitiRefresh: envOr("ZITI_REFRESH_TOKEN", ""),
		ListenAddr:  envOr("LISTEN_ADDR", ":8080"),
		WebDir:      envOr("WEB_DIR", "frontend"),
	}
}

// App is a Browzer application.
type App struct {
	Config     Config
	BFF        *bff.Router
	ZitiClient *ziti.Client
	plugins    []bff.PluginInfo
}

// loadIdentityCert extracts cert and key PEM from a Ziti identity JSON file.
func loadIdentityCert(path string) (cert, key string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	var id struct {
		ID struct {
			Cert string `json:"cert"`
			Key  string `json:"key"`
		} `json:"id"`
	}
	if err := json.Unmarshal(data, &id); err != nil {
		return "", ""
	}
	return strings.TrimPrefix(id.ID.Cert, "pem:"),
		strings.TrimPrefix(id.ID.Key, "pem:")
}

// New creates a new Browzer app with standard plumbing wired up.
func New(cfg Config) *App {
	client := &ziti.Client{
		Addr:         cfg.ZitiAddr,
		RefreshToken: cfg.ZitiRefresh,
		User:         cfg.ZitiUser,
		Pass:         cfg.ZitiPass,
	}

	// Auth priority: cert (isAdmin identity) > OIDC refresh token > password.
	// Cert auth requires isAdmin=true on the identity — only load if no other method is set.
	if identityPath := os.Getenv("ZITI_IDENTITY"); identityPath != "" && cfg.ZitiRefresh == "" && cfg.ZitiPass == "" {
		cert, key := loadIdentityCert(identityPath)
		if cert != "" && key != "" {
			client.CertPEM = cert
			client.KeyPEM = key
			client.UseCert = true
			log.Printf("ziti: using cert-based auth from %s", identityPath)
		}
	} else if cfg.ZitiRefresh != "" {
		log.Printf("ziti: using OIDC refresh token for management API")
	} else if cfg.ZitiPass != "" {
		log.Printf("ziti: using password auth for management API (user=%s)", cfg.ZitiUser)
	}

	router := bff.New(client)

	a := &App{
		Config:     cfg,
		BFF:        router,
		ZitiClient: client,
	}

	// Standard endpoints every Browzer app gets
	router.HandleRaw("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		bff.WriteJSON(w, map[string]string{"status": "ok"})
	})

	router.HandleRaw("GET /api/plugins", func(w http.ResponseWriter, r *http.Request) {
		bff.WriteJSON(w, a.plugins)
	})

	return a
}

// Use registers a plugin. Call before ServeAndListen.
func (a *App) Use(p bff.Plugin) {
	p.Register(a.BFF)
	a.plugins = append(a.plugins, bff.PluginInfo{
		Name:        p.Name(),
		Description: p.Description(),
	})
	log.Printf("plugin loaded: %s", p.Name())
}

// ServeAndListen starts the HTTP server with the SPA fallback.
func (a *App) ServeAndListen() {
	a.BFF.Mux.Handle("/", spaHandler(a.Config.WebDir))

	log.Printf("listening on %s (%d plugins loaded)", a.Config.ListenAddr, len(a.plugins))
	log.Fatal(http.ListenAndServe(a.Config.ListenAddr, a.BFF.Mux))
}

// Serve starts the HTTP server and blocks until ctx is cancelled, then shuts
// down gracefully with a 10-second drain timeout.
func (a *App) Serve(ctx context.Context) error {
	a.BFF.Mux.Handle("/", spaHandler(a.Config.WebDir))

	srv := &http.Server{
		Addr:    a.Config.ListenAddr,
		Handler: a.BFF.Mux,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("listening on %s (%d plugins loaded)", a.Config.ListenAddr, len(a.plugins))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	log.Printf("shutting down...")
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutCtx)
}

func spaHandler(dir string) http.Handler {
	fs := http.Dir(dir)
	fileServer := http.FileServer(fs)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f, err := fs.Open(r.URL.Path); err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, dir+"/index.html")
	})
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required env var %s is not set", key)
	}
	return v
}
