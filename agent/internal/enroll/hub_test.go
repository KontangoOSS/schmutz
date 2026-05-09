package enroll

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// fakeHub is an httptest server that mimics the hub's /api/v1/enroll endpoint.
type fakeHub struct {
	server   *httptest.Server
	response hubClaimResponse
	status   int
}

func newFakeHub(t *testing.T, status int, resp hubClaimResponse) *fakeHub {
	f := &fakeHub{response: resp, status: status}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/enroll", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req hubClaimRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(f.status)
		_ = json.NewEncoder(w).Encode(f.response)
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

// validHubResponse returns a response shape the agent accepts.
// identity_json is a fake but parseable JWT structure. In real usage the
// Ziti SDK would exchange it for an identity file; we test the wrapping
// logic not the SDK internals.
func validHubResponse() hubClaimResponse {
	return hubClaimResponse{
		ZitiIdentity: struct {
			Name         string `json:"name"`
			IdentityJSON string `json:"identity_json"`
		}{
			Name: "machine-test1234",
			// A minimal valid-looking JWT structure (EnrollJWT will fail on
			// it because it can't contact a real Ziti controller — that's
			// expected; we test RegisterHub's *wrapping*, not EnrollJWT).
			IdentityJSON: "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ0ZXN0IiwiZW0iOiJvdHQifQ.sig",
		},
		BaoBundle: struct {
			Role         string `json:"role"`
			RoleID       string `json:"role_id"`
			SecretID     string `json:"secret_id,omitempty"`
			OIDCRole     string `json:"oidc_role"`
			JWTRole      string `json:"jwt_role"`
			Tenant       string `json:"tenant"`
			App          string `json:"app"`
			Deployment   string `json:"deployment"`
			Flavor       string `json:"flavor"`
			ZitiIdentity string `json:"ziti_identity"`
			EntityID     string `json:"entity_id"`
			BaoAddr      string `json:"bao_addr"`
		}{
			Role: "kontango-inventree-prod-02", RoleID: "rid-test",
			SecretID: "test-secret-id-plain",
			OIDCRole: "kontango-inventree-prod-02-token",
			JWTRole:  "kontango-inventree-jwt-app",
			Tenant: "kontango", App: "inventree", Deployment: "prod-02",
			Flavor: "app-host", ZitiIdentity: "machine-test1234",
			EntityID: "ent-test", BaoAddr: "http://127.0.0.1:9999",
		},
	}
}

// TestRegisterHub_MissingRequiredFields confirms validation runs before any
// network call is made.
func TestRegisterHub_MissingRequiredFields(t *testing.T) {
	cases := []struct {
		name string
		cfg  HubEnrollConfig
		want string
	}{
		{"missing controller", HubEnrollConfig{Token: "t", IdentityPath: "/x", AgentJSONPath: "/y"}, "controller URL required"},
		{"missing token", HubEnrollConfig{ControllerURL: "http://x", IdentityPath: "/x", AgentJSONPath: "/y"}, "token required"},
		{"missing identity path", HubEnrollConfig{ControllerURL: "http://x", Token: "t", AgentJSONPath: "/y"}, "identity path required"},
		{"missing agent json", HubEnrollConfig{ControllerURL: "http://x", Token: "t", IdentityPath: "/x"}, "agent.json path required"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := RegisterHub(context.Background(), c.cfg)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("expected %q in error, got %v", c.want, err)
			}
		})
	}
}

// TestRegisterHub_401UnauthorizedError gives a clear token-related message.
func TestRegisterHub_401Error(t *testing.T) {
	f := newFakeHub(t, http.StatusUnauthorized, hubClaimResponse{Error: "invalid or expired token"})
	_, err := RegisterHub(context.Background(), HubEnrollConfig{
		ControllerURL: f.server.URL,
		Token:         "enroll-bad",
		Tenant:        "k", App: "a", Deployment: "d",
		IdentityPath:  "/tmp/test.json",
		AgentJSONPath: "/tmp/test-agent.json",
	})
	if err == nil || !strings.Contains(err.Error(), "operator must re-issue") {
		t.Errorf("expected re-issue hint in 401 error, got %v", err)
	}
}

// TestRegisterHub_409ConsumedError.
func TestRegisterHub_409Error(t *testing.T) {
	f := newFakeHub(t, http.StatusConflict, hubClaimResponse{Error: "token already consumed"})
	_, err := RegisterHub(context.Background(), HubEnrollConfig{
		ControllerURL: f.server.URL,
		Token:         "enroll-consumed",
		Tenant:        "k", App: "a", Deployment: "d",
		IdentityPath:  "/tmp/test.json",
		AgentJSONPath: "/tmp/test-agent.json",
	})
	if err == nil || !strings.Contains(err.Error(), "operator must re-issue") {
		t.Errorf("expected re-issue hint in 409 error, got %v", err)
	}
}

