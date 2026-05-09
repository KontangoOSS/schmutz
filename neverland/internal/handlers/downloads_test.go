package handlers_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"git.konoss.org/kore/schmutz/neverland/internal/handlers"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestDownloads_ServesFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "kontango-boot-x86_64-uefi.iso", "FAKE-ISO-DATA")

	h := handlers.NewDownloadHandler(dir)
	r := mux.NewRouter()
	r.HandleFunc("/api/v1/downloads/{filename}", h.Get).Methods("GET")

	req := httptest.NewRequest("GET", "/api/v1/downloads/kontango-boot-x86_64-uefi.iso", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body, _ := io.ReadAll(w.Body)
	if string(body) != "FAKE-ISO-DATA" {
		t.Fatalf("body mismatch: %q", body)
	}
}

func TestDownloads_RejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	h := handlers.NewDownloadHandler(dir)
	r := mux.NewRouter()
	r.HandleFunc("/api/v1/downloads/{filename}", h.Get).Methods("GET")

	// Test URL-encoded traversal attempt
	req := httptest.NewRequest("GET", "/api/v1/downloads/..%2F..%2Fetc%2Fpasswd", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// mux redirects malicious paths with 301, or handler rejects with 400/404
	if w.Code != http.StatusBadRequest && w.Code != http.StatusNotFound && w.Code != http.StatusMovedPermanently {
		t.Fatalf("expected 400/404/301 on traversal, got %d", w.Code)
	}
}

func TestDownloads_List(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.iso", "x")
	writeFile(t, dir, "b.img", "y")

	h := handlers.NewDownloadHandler(dir)
	req := httptest.NewRequest("GET", "/api/v1/downloads", nil)
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "a.iso") || !strings.Contains(body, "b.img") {
		t.Fatalf("expected both files, got: %s", body)
	}
}
