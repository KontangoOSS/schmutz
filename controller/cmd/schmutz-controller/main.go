package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	agentmod "git.konoss.org/kore/schmutz/controller/internal/controller/agent"
	"git.konoss.org/kore/schmutz/controller/internal/controller/common"
	enrollmod "git.konoss.org/kore/schmutz/controller/internal/controller/enroll"
	"git.konoss.org/kore/schmutz/controller/internal/controller/profiles"
	"git.konoss.org/kore/schmutz/controller/internal/controller/verify"
	"git.konoss.org/kore/schmutz/controller/internal/service"
)

func main() {
	// --- Config from environment ---
	cfg := &Config{
		ListenAddr:      envOr("LISTEN_ADDR", "127.0.0.1:3080"),
		NodeName:        envOr("NODE_NAME", "ctrl-1"),
		ZitiBin:         envOr("ZITI_BIN", "/opt/kontango/bin/ziti"),
		CABundlePath:    envOr("CA_BUNDLE_PATH", "/opt/kontango/pki/ca-bundle.pem"),
		GitHubRelease:   envOr("GITHUB_RELEASE", "https://git.konoss.org/kore/schmutz/releases/latest/download"),
		GitHubRaw:       envOr("GITHUB_RAW", "https://git.konoss.org/kore/schmutz/raw/branch/main"),
		ZitiVersion:     os.Getenv("ZITI_VERSION"),  // if empty, "latest" is used
		ZitiIdentity:    os.Getenv("ZITI_IDENTITY"), // optional — enables dark management API
		ZitiServiceName: envOr("ZITI_SERVICE_NAME", "schmutz-mgmt"),
		WebDir:          envOr("WEB_DIR", "frontend"),
		ProfilesDir:     envOr("PROFILES_DIR", "./profiles"),
		JoinDomain:      envOr("JOIN_DOMAIN", "join.kontango.net"),
	}

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// --- Initialize services ---
	// Bao is optional — enrollment works without it (writes are fire-and-forget).
	// When BAO_ADDR + BAO_TOKEN are set, machine records and audit logs are persisted.
	var store *service.StoreService
	var identitySvc *service.IdentityService
	baoAddr := os.Getenv("BAO_ADDR")
	baoToken := os.Getenv("BAO_TOKEN")
	if baoAddr != "" && baoToken != "" {
		s, err := service.NewStoreService(baoAddr, baoToken)
		if err != nil {
			log.Printf("bao: store service unavailable: %v (continuing without Bao)", err)
		} else {
			store = s
		}
		i, err := service.NewIdentityService(baoAddr, baoToken)
		if err != nil {
			log.Printf("bao: identity service unavailable: %v (continuing without Bao)", err)
		} else {
			identitySvc = i
		}
		log.Printf("bao: connected to %s", baoAddr)
	} else {
		log.Printf("bao: BAO_ADDR/BAO_TOKEN not set — running without Bao (enrollment still works)")
	}

	zitiPass := os.Getenv("ZITI_ADMIN_PASS")
	zitiRefresh := os.Getenv("ZITI_REFRESH_TOKEN")
	if zitiPass == "" && zitiRefresh == "" {
		log.Fatalf("ZITI_ADMIN_PASS or ZITI_REFRESH_TOKEN required")
	}
	ziti := &service.ZitiService{
		Addr:         envOr("ZITI_CTRL_ADDR", "localhost:1280"),
		OIDCAddr:     os.Getenv("ZITI_OIDC_ADDR"), // public ctrl for token exchange, e.g. ctrl-1.konoss.org:443
		User:         envOr("ZITI_ADMIN_USER", "admin"),
		Pass:         zitiPass,
		RefreshToken: zitiRefresh,
	}

	// If a Ziti identity is configured, route management API calls through the overlay.
	// This means port 1280 never needs to be publicly accessible — all management
	// traffic flows dark through the enrolled identity dialing ziti-mgmt.tango.
	if cfg.ZitiIdentity != "" {
		if zt, err := service.NewZitiTransport(cfg.ZitiIdentity); err != nil {
			log.Printf("ziti transport: %v (management API will use direct TCP)", err)
		} else {
			ziti.Transport = zt
			ziti.Addr = envOr("ZITI_CTRL_ADDR", "ziti-mgmt.tango:443")
			log.Printf("ziti: management API routed through overlay (dark)")
		}
	}

	enroll := &service.EnrollmentService{
		ZitiBin:      cfg.ZitiBin,
		CABundlePath: cfg.CABundlePath,
		PublicZtAPI:  os.Getenv("PUBLIC_ZT_API"),
		PublicZtAPIs: os.Getenv("PUBLIC_ZT_APIS"),
	}

	// --- Load node info ---
	nodeInfo := loadNodeInfo(store, cfg.NodeName)
	log.Printf("schmutz-controller %s (%s) on %s", cfg.NodeName, nodeInfo["region"], cfg.ListenAddr)

	// --- Build API ---
	discoverySvc := service.NewDiscoveryService(store)
	securitySvc := service.NewSecurityService(store)
	metricsSvc := service.NewMetricsService(ziti, store)
	aclSvc := service.NewACLService(ziti, store)
	telSvc := service.NewTelemetryService()
	ottStore := service.NewOTTStore()
	// Clean up pre-created Ziti identities whose OTTs expired without being consumed.
	// This prevents abandoned enrollments (machine downloaded but never connected) from
	// accumulating as dead blue-* identities in Ziti.
	ottStore.OnExpire = func(zitiID string) {
		authTok, err := ziti.Authenticate()
		if err != nil {
			log.Printf("ott cleanup: ziti auth: %v", err)
			return
		}
		if err := ziti.DeleteIdentity(authTok, zitiID); err != nil {
			log.Printf("ott cleanup: delete identity %s: %v", zitiID, err)
			return
		}
		log.Printf("ott cleanup: deleted expired identity %s", zitiID)
	}

	var hpStore honeypotStore = store
	if store == nil || os.Getenv("TEST_MODE") != "" || os.Getenv("HONEYPOT_INMEM") != "" {
		hpStore = service.NewTestStore()
		log.Printf("honeypot: using in-memory store (Bao unavailable, TEST_MODE, or HONEYPOT_INMEM)")
	}

	api := &API{
		cfg:       cfg,
		store:     store,
		hpStore:   hpStore,
		ziti:      ziti,
		identity:  identitySvc,
		enroll:    enroll,
		security:  securitySvc,
		discovery: discoverySvc,
		metrics:   metricsSvc,
		acl:       aclSvc,
		telemetry: telSvc,
		node:      nodeInfo,
		ottStore:  ottStore,
	}

	// --- Load enrollment profiles ---
	profileRegistry, _ := profiles.LoadProfiles(cfg.ProfilesDir)

	// --- Build enrollment module (v2 WebSocket) ---
	clients := &common.Clients{Bao: store, Ziti: ziti, Identity: identitySvc}
	verifyMod := verify.New(clients)
	enrollMod := enrollmod.New(clients, verifyMod, enroll, cfg.NodeName)
	enrollMod.GitHubRelease = cfg.GitHubRelease
	enrollMod.ZitiVersion = cfg.ZitiVersion
	enrollMod.Profiles = profileRegistry
	api.enrollMod = enrollMod // wire after construction — enrollMod depends on clients built above

	// Debug mux — all routes, localhost-only, no Ziti required.
	mux := muxAll(api, ziti)

	// --- Start listeners ---

	// Ziti listeners — one per logical service, each on its own local port.
	if cfg.ZitiIdentity != "" {
		zt, err := service.NewZitiTransport(cfg.ZitiIdentity)
		if err != nil {
			log.Printf("ziti transport: %v (overlay listeners disabled)", err)
		} else {
			// Per-service management API listeners.
			// Each binds a Ziti service and a dedicated localhost port.
			// Dial policies are managed in Ziti (not enforced here).
			type svcDef struct {
				name string
				addr string
				mux  *http.ServeMux
			}
			svcs := []svcDef{
				{SvcZitiAdmin, PortZiti, func() *http.ServeMux { m := http.NewServeMux(); registerZiti(m, api, ziti); return m }()},
				{SvcBaoAdmin, PortBao, func() *http.ServeMux { m := http.NewServeMux(); registerBao(m, api); return m }()},
				{SvcIdentityAdmin, PortIdentity, func() *http.ServeMux { m := http.NewServeMux(); registerIdentity(m, api); return m }()},
				{SvcMachineAPI, PortMachine, func() *http.ServeMux { m := http.NewServeMux(); registerMachine(m, api); return m }()},
				{SvcMetrics, PortMetrics, func() *http.ServeMux { m := http.NewServeMux(); registerMetrics(m, api); return m }()},
				{SvcCatalogAPI, PortCatalog, func() *http.ServeMux { m := http.NewServeMux(); registerCatalog(m, api); return m }()},
				{SvcSecurityAPI, PortSecurity, func() *http.ServeMux { m := http.NewServeMux(); registerSecurity(m, api); return m }()},
			}
			for _, s := range svcs {
				s := s // capture
				// Local port listener — also serves direct localhost callers.
				go func() {
					if err := http.ListenAndServe(s.addr, s.mux); err != nil {
						log.Printf("%s local listener %s: %v", s.name, s.addr, err)
					}
				}()
				// Ziti overlay listener — same mux, overlay-encrypted transport.
				go func() {
					ln, err := zt.Listen(s.name)
					if err != nil {
						log.Printf("ziti listen %q: %v", s.name, err)
						return
					}
					log.Printf("controller: %s listening on overlay + %s", s.name, s.addr)
					if err := http.Serve(ln, s.mux); err != nil {
						log.Printf("ziti serve %s: %v", s.name, err)
					}
				}()
			}
			// Ensure telemetry.tango and relay.tango Ziti services exist.
			if authTok, err := ziti.Authenticate(); err == nil && authTok != "" {
				enrollmod.EnsureTelemetryService(clients, authTok)
				enrollmod.EnsureRelayService(clients, authTok)
			} else {
				log.Printf("ziti service bootstrap: auth failed: %v", err)
			}

			// TCP telemetry — agents dial telemetry.tango and stream typed frames.
			if telLn, err := zt.ListenEdge("telemetry.tango"); err != nil {
				log.Printf("tcptelemetry: listener failed: %v", err)
			} else {
				tcpTel := service.NewTCPTelemetryService(telLn, telSvc)
				tcpTel.Start()
				defer tcpTel.Stop()
				api.tcpTel = tcpTel
				startSelfMonitor(tcpTel, store, ziti, cfg.NodeName)

				// relay.tango — fans tagged frames to dashboard consumers
				var relaySvc *service.RelayService
				if relayLn, err := zt.ListenEdge("relay.tango"); err != nil {
					log.Printf("relay: listener failed: %v", err)
				} else {
					relaySvc = service.NewRelayService(relayLn)
					relaySvc.Start()
					defer relaySvc.Stop()
					tcpTel.SetRelay(relaySvc)
				}

			}

			// Config channel — pushes profile/config on connect, instructions on demand
			agentmod.StartConfigListener(zt, telSvc, store)
		}
	}

	// Localhost listener — Caddy proxies public traffic here.
	// All endpoints accessible — Ziti identity is the auth layer, not HTTP routes.
	log.Printf("enrollment API on %s", cfg.ListenAddr)

	// Check if TLS is enabled (test mode or direct HTTPS)
	tlsCert := os.Getenv("TLS_CERT")
	tlsKey := os.Getenv("TLS_KEY")

	if tlsCert != "" && tlsKey != "" {
		// Direct HTTPS mode (testing or standalone)
		log.Printf("TLS enabled: %s", tlsCert)
		if err := http.ListenAndServeTLS(cfg.ListenAddr, tlsCert, tlsKey, mux); err != nil {
			log.Fatalf("server: %v", err)
		}
	} else {
		// Plain HTTP mode (Caddy proxies with TLS)
		if err := http.ListenAndServe(cfg.ListenAddr, mux); err != nil {
			log.Fatalf("server: %v", err)
		}
	}
}

