package join

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type BanChecker interface {
	IsBanned(identifier string) bool
}

type OSChecker interface {
	Resolve(os, version string) (ref string, ok bool)
}

type IdentityCreator interface {
	CreateIdentity(reg *Registration) (zitiJWT string, err error)
}

type Store interface {
	Put(reg *Registration) error
	LookupByHostname(hostname string) (id, nickname string, exists bool)
}

type Handler struct {
	Store      Store
	BanChecker BanChecker
	OSChecker  OSChecker
	Identity   IdentityCreator
}

const maxBodyBytes = 8 * 1024

var (
	hostnameRe  = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,62}$`)
	macAddrRe   = regexp.MustCompile(`^([0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}$`)
	hexRe       = regexp.MustCompile(`^[0-9a-fA-F]+$`)
	printableRe = regexp.MustCompile(`^[\x20-\x7E]+$`)
	validOS     = map[string]bool{"linux": true, "darwin": true, "windows": true}
	validArch   = map[string]bool{"amd64": true, "arm64": true, "arm": true, "386": true, "": true}
)

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/register") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	body := http.MaxBytesReader(w, r.Body, maxBodyBytes)
	var req Request
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		errJSON(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Source IP
	sourceIP := r.RemoteAddr
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		sourceIP = strings.TrimSpace(strings.Split(fwd, ",")[0])
	}
	if idx := strings.LastIndex(sourceIP, ":"); idx > 0 {
		sourceIP = sourceIP[:idx]
	}

	// Validate
	req.Hostname = strings.TrimSpace(req.Hostname)
	req.OS = strings.TrimSpace(strings.ToLower(req.OS))
	req.OSVersion = strings.TrimSpace(req.OSVersion)
	req.Arch = strings.TrimSpace(strings.ToLower(req.Arch))

	if !hostnameRe.MatchString(req.Hostname) {
		errJSON(w, "invalid hostname", http.StatusBadRequest)
		return
	}
	if !validOS[req.OS] {
		errJSON(w, "invalid os", http.StatusBadRequest)
		return
	}
	if req.OSVersion == "" || len(req.OSVersion) > 128 || !printableRe.MatchString(req.OSVersion) {
		errJSON(w, "invalid os_version", http.StatusBadRequest)
		return
	}
	if !validArch[req.Arch] {
		errJSON(w, "invalid arch", http.StatusBadRequest)
		return
	}

	// Sanitize optional fields
	req.CPUInfo = trunc(req.CPUInfo, 128)
	req.KernelVersion = trunc(req.KernelVersion, 128)
	req.MachineID = truncHex(req.MachineID, 64)
	req.SerialNumber = trunc(req.SerialNumber, 64)
	req.HardwareHash = truncHex(req.HardwareHash, 64)
	req.Timezone = trunc(req.Timezone, 64)

	if req.Timezone != "" {
		if _, err := time.LoadLocation(req.Timezone); err != nil {
			req.Timezone = ""
		}
	}

	if len(req.MACAddrs) > 16 {
		req.MACAddrs = req.MACAddrs[:16]
	}
	macs := make([]string, 0, len(req.MACAddrs))
	for _, m := range req.MACAddrs {
		m = strings.TrimSpace(strings.ToLower(m))
		if macAddrRe.MatchString(m) {
			macs = append(macs, m)
		}
	}
	req.MACAddrs = macs

	// Check for duplicate registration — token is one-time-display
	if id, nickname, exists := h.Store.LookupByHostname(req.Hostname); exists {
		log.Printf("register: duplicate hostname %s (id=%s, nickname=%s) from %s", req.Hostname, id, nickname, sourceIP)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{
			ID:       id,
			Nickname: nickname,
			Message:  "already registered — use your existing token",
		})
		return
	}

	// OS check
	var osRef string
	if h.OSChecker != nil {
		ref, ok := h.OSChecker.Resolve(req.OS, req.OSVersion)
		if !ok {
			errJSON(w, "unsupported platform", http.StatusUnprocessableEntity)
			return
		}
		osRef = ref
	}

	// Ban check — same error for everything
	if h.isBanned(req) {
		log.Printf("register: banned: %s from %s", req.Hostname, sourceIP)
		errJSON(w, "registration unavailable", http.StatusServiceUnavailable)
		return
	}

	// Build registration
	reg := NewRegistration(req.Hostname)
	reg.SourceIP = sourceIP
	reg.Timezone = req.Timezone
	reg.OS = req.OS
	reg.OSVersion = req.OSVersion
	reg.OSRef = osRef
	reg.Arch = req.Arch
	reg.HardwareHash = req.HardwareHash
	reg.MACAddrs = req.MACAddrs
	reg.CPUInfo = req.CPUInfo
	reg.MachineID = req.MachineID
	reg.SerialNumber = req.SerialNumber
	reg.KernelVersion = req.KernelVersion

	if req.BrowserRaw != nil {
		bd := req.BrowserRaw
		bd.sanitize()
		reg.Browser = bd
	}

	source := "cli"
	if reg.Browser != nil {
		source = "web"
	}
	log.Printf("register: %s/%s (os=%s arch=%s via=%s) from %s",
		req.Hostname, reg.Nickname, osRef, req.Arch, source, sourceIP)

	// Create identity
	jwt, err := h.Identity.CreateIdentity(reg)
	if err != nil {
		log.Printf("register: identity failed for %s: %v", req.Hostname, err)
		errJSON(w, "registration failed", http.StatusInternalServerError)
		return
	}

	// Store
	if err := h.Store.Put(reg); err != nil {
		log.Printf("register: store failed for %s: %v", req.Hostname, err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(Response{
		ID:       reg.ID,
		Nickname: reg.Nickname,
		ZitiJWT:  jwt,
		Message:  "registered",
	})
}

func (h *Handler) isBanned(req Request) bool {
	if h.BanChecker.IsBanned(req.Hostname) {
		return true
	}
	if req.HardwareHash != "" && h.BanChecker.IsBanned(req.HardwareHash) {
		return true
	}
	for _, m := range req.MACAddrs {
		if h.BanChecker.IsBanned(m) {
			return true
		}
	}
	return false
}

func (bd *BrowserData) sanitize() {
	bd.CanvasHash = truncHex(bd.CanvasHash, 64)
	bd.WebGLHash = truncHex(bd.WebGLHash, 64)
	bd.AudioHash = truncHex(bd.AudioHash, 64)
	bd.CompositeHash = truncHex(bd.CompositeHash, 64)
	bd.GPU = trunc(bd.GPU, 256)
	bd.Screen = trunc(bd.Screen, 32)
	bd.Language = trunc(bd.Language, 16)
	bd.UserAgent = trunc(bd.UserAgent, 512)
	bd.Platform = trunc(bd.Platform, 64)
	bd.STUNIP = trunc(bd.STUNIP, 45)
	if len(bd.LocalIPs) > 8 {
		bd.LocalIPs = bd.LocalIPs[:8]
	}
}

func errJSON(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	io.WriteString(w, `{"message":"`+msg+`"}`)
}

func trunc(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" || !printableRe.MatchString(s) {
		return ""
	}
	if len(s) > max {
		return s[:max]
	}
	return s
}

func truncHex(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" || !hexRe.MatchString(s) {
		return ""
	}
	if len(s) > max {
		return s[:max]
	}
	return s
}
