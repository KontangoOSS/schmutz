package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
	tinkv1 "github.com/tinkerbell/tink/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"git.konoss.org/kore/schmutz/neverland/internal/beacon"
	"git.konoss.org/kore/schmutz/neverland/internal/respond"
)

// BeaconStore is the contract Postgres satisfies in production and a fake satisfies in tests.
type BeaconStore interface {
	Insert(ctx context.Context, r beacon.Row) error
}

// BeaconConfig groups static config for the handler.
type BeaconConfig struct {
	BootBaseURL string // e.g. "https://boot.kontango.net"
	Namespace   string // tink-system
}

// BeaconHandler accepts boot beacons and reconciles Hardware CRs.
type BeaconHandler struct {
	k8s   client.Client
	store BeaconStore
	cfg   BeaconConfig
}

// NewBeaconHandler constructs a BeaconHandler.
func NewBeaconHandler(c client.Client, s BeaconStore, cfg BeaconConfig) *BeaconHandler {
	return &BeaconHandler{k8s: c, store: s, cfg: cfg}
}

// BeaconRequest is the JSON body of POST /api/v1/beacon.
type BeaconRequest struct {
	Level     string            `json:"level"`
	// SessionID is accepted for forward compatibility but currently ignored —
	// sessions are derived from the Hardware CR UID + MAC server-side. Phase 2
	// may use this as a hint for cross-level correlation.
	SessionID string            `json:"session_id,omitempty"`
	FP        BeaconFingerprint `json:"fingerprint"`
}

// BeaconFingerprint holds the iPXE-collectable subset (Hook level adds dmi/cpu/etc but is JSON-passthrough).
type BeaconFingerprint struct {
	MAC       string `json:"mac"`
	IP        string `json:"ip,omitempty"`
	Arch      string `json:"arch,omitempty"`
	Platform  string `json:"platform,omitempty"`
	UserClass string `json:"userclass,omitempty"`
}

// BeaconResponse is what the iPXE script consumes.
type BeaconResponse struct {
	SessionID string `json:"session_id"`
	ShortCode string `json:"short_code"`
	ClaimURL  string `json:"claim_url"`
	SkipClaim bool   `json:"skip_claim"`
	WatchURL  string `json:"watch_url"`
}

