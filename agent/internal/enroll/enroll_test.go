package enroll_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KontangoOSS/schmutz/agent/internal/enroll"
)

func TestNeedsEnrollment_noFile(t *testing.T) {
	dir := t.TempDir()
	if !enroll.NeedsEnrollment(filepath.Join(dir, "identity.json")) {
		t.Error("expected true when file missing")
	}
}

func TestNeedsEnrollment_fileExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.json")
	os.WriteFile(path, []byte(`{}`), 0600)
	if enroll.NeedsEnrollment(path) {
		t.Error("expected false when file exists")
	}
}

// sseEvent writes a single SSE event to a ResponseWriter.
func sseEvent(w http.ResponseWriter, event string, data any) {
	b, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func TestRegister_approved(t *testing.T) {
	fakeIdentity := `{"ztAPI":"https://ctrl.example.com","id":{"cert":"","key":"","ca":""}}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("expected Accept: text/event-stream")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		sseEvent(w, "verify", map[string]any{"check": "fingerprint", "passed": true})
		sseEvent(w, "decision", map[string]any{"status": "approved", "reason": "known device"})
		sseEvent(w, "progress", map[string]any{"step": "provisioning_identity"})
		sseEvent(w, "identity", map[string]any{
			"status":   "approved",
			"identity": fakeIdentity,
			"nickname": "test-slug",
			"services": []string{"ssh-test-slug"},
		})
	}))
	defer srv.Close()

	info := enroll.DeviceInfo{
		Hostname:    "host",
		OS:          "linux",
		Arch:        "amd64",
		Platform:    "lxc",
		Fingerprint: "fp123",
	}
	result, err := enroll.Register(context.Background(), srv.URL, info)
	if err != nil {
		t.Fatal(err)
	}
	if result.Slug != "test-slug" {
		t.Errorf("slug: got %q, want %q", result.Slug, "test-slug")
	}
	if result.Status != "approved" {
		t.Errorf("status: got %q, want %q", result.Status, "approved")
	}
	if len(result.Services) != 1 || result.Services[0] != "ssh-test-slug" {
		t.Errorf("services: got %v", result.Services)
	}
	if string(result.IdentityJSON) != fakeIdentity {
		t.Errorf("identity JSON mismatch: got %q", result.IdentityJSON)
	}
}

func TestRegister_quarantine(t *testing.T) {
	fakeIdentity := `{"ztAPI":"https://ctrl.example.com","id":{"cert":"","key":"","ca":""}}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		sseEvent(w, "decision", map[string]any{"status": "quarantine", "reason": "new device"})
		sseEvent(w, "identity", map[string]any{
			"status":   "quarantine",
			"identity": fakeIdentity,
			"nickname": "new-host-1",
			"services": []string{},
		})
	}))
	defer srv.Close()

	info := enroll.DeviceInfo{Hostname: "new-host", OS: "linux", Arch: "amd64"}
	result, err := enroll.Register(context.Background(), srv.URL, info)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "quarantine" {
		t.Errorf("status: got %q, want quarantine", result.Status)
	}
	if result.Slug != "new-host-1" {
		t.Errorf("slug: got %q", result.Slug)
	}
}

func TestRegister_rejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		sseEvent(w, "decision", map[string]any{"status": "rejected", "reason": "banned"})
	}))
	defer srv.Close()

	info := enroll.DeviceInfo{Hostname: "host", OS: "linux", Arch: "amd64", Fingerprint: "fp"}
	_, err := enroll.Register(context.Background(), srv.URL, info)
	if err == nil {
		t.Fatal("expected error for rejected device")
	}
	if !strings.Contains(err.Error(), "rejected") {
		t.Errorf("expected 'rejected' in error, got: %v", err)
	}
}

func TestRegister_httpError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	info := enroll.DeviceInfo{Hostname: "host", OS: "linux", Arch: "amd64"}
	_, err := enroll.Register(context.Background(), srv.URL, info)
	if err == nil {
		t.Fatal("expected error for HTTP 403")
	}
}

func TestRegister_streamEndedWithoutIdentity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		sseEvent(w, "progress", map[string]any{"step": "starting"})
		// No identity event — stream closes
	}))
	defer srv.Close()

	info := enroll.DeviceInfo{Hostname: "host", OS: "linux", Arch: "amd64"}
	_, err := enroll.Register(context.Background(), srv.URL, info)
	if err == nil {
		t.Fatal("expected error when stream ends without identity")
	}
	if !strings.Contains(err.Error(), "identity event") {
		t.Errorf("expected 'identity event' in error, got: %v", err)
	}
}

