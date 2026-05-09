package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/KontangoOSS/schmutz/enroll/internal/audit"
	"github.com/KontangoOSS/schmutz/enroll/internal/bao"
	"github.com/KontangoOSS/schmutz/enroll/internal/ziti"
)

// CommandExecutor runs an external binary. The bootstrap mode subprocesses
// out to `bao` and `ziti` since reimplementing their wire protocols here
// would dwarf the handler logic. stdin is optional — pass nil when no input
// is piped to the subprocess.
type CommandExecutor interface {
	Run(ctx context.Context, stdin io.Reader, name string, args ...string) ([]byte, error)
}

// BreakGlassCreator creates the genesis admin identity. Implementation
// uses the Ziti management API + writes to disk + mirrors to Bao.
// Defined as an interface so bootstrap_test.go can stub it.
type BreakGlassCreator interface {
	Create(ctx context.Context) (BreakGlassResult, error)
}

type BreakGlassResult struct {
	IdentityName string
	IdentityJSON []byte
	LocalPath    string
	BaoPath      string
}

type BootstrapHandler struct {
	exec CommandExecutor
	bg   BreakGlassCreator
	au   audit.Logger
}

func NewBootstrapHandler(ex CommandExecutor, bg BreakGlassCreator, au audit.Logger) *BootstrapHandler {
	return &BootstrapHandler{exec: ex, bg: bg, au: au}
}

// bootstrapActor returns the actor name used in bootstrap audit entries.
// Bootstrap mode has no Ziti caller identity; the literal "bootstrap" is
// sufficient for v1 and matches the spec ("bootstrap:<hostname>" was the
// originally proposed form, but a single string here is simpler and the
// hostname is already in journal output).
func bootstrapActor() string {
	return "bootstrap"
}

func (h *BootstrapHandler) BaoInit() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out, err := h.exec.Run(r.Context(), nil, "bao", "operator", "init",
			"-key-shares=5", "-key-threshold=3", "-format=table")
		if err != nil {
			h.au.Record(r.Context(), audit.Event{
				Actor: bootstrapActor(), Action: audit.ActionBootstrapBaoInit,
				Result: audit.ResultError, Metadata: map[string]string{"error": err.Error()},
			})
			writeErr(w, http.StatusInternalServerError, "bao init failed: "+err.Error())
			return
		}
		keys, root := parseInitOutput(string(out))
		h.au.Record(r.Context(), audit.Event{
			Actor: bootstrapActor(), Action: audit.ActionBootstrapBaoInit, Result: audit.ResultOK,
			Metadata: map[string]string{"unseal_key_count": strconv.Itoa(len(keys))},
		})
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"unseal_keys": keys,
			"root_token":  root,
		})
	})
}

func parseInitOutput(out string) ([]string, string) {
	var keys []string
	var root string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "Unseal Key ") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				keys = append(keys, strings.TrimSpace(parts[1]))
			}
		}
		if strings.HasPrefix(line, "Root Token:") {
			root = strings.TrimSpace(strings.TrimPrefix(line, "Root Token:"))
		}
	}
	return keys, root
}

type distributeReq struct {
	Keys  []string `json:"keys"`
	Peers []string `json:"peers"`
}

func (h *BootstrapHandler) DistributeKeys() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req distributeReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid json body")
			return
		}
		if len(req.Keys) == 0 {
			writeErr(w, http.StatusBadRequest, "keys required")
			return
		}
		// In v1, distribution writes to /root/.bao-backup/ on each peer
		// via SSH. Keys are joined with newlines and piped to the remote
		// `cat` via stdin so bytes actually reach the file.
		keysPayload := strings.Join(req.Keys, "\n") + "\n"
		for _, peer := range req.Peers {
			args := []string{"-o", "StrictHostKeyChecking=no", "root@" + peer,
				"mkdir -p /root/.bao-backup && cat > /root/.bao-backup/keys.txt"}
			if _, err := h.exec.Run(r.Context(), strings.NewReader(keysPayload), "ssh", args...); err != nil {
				h.au.Record(r.Context(), audit.Event{
					Actor: bootstrapActor(), Action: audit.ActionBootstrapBaoDistributeKeys,
					Result: audit.ResultError, Metadata: map[string]string{
						"peer": peer, "error": err.Error(),
					},
				})
				writeErr(w, http.StatusInternalServerError, "distribute to "+peer+" failed: "+err.Error())
				return
			}
		}
		h.au.Record(r.Context(), audit.Event{
			Actor: bootstrapActor(), Action: audit.ActionBootstrapBaoDistributeKeys, Result: audit.ResultOK,
			Metadata: map[string]string{"peer_count": strconv.Itoa(len(req.Peers))},
		})
		w.WriteHeader(http.StatusOK)
	})
}

type joinPeerReq struct {
	Leader string `json:"leader"`
	Self   string `json:"self"`
}

