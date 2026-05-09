package handlers

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	rufio "github.com/tinkerbell/rufio/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"git.konoss.org/kore/schmutz/neverland/internal/respond"
)

// BMCHandler handles CRUD operations for Rufio Machine, Job, and Task resources.
type BMCHandler struct {
	client    client.Client
	namespace string
}

// NewBMCHandler constructs a BMCHandler.
func NewBMCHandler(c client.Client, namespace string) *BMCHandler {
	return &BMCHandler{client: c, namespace: namespace}
}

// ── Machines ──────────────────────────────────────────────────────────────────

// ListMachines returns all Machine objects in the configured namespace.
func (h *BMCHandler) ListMachines(w http.ResponseWriter, r *http.Request) {
	var list rufio.MachineList
	if err := h.client.List(r.Context(), &list, client.InNamespace(h.namespace)); err != nil {
		log.Printf("bmc machine list: %v", err)
		respond.Error(w, http.StatusInternalServerError, "failed to list machines")
		return
	}
	items := list.Items
	if items == nil {
		items = []rufio.Machine{}
	}
	respond.JSON(w, http.StatusOK, map[string]interface{}{"items": items})
}

// GetMachine returns a single Machine object by name.
func (h *BMCHandler) GetMachine(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	if !validK8sName(name) {
		respond.Error(w, http.StatusBadRequest, "invalid machine name")
		return
	}
	var m rufio.Machine
	if err := h.client.Get(r.Context(), types.NamespacedName{Name: name, Namespace: h.namespace}, &m); err != nil {
		if apierrors.IsNotFound(err) {
			respond.Error(w, http.StatusNotFound, "machine not found")
		} else {
			respond.Error(w, http.StatusInternalServerError, "failed to get machine")
		}
		return
	}
	respond.JSON(w, http.StatusOK, m)
}

// CreateMachineRequest is the request body for creating a Machine object.
type CreateMachineRequest struct {
	Name          string `json:"name"`
	Host          string `json:"host"`
	Port          int    `json:"port"`
	AuthSecretRef string `json:"authSecretRef"`
	InsecureTLS   bool   `json:"insecureTLS"`
}

// CreateMachine provisions a new Machine object in Kubernetes.
func (h *BMCHandler) CreateMachine(w http.ResponseWriter, r *http.Request) {
	var req CreateMachineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || req.Host == "" || req.AuthSecretRef == "" {
		respond.Error(w, http.StatusBadRequest, "name, host, and authSecretRef are required")
		return
	}
	if req.Port == 0 {
		req.Port = 623
	}

	m := &rufio.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Name,
			Namespace: h.namespace,
		},
		Spec: rufio.MachineSpec{
			Connection: rufio.Connection{
				Host: req.Host,
				Port: req.Port,
				AuthSecretRef: corev1.SecretReference{
					Name:      req.AuthSecretRef,
					Namespace: h.namespace,
				},
				InsecureTLS: req.InsecureTLS,
			},
		},
	}

	if err := h.client.Create(r.Context(), m); err != nil {
		log.Printf("bmc machine create: %v", err)
		respond.Error(w, http.StatusConflict, "failed to create machine (may already exist)")
		return
	}
	respond.JSON(w, http.StatusCreated, m)
}

// PatchMachine applies a merge-patch to an existing Machine object.
func (h *BMCHandler) PatchMachine(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	if !validK8sName(name) {
		respond.Error(w, http.StatusBadRequest, "invalid machine name")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		respond.Error(w, http.StatusBadRequest, "failed to read body")
		return
	}

	var m rufio.Machine
	if err := h.client.Get(r.Context(), types.NamespacedName{Name: name, Namespace: h.namespace}, &m); err != nil {
		if apierrors.IsNotFound(err) {
			respond.Error(w, http.StatusNotFound, "machine not found")
		} else {
			respond.Error(w, http.StatusInternalServerError, "failed to get machine")
		}
		return
	}

	if err := h.client.Patch(r.Context(), &m, client.RawPatch(types.MergePatchType, body)); err != nil {
		log.Printf("bmc machine patch: %v", err)
		respond.Error(w, http.StatusInternalServerError, "patch failed")
		return
	}
	respond.JSON(w, http.StatusOK, m)
}

