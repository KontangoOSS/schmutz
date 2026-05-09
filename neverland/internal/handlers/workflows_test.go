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

func TestListWorkflows_Empty(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(testScheme()).Build()
	h := handlers.NewWorkflowHandler(fakeClient, "tink-system")

	req := httptest.NewRequest("GET", "/api/v1/workflows", nil)
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestCreateWorkflow_MissingFields(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(testScheme()).Build()
	h := handlers.NewWorkflowHandler(fakeClient, "tink-system")

	body, _ := json.Marshal(map[string]interface{}{"name": "wf1"}) // missing templateRef and hardwareRef
	req := httptest.NewRequest("POST", "/api/v1/workflows", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateWorkflow_Valid(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(testScheme()).Build()
	h := handlers.NewWorkflowHandler(fakeClient, "tink-system")

	body, _ := json.Marshal(map[string]interface{}{
		"name":        "myhost",
		"templateRef": "ubuntu-test",
		"hardwareRef": "myhost",
		"mac":         "bc:24:11:aa:bb:cc",
	})
	req := httptest.NewRequest("POST", "/api/v1/workflows", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetWorkflow_NotFound(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(testScheme()).Build()
	h := handlers.NewWorkflowHandler(fakeClient, "tink-system")

	router := mux.NewRouter()
	router.HandleFunc("/api/v1/workflows/{name}", h.Get)

	req := httptest.NewRequest("GET", "/api/v1/workflows/ghost", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestReprovision_DeletesAndRecreates(t *testing.T) {
	wf := &tinkv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{Name: "myhost", Namespace: "tink-system"},
		Spec: tinkv1.WorkflowSpec{
			TemplateRef: "ubuntu-test",
			HardwareRef: "myhost",
			HardwareMap: map[string]string{"device_1": "bc:24:11:aa:bb:cc"},
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(wf).Build()
	h := handlers.NewWorkflowHandler(fakeClient, "tink-system")

	router := mux.NewRouter()
	router.HandleFunc("/api/v1/workflows/{name}/reprovision", h.Reprovision).Methods("POST")

	req := httptest.NewRequest("POST", "/api/v1/workflows/myhost/reprovision", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 after reprovision, got %d: %s", w.Code, w.Body.String())
	}
}
