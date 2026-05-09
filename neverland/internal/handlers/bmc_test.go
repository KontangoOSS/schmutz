package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	rufio "github.com/tinkerbell/rufio/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"git.konoss.org/kore/schmutz/neverland/internal/handlers"
)

func bmcScheme() *runtime.Scheme {
	s := testScheme()
	_ = rufio.AddToScheme(s)
	return s
}

func TestListBMCMachines_Empty(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(bmcScheme()).Build()
	h := handlers.NewBMCHandler(fakeClient, "tink-system")

	req := httptest.NewRequest("GET", "/api/v1/bmc/machines", nil)
	w := httptest.NewRecorder()
	h.ListMachines(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListBMCJobs_Empty(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(bmcScheme()).Build()
	h := handlers.NewBMCHandler(fakeClient, "tink-system")

	req := httptest.NewRequest("GET", "/api/v1/bmc/jobs", nil)
	w := httptest.NewRecorder()
	h.ListJobs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestListBMCTasks_Empty(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(bmcScheme()).Build()
	h := handlers.NewBMCHandler(fakeClient, "tink-system")

	req := httptest.NewRequest("GET", "/api/v1/bmc/tasks", nil)
	w := httptest.NewRecorder()
	h.ListTasks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
