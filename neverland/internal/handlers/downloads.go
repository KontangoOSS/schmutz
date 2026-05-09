package handlers

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"git.konoss.org/kore/schmutz/neverland/internal/respond"
)

// DownloadHandler serves bootable images from a single directory.
type DownloadHandler struct {
	dir string
}

// NewDownloadHandler constructs a DownloadHandler scoped to dir.
func NewDownloadHandler(dir string) *DownloadHandler {
	return &DownloadHandler{dir: dir}
}

// DownloadInfo is one entry in the listing.
type DownloadInfo struct {
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	URL      string    `json:"url"`
}

// List returns all files in the downloads dir as JSON.
func (h *DownloadHandler) List(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(h.dir)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "failed to read downloads directory")
		return
	}
	items := make([]DownloadInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		items = append(items, DownloadInfo{
			Name:     e.Name(),
			Size:     info.Size(),
			Modified: info.ModTime(),
			URL:      "/api/v1/downloads/" + e.Name(),
		})
	}
	respond.JSON(w, http.StatusOK, map[string]interface{}{"items": items})
}

// Get serves a single file by name.
func (h *DownloadHandler) Get(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["filename"]
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..") {
		respond.Error(w, http.StatusBadRequest, "invalid filename")
		return
	}
	full := filepath.Join(h.dir, name)
	clean, err := filepath.Abs(full)
	if err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid path")
		return
	}
	dirAbs, _ := filepath.Abs(h.dir)
	if !strings.HasPrefix(clean, dirAbs+string(filepath.Separator)) && clean != dirAbs {
		respond.Error(w, http.StatusBadRequest, "outside downloads directory")
		return
	}
	if _, err := os.Stat(clean); os.IsNotExist(err) {
		respond.Error(w, http.StatusNotFound, "file not found")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+name+"\"")
	http.ServeFile(w, r, clean)
}
