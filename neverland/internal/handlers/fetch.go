package handlers

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"git.konoss.org/kore/schmutz/neverland/internal/respond"
)

// FetchStatus represents the state of an in-progress artifact fetch.
type FetchStatus string

const (
	FetchPending     FetchStatus = "pending"
	FetchDownloading FetchStatus = "downloading"
	FetchConverting  FetchStatus = "converting"
	FetchCompressing FetchStatus = "compressing"
	FetchDone        FetchStatus = "done"
	FetchError       FetchStatus = "error"
)

// FetchJob tracks a single fetch+convert operation.
type FetchJob struct {
	ID        string      `json:"id"`
	URL       string      `json:"url"`
	Name      string      `json:"name"`
	Status    FetchStatus `json:"status"`
	Message   string      `json:"message,omitempty"`
	OutputURL string      `json:"outputURL,omitempty"`
	StartedAt time.Time   `json:"startedAt"`
	DoneAt    *time.Time  `json:"doneAt,omitempty"`
}

// FetchRequest is the POST body for starting a fetch job.
type FetchRequest struct {
	URL  string `json:"url"`
	Name string `json:"name"`
}

// fetchStore holds all in-flight and completed fetch jobs.
type fetchStore struct {
	mu   sync.RWMutex
	jobs map[string]*FetchJob
}

var globalFetchStore = &fetchStore{jobs: make(map[string]*FetchJob)}

func (s *fetchStore) add(j *FetchJob) {
	s.mu.Lock()
	s.jobs[j.ID] = j
	s.mu.Unlock()
}

func (s *fetchStore) get(id string) (*FetchJob, bool) {
	s.mu.RLock()
	j, ok := s.jobs[id]
	s.mu.RUnlock()
	return j, ok
}

func (s *fetchStore) update(id string, fn func(*FetchJob)) {
	s.mu.Lock()
	if j, ok := s.jobs[id]; ok {
		fn(j)
	}
	s.mu.Unlock()
}

// FetchHandler handles async artifact fetch+convert jobs.
type FetchHandler struct {
	artifactsPath string
	nginxURL      string
}

// NewFetchHandler constructs a FetchHandler.
func NewFetchHandler(artifactsPath, nginxURL string) *FetchHandler {
	return &FetchHandler{artifactsPath: artifactsPath, nginxURL: nginxURL}
}

// Start accepts a URL, kicks off a background fetch+convert goroutine, and returns a job ID.
func (h *FetchHandler) Start(w http.ResponseWriter, r *http.Request) {
	var req FetchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.URL == "" {
		respond.Error(w, http.StatusBadRequest, "url is required")
		return
	}

	// Derive output filename: caller can override, otherwise we guess from the URL.
	name := sanitizeName(req.Name)
	if name == "" {
		name = sanitizeName(filepath.Base(req.URL))
	}
	if name == "" {
		respond.Error(w, http.StatusBadRequest, "could not derive output filename — provide name")
		return
	}
	// Always end up as .raw.gz for Tinkerbell compatibility.
	name = ensureRawGzSuffix(name)

	job := &FetchJob{
		ID:        uuid.New().String(),
		URL:       req.URL,
		Name:      name,
		Status:    FetchPending,
		StartedAt: time.Now(),
	}
	globalFetchStore.add(job)

	go h.run(job)

	respond.JSON(w, http.StatusAccepted, job)
}

// Get returns the current status of a fetch job.
func (h *FetchHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	job, ok := globalFetchStore.get(id)
	if !ok {
		respond.Error(w, http.StatusNotFound, "job not found")
		return
	}
	respond.JSON(w, http.StatusOK, job)
}

// List returns all fetch jobs (recent + in-flight).
func (h *FetchHandler) List(w http.ResponseWriter, r *http.Request) {
	globalFetchStore.mu.RLock()
	jobs := make([]*FetchJob, 0, len(globalFetchStore.jobs))
	for _, j := range globalFetchStore.jobs {
		jobs = append(jobs, j)
	}
	globalFetchStore.mu.RUnlock()
	respond.JSON(w, http.StatusOK, map[string]interface{}{"items": jobs})
}

