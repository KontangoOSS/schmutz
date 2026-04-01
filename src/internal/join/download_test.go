package join

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadAndExtractTarGz(t *testing.T) {
	content := []byte("#!/bin/sh\necho hello")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gz := gzip.NewWriter(w)
		tw := tar.NewWriter(gz)
		tw.WriteHeader(&tar.Header{Name: "test-binary", Size: int64(len(content)), Mode: 0755})
		tw.Write(content)
		tw.Close()
		gz.Close()
	}))
	defer srv.Close()

	dir := t.TempDir()
	if err := downloadAndExtractTarGz(srv.URL, dir, "test-binary"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "test-binary"))
	if string(data) != string(content) {
		t.Errorf("got %q, want %q", data, content)
	}
}

func TestDownloadAndExtractTarGz_MissingBinary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gz := gzip.NewWriter(w)
		tw := tar.NewWriter(gz)
		tw.WriteHeader(&tar.Header{Name: "wrong", Size: 5, Mode: 0755})
		tw.Write([]byte("hello"))
		tw.Close()
		gz.Close()
	}))
	defer srv.Close()
	if err := downloadAndExtractTarGz(srv.URL, t.TempDir(), "expected"); err == nil {
		t.Fatal("expected error")
	}
}

func TestDownloadAndExtractTarGz_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()
	if err := downloadAndExtractTarGz(srv.URL, t.TempDir(), "binary"); err == nil {
		t.Fatal("expected error")
	}
}

func TestDownloadAndExtractZip(t *testing.T) {
	content := []byte("hello from zip")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tmp, _ := os.CreateTemp("", "*.zip")
		zw := zip.NewWriter(tmp)
		fw, _ := zw.Create("test.exe")
		fw.Write(content)
		zw.Close()
		tmp.Close()
		defer os.Remove(tmp.Name())
		http.ServeFile(w, r, tmp.Name())
	}))
	defer srv.Close()

	dir := t.TempDir()
	if err := downloadAndExtractZip(srv.URL, dir, "test.exe"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "test.exe"))
	if string(data) != string(content) {
		t.Errorf("got %q, want %q", data, content)
	}
}
