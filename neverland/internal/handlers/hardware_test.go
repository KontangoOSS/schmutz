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
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"git.konoss.org/kore/schmutz/neverland/internal/handlers"
)

func testScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = tinkv1.AddToScheme(s)
	return s
}

func TestListHardware_Empty(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(testScheme()).Build()
	h := handlers.NewHardwareHandler(fakeClient, "tink-system")

	req := httptest.NewRequest("GET", "/api/v1/hardware", nil)
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	items, ok := resp["items"].([]interface{})
	if !ok || len(items) != 0 {
		t.Fatalf("expected empty items array, got %v", resp["items"])
	}
}

func TestListHardware_OneItem(t *testing.T) {
	hw := &tinkv1.Hardware{
		ObjectMeta: metav1.ObjectMeta{Name: "myhost", Namespace: "tink-system"},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(hw).Build()
	h := handlers.NewHardwareHandler(fakeClient, "tink-system")

	req := httptest.NewRequest("GET", "/api/v1/hardware", nil)
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
}

func TestGetHardware_NotFound(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(testScheme()).Build()
	h := handlers.NewHardwareHandler(fakeClient, "tink-system")

	router := mux.NewRouter()
	router.HandleFunc("/api/v1/hardware/{name}", h.Get)

	req := httptest.NewRequest("GET", "/api/v1/hardware/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestCreateHardware_Valid(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(testScheme()).Build()
	h := handlers.NewHardwareHandler(fakeClient, "tink-system")

	body, _ := json.Marshal(map[string]interface{}{
		"name": "newhost",
		"mac":  "bc:24:11:aa:bb:cc",
		"ip":   "10.11.50.20",
	})
	req := httptest.NewRequest("POST", "/api/v1/hardware", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteHardware_NotFound(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(testScheme()).Build()
	h := handlers.NewHardwareHandler(fakeClient, "tink-system")

	router := mux.NewRouter()
	router.HandleFunc("/api/v1/hardware/{name}", h.Delete).Methods("DELETE")

	req := httptest.NewRequest("DELETE", "/api/v1/hardware/ghost", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}
