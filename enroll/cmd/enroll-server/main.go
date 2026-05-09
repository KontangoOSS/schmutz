package main

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"time"

	"github.com/KontangoOSS/schmutz/enroll/internal/bao"
	"github.com/KontangoOSS/schmutz/enroll/internal/config"
	"github.com/KontangoOSS/schmutz/enroll/internal/forgejo"
	"github.com/KontangoOSS/schmutz/enroll/internal/handlers"
	"github.com/KontangoOSS/schmutz/enroll/internal/ziti"
)

const Version = "0.2.0"

func main() {
	c, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	log.Printf("enroll-server v%s starting on %s", Version, c.ListenAddr)

	baoClient := bao.NewHTTP(c.BaoAddr, c.BaoToken, c.BaoMount, c.BaoTokenPrefix, c.BaoSkipVerify)
	zitiClient := ziti.NewHTTP(c.ZitiAPI, c.ZitiUsername, c.ZitiPassword, true)

	mux := http.NewServeMux()

	// Legacy endpoints kept for backward compat while migration is in progress.
	mux.Handle("/api/enroll", handlers.NewEnrollHandler(baoClient, zitiClient, generateName))
	mux.Handle("/healthz", handlers.NewHealthHandler(baoClient, zitiClient))

	// /api/bao-bundle — re-issue bundle for an already-enrolled agent.
	if oa, ok := baoClient.(bao.OIDCAdmin); ok {
		mux.Handle("/api/bao-bundle", handlers.NewBaoBundleHandler(
			oa, baoClient, c.AgentBaoAddr, handlers.DefaultBaoBundleConfig(),
		))
	} else {
		log.Printf("warning: bao client does not implement OIDCAdmin; /api/bao-bundle disabled")
	}

	// Hub endpoints — the new enrollment API.
	// Only mounted if FORGEJO_URL + FORGEJO_TOKEN are configured; servers
	// without Forgejo access run in "legacy-only" mode.
	if c.ForgejoURL != "" && c.ForgejoToken != "" {
		forgejoClient := forgejo.NewCachedClient(
			forgejo.NewClient(c.ForgejoURL, c.ForgejoOrg, c.ForgejoToken, 0),
			60*time.Second,
		)
		enrollStore := bao.NewEnrollmentStore(baoClient)

		var oa bao.OIDCAdmin
		var ok bool
		if oa, ok = baoClient.(bao.OIDCAdmin); !ok {
			log.Fatalf("bao client does not implement OIDCAdmin; cannot start hub")
		}
		nsKV := bao.NewNamespacedKV(c.BaoAddr, c.BaoToken, c.BaoSkipVerify)

		hubHandler := handlers.NewHubHandler(handlers.HubConfig{
			ForgejoClient: forgejoClient,
			EnrollStore:   enrollStore,
			BaoAdmin:      oa,
			BaoKV:         baoClient,
			BaoNS:         nsKV,
			ZitiClient:    zitiClient,
			AdminToken:    c.HubAdminToken,
			BaoAddr:       c.AgentBaoAddr,
		})
		// Route hub paths to the hub handler.
		mux.Handle("/api/v1/", hubHandler)
		log.Printf("hub: mounted /api/v1/* (forgejo=%s org=%s)", c.ForgejoURL, c.ForgejoOrg)
	} else {
		log.Printf("hub: disabled (set FORGEJO_URL + FORGEJO_TOKEN to enable)")
	}

	if err := http.ListenAndServe(c.ListenAddr, mux); err != nil {
		log.Fatalf("listen: %v", err)
	}
}

func generateName() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return "machine-" + hex.EncodeToString(b)
}
