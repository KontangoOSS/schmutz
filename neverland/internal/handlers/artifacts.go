package handlers

import (
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"git.konoss.org/kore/schmutz/neverland/internal/respond"
)

type ArtifactHandler struct {
	artifactsPath string
	nginxURL      string
}

func NewArtifactHandler(artifactsPath, nginxURL string) *ArtifactHandler {
	return &ArtifactHandler{artifactsPath: artifactsPath, nginxURL: nginxURL}
}

type ArtifactInfo struct {
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	URL      string    `json:"url"`
}

func (h *ArtifactHandler) List(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(h.artifactsPath)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "failed to read artifacts directory")
		return
	}

	items := make([]ArtifactInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		items = append(items, ArtifactInfo{
			Name:     e.Name(),
			Size:     info.Size(),
			Modified: info.ModTime(),
			URL:      h.nginxURL + "/" + e.Name(),
		})
	}
	respond.JSON(w, http.StatusOK, map[string]interface{}{"items": items})
}

func (h *ArtifactHandler) Upload(w http.ResponseWriter, r *http.Request) {
	// 10 GB max upload size
	r.Body = http.MaxBytesReader(w, r.Body, 10<<30)

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		respond.Error(w, http.StatusBadRequest, "failed to parse multipart form: "+err.Error())
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		respond.Error(w, http.StatusBadRequest, "missing 'file' field in form")
		return
	}
	defer file.Close()

	filename := filepath.Base(header.Filename)
	if filename == "" || strings.Contains(filename, "..") {
		respond.Error(w, http.StatusBadRequest, "invalid filename")
		return
	}

	dest := filepath.Join(h.artifactsPath, filename)
	out, err := os.Create(dest)
	if err != nil {
		log.Printf("artifact upload create: %v", err)
		respond.Error(w, http.StatusInternalServerError, "failed to create file")
		return
	}
	defer out.Close()

	n, err := io.Copy(out, file)
	if err != nil {
		log.Printf("artifact upload copy: %v", err)
		respond.Error(w, http.StatusInternalServerError, "failed to write file")
		return
	}

	respond.JSON(w, http.StatusCreated, map[string]interface{}{
		"name": filename,
		"size": n,
		"url":  h.nginxURL + "/" + filename,
	})
}

func (h *ArtifactHandler) Delete(w http.ResponseWriter, r *http.Request) {
	filename := filepath.Base(mux.Vars(r)["filename"])
	if strings.Contains(filename, "..") {
		respond.Error(w, http.StatusBadRequest, "invalid filename")
		return
	}

	dest := filepath.Join(h.artifactsPath, filename)
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		respond.Error(w, http.StatusNotFound, "artifact not found")
		return
	}

	if err := os.Remove(dest); err != nil {
		respond.Error(w, http.StatusInternalServerError, "failed to delete artifact")
		return
	}
	respond.JSON(w, http.StatusOK, map[string]string{"deleted": filename})
}

// Download proxies the file download through nginx.
func (h *ArtifactHandler) Download(w http.ResponseWriter, r *http.Request) {
	filename := filepath.Base(mux.Vars(r)["filename"])
	if strings.Contains(filename, "..") {
		respond.Error(w, http.StatusBadRequest, "invalid filename")
		return
	}

	target := h.nginxURL + "/" + filename
	resp, err := http.Get(target) //nolint:noctx
	if err != nil {
		respond.Error(w, http.StatusBadGateway, "failed to proxy from nginx")
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.Header().Set("Content-Length", resp.Header.Get("Content-Length"))
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
