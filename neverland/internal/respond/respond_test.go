package respond_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"git.konoss.org/kore/schmutz/neverland/internal/respond"
)

func TestJSON_WritesStatusAndBody(t *testing.T) {
	w := httptest.NewRecorder()
	respond.JSON(w, http.StatusOK, map[string]string{"hello": "world"})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %q", ct)
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["hello"] != "world" {
		t.Fatalf("expected world, got %q", body["hello"])
	}
}

func TestError_WritesErrorJSON(t *testing.T) {
	w := httptest.NewRecorder()
	respond.Error(w, http.StatusNotFound, "hardware not found")

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error"] != "hardware not found" {
		t.Fatalf("expected 'hardware not found', got %q", body["error"])
	}
}