// DeleteMachine removes a Machine object by name.
func (h *BMCHandler) DeleteMachine(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	if !validK8sName(name) {
		respond.Error(w, http.StatusBadRequest, "invalid machine name")
		return
	}
	var m rufio.Machine
	if err := h.client.Get(r.Context(), types.NamespacedName{Name: name, Namespace: h.namespace}, &m); err != nil {
		if apierrors.IsNotFound(err) {
			respond.Error(w, http.StatusNotFound, "machine not found")
		} else {
			respond.Error(w, http.StatusInternalServerError, "failed to get machine")
		}
		return
	}
	if err := h.client.Delete(r.Context(), &m); err != nil {
		log.Printf("bmc machine delete: %v", err)
		respond.Error(w, http.StatusInternalServerError, "delete failed")
		return
	}
	respond.JSON(w, http.StatusOK, map[string]string{"deleted": name})
}

// ── Jobs ──────────────────────────────────────────────────────────────────────

// ListJobs returns all Job objects in the configured namespace.
func (h *BMCHandler) ListJobs(w http.ResponseWriter, r *http.Request) {
	var list rufio.JobList
	if err := h.client.List(r.Context(), &list, client.InNamespace(h.namespace)); err != nil {
		log.Printf("bmc job list: %v", err)
		respond.Error(w, http.StatusInternalServerError, "failed to list jobs")
		return
	}
	items := list.Items
	if items == nil {
		items = []rufio.Job{}
	}
	respond.JSON(w, http.StatusOK, map[string]interface{}{"items": items})
}

// GetJob returns a single Job object by name.
func (h *BMCHandler) GetJob(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	if !validK8sName(name) {
		respond.Error(w, http.StatusBadRequest, "invalid job name")
		return
	}
	var j rufio.Job
	if err := h.client.Get(r.Context(), types.NamespacedName{Name: name, Namespace: h.namespace}, &j); err != nil {
		if apierrors.IsNotFound(err) {
			respond.Error(w, http.StatusNotFound, "job not found")
		} else {
			respond.Error(w, http.StatusInternalServerError, "failed to get job")
		}
		return
	}
	respond.JSON(w, http.StatusOK, j)
}

// CreateJob provisions a new Job object decoded directly from the request body.
func (h *BMCHandler) CreateJob(w http.ResponseWriter, r *http.Request) {
	var j rufio.Job
	if err := json.NewDecoder(r.Body).Decode(&j); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	j.Namespace = h.namespace

	if err := h.client.Create(r.Context(), &j); err != nil {
		log.Printf("bmc job create: %v", err)
		respond.Error(w, http.StatusConflict, "failed to create job (may already exist)")
		return
	}
	respond.JSON(w, http.StatusCreated, j)
}

// PatchJob applies a merge-patch to an existing Job object.
func (h *BMCHandler) PatchJob(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	if !validK8sName(name) {
		respond.Error(w, http.StatusBadRequest, "invalid job name")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		respond.Error(w, http.StatusBadRequest, "failed to read body")
		return
	}

	var j rufio.Job
	if err := h.client.Get(r.Context(), types.NamespacedName{Name: name, Namespace: h.namespace}, &j); err != nil {
		if apierrors.IsNotFound(err) {
			respond.Error(w, http.StatusNotFound, "job not found")
		} else {
			respond.Error(w, http.StatusInternalServerError, "failed to get job")
		}
		return
	}

	if err := h.client.Patch(r.Context(), &j, client.RawPatch(types.MergePatchType, body)); err != nil {
		log.Printf("bmc job patch: %v", err)
		respond.Error(w, http.StatusInternalServerError, "patch failed")
		return
	}
	respond.JSON(w, http.StatusOK, j)
}

