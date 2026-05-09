package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/KontangoOSS/schmutz/enroll/internal/audit"
)

type fakeExec struct {
	mu     sync.Mutex
	calls  []string
	stdins []string // captured stdin content per call (empty string if nil stdin)
	output string
	err    error
}

func (f *fakeExec) Run(ctx context.Context, stdin io.Reader, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cmd := name
	for _, a := range args {
		cmd += " " + a
	}
	f.calls = append(f.calls, cmd)
	var stdinContent string
	if stdin != nil {
		b, _ := io.ReadAll(stdin)
		stdinContent = string(b)
	}
	f.stdins = append(f.stdins, stdinContent)
	if f.err != nil {
		return nil, f.err
	}
	return []byte(f.output), nil
}

type fakeBreakGlass struct {
	created bool
	err     error
}

func (f *fakeBreakGlass) Create(ctx context.Context) (BreakGlassResult, error) {
	if f.err != nil {
		return BreakGlassResult{}, f.err
	}
	f.created = true
	return BreakGlassResult{
		IdentityName: BreakGlassIdentityName,
		IdentityJSON: []byte(`{"identity":"break-glass"}`),
		LocalPath:    "/root/.schmutz/break-glass.json",
		BaoPath:      "schmutz/break-glass/identity",
	}, nil
}

func TestBootstrap_BaoInit(t *testing.T) {
	ex := &fakeExec{output: "Initialized\nUnseal Key 1: aaa\nUnseal Key 2: bbb\nRoot Token: roo"}
	au := &captureAudit{}
	h := NewBootstrapHandler(ex, &fakeBreakGlass{}, au)
	req := httptest.NewRequest("POST", "/api/bootstrap/bao/init", nil)
	w := httptest.NewRecorder()
	h.BaoInit().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var resp struct {
		UnsealKeys []string `json:"unseal_keys"`
		RootToken  string   `json:"root_token"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.UnsealKeys) < 2 {
		t.Errorf("got %d unseal keys", len(resp.UnsealKeys))
	}
	if resp.RootToken != "roo" {
		t.Errorf("root token = %q", resp.RootToken)
	}
	if len(ex.calls) == 0 || ex.calls[0][:13] != "bao operator " {
		t.Errorf("expected bao operator init call, got %v", ex.calls)
	}
	if len(au.events) != 1 || au.events[0].Action != audit.ActionBootstrapBaoInit {
		t.Errorf("audit: %+v", au.events)
	}
}

func TestBootstrap_BaoInit_ExecError(t *testing.T) {
	ex := &fakeExec{err: errors.New("bao not installed")}
	h := NewBootstrapHandler(ex, &fakeBreakGlass{}, &captureAudit{})
	req := httptest.NewRequest("POST", "/api/bootstrap/bao/init", nil)
	w := httptest.NewRecorder()
	h.BaoInit().ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status %d, want 500", w.Code)
	}
}

func TestBootstrap_DistributeKeys(t *testing.T) {
	ex := &fakeExec{output: "ok"}
	h := NewBootstrapHandler(ex, &fakeBreakGlass{}, &captureAudit{})
	body := bytes.NewBufferString(`{"keys":["aaa","bbb","ccc"],"peers":["10.0.0.2","10.0.0.3"]}`)
	req := httptest.NewRequest("POST", "/api/bootstrap/bao/distribute-keys", body)
	w := httptest.NewRecorder()
	h.DistributeKeys().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status %d: %s", w.Code, w.Body)
	}
	// Bug-A regression: command must start with "-o" (not "ssh -o").
	// Run is called with name="ssh"; args should NOT prepend another "ssh".
	if len(ex.calls) != 2 {
		t.Fatalf("got %d ssh calls, want 2", len(ex.calls))
	}
	for i, c := range ex.calls {
		if !strings.HasPrefix(c, "ssh -o") {
			t.Errorf("call[%d] = %q, want prefix \"ssh -o\" (no double-ssh)", i, c)
		}
	}
	// Bug-B regression: each ssh invocation must have received the keys via stdin.
	if len(ex.stdins) != 2 {
		t.Fatalf("got %d stdins, want 2", len(ex.stdins))
	}
	for i, s := range ex.stdins {
		if s != "aaa\nbbb\nccc\n" {
			t.Errorf("stdin[%d] = %q, want %q", i, s, "aaa\nbbb\nccc\n")
		}
	}
}

func TestBootstrap_JoinPeer(t *testing.T) {
	ex := &fakeExec{output: "joined"}
	au := &captureAudit{}
	h := NewBootstrapHandler(ex, &fakeBreakGlass{}, au)
	body := bytes.NewBufferString(`{"leader":"10.0.0.1","self":"10.0.0.2"}`)
	req := httptest.NewRequest("POST", "/api/bootstrap/bao/join-peer", body)
	w := httptest.NewRecorder()
	h.JoinPeer().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if len(ex.calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(ex.calls))
	}
	if !strings.HasPrefix(ex.calls[0], "bao operator raft join https://10.0.0.1:8200") {
		t.Errorf("call = %q, want bao operator raft join with leader URL", ex.calls[0])
	}
	if len(au.events) != 1 || au.events[0].Action != audit.ActionBootstrapBaoJoinPeer {
		t.Errorf("audit: %+v", au.events)
	}
}

func TestBootstrap_JoinPeer_RequiresLeader(t *testing.T) {
	h := NewBootstrapHandler(&fakeExec{}, &fakeBreakGlass{}, &captureAudit{})
	body := bytes.NewBufferString(`{"leader":"","self":"10.0.0.2"}`)
	req := httptest.NewRequest("POST", "/api/bootstrap/bao/join-peer", body)
	w := httptest.NewRecorder()
	h.JoinPeer().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400", w.Code)
	}
}

func TestBootstrap_ApplyEnrollPolicy(t *testing.T) {
	ex := &fakeExec{output: "Success! Uploaded policy: enroll-server"}
	au := &captureAudit{}
	h := NewBootstrapHandler(ex, &fakeBreakGlass{}, au)
	req := httptest.NewRequest("POST", "/api/bootstrap/apply-enroll-policy", nil)
	w := httptest.NewRecorder()
	h.ApplyEnrollPolicy().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if len(ex.calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(ex.calls))
	}
	// The path is hardcoded; verify it didn't drift.
	if ex.calls[0] != "bao policy write enroll-server /etc/kontango/bao/enroll-server.hcl" {
		t.Errorf("call = %q", ex.calls[0])
	}
	if len(au.events) != 1 || au.events[0].Action != audit.ActionBootstrapApplyEnrollPolicy {
		t.Errorf("audit: %+v", au.events)
	}
}

func TestBootstrap_CreateBreakGlass(t *testing.T) {
	bg := &fakeBreakGlass{}
	au := &captureAudit{}
	h := NewBootstrapHandler(&fakeExec{}, bg, au)
	req := httptest.NewRequest("POST", "/api/bootstrap/create-break-glass", nil)
	w := httptest.NewRecorder()
	h.CreateBreakGlass().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if !bg.created {
		t.Error("break-glass should have been created")
	}
	if len(au.events) != 1 || au.events[0].Action != audit.ActionBootstrapCreateBreakGlass {
		t.Errorf("audit: %+v", au.events)
	}
}
