// server/cmd/schmutz-server/main.go

// Command schmutz-server is the administrative control plane for the Ziti
// overlay. It runs in two modes: normal (Ziti SDK listener + full handler
// set, gated by RBAC middleware) or bootstrap (plain HTTP on localhost,
// limited to bootstrap operations like bao init and break-glass creation).
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/KontangoOSS/schmutz/enroll/internal/admin"
	"github.com/KontangoOSS/schmutz/enroll/internal/audit"
	"github.com/KontangoOSS/schmutz/enroll/internal/bao"
	"github.com/KontangoOSS/schmutz/enroll/internal/config"
	"github.com/KontangoOSS/schmutz/enroll/internal/ziti"
)

const Version = "0.1.0"

func main() {
	c, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	log.Printf("schmutz-server v%s starting (bootstrap=%v) on %s",
		Version, c.SchmutzBootstrap, c.SchmutzListenAddr)

	if c.SchmutzBootstrap {
		runBootstrap(c)
		return
	}
	runNormal(c)
}

func runNormal(c *config.Config) {
	baoClient := bao.NewHTTP(c.BaoAddr, c.BaoToken, c.BaoMount, c.BaoTokenPrefix, c.BaoSkipVerify)
	zitiClient := ziti.NewHTTP(c.ZitiAPI, c.ZitiUsername, c.ZitiPassword, true)
	auditLog := audit.NewBaoLogger(baoClient)
	queryEngine := audit.NewQuerier(baoClient)

	identitiesH := admin.NewIdentitiesHandler(zitiClient, auditLog)
	tokensH := admin.NewTokensHandler(baoClient, auditLog)
	auditH := admin.NewAuditHandlerFromQuerier(queryEngine)
	metaH := admin.NewMetaHandler(baoClient, zitiClient)

	mux := http.NewServeMux()
	// Identities
	mux.Handle("GET /api/identities", admin.RequireAdmin(identitiesH.List()))
	mux.Handle("POST /api/identities", admin.RequireAdmin(identitiesH.Create()))
	mux.Handle("GET /api/identities/{name}", admin.RequireAdmin(identitiesH.Get()))
	mux.Handle("PATCH /api/identities/{name}", admin.RequireAdmin(identitiesH.Patch()))
	mux.Handle("DELETE /api/identities/{name}", admin.RequireAdmin(identitiesH.Delete()))
	mux.Handle("POST /api/identities/{name}/approve", admin.RequireAdmin(identitiesH.Approve()))
	mux.Handle("POST /api/identities/{name}/deny", admin.RequireAdmin(identitiesH.Deny()))
	// Tokens
	mux.Handle("POST /api/tokens", admin.RequireAdmin(tokensH.Issue()))
	mux.Handle("GET /api/tokens", admin.RequireAdmin(tokensH.List()))
	mux.Handle("GET /api/tokens/{token}", admin.RequireAdmin(tokensH.Get()))
	mux.Handle("DELETE /api/tokens/{token}", admin.RequireAdmin(tokensH.Revoke()))
	// Audit + Meta
	mux.Handle("GET /api/audit", admin.RequireAdmin(auditH.Query()))
	mux.Handle("GET /healthz", metaH.Healthz())
	mux.Handle("GET /api/whoami", admin.RequireAdmin(metaH.Whoami()))

	listener, err := ziti.NewSDKListener(c.SchmutzZitiIdentityFile, c.SchmutzServiceName)
	if err != nil {
		log.Fatalf("ziti listener: %v", err)
	}
	defer listener.Close()

	srv := &http.Server{
		Handler:     mux,
		ConnContext: ziti.ConnContextWithResolver(zitiClient),
		ReadTimeout: 30 * time.Second,
	}

	// Graceful shutdown.
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		log.Println("shutdown requested")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	log.Printf("schmutz-server listening on Ziti service %q", c.SchmutzServiceName)
	if err := srv.Serve(listener.Listener()); err != nil && err != http.ErrServerClosed {
		log.Fatalf("serve: %v", err)
	}
}