// Handle is the GET/POST /api/v1/beacon entrypoint.
// GET is used by iPXE imgfetch (query-string parameters, level defaults to "ipxe").
// POST is used by Hook OS (JSON body, level must be "ipxe" or "hook").
func (h *BeaconHandler) Handle(w http.ResponseWriter, r *http.Request) {
	var req BeaconRequest

	switch r.Method {
	case http.MethodGet:
		// iPXE imgfetch: query-string parameters only, level defaults to "ipxe"
		req.Level = r.URL.Query().Get("level")
		if req.Level == "" {
			req.Level = "ipxe"
		}
		req.FP = BeaconFingerprint{
			MAC:       r.URL.Query().Get("mac"),
			IP:        r.URL.Query().Get("ip"),
			Arch:      r.URL.Query().Get("arch"),
			Platform:  r.URL.Query().Get("platform"),
			UserClass: r.URL.Query().Get("userclass"),
		}
	case http.MethodPost:
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respond.Error(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
	default:
		respond.Error(w, http.StatusMethodNotAllowed, "use GET (iPXE) or POST (Hook)")
		return
	}

	if req.FP.MAC == "" {
		respond.Error(w, http.StatusBadRequest, "fingerprint.mac is required")
		return
	}
	if req.Level != "ipxe" && req.Level != "hook" {
		respond.Error(w, http.StatusBadRequest, "level must be 'ipxe' or 'hook'")
		return
	}

	mac := strings.ToLower(req.FP.MAC)
	hwName := hardwareNameForMAC(mac)
	ctx := r.Context()

	_, sessionID, claimedBy, err := h.upsertHardware(ctx, hwName, mac, req.FP)
	if err != nil {
		log.Printf("beacon: upsert hardware: %v", err)
		respond.Error(w, http.StatusInternalServerError, "failed to upsert hardware")
		return
	}

	// json.Marshal of a concrete struct with no custom marshalers cannot fail.
	payload, _ := json.Marshal(req)
	if err := h.store.Insert(ctx, beacon.Row{
		SessionID: sessionID,
		Level:     req.Level,
		MAC:       mac,
		IP:        req.FP.IP,
		Payload:   payload,
	}); err != nil {
		log.Printf("beacon: store insert: %v", err)
		// Non-fatal — Hardware CR is the source of truth, log is forensics.
	}

	skip := claimedBy != "" || strings.EqualFold(req.FP.UserClass, "Tinkerbell")
	short := beacon.ShortCode(sessionID)

	resp := BeaconResponse{
		SessionID: sessionID.String(),
		ShortCode: short,
		ClaimURL:  fmt.Sprintf("%s/claim?code=%s", strings.TrimRight(h.cfg.BootBaseURL, "/"), short),
		SkipClaim: skip,
		WatchURL:  fmt.Sprintf("%s/api/v1/claim/%s/watch", wsBase(h.cfg.BootBaseURL), sessionID.String()),
	}

	if r.URL.Query().Get("ipxe") == "1" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprintf(w, "#!ipxe\n")
		fmt.Fprintf(w, "set kontango_session_id %s\n", resp.SessionID)
		fmt.Fprintf(w, "set kontango_short_code %s\n", resp.ShortCode)
		fmt.Fprintf(w, "set kontango_skip_claim %d\n", boolToInt(skip))
		fmt.Fprintf(w, "set kontango_claim_url %s\n", resp.ClaimURL)
		return
	}
	respond.JSON(w, http.StatusOK, resp)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// hardwareNameForMAC turns "bc:24:11:aa:bb:cc" into "auto-bc24-11aa-bbcc".
// Strips colons, lowercases, group as 4-char chunks for human readability.
func hardwareNameForMAC(mac string) string {
	clean := strings.ReplaceAll(strings.ToLower(mac), ":", "")
	if len(clean) != 12 {
		return "auto-" + clean
	}
	return fmt.Sprintf("auto-%s-%s-%s", clean[0:4], clean[4:8], clean[8:12])
}

// upsertHardware fetches an existing Hardware CR by name, creates one if missing,
// returns the CR plus the session UUID derived from its UID and the claimed-by annotation.
func (h *BeaconHandler) upsertHardware(ctx context.Context, name, mac string, fp BeaconFingerprint) (*tinkv1.Hardware, uuid.UUID, string, error) {
	var hw tinkv1.Hardware
	err := h.k8s.Get(ctx, types.NamespacedName{Name: name, Namespace: h.cfg.Namespace}, &hw)
	if err == nil {
		// existing — derive session and read annotation
		sid := sessionIDFor(string(hw.UID), mac)
		claimedBy := hw.Annotations["kontango.io/claimed-by"]
		return &hw, sid, claimedBy, nil
	}
	if !apierrors.IsNotFound(err) {
		return nil, uuid.Nil, "", err
	}

	// create
	allow := false
	hw = tinkv1.Hardware{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: h.cfg.Namespace,
			Annotations: map[string]string{
				"kontango.io/discovery-source": "beacon",
				"kontango.io/first-seen":       metav1.Now().Format("2006-01-02T15:04:05Z07:00"),
			},
		},
		Spec: tinkv1.HardwareSpec{
			Interfaces: []tinkv1.Interface{{
				DHCP: &tinkv1.DHCP{
					MAC:      mac,
					Hostname: name,
					Arch:     fp.Arch,
				},
				Netboot: &tinkv1.Netboot{AllowPXE: &allow},
			}},
		},
	}
	err = h.k8s.Create(ctx, &hw)
	if err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return nil, uuid.Nil, "", err
		}
		// Lost the race with a concurrent beacon — fetch the winner.
		var existing tinkv1.Hardware
		if e := h.k8s.Get(ctx, types.NamespacedName{Name: name, Namespace: h.cfg.Namespace}, &existing); e != nil {
			return nil, uuid.Nil, "", e
		}
		sid := sessionIDFor(string(existing.UID), mac)
		return &existing, sid, existing.Annotations["kontango.io/claimed-by"], nil
	}
	sid := sessionIDFor(string(hw.UID), mac)
	return &hw, sid, "", nil
}

// sessionIDFor derives a stable UUIDv5 from the Hardware UID and MAC.
// Same (uid, mac) always produces the same UUID. Used as the beacon
// session identifier so re-boots see the same session.
func sessionIDFor(uid, mac string) uuid.UUID {
	if uid == "" {
		// fall back to MAC-only for tests with the fake client (no UID populated)
		uid = "no-uid"
	}
	return uuid.NewSHA1(uuid.NameSpaceDNS, []byte(uid+"|"+mac))
}

// wsBase rewrites https→wss and http→ws.
func wsBase(s string) string {
	if strings.HasPrefix(s, "https://") {
		return "wss://" + strings.TrimPrefix(s, "https://")
	}
	if strings.HasPrefix(s, "http://") {
		return "ws://" + strings.TrimPrefix(s, "http://")
	}
	return s
}
