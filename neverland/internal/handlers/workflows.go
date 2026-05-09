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

// WorkflowHandler handles CRUD operations for Tinkerbell Workflow resources.
type WorkflowHandler struct {
	client    client.Client
	namespace string
}

// NewWorkflowHandler constructs a WorkflowHandler.
func NewWorkflowHandler(c client.Client, namespace string) *WorkflowHandler {
	return &WorkflowHandler{client: c, namespace: namespace}
}

// List returns all Workflow objects in the configured namespace.
func (h *WorkflowHandler) List(w http.ResponseWriter, r *http.Request) {
	var list tinkv1.WorkflowList
	if err := h.client.List(r.Context(), &list, client.InNamespace(h.namespace)); err != nil {
		respond.Error(w, http.StatusInternalServerError, "failed to list workflows")
		return
	}
	items := list.Items
	if items == nil {
		items = []tinkv1.Workflow{}
	}
	respond.JSON(w, http.StatusOK, map[string]interface{}{"items": items})
}

// Get returns a single Workflow object by name.
func (h *WorkflowHandler) Get(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	var wf tinkv1.Workflow
	if err := h.client.Get(r.Context(), types.NamespacedName{Name: name, Namespace: h.namespace}, &wf); err != nil {
		if apierrors.IsNotFound(err) {
			respond.Error(w, http.StatusNotFound, "workflow not found")
		} else {
			respond.Error(w, http.StatusInternalServerError, "failed to get workflow")
		}
		return
	}
	respond.JSON(w, http.StatusOK, wf)
}

// CreateWorkflowRequest is the request body for creating a Workflow object.
type CreateWorkflowRequest struct {
	Name        string            `json:"name"`
	TemplateRef string            `json:"templateRef"`
	HardwareRef string            `json:"hardwareRef"`
	MAC         string            `json:"mac"`
	HardwareMap map[string]string `json:"hardwareMap"`
}

// Create provisions a new Workflow object in Kubernetes.
func (h *WorkflowHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || req.TemplateRef == "" || req.HardwareRef == "" {
		respond.Error(w, http.StatusBadRequest, "name, templateRef, and hardwareRef are required")
		return
	}

	hwMap := req.HardwareMap
	if hwMap == nil && req.MAC != "" {
		hwMap = map[string]string{"device_1": req.MAC}
	}

	wf := &tinkv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{Name: req.Name, Namespace: h.namespace},
		Spec: tinkv1.WorkflowSpec{
			TemplateRef: req.TemplateRef,
			HardwareRef: req.HardwareRef,
			HardwareMap: hwMap,
		},
	}
	if err := h.client.Create(r.Context(), wf); err != nil {
		log.Printf("workflow create: %v", err)
		respond.Error(w, http.StatusConflict, "failed to create workflow")
		return
	}
	respond.JSON(w, http.StatusCreated, wf)
}

// Patch applies a merge-patch to an existing Workflow object.
func (h *WorkflowHandler) Patch(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]

	body, err := io.ReadAll(r.Body)
	if err != nil {
		respond.Error(w, http.StatusBadRequest, "failed to read body")
		return
	}

	var wf tinkv1.Workflow
	if err := h.client.Get(r.Context(), types.NamespacedName{Name: name, Namespace: h.namespace}, &wf); err != nil {
		respond.Error(w, http.StatusNotFound, "workflow not found")
		return
	}
	if err := h.client.Patch(r.Context(), &wf, client.RawPatch(types.MergePatchType, body)); err != nil {
		respond.Error(w, http.StatusInternalServerError, "patch failed")
		return
	}
	respond.JSON(w, http.StatusOK, wf)
}

// Delete removes a Workflow object by name.
func (h *WorkflowHandler) Delete(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	var wf tinkv1.Workflow
	if err := h.client.Get(r.Context(), types.NamespacedName{Name: name, Namespace: h.namespace}, &wf); err != nil {
		if apierrors.IsNotFound(err) {
			respond.Error(w, http.StatusNotFound, "workflow not found")
		} else {
			respond.Error(w, http.StatusInternalServerError, "failed to get workflow")
		}
		return
	}
	if err := h.client.Delete(r.Context(), &wf); err != nil {
		respond.Error(w, http.StatusInternalServerError, "delete failed")
		return
	}
	respond.JSON(w, http.StatusOK, map[string]string{"deleted": name})
}

