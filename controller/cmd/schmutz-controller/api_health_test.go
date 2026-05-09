package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"git.konoss.org/kore/schmutz/controller/internal/service"
)

// TestMain sets VAULT_MAX_RETRIES=0 so that tests using an unreachable Bao address
// fail immediately (ECONNREFUSED) rather than waiting for vault's default 2 retries.
func TestMain(m *testing.M) {
	os.Setenv("VAULT_MAX_RETRIES", "0")
	os.Exit(m.Run())
}

// newUnreachableStore returns a StoreService pointing at an unreachable address.
// This lets tests exercise error paths without panicking on nil dereference.
// VAULT_MAX_RETRIES=0 is set in TestMain so the vault client doesn't retry,
// keeping tests fast (ECONNREFUSED is immediate but retries add seconds each).
func newUnreachableStore(t *testing.T) *service.StoreService {
	t.Helper()
	s, err := service.NewStoreService("http://127.0.0.1:19999", "fake-token")
	if err != nil {
		t.Fatalf("newUnreachableStore: %v", err)
	}
	return s
}

// newUnreachableZiti returns a ZitiService pointing at an unreachable address.
func newUnreachableZiti() *service.ZitiService {
	return &service.ZitiService{Addr: "127.0.0.1:19998", User: "admin", Pass: "x"}
}

// newTestAPIWithNode returns an API with a node map set for Whoami tests.
func newTestAPIWithNode(node map[string]string) *API {
	api := newTestAPI()
	api.node = node
	return api
}

// TestHealth_UnreachableServices verifies that Health returns degraded+503
// when both Bao and Ziti are unreachable (connection refused).
func TestHealth_UnreachableServices(t *testing.T) {
	api := newTestAPI()
	api.store = newUnreachableStore(t)
	api.ziti = newUnreachableZiti()

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rr := httptest.NewRecorder()
	api.Health(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rr.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "degraded" {
		t.Errorf("status=%q, want degraded", resp["status"])
	}
	if resp["bao"] != false {
		t.Errorf("bao=%v, want false", resp["bao"])
	}
	if resp["ziti"] != false {
		t.Errorf("ziti=%v, want false", resp["ziti"])
	}
}

// TestHealth_ResponseIsJSON verifies Content-Type is application/json.
func TestHealth_ResponseIsJSON(t *testing.T) {
	api := newTestAPI()
	api.store = newUnreachableStore(t)
	api.ziti = newUnreachableZiti()

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rr := httptest.NewRecorder()
	api.Health(rr, req)

	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type=%q, want application/json", ct)
	}
}

