package enroll_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/KontangoOSS/schmutz/internal/enroll"
)

func TestNeedsEnrollment_noFile(t *testing.T) {
	dir := t.TempDir()
	if !enroll.NeedsEnrollment(filepath.Join(dir, "identity.json")) {
		t.Error("expected true when file missing")
	}
}

func TestNeedsEnrollment_fileExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.json")
	os.WriteFile(path, []byte(`{}`), 0600)
	if enroll.NeedsEnrollment(path) {
		t.Error("expected false when file exists")
	}
}

func TestRegister_approved(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"status": "approved",
			"jwt":    "eyJFAKE",
			"slug":   "test-slug",
		})
	}))
	defer srv.Close()

	info := enroll.DeviceInfo{
		Hostname:    "host",
		OS:          "linux",
		Arch:        "amd64",
		Platform:    "lxc",
		Fingerprint: "fp123",
	}
	jwt, slug, err := enroll.Register(context.Background(), srv.URL, info)
	if err != nil {
		t.Fatal(err)
	}
	if jwt != "eyJFAKE" {
		t.Errorf("jwt: got %q", jwt)
	}
	if slug != "test-slug" {
		t.Errorf("slug: got %q", slug)
	}
}

func TestRegister_banned(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "busy",
			"message": "banned",
		})
	}))
	defer srv.Close()

	info := enroll.DeviceInfo{Hostname: "host", OS: "linux", Arch: "amd64", Fingerprint: "fp"}
	_, _, err := enroll.Register(context.Background(), srv.URL, info)
	if err == nil {
		t.Fatal("expected error for banned device")
	}
}

func TestEnrollJWT_emptyToken(t *testing.T) {
	err := enroll.EnrollJWT("", "/tmp/should-not-exist.json")
	if err == nil {
		t.Fatal("expected error for empty jwt")
	}
}