// Reprovision deletes the existing workflow and recreates it with the same spec.
func (h *WorkflowHandler) Reprovision(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	var existing tinkv1.Workflow
	if err := h.client.Get(r.Context(), types.NamespacedName{Name: name, Namespace: h.namespace}, &existing); err != nil {
		respond.Error(w, http.StatusNotFound, "workflow not found")
		return
	}

	savedSpec := existing.Spec
	if err := h.client.Delete(r.Context(), &existing); err != nil {
		respond.Error(w, http.StatusInternalServerError, "failed to delete workflow for reprovision")
		return
	}

	newWF := &tinkv1.Workflow{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: h.namespace},
		Spec:       savedSpec,
	}
	if err := h.client.Create(r.Context(), newWF); err != nil {
		log.Printf("workflow reprovision create: %v", err)
		respond.Error(w, http.StatusInternalServerError, "failed to recreate workflow")
		return
	}
	respond.JSON(w, http.StatusCreated, newWF)
}

// WorkflowAction is the per-action status extracted from workflow status tasks.
type WorkflowAction struct {
	Name      string `json:"name"`
	Image     string `json:"image"`
	Status    string `json:"status"`
	Seconds   int64  `json:"seconds"`
	Message   string `json:"message"`
	StartedAt string `json:"startedAt,omitempty"`
}

// WorkflowTasksResponse is the response body for the tasks endpoint.
type WorkflowTasksResponse struct {
	WorkflowName string           `json:"workflowName"`
	State        string           `json:"state"`
	Tasks        []WorkflowAction `json:"tasks"`
}

// Tasks returns per-action status for a workflow, flattened from status.tasks.
func (h *WorkflowHandler) Tasks(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	var wf tinkv1.Workflow
	if err := h.client.Get(r.Context(), types.NamespacedName{Name: name, Namespace: h.namespace}, &wf); err != nil {
		if apierrors.IsNotFound(err) {
			respond.Error(w, http.StatusNotFound, "workflow not found")
		} else {
			respond.Error(w, http.StatusInternalServerError, "failed to get workflow")
		}
		return
	}

	actions := []WorkflowAction{}
	for _, task := range wf.Status.Tasks {
		for _, a := range task.Actions {
			startedAt := ""
			if a.StartedAt != nil {
				startedAt = a.StartedAt.UTC().Format(time.RFC3339)
			}
			actions = append(actions, WorkflowAction{
				Name:      a.Name,
				Image:     a.Image,
				Status:    string(a.Status),
				Seconds:   a.Seconds,
				Message:   a.Message,
				StartedAt: startedAt,
			})
		}
	}
	respond.JSON(w, http.StatusOK, WorkflowTasksResponse{
		WorkflowName: name,
		State:        string(wf.Status.State),
		Tasks:        actions,
	})
}

// Watch streams workflow state changes as Server-Sent Events.
func (h *WorkflowHandler) Watch(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]

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

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var lastState string
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			var wf tinkv1.Workflow
			if err := h.client.Get(r.Context(), types.NamespacedName{Name: name, Namespace: h.namespace}, &wf); err != nil {
				if apierrors.IsNotFound(err) {
					fmt.Fprintf(w, "event: error\ndata: {\"error\":\"workflow not found\"}\n\n")
					fmt.Fprintf(w, "event: done\ndata: {\"state\":\"error\"}\n\n")
					flusher.Flush()
					return
				}
				fmt.Fprintf(w, "event: error\ndata: {\"error\":\"failed to get workflow\"}\n\n")
				fmt.Fprintf(w, "event: done\ndata: {\"state\":\"error\"}\n\n")
				flusher.Flush()
				return
			}
			state := string(wf.Status.State)
			if state != lastState {
				lastState = state
				data, _ := json.Marshal(map[string]interface{}{
					"name":  name,
					"state": state,
				})
				fmt.Fprintf(w, "event: state_change\ndata: %s\n\n", data)
				flusher.Flush()
			}
			if state == "STATE_SUCCESS" || state == "STATE_FAILED" || state == "STATE_TIMEOUT" {
				fmt.Fprintf(w, "event: done\ndata: {\"state\":\"%s\"}\n\n", state)
				flusher.Flush()
				return
			}
		}
	}
}
