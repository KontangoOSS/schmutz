package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	tinkv1 "github.com/tinkerbell/tink/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"git.konoss.org/kore/schmutz/neverland/internal/handlers"
)

// TestStatus_AlwaysReturns200 verifies the handler returns 200 even when
// every probed dependency is unreachable. The dashboard relies on this so
// it can render even partial status.
func TestStatus_AlwaysReturns200(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(testScheme()).Build()
	h := handlers.NewStatusHandler(fakeClient, "tink-system")

	req := httptest.NewRequest("GET", "/api/v1/status", nil)
	w := httptest.NewRecorder()
	h.Status(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	for _, key := range []string{"neverland", "tink_server", "hegel", "smee", "hook_files", "counts"} {
		if _, ok := resp[key]; !ok {
			t.Errorf("missing key %q in response: %v", key, resp)
		}
	}

	// In a unit test, no in-cluster services are reachable.
	for _, svc := range []string{"tink_server", "hegel", "smee", "hook_files"} {
		block, _ := resp[svc].(map[string]interface{})
		if block == nil {
			t.Errorf("%s: nil block", svc)
			continue
		}
		if reachable, _ := block["reachable"].(bool); reachable {
			t.Errorf("%s: expected reachable=false in unit test, got true", svc)
		}
	}
}

// TestStatus_CountsCRDs checks the counts block reflects what's in the fake
// k8s client.
func TestStatus_CountsCRDs(t *testing.T) {
	hw := &tinkv1.Hardware{ObjectMeta: metav1.ObjectMeta{Name: "host-a", Namespace: "tink-system"}}
	tpl := &tinkv1.Template{ObjectMeta: metav1.ObjectMeta{Name: "tpl-a", Namespace: "tink-system"}}
	wf := &tinkv1.Workflow{ObjectMeta: metav1.ObjectMeta{Name: "wf-a", Namespace: "tink-system"}}

	fakeClient := fake.NewClientBuilder().
		WithScheme(testScheme()).
		WithObjects(hw, tpl, wf).
		Build()
	h := handlers.NewStatusHandler(fakeClient, "tink-system")

	req := httptest.NewRequest("GET", "/api/v1/status", nil)
	w := httptest.NewRecorder()
	h.Status(w, req)

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	counts, _ := resp["counts"].(map[string]interface{})
	if counts == nil {
		t.Fatal("missing counts block")
	}

	want := map[string]float64{"hardware": 1, "templates": 1, "workflows": 1}
	for k, v := range want {
		if got, _ := counts[k].(float64); got != v {
			t.Errorf("counts.%s = %v, want %v", k, counts[k], v)
		}
	}
}