// TestRegisterHub_400MismatchError shows the token-mismatch message.
func TestRegisterHub_400Error(t *testing.T) {
	f := newFakeHub(t, http.StatusBadRequest, hubClaimResponse{Error: "token/request mismatch"})
	_, err := RegisterHub(context.Background(), HubEnrollConfig{
		ControllerURL: f.server.URL,
		Token:         "enroll-wrong",
		Tenant:        "k", App: "wrong-app", Deployment: "d",
		IdentityPath:  "/tmp/test.json",
		AgentJSONPath: "/tmp/test-agent.json",
	})
	if err == nil || !strings.Contains(err.Error(), "check tenant/app/deployment") {
		t.Errorf("expected mismatch hint in 400 error, got %v", err)
	}
}

// TestRegisterHub_500ShouldMentionTokenConsumed shows the "token may be consumed"
// hint on 5xx to guide the operator.
func TestRegisterHub_500Error(t *testing.T) {
	f := newFakeHub(t, http.StatusInternalServerError, hubClaimResponse{Error: "bao provisioning failed"})
	_, err := RegisterHub(context.Background(), HubEnrollConfig{
		ControllerURL: f.server.URL,
		Token:         "enroll-fail",
		Tenant:        "k", App: "a", Deployment: "d",
		IdentityPath:  "/tmp/test.json",
		AgentJSONPath: "/tmp/test-agent.json",
	})
	if err == nil || !strings.Contains(err.Error(), "token may be consumed") {
		t.Errorf("expected 'token may be consumed' hint in 500 error, got %v", err)
	}
}

// TestRegisterHub_MissingZitiJWT detects a malformed response before trying EnrollJWT.
func TestRegisterHub_MissingZitiJWT(t *testing.T) {
	resp := validHubResponse()
	resp.ZitiIdentity.IdentityJSON = "" // missing JWT
	f := newFakeHub(t, http.StatusOK, resp)
	_, err := RegisterHub(context.Background(), HubEnrollConfig{
		ControllerURL: f.server.URL,
		Token:         "enroll-test",
		Tenant:        "kontango", App: "inventree", Deployment: "prod-02",
		IdentityPath:  "/tmp/test.json",
		AgentJSONPath: "/tmp/test-agent.json",
	})
	if err == nil || !strings.Contains(err.Error(), "missing ziti identity JSON") {
		t.Errorf("expected missing-identity-json error, got %v", err)
	}
}

// TestRegisterHub_MissingBaoBundle detects incomplete bao bundle.
func TestRegisterHub_MissingBaoBundle(t *testing.T) {
	resp := validHubResponse()
	resp.BaoBundle.RoleID = "" // corrupt bundle
	f := newFakeHub(t, http.StatusOK, resp)
	_, err := RegisterHub(context.Background(), HubEnrollConfig{
		ControllerURL: f.server.URL,
		Token:         "enroll-test",
		Tenant:        "kontango", App: "inventree", Deployment: "prod-02",
		IdentityPath:  "/tmp/test.json",
		AgentJSONPath: "/tmp/test-agent.json",
	})
	if err == nil || !strings.Contains(err.Error(), "missing bao bundle") {
		t.Errorf("expected missing-bundle error, got %v", err)
	}
}

// TestRegisterHub_HubDown returns a clear connectivity error.
func TestRegisterHub_HubDown(t *testing.T) {
	_, err := RegisterHub(context.Background(), HubEnrollConfig{
		ControllerURL: "http://127.0.0.1:1", // nothing listening
		Token:         "enroll-test",
		Tenant:        "kontango", App: "inventree", Deployment: "prod-02",
		IdentityPath:  "/tmp/test.json",
		AgentJSONPath: "/tmp/test-agent.json",
	})
	if err == nil || !strings.Contains(err.Error(), "check overlay connectivity") {
		t.Errorf("expected connectivity hint, got %v", err)
	}
}

// TestRegisterHub_SendsHardwareFacts verifies the hardware map reaches the hub.
func TestRegisterHub_SendsHardwareFacts(t *testing.T) {
	var capturedReq hubClaimRequest
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/enroll", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedReq)
		// Return a response that fails at the Ziti SDK step (no real controller)
		// so we can test the request capture without needing a live Ziti.
		w.Header().Set("Content-Type", "application/json")
		// Return valid shape but with an obviously-invalid JWT to abort at EnrollJWT.
		resp := validHubResponse()
		json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	RegisterHub(context.Background(), HubEnrollConfig{ //nolint:errcheck
		ControllerURL: srv.URL,
		Token:         "enroll-test",
		Tenant:        "kontango", App: "inventree", Deployment: "prod-02",
		IdentityPath:  filepath.Join(dir, "identity.json"),
		AgentJSONPath: filepath.Join(dir, "agent.json"),
		Hardware:      map[string]interface{}{"os": "linux", "arch": "amd64"},
	})
	if capturedReq.Token != "enroll-test" {
		t.Errorf("token not sent: %q", capturedReq.Token)
	}
	if capturedReq.Hardware["os"] != "linux" {
		t.Errorf("hardware not sent: %v", capturedReq.Hardware)
	}
	if capturedReq.Tenant != "kontango" || capturedReq.App != "inventree" {
		t.Errorf("tenant/app not sent: %q/%q", capturedReq.Tenant, capturedReq.App)
	}
}
