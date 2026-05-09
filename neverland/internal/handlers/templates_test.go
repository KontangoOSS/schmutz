package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	tinkv1 "github.com/tinkerbell/tink/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"git.konoss.org/kore/schmutz/neverland/internal/handlers"
)

func TestListTemplates_Empty(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(testScheme()).Build()
	h := handlers.NewTemplateHandler(fakeClient, "tink-system")

	req := httptest.NewRequest("GET", "/api/v1/templates", nil)
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

func TestGetTemplate_NotFound(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(testScheme()).Build()
	h := handlers.NewTemplateHandler(fakeClient, "tink-system")

	router := mux.NewRouter()
	router.HandleFunc("/api/v1/templates/{name}", h.Get)

	req := httptest.NewRequest("GET", "/api/v1/templates/ghost", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestCreateTemplate_Valid(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(testScheme()).Build()
	h := handlers.NewTemplateHandler(fakeClient, "tink-system")

	body, _ := json.Marshal(map[string]interface{}{
		"name": "ubuntu-test",
		"data": "version: \"0.1\"\nname: ubuntu-test\nglobal_timeout: 1800\ntasks: []\n",
	})
	req := httptest.NewRequest("POST", "/api/v1/templates", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

// Suppress unused import warnings for tinkv1 and metav1 if only used in other test files.
var _ = tinkv1.Template{}
var _ = metav1.ObjectMeta{}