func runBootstrap(c *config.Config) {
	// Bootstrap mode: no Ziti, no Bao client. Everything is subprocesses
	// + direct break-glass creation against a freshly initialized Ziti.
	auditStub := bootstrapAuditStub{}
	exec := admin.NewOSExec()

	// Break-glass creator needs Ziti + AdminKV. Both come up later in the
	// bootstrap flow, so the creator is constructed lazily — read identity
	// + bao addr from env at the moment create-break-glass is called.
	bg := &lazyBreakGlass{
		identityFile: c.SchmutzZitiIdentityFile,
		baoPath:      "schmutz/break-glass/identity",
	}

	bootH := admin.NewBootstrapHandler(exec, bg, auditStub)

	mux := http.NewServeMux()
	mux.Handle("POST /api/bootstrap/bao/init", bootH.BaoInit())
	mux.Handle("POST /api/bootstrap/bao/distribute-keys", bootH.DistributeKeys())
	mux.Handle("POST /api/bootstrap/bao/join-peer", bootH.JoinPeer())
	mux.Handle("POST /api/bootstrap/bao/apply-enroll-policy", bootH.ApplyEnrollPolicy())
	mux.Handle("POST /api/bootstrap/create-break-glass", bootH.CreateBreakGlass())
	mux.Handle("GET /healthz", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","mode":"bootstrap"}`))
	}))

	srv := &http.Server{
		Addr:        c.SchmutzListenAddr,
		Handler:     mux,
		ReadTimeout: 30 * time.Second,
	}
	log.Printf("schmutz-server BOOTSTRAP MODE listening on %s", c.SchmutzListenAddr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("serve: %v", err)
	}
}

// bootstrapAuditStub writes audit events to stderr. Bao isn't writable yet
// during early bootstrap; later steps (apply-enroll-policy, create-break-glass)
// run after Bao is up but the stub stays simple — journal logs are the
// audit trail for one-time setup.
type bootstrapAuditStub struct{}

func (bootstrapAuditStub) Record(ctx context.Context, ev audit.Event) error {
	log.Printf("AUDIT actor=%s action=%s resource=%s result=%s",
		ev.Actor, ev.Action, ev.Resource, ev.Result)
	return nil
}

// lazyBreakGlass defers wiring Ziti + Bao clients until create-break-glass
// is actually called. By that point Ziti and Bao must be up.
type lazyBreakGlass struct {
	identityFile string
	baoPath      string
}

func (l *lazyBreakGlass) Create(ctx context.Context) (admin.BreakGlassResult, error) {
	// Read env at call time so bootstrap operator can set them just before
	// the create-break-glass call without restarting the server.
	baoAddr := os.Getenv("BAO_ADDR")
	baoToken := os.Getenv("BAO_TOKEN")
	zitiAPI := os.Getenv("ZITI_API")
	zitiUser := os.Getenv("ZITI_USERNAME")
	zitiPass := os.Getenv("ZITI_PASSWORD")
	if baoAddr == "" || baoToken == "" || zitiAPI == "" || zitiUser == "" || zitiPass == "" {
		return admin.BreakGlassResult{}, errEnvMissing
	}
	z := ziti.NewHTTP(zitiAPI, zitiUser, zitiPass, true)
	b := bao.NewHTTP(baoAddr, baoToken, "secret", "enroll-tokens",
		os.Getenv("BAO_SKIP_VERIFY") == "1")
	creator := admin.NewBreakGlassCreator(z, b, l.identityFile, l.baoPath)
	return creator.Create(ctx)
}

var errEnvMissing = errors.New("BAO_ADDR/BAO_TOKEN/ZITI_API/ZITI_USERNAME/ZITI_PASSWORD must all be set before create-break-glass")