// --- Config ---

type Config struct {
	ListenAddr      string
	NodeName        string
	ZitiBin         string
	CABundlePath    string
	GitHubRelease   string // e.g. https://git.konoss.org/kore/schmutz/releases/latest/download
	GitHubRaw       string // e.g. https://raw.githubusercontent.com/KontangoOSS/schmutz/main
	ZitiVersion     string // ziti binary version to install on agents; empty = "latest"
	ZitiIdentity    string // path to Ziti identity JSON for hosting the management API as a dark service
	ZitiServiceName string // Ziti service name for the management API (e.g., "schmutz-mgmt")
	WebDir          string // directory containing static web pages (landing, guide)
	ProfilesDir     string // directory containing enrollment profile YAML files
	JoinDomain      string // public hostname for claim URLs, e.g. join.kontango.net
}

// --- API handlers ---

type API struct {
	cfg          *Config
	store        *service.StoreService
	hpStore      honeypotStore
	ziti         *service.ZitiService
	identity     *service.IdentityService
	enroll       *service.EnrollmentService
	security     *service.SecurityService
	discovery    *service.DiscoveryService
	metrics      *service.MetricsService
	acl          *service.ACLService
	telemetry *service.TelemetryService
	tcpTel    *service.TCPTelemetryService
	node      map[string]string
	ottStore  *service.OTTStore
	enrollMod *enrollmod.Module
}