func (h *BootstrapHandler) JoinPeer() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req joinPeerReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid json body")
			return
		}
		if req.Leader == "" {
			writeErr(w, http.StatusBadRequest, "leader is required")
			return
		}
		if _, err := h.exec.Run(r.Context(), nil, "bao", "operator", "raft", "join",
			"https://"+req.Leader+":8200"); err != nil {
			h.au.Record(r.Context(), audit.Event{
				Actor: bootstrapActor(), Action: audit.ActionBootstrapBaoJoinPeer,
				Result: audit.ResultError, Metadata: map[string]string{
					"leader": req.Leader, "error": err.Error(),
				},
			})
			writeErr(w, http.StatusInternalServerError, "raft join failed: "+err.Error())
			return
		}
		h.au.Record(r.Context(), audit.Event{
			Actor: bootstrapActor(), Action: audit.ActionBootstrapBaoJoinPeer, Result: audit.ResultOK,
			Metadata: map[string]string{"leader": req.Leader, "self": req.Self},
		})
		w.WriteHeader(http.StatusOK)
	})
}

func (h *BootstrapHandler) ApplyEnrollPolicy() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Apply the canonical enroll-server policy. Path is fixed.
		if _, err := h.exec.Run(r.Context(), nil, "bao", "policy", "write",
			"enroll-server", "/etc/kontango/bao/enroll-server.hcl"); err != nil {
			h.au.Record(r.Context(), audit.Event{
				Actor: bootstrapActor(), Action: audit.ActionBootstrapApplyEnrollPolicy,
				Result: audit.ResultError, Metadata: map[string]string{"error": err.Error()},
			})
			writeErr(w, http.StatusInternalServerError, "apply policy failed: "+err.Error())
			return
		}
		h.au.Record(r.Context(), audit.Event{
			Actor: bootstrapActor(), Action: audit.ActionBootstrapApplyEnrollPolicy, Result: audit.ResultOK,
		})
		w.WriteHeader(http.StatusOK)
	})
}

func (h *BootstrapHandler) CreateBreakGlass() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		res, err := h.bg.Create(r.Context())
		if err != nil {
			h.au.Record(r.Context(), audit.Event{
				Actor: bootstrapActor(), Action: audit.ActionBootstrapCreateBreakGlass,
				Result: audit.ResultError, Metadata: map[string]string{"error": err.Error()},
			})
			writeErr(w, http.StatusInternalServerError, "break-glass creation failed: "+err.Error())
			return
		}
		h.au.Record(r.Context(), audit.Event{
			Actor: bootstrapActor(), Action: audit.ActionBootstrapCreateBreakGlass, Result: audit.ResultOK,
			Resource: res.IdentityName,
			Metadata: map[string]string{
				"local_path": res.LocalPath,
				"bao_path":   res.BaoPath,
			},
		})
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"identity_name": res.IdentityName,
			"local_path":    res.LocalPath,
			"bao_path":      res.BaoPath,
		})
	})
}

// --- Production implementations of the CommandExecutor + BreakGlassCreator interfaces ---

type osExec struct{}

func NewOSExec() CommandExecutor { return &osExec{} }

func (osExec) Run(ctx context.Context, stdin io.Reader, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != nil {
		cmd.Stdin = stdin
	}
	return cmd.CombinedOutput()
}

// realBreakGlass creates the break-glass identity via the Ziti management API,
// writes the identity JSON to /root/.schmutz/break-glass.json, and mirrors
// it to Bao at secret/data/schmutz/break-glass/identity.
type realBreakGlass struct {
	z         ziti.Client
	kv        bao.AdminKV
	localPath string
	baoPath   string
}

func NewBreakGlassCreator(z ziti.Client, kv bao.AdminKV, localPath, baoPath string) BreakGlassCreator {
	return &realBreakGlass{z: z, kv: kv, localPath: localPath, baoPath: baoPath}
}

func (r *realBreakGlass) Create(ctx context.Context) (BreakGlassResult, error) {
	res, err := r.z.CreateBareIdentity(ctx, BreakGlassIdentityName,
		[]string{"admins", "admins-break-glass"},
		map[string]string{"break-glass": "true", "created-by": "bootstrap"})
	if err != nil {
		return BreakGlassResult{}, err
	}
	// Write OTT JWT to a tempfile, run `ziti edge enroll`, get the identity JSON back.
	jwtPath := r.localPath + ".jwt"
	if err := os.WriteFile(jwtPath, []byte(res.JWT), 0600); err != nil {
		return BreakGlassResult{}, err
	}
	defer os.Remove(jwtPath)
	if err := exec.CommandContext(ctx, "ziti", "edge", "enroll",
		"-j", jwtPath, "-o", r.localPath).Run(); err != nil {
		return BreakGlassResult{}, err
	}
	if err := os.Chmod(r.localPath, 0600); err != nil {
		return BreakGlassResult{}, err
	}
	idJSON, err := os.ReadFile(r.localPath)
	if err != nil {
		return BreakGlassResult{}, err
	}
	// Mirror to Bao.
	body, _ := json.Marshal(map[string]string{"identity": string(idJSON)})
	if err := r.kv.WriteJSON(ctx, r.baoPath, body); err != nil {
		return BreakGlassResult{}, err
	}
	return BreakGlassResult{
		IdentityName: BreakGlassIdentityName,
		IdentityJSON: idJSON,
		LocalPath:    r.localPath,
		BaoPath:      r.baoPath,
	}, nil
}