// run executes the fetch → convert → compress pipeline for a job.
func (h *FetchHandler) run(job *FetchJob) {
	setStatus := func(s FetchStatus, msg string) {
		globalFetchStore.update(job.ID, func(j *FetchJob) {
			j.Status = s
			j.Message = msg
		})
		log.Printf("[fetch %s] %s: %s", job.ID, s, msg)
	}

	fail := func(msg string) {
		now := time.Now()
		globalFetchStore.update(job.ID, func(j *FetchJob) {
			j.Status = FetchError
			j.Message = msg
			j.DoneAt = &now
		})
		log.Printf("[fetch %s] error: %s", job.ID, msg)
	}

	// ── 1. Download ───────────────────────────────────────────────────────────

	setStatus(FetchDownloading, "downloading "+job.URL)

	resp, err := http.Get(job.URL) //nolint:noctx
	if err != nil {
		fail("download failed: " + err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fail(fmt.Sprintf("download returned HTTP %d", resp.StatusCode))
		return
	}

	// Write to a temp file first so we can inspect the format.
	tmp, err := os.CreateTemp(h.artifactsPath, ".fetch-*")
	if err != nil {
		fail("failed to create temp file: " + err.Error())
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		fail("download interrupted: " + err.Error())
		return
	}
	tmp.Close()

	// ── 2. Detect format and convert if needed ─────────────────────────────────

	srcPath := tmpPath
	needsConvert := isQcow2(tmpPath)

	if needsConvert {
		setStatus(FetchConverting, "converting qcow2 → raw")

		rawPath := tmpPath + ".raw"
		defer os.Remove(rawPath)

		cmd := exec.Command("qemu-img", "convert", "-f", "qcow2", "-O", "raw", srcPath, rawPath)
		if out, err := cmd.CombinedOutput(); err != nil {
			fail("qemu-img convert failed: " + err.Error() + " — " + string(out))
			return
		}
		srcPath = rawPath
	}

	// ── 3. Gzip compress ──────────────────────────────────────────────────────

	setStatus(FetchCompressing, "compressing → raw.gz")

	destPath := filepath.Join(h.artifactsPath, job.Name)
	outFile, err := os.Create(destPath)
	if err != nil {
		fail("failed to create output file: " + err.Error())
		return
	}

	gz := gzip.NewWriter(outFile)
	srcFile, err := os.Open(srcPath)
	if err != nil {
		outFile.Close()
		fail("failed to open source for compression: " + err.Error())
		return
	}

	if _, err := io.Copy(gz, srcFile); err != nil {
		srcFile.Close()
		gz.Close()
		outFile.Close()
		os.Remove(destPath)
		fail("compression failed: " + err.Error())
		return
	}
	srcFile.Close()
	gz.Close()
	outFile.Close()

	// ── 4. Done ───────────────────────────────────────────────────────────────

	now := time.Now()
	outputURL := job.URL // set below
	outputURL = ""
	globalFetchStore.update(job.ID, func(j *FetchJob) {
		j.Status = FetchDone
		j.Message = "ready"
		j.DoneAt = &now
		j.OutputURL = fmt.Sprintf("%s/%s", strings.TrimRight(h.nginxURL, "/"), job.Name)
		outputURL = j.OutputURL
	})
	log.Printf("[fetch %s] done → %s", job.ID, outputURL)
}

// isQcow2 checks the magic bytes of a file to detect qcow2 format.
// qcow2 files start with magic "QFI\xfb" (0x514649fb).
func isQcow2(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	magic := make([]byte, 4)
	if _, err := io.ReadFull(f, magic); err != nil {
		return false
	}
	return magic[0] == 0x51 && magic[1] == 0x46 && magic[2] == 0x49 && magic[3] == 0xfb
}

// sanitizeName strips path components and unsafe characters from a filename.
func sanitizeName(name string) string {
	name = filepath.Base(name)
	if name == "." || name == "/" {
		return ""
	}
	// Strip query strings
	if i := strings.IndexByte(name, '?'); i >= 0 {
		name = name[:i]
	}
	return name
}

// ensureRawGzSuffix normalises the output filename to end in .raw.gz.
// .qcow2 → .raw.gz, .raw → .raw.gz, .img → .raw.gz, .raw.gz → unchanged.
func ensureRawGzSuffix(name string) string {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".raw.gz") {
		return name
	}
	for _, ext := range []string{".qcow2", ".raw", ".img", ".img.xz", ".iso"} {
		if strings.HasSuffix(lower, ext) {
			return name[:len(name)-len(ext)] + ".raw.gz"
		}
	}
	return name + ".raw.gz"
}