func (a *API) LandingPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, a.cfg.WebDir+"/join-index.html")
}


func (a *API) GuidePage(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, a.cfg.WebDir+"/guide.html")
}

func (a *API) ServeCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	http.ServeFile(w, r, a.cfg.WebDir+r.URL.Path)
}

func (a *API) ServeJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	http.ServeFile(w, r, a.cfg.WebDir+r.URL.Path)
}

func (a *API) Health(w http.ResponseWriter, r *http.Request) {
	baoOK := false
	zitiOK := false

	// Bao: check seal status — works in all namespaces and confirms Bao is reachable and unsealed.
	if sealStatus, err := a.store.SealStatus(); err == nil && sealStatus != nil {
		if sealed, ok := sealStatus["sealed"].(bool); ok && !sealed {
			baoOK = true
		}
	}

	// Ziti: authenticate to confirm management API is reachable
	if tok, err := a.ziti.Authenticate(); err == nil && tok != "" {
		zitiOK = true
	}

	status := "ok"
	code := http.StatusOK
	if !baoOK || !zitiOK {
		status = "degraded"
		code = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": status,
		"bao":    baoOK,
		"ziti":   zitiOK,
	})
}

func (a *API) HealthDevice(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	id, _ := payload["id"].(string)
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}

	// Validate enrollment tag (prevents bots from sending fake health data)
	tag, _ := payload["tag"].(string)
	if tag == "" {
		log.Printf("healthcheck: missing tag from %s — likely bot", id)
		http.Error(w, "missing tag", http.StatusForbidden)
		return
	}

	// Verify tag matches what was issued during enrollment
	var storedTag string

	// Try Bao first (production mode)
	enrollmentData, _ := a.store.Get("secret", "devices/"+id+"/enrollment-tag")
	if enrollmentData != nil {
		storedTag, _ = enrollmentData["tag"].(string)
	}

	// Fall back to in-memory tag store (used when Bao is unavailable)
	if storedTag == "" {
		enrollmod.EnrollTagMu.Lock()
		storedTag = enrollmod.EnrollTagStore[id]
		enrollmod.EnrollTagMu.Unlock()
	}

	if storedTag == "" {
		log.Printf("healthcheck: no stored tag for %s", id)
		http.Error(w, "invalid tag", http.StatusForbidden)
		return
	}

	if storedTag != tag {
		log.Printf("healthcheck: tag mismatch for %s — bot attempt detected", id)
		http.Error(w, "invalid tag", http.StatusForbidden)
		return
	}

	// Store device heartbeat in Bao
	heartbeat := map[string]interface{}{
		"timestamp": time.Now().Unix(),
		"nickname":  payload["nickname"],
		"tunnel":    payload["tunnel"],
		"agent":     payload["agent"],
		"tag":       tag, // Include tag for audit trail
	}
	a.store.Put("secret", "devices/"+id+"/heartbeat", heartbeat)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (a *API) Whoami(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"node":   a.node["node"],
		"region": a.node["region"],
	})
}

