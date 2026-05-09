package handlers

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/template"

	"github.com/gorilla/mux"
	"git.konoss.org/kore/schmutz/neverland/internal/respond"
)
// io is used by proxyFile

// hookVariants lists the available Hook OS variants served by tink-stack nginx.
// Each entry maps a short variant name to its kernel and initrd filenames.
var hookVariants = map[string]hookFiles{
	"x86_64":             {Kernel: "vmlinuz-x86_64", Initrd: "initramfs-x86_64"},
	"aarch64":            {Kernel: "vmlinuz-aarch64", Initrd: "initramfs-aarch64"},
	"latest-lts-x86_64":  {Kernel: "vmlinuz-latest-lts-x86_64", Initrd: "initramfs-latest-lts-x86_64"},
	"latest-lts-aarch64": {Kernel: "vmlinuz-latest-lts-aarch64", Initrd: "initramfs-latest-lts-aarch64"},
}

// archAliases maps iPXE-reported ${buildarch} values onto canonical hookVariants
// keys. Legacy BIOS iPXE binaries report "i386" even on x86_64 hardware (the
// firmware itself is 32-bit). amd64/x86_64 are synonyms; arm64/aarch64 are
// synonyms.
var archAliases = map[string]string{
	"i386":  "x86_64",
	"amd64": "x86_64",
	"arm64": "aarch64",
}

// resolveVariant returns the canonical hookVariants key for an incoming variant
// string, applying the archAliases map. Returns the input unchanged if no alias
// matches — the existing 404 path then handles unknown variants.
func resolveVariant(v string) string {
	if alias, ok := archAliases[v]; ok {
		return alias
	}
	return v
}

type hookFiles struct {
	Kernel string
	Initrd string
}

// HookHandler serves Hook OS kernel/initrd files and iPXE scripts.
// Files are proxied from the tink-stack nginx. iPXE scripts embed an OTT tag
// so schmutz-controller can fingerprint the download at boot time.
type HookHandler struct {
	nginxURL       string // e.g. http://tink-stack.tink-system.svc.cluster.local:8080
	enrollURL      string // e.g. https://join.kontango.net — schmutz-controller /api/v1/start
}

// NewHookHandler constructs a HookHandler.
func NewHookHandler(nginxURL, enrollURL string) *HookHandler {
	return &HookHandler{
		nginxURL:  strings.TrimRight(nginxURL, "/"),
		enrollURL: strings.TrimRight(enrollURL, "/"),
	}
}

// HookVariant describes a single available Hook OS variant.
type HookVariant struct {
	Variant   string `json:"variant"`
	KernelURL string `json:"kernel_url"`
	InitrdURL string `json:"initrd_url"`
	IPXEURL   string `json:"ipxe_url"`
}

// List returns all available Hook OS variants with their download URLs.
func (h *HookHandler) List(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if host == "" {
		host = "boot.kontango.net"
	}
	scheme := "https"
	if r.TLS == nil && !strings.HasPrefix(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "http"
	}
	base := fmt.Sprintf("%s://%s", scheme, host)

	variants := make([]HookVariant, 0, len(hookVariants))
	for name := range hookVariants {
		variants = append(variants, HookVariant{
			Variant:   name,
			KernelURL: fmt.Sprintf("%s/api/v1/hook/%s/kernel", base, name),
			InitrdURL: fmt.Sprintf("%s/api/v1/hook/%s/initrd", base, name),
			IPXEURL:   fmt.Sprintf("%s/api/v1/hook/%s/hook.ipxe", base, name),
		})
	}
	respond.JSON(w, http.StatusOK, map[string]interface{}{"items": variants})
}

// Kernel proxies the kernel file for the given variant from nginx.
// Forces Content-Type: application/octet-stream so upstream proxies (Caddy)
// don't apply gzip encoding to an already-binary file.
func (h *HookHandler) Kernel(w http.ResponseWriter, r *http.Request) {
	variant := resolveVariant(mux.Vars(r)["variant"])
	files, ok := hookVariants[variant]
	if !ok {
		respond.Error(w, http.StatusNotFound, "unknown hook variant")
		return
	}
	h.proxyBinary(w, r, files.Kernel)
}

