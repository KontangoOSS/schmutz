package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"git.konoss.org/kore/schmutz/neverland/internal/handlers"
)

func bootScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = appsv1.AddToScheme(s)
	return s
}

func smeeDeployment() *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "smee", Namespace: "tink-system"},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "smee"},
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "smee",
							Env: []corev1.EnvVar{
								{Name: "SMEE_EXTRA_KERNEL_ARGS", Value: "tink_worker_image=quay.io/tinkerbell/tink-worker:v0.12.1"},
								{Name: "SMEE_OSIE_URL", Value: "http://10.11.30.91:8080"},
								{Name: "SMEE_ISO_ENABLED", Value: "false"},
							},
						},
					},
				},
			},
		},
	}
}

func TestGetBoot_ReturnsSmeeConfig(t *testing.T) {
	fakeClient := fake.NewClientBuilder().
		WithScheme(bootScheme()).
		WithObjects(smeeDeployment()).
		Build()
	h := handlers.NewBootHandler(fakeClient, "tink-system", "smee")

	req := httptest.NewRequest("GET", "/api/v1/boot", nil)
	w := httptest.NewRecorder()
	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["osieURL"] != "http://10.11.30.91:8080" {
		t.Fatalf("expected osieURL, got %v", resp["osieURL"])
	}
	if resp["isoEnabled"] != false {
		t.Fatalf("expected isoEnabled=false, got %v", resp["isoEnabled"])
	}
}