func (a *API) Controllers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	info := loadControllerInfo(a.store, a.cfg.NodeName)
	if info != nil {
		json.NewEncoder(w).Encode([]map[string]interface{}{info})
	} else {
		json.NewEncoder(w).Encode([]map[string]interface{}{})
	}
}

func (a *API) OSCatalog(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(loadOSCatalog(a.store))
}

func (a *API) Download(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/download/")
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Allowed binaries — reject anything else
	allowed := map[string]bool{
		"schmutz-join-linux-amd64": true, "schmutz-join-linux-amd64.sha256": true,
		"schmutz-join-linux-arm64": true, "schmutz-join-linux-arm64.sha256": true,
		"schmutz-join-linux-arm": true, "schmutz-join-linux-arm.sha256": true,
		"schmutz-join-darwin-amd64": true, "schmutz-join-darwin-amd64.sha256": true,
		"schmutz-join-darwin-arm64": true, "schmutz-join-darwin-arm64.sha256": true,
		"schmutz-join-windows-amd64.exe": true, "schmutz-join-windows-amd64.exe.sha256": true,
		"schmutz-linux-amd64": true, "schmutz-linux-amd64.sha256": true,
		"schmutz-linux-arm64": true, "schmutz-linux-arm64.sha256": true,
		"schmutz-linux-arm": true, "schmutz-linux-arm.sha256": true,
		"schmutz-darwin-amd64": true, "schmutz-darwin-amd64.sha256": true,
		"schmutz-darwin-arm64": true, "schmutz-darwin-arm64.sha256": true,
		"schmutz-windows-amd64.exe": true, "schmutz-windows-amd64.exe.sha256": true,
		"caddy-linux-amd64": true, "caddy-linux-amd64.sha256": true,
		"caddy-linux-arm64": true, "caddy-linux-arm64.sha256": true,
		"caddy-linux-arm": true, "caddy-linux-arm.sha256": true,
		"caddy-darwin-amd64": true, "caddy-darwin-amd64.sha256": true,
		"caddy-darwin-arm64": true, "caddy-darwin-arm64.sha256": true,
		"caddy-windows-amd64.exe": true, "caddy-windows-amd64.exe.sha256": true,
	}
	if !allowed[name] {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Capture appetizer at download time and issue a short-lived OTT.
	// The OTT travels with the binary (embedded in .env on first run).
	// When the agent connects via walkin.tango it presents the OTT —
	// we compare the download appetizer against the Ziti connection.
	app := captureAppetizer(r)
	// Download-path OTTs don't pre-create a Ziti identity — empty zitiJWT/zitiID.
	// The agent will call /api/v1/start to get a JWT before connecting via /api/v1/ws.
	ott := a.ottStore.Issue(app.JA4, app.RealIP, app.TLSVersion, r.Header.Get("User-Agent"), "", "")
	log.Printf("download: issued OTT for %s ja4=%.16s binary=%s", app.RealIP, app.JA4, name)

	// Serve local file if it exists, otherwise redirect to GitHub
	localPath := "/opt/kontango/join/bin/" + name
	if _, err := os.Stat(localPath); err == nil {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Enrollment-Token", ott)
		http.ServeFile(w, r, localPath)
		return
	}

	// For GitHub redirects, return OTT in header before redirecting
	w.Header().Set("X-Enrollment-Token", ott)
	http.Redirect(w, r, a.cfg.GitHubRelease+"/"+name, http.StatusFound)
}

// --- Helpers ---

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("%s required", key)
	}
	return v
}

func loadNodeInfo(store *service.StoreService, nodeName string) map[string]string {
	info := map[string]string{"node": nodeName}
	data, err := store.Get("secret", "infra/controllers/"+nodeName)
	if err == nil && data != nil {
		for _, k := range []string{"region", "ip", "domain"} {
			if v, ok := data[k].(string); ok {
				info[k] = v
			}
		}
	}
	return info
}

func loadControllerInfo(store *service.StoreService, nodeName string) map[string]interface{} {
	data, err := store.Get("secret", "infra/controllers/"+nodeName)
	if err != nil || data == nil {
		return nil
	}
	ip, _ := data["ip"].(string)
	domain, _ := data["domain"].(string)
	tangoAddr, _ := data["tango_addr"].(string)

	var hosts []string
	if tangoAddr != "" {
		hosts = append(hosts, tangoAddr)
	}
	if domain != "" {
		hosts = append(hosts, domain)
	}

	return map[string]interface{}{
		"ip":    ip,
		"hosts": hosts,
	}
}