// Initrd proxies the initramfs file for the given variant from nginx.
// Forces Content-Type: application/octet-stream — see Kernel().
func (h *HookHandler) Initrd(w http.ResponseWriter, r *http.Request) {
	variant := resolveVariant(mux.Vars(r)["variant"])
	files, ok := hookVariants[variant]
	if !ok {
		respond.Error(w, http.StatusNotFound, "unknown hook variant")
		return
	}
	h.proxyBinary(w, r, files.Initrd)
}

// IPXEScript returns a ready-to-use iPXE script for the given variant.
// The machine calls join.kontango.net/api/v1/start itself at boot time to obtain
// its own OTT — fingerprint is captured against the real machine, not neverland.
func (h *HookHandler) IPXEScript(w http.ResponseWriter, r *http.Request) {
	variant := resolveVariant(mux.Vars(r)["variant"])
	_, ok := hookVariants[variant]
	if !ok {
		respond.Error(w, http.StatusNotFound, "unknown hook variant")
		return
	}

	host := r.Host
	if host == "" {
		host = "boot.kontango.net"
	}
	scheme := "https"
	if r.TLS == nil && !strings.HasPrefix(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "http"
	}
	base := fmt.Sprintf("%s://%s", scheme, host)

	data := struct {
		KernelURL  string
		InitrdURL  string
		EnrollURL  string
		Variant    string
		TinkServer string
		SyslogHost string
	}{
		KernelURL:  fmt.Sprintf("%s/api/v1/hook/%s/kernel", base, variant),
		InitrdURL:  fmt.Sprintf("%s/api/v1/hook/%s/initrd", base, variant),
		EnrollURL:  h.enrollURL,
		Variant:    variant,
		TinkServer: "10.11.30.91:42113",
		SyslogHost: "10.11.30.91",
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := ipxeTemplate.Execute(w, data); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

// proxyFile fetches a file from the tink-stack nginx and streams it to the client.
// Preserves upstream Content-Type — used for the iPXE script proxy.
func (h *HookHandler) proxyFile(w http.ResponseWriter, r *http.Request, filename string) {
	target := h.nginxURL + "/" + filename
	resp, err := http.Get(target) //nolint:noctx
	if err != nil {
		respond.Error(w, http.StatusBadGateway, "failed to fetch hook file from nginx")
		return
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", cl)
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// proxyBinary fetches a binary file from nginx and streams it as
// application/octet-stream. The forced binary content-type prevents Caddy's
// `encode gzip` from compressing already-binary kernel/initrd files
// (gzipped binaries break iPXE's loader).
func (h *HookHandler) proxyBinary(w http.ResponseWriter, r *http.Request, filename string) {
	target := h.nginxURL + "/" + filename
	resp, err := http.Get(target) //nolint:noctx
	if err != nil {
		respond.Error(w, http.StatusBadGateway, "failed to fetch hook file from nginx")
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", cl)
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

var ipxeTemplate = template.Must(template.New("hook").Parse(`#!ipxe
echo Kontango Hook OS variant: {{.Variant}}
echo Fetching kernel...
kernel {{.KernelURL}} tink_worker_image=quay.io/tinkerbell/tink-worker:v0.12.1 grpc_authority={{.TinkServer}} tinkerbell_tls=false worker_id=${net0/mac} hw_addr=${net0/mac} facility=kontango syslog_host={{.SyslogHost}} modules=loop,squashfs,sd-mod,usb-storage intel_iommu=on iommu=pt initrd=initramfs-x86_64 console=tty0 console=ttyS0,115200
echo Fetching initramfs...
initrd {{.InitrdURL}}
echo Booting...
boot || echo Boot failed -- check kernel/initrd URLs
`))
