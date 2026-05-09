package handlers

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	tinkv1 "github.com/tinkerbell/tink/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"git.konoss.org/kore/schmutz/neverland/internal/respond"
)

// TemplateHandler handles CRUD operations for Tinkerbell Template resources.
type TemplateHandler struct {
	client    client.Client
	namespace string
}

// NewTemplateHandler constructs a TemplateHandler.
func NewTemplateHandler(c client.Client, namespace string) *TemplateHandler {
	return &TemplateHandler{client: c, namespace: namespace}
}

// List returns all Template objects in the configured namespace.
func (h *TemplateHandler) List(w http.ResponseWriter, r *http.Request) {
	var list tinkv1.TemplateList
	if err := h.client.List(r.Context(), &list, client.InNamespace(h.namespace)); err != nil {
		respond.Error(w, http.StatusInternalServerError, "failed to list templates")
		return
	}
	items := list.Items
	if items == nil {
		items = []tinkv1.Template{}
	}
	respond.JSON(w, http.StatusOK, map[string]interface{}{"items": items})
}

// Get returns a single Template object by name.
func (h *TemplateHandler) Get(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	var tpl tinkv1.Template
	if err := h.client.Get(r.Context(), types.NamespacedName{Name: name, Namespace: h.namespace}, &tpl); err != nil {
		if apierrors.IsNotFound(err) {
			respond.Error(w, http.StatusNotFound, "template not found")
		} else {
			respond.Error(w, http.StatusInternalServerError, "failed to get template")
		}
		return
	}
	respond.JSON(w, http.StatusOK, tpl)
}

// CreateTemplateRequest is the request body for creating a Template object.
type CreateTemplateRequest struct {
	Name string `json:"name"`
	Data string `json:"data"`
}

// Create provisions a new Template object in Kubernetes.
func (h *TemplateHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || req.Data == "" {
		respond.Error(w, http.StatusBadRequest, "name and data are required")
		return
	}

	tpl := &tinkv1.Template{
		ObjectMeta: metav1.ObjectMeta{Name: req.Name, Namespace: h.namespace},
		Spec:       tinkv1.TemplateSpec{Data: &req.Data},
	}
	if err := h.client.Create(r.Context(), tpl); err != nil {
		log.Printf("template create: %v", err)
		respond.Error(w, http.StatusConflict, "failed to create template")
		return
	}
	respond.JSON(w, http.StatusCreated, tpl)
}

// Patch applies a merge-patch to an existing Template object.
func (h *TemplateHandler) Patch(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]

	body, err := io.ReadAll(r.Body)
	if err != nil {
		respond.Error(w, http.StatusBadRequest, "failed to read body")
		return
	}

	var tpl tinkv1.Template
	if err := h.client.Get(r.Context(), types.NamespacedName{Name: name, Namespace: h.namespace}, &tpl); err != nil {
		respond.Error(w, http.StatusNotFound, "template not found")
		return
	}
	if err := h.client.Patch(r.Context(), &tpl, client.RawPatch(types.MergePatchType, body)); err != nil {
		respond.Error(w, http.StatusInternalServerError, "patch failed")
		return
	}
	respond.JSON(w, http.StatusOK, tpl)
}

// Delete removes a Template object by name.
func (h *TemplateHandler) Delete(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	var tpl tinkv1.Template
	if err := h.client.Get(r.Context(), types.NamespacedName{Name: name, Namespace: h.namespace}, &tpl); err != nil {
		if apierrors.IsNotFound(err) {
			respond.Error(w, http.StatusNotFound, "template not found")
		} else {
			respond.Error(w, http.StatusInternalServerError, "failed to get template")
		}
		return
	}
	if err := h.client.Delete(r.Context(), &tpl); err != nil {
		respond.Error(w, http.StatusInternalServerError, "delete failed")
		return
	}
	respond.JSON(w, http.StatusOK, map[string]string{"deleted": name})
}
