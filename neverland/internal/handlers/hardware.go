package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	tinkv1 "github.com/tinkerbell/tink/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"git.konoss.org/kore/schmutz/neverland/internal/respond"
)

// HardwareHandler handles CRUD operations for Tinkerbell Hardware resources.
type HardwareHandler struct {
	client    client.Client
	namespace string
}

// NewHardwareHandler constructs a HardwareHandler.
func NewHardwareHandler(c client.Client, namespace string) *HardwareHandler {
	return &HardwareHandler{client: c, namespace: namespace}
}

// validK8sName validates a Kubernetes-compatible name.
func validK8sName(name string) bool {
	if len(name) == 0 || len(name) > 253 {
		return false
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '.') {
			return false
		}
	}
	return true
}

// List returns all Hardware objects in the configured namespace.
func (h *HardwareHandler) List(w http.ResponseWriter, r *http.Request) {
	var list tinkv1.HardwareList
	if err := h.client.List(r.Context(), &list, client.InNamespace(h.namespace)); err != nil {
		log.Printf("hardware list: %v", err)
		respond.Error(w, http.StatusInternalServerError, "failed to list hardware")
		return
	}
	// Ensure items is always an array, never null.
	items := list.Items
	if items == nil {
		items = []tinkv1.Hardware{}
	}
	respond.JSON(w, http.StatusOK, map[string]interface{}{"items": items})
}

// Get returns a single Hardware object by name.
func (h *HardwareHandler) Get(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	if !validK8sName(name) {
		respond.Error(w, http.StatusBadRequest, "invalid hardware name")
		return
	}
	var hw tinkv1.Hardware
	if err := h.client.Get(r.Context(), types.NamespacedName{Name: name, Namespace: h.namespace}, &hw); err != nil {
		respond.Error(w, http.StatusNotFound, "hardware not found")
		return
	}
	respond.JSON(w, http.StatusOK, hw)
}

// CreateHardwareRequest is the request body for creating a Hardware object.
type CreateHardwareRequest struct {
	Name     string `json:"name"`
	MAC      string `json:"mac"`
	IP       string `json:"ip"`
	Netmask  string `json:"netmask"`
	Gateway  string `json:"gateway"`
	Hostname string `json:"hostname"`
	Disk     string `json:"disk"`
}

// Create provisions a new Hardware object in Kubernetes.
func (h *HardwareHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateHardwareRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || req.MAC == "" || req.IP == "" {
		respond.Error(w, http.StatusBadRequest, "name, mac, and ip are required")
		return
	}

	// Apply defaults.
	if req.Hostname == "" {
		req.Hostname = req.Name
	}
	if req.Gateway == "" {
		req.Gateway = "10.11.30.20"
	}
	if req.Netmask == "" {
		req.Netmask = "255.255.255.0"
	}
	if req.Disk == "" {
		req.Disk = "/dev/vda"
	}

	allowPXE := true
	allowWorkflow := true

	hw := &tinkv1.Hardware{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Name,
			Namespace: h.namespace,
		},
		Spec: tinkv1.HardwareSpec{
			Disks: []tinkv1.Disk{
				{Device: req.Disk},
			},
			Metadata: &tinkv1.HardwareMetadata{
				Facility: &tinkv1.MetadataFacility{
					FacilityCode: "kontango",
				},
				Instance: &tinkv1.MetadataInstance{
					Hostname: req.Hostname,
					AllowPxe: true,
				},
			},
			Interfaces: []tinkv1.Interface{
				{
					Netboot: &tinkv1.Netboot{
						AllowPXE:      &allowPXE,
						AllowWorkflow: &allowWorkflow,
					},
					DHCP: &tinkv1.DHCP{
						MAC:      req.MAC,
						Hostname: req.Hostname,
						IP: &tinkv1.IP{
							Address: req.IP,
							Netmask: req.Netmask,
							Gateway: req.Gateway,
							Family:  4,
						},
					},
				},
			},
		},
	}

	if err := h.client.Create(r.Context(), hw); err != nil {
		log.Printf("hardware create: %v", err)
		respond.Error(w, http.StatusConflict, "failed to create hardware (may already exist)")
		return
	}
	respond.JSON(w, http.StatusCreated, hw)
}