// TestHealthDevice_MissingID verifies 400 when id is absent.
func TestHealthDevice_MissingID(t *testing.T) {
	api := newTestAPI()
	// store is nil, which means Get returns nil/nil — but id check fires first
	body := `{"tag":"sometag"}`
	req := httptest.NewRequest(http.MethodPost, "/api/health/device", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	api.HealthDevice(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// TestHealthDevice_MissingTag verifies 403 when tag is absent (but id is present).
// store.Get returns nil when store is nil, so no stored tag — handler returns 403.
func TestHealthDevice_MissingTag(t *testing.T) {
	api := newTestAPI()
	body := `{"id":"machine-xyz"}`
	req := httptest.NewRequest(http.MethodPost, "/api/health/device", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	api.HealthDevice(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

// TestHealthDevice_WrongMethod verifies GET → 405.
func TestHealthDevice_WrongMethod(t *testing.T) {
	api := newTestAPI()
	req := httptest.NewRequest(http.MethodGet, "/api/health/device", nil)
	rr := httptest.NewRecorder()
	api.HealthDevice(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

// TestHealthDevice_InvalidJSON verifies 400 on bad JSON body.
func TestHealthDevice_InvalidJSON(t *testing.T) {
	api := newTestAPI()
	req := httptest.NewRequest(http.MethodPost, "/api/health/device", strings.NewReader("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	api.HealthDevice(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// TestWhoami_ReturnsNodeField verifies GET returns node and region from api.node.
func TestWhoami_ReturnsNodeField(t *testing.T) {
	api := newTestAPIWithNode(map[string]string{
		"node":   "ctrl-1",
		"region": "nyc3",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/whoami", nil)
	rr := httptest.NewRecorder()
	api.Whoami(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["node"] != "ctrl-1" {
		t.Errorf("node=%q, want ctrl-1", resp["node"])
	}
	if resp["region"] != "nyc3" {
		t.Errorf("region=%q, want nyc3", resp["region"])
	}
}

// TestWhoami_EmptyNodeMap verifies Whoami doesn't panic when api.node is nil.
func TestWhoami_EmptyNodeMap(t *testing.T) {
	api := newTestAPI()
	// api.node is nil

	req := httptest.NewRequest(http.MethodGet, "/api/whoami", nil)
	rr := httptest.NewRecorder()
	api.Whoami(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

// TestControllers_NilStore verifies Controllers returns empty array when store is nil.
func TestControllers_NilStore(t *testing.T) {
	api := newTestAPIWithNode(map[string]string{"node": "ctrl-1"})
	// store is nil, cfg is nil — need cfg for NodeName
	api.cfg = &Config{NodeName: "ctrl-1"}

	req := httptest.NewRequest(http.MethodGet, "/api/controllers", nil)
	rr := httptest.NewRecorder()
	api.Controllers(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp []interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp) != 0 {
		t.Errorf("expected empty array, got %d items", len(resp))
	}
}

// TestOSCatalog_GET verifies GET returns 200 with JSON.
func TestOSCatalog_GET(t *testing.T) {
	api := newTestAPI()
	// store is nil — loadOSCatalog handles nil gracefully

	req := httptest.NewRequest(http.MethodGet, "/api/os", nil)
	rr := httptest.NewRecorder()
	api.OSCatalog(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type=%q, want application/json", ct)
	}
	// Must decode without error
	var resp interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
}

// TestDownload_PathTraversal verifies ../../etc/passwd → 404.
func TestDownload_PathTraversal(t *testing.T) {
	api := newTestAPIWithNode(nil)
	api.cfg = &Config{GitHubRelease: "https://github.com/example/releases"}
	api.ottStore = service.NewOTTStore()

	req := httptest.NewRequest(http.MethodGet, "/download/../../etc/passwd", nil)
	req.URL.Path = "/download/../../etc/passwd"
	rr := httptest.NewRecorder()
	api.Download(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

// TestDownload_UnknownBinary verifies an unlisted binary name → 404.
func TestDownload_UnknownBinary(t *testing.T) {
	api := newTestAPIWithNode(nil)
	api.cfg = &Config{GitHubRelease: "https://github.com/example/releases"}
	api.ottStore = service.NewOTTStore()

	req := httptest.NewRequest(http.MethodGet, "/download/evil-binary", nil)
	rr := httptest.NewRecorder()
	api.Download(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

// TestDownload_ValidBinary verifies known binary → issues OTT and redirects (302).
func TestDownload_ValidBinary(t *testing.T) {
	api := newTestAPIWithNode(nil)
	api.cfg = &Config{GitHubRelease: "https://github.com/example/releases"}
	api.ottStore = service.NewOTTStore()

	req := httptest.NewRequest(http.MethodGet, "/download/schmutz-linux-amd64", nil)
	rr := httptest.NewRecorder()
	api.Download(rr, req)

	// No local file at /opt/kontango/join/bin/ in test, so expect redirect
	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rr.Code)
	}
	ott := rr.Header().Get("X-Enrollment-Token")
	if ott == "" {
		t.Error("X-Enrollment-Token missing on download redirect")
	}
	loc := rr.Header().Get("Location")
	if !strings.Contains(loc, "schmutz-linux-amd64") {
		t.Errorf("redirect location=%q, expected to contain binary name", loc)
	}
}

// TestDownload_EmptyName verifies trailing slash with no name → 404.
func TestDownload_EmptyName(t *testing.T) {
	api := newTestAPIWithNode(nil)
	api.cfg = &Config{GitHubRelease: "https://github.com/example/releases"}
	api.ottStore = service.NewOTTStore()

	req := httptest.NewRequest(http.MethodGet, "/download/", nil)
	rr := httptest.NewRecorder()
	api.Download(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for empty name, got %d", rr.Code)
	}
}
