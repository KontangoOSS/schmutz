package handlers_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gorilla/mux"
	"git.konoss.org/kore/schmutz/neverland/internal/handlers"
)

func TestListArtifacts_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	h := handlers.NewArtifactHandler(dir, "http://localhost:8080")

	req := httptest.NewRequest("GET", "/api/v1/artifacts", nil)
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	items := resp["items"].([]interface{})
	if len(items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(items))
	}
}

func TestListArtifacts_WithFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ubuntu.raw.gz"), []byte("fake"), 0644)

	h := handlers.NewArtifactHandler(dir, "http://localhost:8080")

	req := httptest.NewRequest("GET", "/api/v1/artifacts", nil)
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	items := resp["items"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	item := items[0].(map[string]interface{})
	if item["name"] != "ubuntu.raw.gz" {
		t.Fatalf("expected ubuntu.raw.gz, got %v", item["name"])
	}
}

func TestDeleteArtifact_NotFound(t *testing.T) {
	dir := t.TempDir()
	h := handlers.NewArtifactHandler(dir, "http://localhost:8080")

	router := mux.NewRouter()
	router.HandleFunc("/api/v1/artifacts/{filename}", h.Delete).Methods("DELETE")

	req := httptest.NewRequest("DELETE", "/api/v1/artifacts/ghost.gz", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestDeleteArtifact_Existing(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ubuntu.raw.gz"), []byte("fake"), 0644)

	h := handlers.NewArtifactHandler(dir, "http://localhost:8080")

	router := mux.NewRouter()
	router.HandleFunc("/api/v1/artifacts/{filename}", h.Delete).Methods("DELETE")

	req := httptest.NewRequest("DELETE", "/api/v1/artifacts/ubuntu.raw.gz", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "ubuntu.raw.gz")); !os.IsNotExist(err) {
		t.Fatal("file should have been deleted")
	}
}

func TestUploadArtifact(t *testing.T) {
	dir := t.TempDir()
	h := handlers.NewArtifactHandler(dir, "http://localhost:8080")

	var buf bytes.Buffer
	mp := multipart.NewWriter(&buf)
	fw, _ := mp.CreateFormFile("file", "test.raw.gz")
	fw.Write([]byte("fake image data"))
	mp.Close()

	req := httptest.NewRequest("POST", "/api/v1/artifacts", &buf)
	req.Header.Set("Content-Type", mp.FormDataContentType())
	w := httptest.NewRecorder()
	h.Upload(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "test.raw.gz")); err != nil {
		t.Fatal("uploaded file not found on disk")
	}
}