// Patch applies a merge-patch to an existing Hardware object.
func (h *HardwareHandler) Patch(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	if !validK8sName(name) {
		respond.Error(w, http.StatusBadRequest, "invalid hardware name")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		respond.Error(w, http.StatusBadRequest, "failed to read body")
		return
	}

	var hw tinkv1.Hardware
	if err := h.client.Get(r.Context(), types.NamespacedName{Name: name, Namespace: h.namespace}, &hw); err != nil {
		respond.Error(w, http.StatusNotFound, "hardware not found")
		return
	}

	if err := h.client.Patch(r.Context(), &hw, client.RawPatch(types.MergePatchType, body)); err != nil {
		log.Printf("hardware patch: %v", err)
		respond.Error(w, http.StatusInternalServerError, "patch failed: "+err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, hw)
}

// Delete removes a Hardware object by name.
func (h *HardwareHandler) Delete(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	if !validK8sName(name) {
		respond.Error(w, http.StatusBadRequest, "invalid hardware name")
		return
	}
	var hw tinkv1.Hardware
	if err := h.client.Get(r.Context(), types.NamespacedName{Name: name, Namespace: h.namespace}, &hw); err != nil {
		respond.Error(w, http.StatusNotFound, "hardware not found")
		return
	}
	if err := h.client.Delete(r.Context(), &hw); err != nil {
		log.Printf("hardware delete: %v", err)
		respond.Error(w, http.StatusInternalServerError, "delete failed")
		return
	}
	respond.JSON(w, http.StatusOK, map[string]string{"deleted": name})
}

// NetbootRequest is the request body for the netboot toggle endpoint.
type NetbootRequest struct {
	AllowPXE      *bool `json:"allowPXE"`
	AllowWorkflow *bool `json:"allowWorkflow"`
}

// Netboot updates the allowPXE and allowWorkflow flags on a Hardware CR's first interface.
func (h *HardwareHandler) Netboot(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	if !validK8sName(name) {
		respond.Error(w, http.StatusBadRequest, "invalid hardware name")
		return
	}

	var req NetbootRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.AllowPXE == nil && req.AllowWorkflow == nil {
		respond.Error(w, http.StatusBadRequest, "at least one of allowPXE or allowWorkflow is required")
		return
	}

	var hw tinkv1.Hardware
	if err := h.client.Get(r.Context(), types.NamespacedName{Name: name, Namespace: h.namespace}, &hw); err != nil {
		if apierrors.IsNotFound(err) {
			respond.Error(w, http.StatusNotFound, "hardware not found")
		} else {
			respond.Error(w, http.StatusInternalServerError, "failed to get hardware")
		}
		return
	}

	if len(hw.Spec.Interfaces) == 0 {
		respond.Error(w, http.StatusUnprocessableEntity, "hardware has no interfaces")
		return
	}

	if hw.Spec.Interfaces[0].Netboot == nil {
		hw.Spec.Interfaces[0].Netboot = &tinkv1.Netboot{}
	}
	if req.AllowPXE != nil {
		hw.Spec.Interfaces[0].Netboot.AllowPXE = req.AllowPXE
	}
	if req.AllowWorkflow != nil {
		hw.Spec.Interfaces[0].Netboot.AllowWorkflow = req.AllowWorkflow
	}

	if err := h.client.Update(r.Context(), &hw); err != nil {
		log.Printf("hardware netboot update: %v", err)
		respond.Error(w, http.StatusInternalServerError, "failed to update netboot flags")
		return
	}
	respond.JSON(w, http.StatusOK, map[string]interface{}{
		"name":          name,
		"allowPXE":      hw.Spec.Interfaces[0].Netboot.AllowPXE,
		"allowWorkflow": hw.Spec.Interfaces[0].Netboot.AllowWorkflow,
	})
}

// Watch streams hardware annotation/spec changes as Server-Sent Events.
func (h *HardwareHandler) Watch(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	if !validK8sName(name) {
		respond.Error(w, http.StatusBadRequest, "invalid hardware name")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		respond.Error(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	var lastAnnotations string
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			var hw tinkv1.Hardware
			if err := h.client.Get(r.Context(), types.NamespacedName{Name: name, Namespace: h.namespace}, &hw); err != nil {
				if apierrors.IsNotFound(err) {
					fmt.Fprintf(w, "event: error\ndata: {\"error\":\"hardware not found\"}\n\n")
					flusher.Flush()
					return
				}
				fmt.Fprintf(w, "event: error\ndata: {\"error\":\"failed to get hardware\"}\n\n")
				flusher.Flush()
				return
			}
			annJSON, _ := json.Marshal(hw.Annotations)
			cur := string(annJSON)
			if cur != lastAnnotations {
				lastAnnotations = cur
				data, _ := json.Marshal(map[string]interface{}{
					"name":        name,
					"annotations": hw.Annotations,
				})
				fmt.Fprintf(w, "event: update\ndata: %s\n\n", data)
				flusher.Flush()
			}
		}
	}
}

func boolPtr(b bool) *bool { return &b }

// Ensure boolPtr is used (suppress unused warning if inlined above).
var _ = boolPtr