func TestEnrollJWT_emptyToken(t *testing.T) {
	err := enroll.EnrollJWT("", "/tmp/should-not-exist.json")
	if err == nil {
		t.Fatal("expected error for empty jwt")
	}
}

func TestCheckIdentityCA_missing(t *testing.T) {
	// Non-existent file is not an error — device just isn't enrolled yet.
	if err := enroll.CheckIdentityCA("/tmp/does-not-exist-schmutz.json"); err != nil {
		t.Errorf("expected nil for missing file, got: %v", err)
	}
}

func TestCheckIdentityCA_noCAField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.json")
	// Identity with no CA field — should fail with a clear message.
	os.WriteFile(path, []byte(`{"id":{"cert":"","key":"","ca":""}}`), 0600)
	err := enroll.CheckIdentityCA(path)
	if err == nil {
		t.Fatal("expected error for identity with no CA")
	}
	if !strings.Contains(err.Error(), "no CA") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestCheckIdentityCA_kontangoCA(t *testing.T) {
	// Simulate an identity that was enrolled using the Kontango/Bao CA.
	// We use a fake PEM with the forbidden CN.
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.json")

	// This is a self-signed cert with CN=Kontango Root CA — same pattern as the real one.
	// Generated inline so the test has no external dependency.
	fakeCAPEM := generateSelfSignedCert(t, "Kontango Root CA")
	id := map[string]any{
		"id": map[string]any{
			"cert": fakeCAPEM, // use CA as cert too — just need the CA check to fire
			"key":  "",
			"ca":   fakeCAPEM,
		},
	}
	data, _ := json.Marshal(id)
	os.WriteFile(path, data, 0600)

	err := enroll.CheckIdentityCA(path)
	if err == nil {
		t.Fatal("expected error for Kontango CA in identity")
	}
	if !strings.Contains(err.Error(), "Kontango") {
		t.Errorf("expected Kontango mention in error, got: %v", err)
	}
}

// generateSelfSignedCert creates a minimal self-signed cert PEM with the given CN.
func generateSelfSignedCert(t *testing.T, cn string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IsCA:         true,
		KeyUsage:     x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func TestCheckIdentityCA_pemPrefix(t *testing.T) {
	// Ziti SDK writes CA as "pem:<pem-data>" — verify we strip the prefix correctly
	// and still detect the Kontango CA.
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.json")

	fakeCAPEM := "pem:" + generateSelfSignedCert(t, "Kontango Root CA")
	id := map[string]any{
		"id": map[string]any{
			"cert": fakeCAPEM,
			"key":  "",
			"ca":   fakeCAPEM,
		},
	}
	data, _ := json.Marshal(id)
	os.WriteFile(path, data, 0600)

	err := enroll.CheckIdentityCA(path)
	if err == nil {
		t.Fatal("expected error for Kontango CA with pem: prefix")
	}
	if !strings.Contains(err.Error(), "Kontango") {
		t.Errorf("expected Kontango mention in error, got: %v", err)
	}
}

func TestCheckIdentityCA_zitiCA(t *testing.T) {
	// A CA with a neutral CN (like Ziti uses) should pass the CN check.
	// The cert chain verification will fail since it's self-signed not matching
	// the device cert, but the CA rejection check (the important one) passes.
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.json")

	zitiCAPEM := generateSelfSignedCert(t, "root-ca") // Ziti default CN
	id := map[string]any{
		"id": map[string]any{
			"cert": zitiCAPEM,
			"key":  "",
			"ca":   zitiCAPEM,
		},
	}
	data, _ := json.Marshal(id)
	os.WriteFile(path, data, 0600)

	err := enroll.CheckIdentityCA(path)
	// Should NOT return a Kontango CA error — may return a chain verify error,
	// which is fine (we're using a fake self-signed cert as both CA and device cert).
	if err != nil && strings.Contains(err.Error(), "Kontango") {
		t.Errorf("should not flag a Ziti-named CA as Kontango: %v", err)
	}
}

func TestEnrollJWT_noZitiBinaryNeeded(t *testing.T) {
	// EnrollJWT must not require the ziti CLI binary.
	// A well-formed but invalid JWT produces a parse/SDK error, not "ziti CLI not found".
	err := enroll.EnrollJWT("eyJhbGciOiJSUzI1NiJ9.eyJlbSI6Im90dCJ9.sig", "/tmp/schmutz-test-noziti.json")
	if err == nil {
		t.Fatal("expected error for invalid JWT")
	}
	if strings.Contains(err.Error(), "ziti CLI not found") {
		t.Errorf("EnrollJWT must not depend on ziti CLI, got: %v", err)
	}
}
