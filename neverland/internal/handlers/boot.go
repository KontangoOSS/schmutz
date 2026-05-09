package handlers

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"git.konoss.org/kore/schmutz/neverland/internal/respond"
)

// BootHandler reads and patches boot configuration stored as env vars on the Smee Deployment.
type BootHandler struct {
	client         client.Client
	namespace      string
	smeeDeployment string
}

// NewBootHandler constructs a BootHandler.
func NewBootHandler(c client.Client, namespace, smeeDeployment string) *BootHandler {
	return &BootHandler{client: c, namespace: namespace, smeeDeployment: smeeDeployment}
}

// BootConfig is the shape returned by GET /boot.
type BootConfig struct {
	KernelArgs string `json:"kernelArgs"`
	OsieURL    string `json:"osieURL"`
	ISOEnabled bool   `json:"isoEnabled"`
	ISOURL     string `json:"isoURL"`
}

func (h *BootHandler) getSmeeEnv(r *http.Request) (map[string]string, error) {
	var dep appsv1.Deployment
	if err := h.client.Get(r.Context(), types.NamespacedName{Name: h.smeeDeployment, Namespace: h.namespace}, &dep); err != nil {
		return nil, err
	}
	env := make(map[string]string)
	for _, c := range dep.Spec.Template.Spec.Containers {
		for _, e := range c.Env {
			env[e.Name] = e.Value
		}
	}
	return env, nil
}

// Get returns the current Smee boot configuration.
func (h *BootHandler) Get(w http.ResponseWriter, r *http.Request) {
	env, err := h.getSmeeEnv(r)
	if err != nil {
		respond.Error(w, http.StatusNotFound, "smee deployment not found")
		return
	}

	cfg := BootConfig{
		KernelArgs: env["SMEE_EXTRA_KERNEL_ARGS"],
		OsieURL:    env["SMEE_OSIE_URL"],
		ISOEnabled: env["SMEE_ISO_ENABLED"] == "true",
		ISOURL:     env["SMEE_ISO_URL"],
	}
	respond.JSON(w, http.StatusOK, cfg)
}

// PatchBootRequest is the request body for PATCH /boot.
type PatchBootRequest struct {
	KernelArgs *string `json:"kernelArgs"`
	OsieURL    *string `json:"osieURL"`
	ISOEnabled *bool   `json:"isoEnabled"`
	ISOURL     *string `json:"isoURL"`
}

// Patch updates one or more Smee env vars on the Deployment.
func (h *BootHandler) Patch(w http.ResponseWriter, r *http.Request) {
	var req PatchBootRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var dep appsv1.Deployment
	if err := h.client.Get(r.Context(), types.NamespacedName{Name: h.smeeDeployment, Namespace: h.namespace}, &dep); err != nil {
		respond.Error(w, http.StatusNotFound, "smee deployment not found")
		return
	}

	updates := map[string]string{}
	if req.KernelArgs != nil {
		updates["SMEE_EXTRA_KERNEL_ARGS"] = *req.KernelArgs
	}
	if req.OsieURL != nil {
		updates["SMEE_OSIE_URL"] = *req.OsieURL
	}
	if req.ISOEnabled != nil {
		if *req.ISOEnabled {
			updates["SMEE_ISO_ENABLED"] = "true"
		} else {
			updates["SMEE_ISO_ENABLED"] = "false"
		}
	}
	if req.ISOURL != nil {
		updates["SMEE_ISO_URL"] = *req.ISOURL
	}

	for i := range dep.Spec.Template.Spec.Containers {
		for j := range dep.Spec.Template.Spec.Containers[i].Env {
			envName := dep.Spec.Template.Spec.Containers[i].Env[j].Name
			if val, ok := updates[envName]; ok {
				dep.Spec.Template.Spec.Containers[i].Env[j].Value = val
				delete(updates, envName)
			}
		}
		// Add any new env vars not already present on this container.
		for name, val := range updates {
			dep.Spec.Template.Spec.Containers[i].Env = append(dep.Spec.Template.Spec.Containers[i].Env, corev1.EnvVar{
				Name:  name,
				Value: val,
			})
		}
		// Only process the first (smee) container.
		break
	}

	if err := h.client.Update(r.Context(), &dep); err != nil {
		log.Printf("boot patch: %v", err)
		respond.Error(w, http.StatusInternalServerError, "failed to update smee deployment")
		return
	}
	respond.JSON(w, http.StatusOK, map[string]string{"status": "updated", "note": "smee will roll over automatically"})
}

// IPXEScript proxies the generated per-MAC iPXE script from Smee's HTTP server.
func (h *BootHandler) IPXEScript(w http.ResponseWriter, r *http.Request) {
	mac := mux.Vars(r)["mac"]
	if mac == "" {
		respond.Error(w, http.StatusBadRequest, "mac is required")
		return
	}
	// Smee serves iPXE scripts on :7171 — use cluster DNS when running in-cluster.
	target := "http://smee." + h.namespace + ".svc.cluster.local:7171/" + mac + "/auto.ipxe"
	resp, err := http.Get(target) //nolint:noctx
	if err != nil {
		respond.Error(w, http.StatusBadGateway, "failed to fetch iPXE script from smee")
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
