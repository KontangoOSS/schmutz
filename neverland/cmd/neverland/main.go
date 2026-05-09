package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5/pgxpool"

	"git.konoss.org/kore/schmutz/neverland/internal/beacon"
	"git.konoss.org/kore/schmutz/neverland/internal/config"
	"git.konoss.org/kore/schmutz/neverland/internal/docs"
	"git.konoss.org/kore/schmutz/neverland/internal/handlers"
	"git.konoss.org/kore/schmutz/neverland/internal/k8s"
	"git.konoss.org/kore/schmutz/neverland/internal/menuconfig"
	"git.konoss.org/kore/schmutz/neverland/internal/middleware"
)

func main() {
	log.SetFlags(log.Ltime | log.Lshortfile)
	log.Println("neverland starting")

	cfg := config.Load()

	kc, err := k8s.NewClient(cfg.Kubeconfig)
	if err != nil {
		log.Fatalf("k8s client: %v", err)
	}

	// Beacon store — Postgres if configured, otherwise log-only.
	var beaconStore handlers.BeaconStore = nopBeaconStore{}
	if cfg.PostgresDSN != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		pp, err := pgxpool.New(ctx, cfg.PostgresDSN)
		cancel()
		if err != nil {
			log.Fatalf("postgres connect: %v", err)
		}
		beaconStore = beacon.NewStore(pp)
		log.Println("beacon store: postgres")
	} else {
		log.Println("beacon store: nop (POSTGRES_DSN not set)")
	}

	hw := handlers.NewHardwareHandler(kc, cfg.TinkNamespace)
	tpl := handlers.NewTemplateHandler(kc, cfg.TinkNamespace)
	wf := handlers.NewWorkflowHandler(kc, cfg.TinkNamespace)
	bmc := handlers.NewBMCHandler(kc, cfg.TinkNamespace)
	art := handlers.NewArtifactHandler(cfg.ArtifactsPath, cfg.NginxURL)
	fetch := handlers.NewFetchHandler(cfg.ArtifactsPath, cfg.NginxURL)
	boot := handlers.NewBootHandler(kc, cfg.TinkNamespace, cfg.SmeeDeployment)
	hook := handlers.NewHookHandler(cfg.NginxURL, cfg.EnrollURL)
	status := handlers.NewStatusHandler(kc, cfg.TinkNamespace)
	gitFetcher := menuconfig.NewGitFetcher(menuconfig.GitFetcherConfig{
		APIBase: cfg.BootConfigAPIBase,
		Owner:   cfg.BootConfigOwner,
		Repo:    cfg.BootConfigRepo,
		Ref:     cfg.BootConfigRef,
		Token:   cfg.BootConfigToken,
	})
	bootConfigCache := menuconfig.NewMenuConfigCache(gitFetcher, cfg.BootConfigCacheTTL)
	menuCfg := handlers.NewMenuConfigHandler(bootConfigCache)
	menu := handlers.NewMenuHandler(cfg.BootBaseURL, bootConfigCache)
	bcn := handlers.NewBeaconHandler(kc, beaconStore, handlers.BeaconConfig{
		BootBaseURL: cfg.BootBaseURL,
		Namespace:   cfg.TinkNamespace,
	})
	dl := handlers.NewDownloadHandler(cfg.DownloadsPath)

	r := mux.NewRouter()
	r.HandleFunc("/health", handlers.Health).Methods("GET")

	api := r.PathPrefix("/api/v1").Subrouter()
	api.HandleFunc("/status", status.Status).Methods("GET")

	// Hardware/Templates/Workflows/BMC routes — unchanged from previous build.
	api.HandleFunc("/hardware", hw.List).Methods("GET")
	api.HandleFunc("/hardware", hw.Create).Methods("POST")
	api.HandleFunc("/hardware/{name}", hw.Get).Methods("GET")
	api.HandleFunc("/hardware/{name}", hw.Patch).Methods("PATCH")
	api.HandleFunc("/hardware/{name}", hw.Delete).Methods("DELETE")
	api.HandleFunc("/hardware/{name}/netboot", hw.Netboot).Methods("POST")
	api.HandleFunc("/hardware/{name}/watch", hw.Watch).Methods("GET")
	api.HandleFunc("/templates", tpl.List).Methods("GET")
	api.HandleFunc("/templates", tpl.Create).Methods("POST")
	api.HandleFunc("/templates/{name}", tpl.Get).Methods("GET")
	api.HandleFunc("/templates/{name}", tpl.Patch).Methods("PATCH")
	api.HandleFunc("/templates/{name}", tpl.Delete).Methods("DELETE")
	api.HandleFunc("/workflows", wf.List).Methods("GET")
	api.HandleFunc("/workflows", wf.Create).Methods("POST")
	api.HandleFunc("/workflows/{name}", wf.Get).Methods("GET")
	api.HandleFunc("/workflows/{name}", wf.Patch).Methods("PATCH")
	api.HandleFunc("/workflows/{name}", wf.Delete).Methods("DELETE")
	api.HandleFunc("/workflows/{name}/reprovision", wf.Reprovision).Methods("POST")
	api.HandleFunc("/workflows/{name}/watch", wf.Watch).Methods("GET")
	api.HandleFunc("/workflows/{name}/tasks", wf.Tasks).Methods("GET")
	api.HandleFunc("/bmc/machines", bmc.ListMachines).Methods("GET")
	api.HandleFunc("/bmc/machines", bmc.CreateMachine).Methods("POST")
	api.HandleFunc("/bmc/machines/{name}", bmc.GetMachine).Methods("GET")
	api.HandleFunc("/bmc/machines/{name}", bmc.PatchMachine).Methods("PATCH")
	api.HandleFunc("/bmc/machines/{name}", bmc.DeleteMachine).Methods("DELETE")
	api.HandleFunc("/bmc/jobs", bmc.ListJobs).Methods("GET")
	api.HandleFunc("/bmc/jobs", bmc.CreateJob).Methods("POST")
	api.HandleFunc("/bmc/jobs/{name}", bmc.GetJob).Methods("GET")
	api.HandleFunc("/bmc/jobs/{name}", bmc.PatchJob).Methods("PATCH")
	api.HandleFunc("/bmc/jobs/{name}", bmc.DeleteJob).Methods("DELETE")
	api.HandleFunc("/bmc/tasks", bmc.ListTasks).Methods("GET")
	api.HandleFunc("/bmc/tasks", bmc.CreateTask).Methods("POST")
	api.HandleFunc("/bmc/tasks/{name}", bmc.GetTask).Methods("GET")
	api.HandleFunc("/bmc/tasks/{name}", bmc.PatchTask).Methods("PATCH")
	api.HandleFunc("/bmc/tasks/{name}", bmc.DeleteTask).Methods("DELETE")
	api.HandleFunc("/artifacts", art.List).Methods("GET")
	api.HandleFunc("/artifacts", art.Upload).Methods("POST")
	api.HandleFunc("/artifacts/fetch", fetch.List).Methods("GET")
	api.HandleFunc("/artifacts/fetch", fetch.Start).Methods("POST")
	api.HandleFunc("/artifacts/fetch/{id}", fetch.Get).Methods("GET")
	api.HandleFunc("/artifacts/{filename}", art.Download).Methods("GET")
	api.HandleFunc("/artifacts/{filename}", art.Delete).Methods("DELETE")
	api.HandleFunc("/boot", boot.Get).Methods("GET")
	api.HandleFunc("/boot", boot.Patch).Methods("PATCH")
	api.HandleFunc("/boot/ipxe/{mac}", boot.IPXEScript).Methods("GET")
	api.HandleFunc("/hook", hook.List).Methods("GET")
	api.HandleFunc("/hook/{variant}/kernel", hook.Kernel).Methods("GET")
	api.HandleFunc("/hook/{variant}/initrd", hook.Initrd).Methods("GET")
	api.HandleFunc("/hook/{variant}/hook.ipxe", hook.IPXEScript).Methods("GET")

	// New in Phase 1
	rl := middleware.RateLimitPerIP(60, time.Minute)
	api.Handle("/menu.ipxe", rl(http.HandlerFunc(menu.Handle))).Methods("GET")
	api.HandleFunc("/menu/theme", menuCfg.GetTheme).Methods("GET")
	api.HandleFunc("/menu/entries", menuCfg.GetEntries).Methods("GET")
	api.Handle("/beacon", rl(http.HandlerFunc(bcn.Handle))).Methods("GET", "POST")
	api.HandleFunc("/downloads", dl.List).Methods("GET")
	api.HandleFunc("/downloads/{filename}", dl.Get).Methods("GET")

	// Docs catch-all must be last
	r.PathPrefix("/").Handler(docs.Handler())

	handler := middleware.Logging(middleware.CORS(r))
	log.Printf("listening on %s", cfg.ListenAddr)
	if err := http.ListenAndServe(cfg.ListenAddr, handler); err != nil {
		log.Fatal(err)
	}
}

// nopBeaconStore is used when POSTGRES_DSN is not set. Beacons still produce
// Hardware CRs (the source of truth), but no forensic log is kept.
type nopBeaconStore struct{}

func (nopBeaconStore) Insert(_ context.Context, _ beacon.Row) error { return nil }