// DeleteJob removes a Job object by name.
func (h *BMCHandler) DeleteJob(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	if !validK8sName(name) {
		respond.Error(w, http.StatusBadRequest, "invalid job name")
		return
	}
	var j rufio.Job
	if err := h.client.Get(r.Context(), types.NamespacedName{Name: name, Namespace: h.namespace}, &j); err != nil {
		if apierrors.IsNotFound(err) {
			respond.Error(w, http.StatusNotFound, "job not found")
		} else {
			respond.Error(w, http.StatusInternalServerError, "failed to get job")
		}
		return
	}
	if err := h.client.Delete(r.Context(), &j); err != nil {
		log.Printf("bmc job delete: %v", err)
		respond.Error(w, http.StatusInternalServerError, "delete failed")
		return
	}
	respond.JSON(w, http.StatusOK, map[string]string{"deleted": name})
}

// ── Tasks ─────────────────────────────────────────────────────────────────────

// ListTasks returns all Task objects in the configured namespace.
func (h *BMCHandler) ListTasks(w http.ResponseWriter, r *http.Request) {
	var list rufio.TaskList
	if err := h.client.List(r.Context(), &list, client.InNamespace(h.namespace)); err != nil {
		log.Printf("bmc task list: %v", err)
		respond.Error(w, http.StatusInternalServerError, "failed to list tasks")
		return
	}
	items := list.Items
	if items == nil {
		items = []rufio.Task{}
	}
	respond.JSON(w, http.StatusOK, map[string]interface{}{"items": items})
}

// GetTask returns a single Task object by name.
func (h *BMCHandler) GetTask(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	if !validK8sName(name) {
		respond.Error(w, http.StatusBadRequest, "invalid task name")
		return
	}
	var t rufio.Task
	if err := h.client.Get(r.Context(), types.NamespacedName{Name: name, Namespace: h.namespace}, &t); err != nil {
		if apierrors.IsNotFound(err) {
			respond.Error(w, http.StatusNotFound, "task not found")
		} else {
			respond.Error(w, http.StatusInternalServerError, "failed to get task")
		}
		return
	}
	respond.JSON(w, http.StatusOK, t)
}

// CreateTask provisions a new Task object decoded directly from the request body.
func (h *BMCHandler) CreateTask(w http.ResponseWriter, r *http.Request) {
	var t rufio.Task
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	t.Namespace = h.namespace

	if err := h.client.Create(r.Context(), &t); err != nil {
		log.Printf("bmc task create: %v", err)
		respond.Error(w, http.StatusConflict, "failed to create task (may already exist)")
		return
	}
	respond.JSON(w, http.StatusCreated, t)
}

// PatchTask applies a merge-patch to an existing Task object.
func (h *BMCHandler) PatchTask(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	if !validK8sName(name) {
		respond.Error(w, http.StatusBadRequest, "invalid task name")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		respond.Error(w, http.StatusBadRequest, "failed to read body")
		return
	}

	var t rufio.Task
	if err := h.client.Get(r.Context(), types.NamespacedName{Name: name, Namespace: h.namespace}, &t); err != nil {
		if apierrors.IsNotFound(err) {
			respond.Error(w, http.StatusNotFound, "task not found")
		} else {
			respond.Error(w, http.StatusInternalServerError, "failed to get task")
		}
		return
	}

	if err := h.client.Patch(r.Context(), &t, client.RawPatch(types.MergePatchType, body)); err != nil {
		log.Printf("bmc task patch: %v", err)
		respond.Error(w, http.StatusInternalServerError, "patch failed")
		return
	}
	respond.JSON(w, http.StatusOK, t)
}

// DeleteTask removes a Task object by name.
func (h *BMCHandler) DeleteTask(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	if !validK8sName(name) {
		respond.Error(w, http.StatusBadRequest, "invalid task name")
		return
	}
	var t rufio.Task
	if err := h.client.Get(r.Context(), types.NamespacedName{Name: name, Namespace: h.namespace}, &t); err != nil {
		if apierrors.IsNotFound(err) {
			respond.Error(w, http.StatusNotFound, "task not found")
		} else {
			respond.Error(w, http.StatusInternalServerError, "failed to get task")
		}
		return
	}
	if err := h.client.Delete(r.Context(), &t); err != nil {
		log.Printf("bmc task delete: %v", err)
		respond.Error(w, http.StatusInternalServerError, "delete failed")
		return
	}
	respond.JSON(w, http.StatusOK, map[string]string{"deleted": name})
}
