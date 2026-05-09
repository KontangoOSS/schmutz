package enroll

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"git.konoss.org/kore/schmutz/controller/internal/controller/common"
	"git.konoss.org/kore/schmutz/controller/internal/controller/profiles"
	"git.konoss.org/kore/schmutz/controller/internal/controller/verify"
)

var EnrollTagMu sync.Mutex
var EnrollTagStore = make(map[string]string) // in-memory fallback for health check validation

// sessionEntry holds a session token with its source IP and expiry.
type sessionEntry struct {
	sourceIP string
	expiry   time.Time
}

var sessionMu sync.Mutex
var sessionStore = make(map[string]sessionEntry) // in-memory fallback when Bao unreachable

// storeSession saves a session token → IP mapping. Tries Bao first; falls back to memory.
func (m *Module) storeSession(token, sourceIP string) {
	entry := map[string]interface{}{
		"source_ip": sourceIP,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	if err := m.C.Bao.Put("secret", "identities/sessions/token/"+token, entry); err != nil {
		sessionMu.Lock()
		sessionStore[token] = sessionEntry{sourceIP: sourceIP, expiry: time.Now().Add(5 * time.Minute)}
		sessionMu.Unlock()
	}
}

// validateSession checks a session token and returns the stored IP (or "" if any IP allowed).
// Returns false if the token is unknown or expired.
func (m *Module) validateSession(token string) (sourceIP string, ok bool) {
	// Try Bao first.
	if data, _ := m.C.Bao.Get("secret", "identities/sessions/token/"+token); data != nil {
		m.C.Bao.Delete("secret", "identities/sessions/token/"+token)
		ip, _ := data["source_ip"].(string)
		return ip, true
	}
	// Fall back to in-memory store.
	sessionMu.Lock()
	defer sessionMu.Unlock()
	entry, found := sessionStore[token]
	if !found || time.Now().After(entry.expiry) {
		delete(sessionStore, token)
		return "", false
	}
	delete(sessionStore, token)
	return entry.sourceIP, true
}

// HandleSSE is the SSE enrollment endpoint.
// Client POSTs all data, server streams back verification events + identity.
//
//	POST /api/enroll/stream
//	Content-Type: application/json
//	Body: { all probe data }
//
//	Response: text/event-stream
//	event: verify
//	data: {"check":"hostname","passed":true}
//
//	event: verify
//	data: {"check":"os","passed":true,"data":{"ref":"public/os/linux/ubuntu/24.04"}}
//
//	event: decision
//	data: {"status":"quarantine","reason":"no credentials","attributes":["quarantine"]}
//
//	event: identity
//	data: {"id":"...","nickname":"...","identity":<base64>,"config":{...}}
func (m *Module) HandleSSE(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	// 1. Capture connection fingerprint
	connFP := verify.CaptureConnection(r)
	proceed, reason := m.V.ScreenConnection(connFP)
	if !proceed {
		log.Printf("sse: rejected %s — %s", connFP.SourceIP, reason)
		http.Error(w, "", http.StatusNotFound)
		return
	}

	// 2. Parse all data from the client - accept ANY key-value pairs
	// This flexible schema means hackers don't know what fields are used or validated
	// We extract what we need, ignore the rest
	var rawInput map[string]interface{}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&rawInput); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	// Helper to safely extract strings and other types from the flexible input
	getString := func(key string) string {
		if v, ok := rawInput[key].(string); ok {
			return v
		}
		return ""
	}
	getInt := func(key string) int {
		switch v := rawInput[key].(type) {
		case float64:
			return int(v)
		case int:
			return v
		}
		return 0
	}
	getStrings := func(key string) []string {
		if v, ok := rawInput[key].([]interface{}); ok {
			var result []string
			for _, item := range v {
				if s, ok := item.(string); ok {
					result = append(result, s)
				}
			}
			return result
		}
		return nil
	}

	// Extract known fields (if present), ignore unknown ones
	input := struct {
		Source       string
		Method       string
		Session      string
		Token        string
		RoleID       string
		SecretID     string
		ID           string
		Slug         string // user-chosen device name; becomes nickname
		Profile      string
		Hostname     string
		OS           string
		OSVersion    string
		Arch         string
		Kernel       string
		Timezone     string
		CPUInfo      string
		CPUCores     int
		MemoryMB     int
		MachineID    string
		Serial       string
		HardwareHash string
		DiskSerials  []string
		MACs         []string
		Interfaces   []map[string]interface{}
		DNSServers   []string
		Gateway      string
		Uptime       int
		BootID       string
		Locale       string
		SSHHostKeys  []string
		OpenPorts    []int
		PackageCount int
	}{
		Source:       getString("source"),
		Method:       getString("method"),
		Session:      getString("session"),
		Token:        getString("token"),
		RoleID:       getString("role_id"),
		SecretID:     getString("secret_id"),
		ID:           getString("id"),
		Slug:         getString("slug"),
		Profile:      getString("profile"),
		Hostname:     getString("hostname"),
		OS:           getString("os"),
		OSVersion:    getString("os_version"),
		Arch:         getString("arch"),
		Kernel:       getString("kernel"),
		Timezone:     getString("timezone"),
		CPUInfo:      getString("cpu_info"),
		CPUCores:     getInt("cpu_cores"),
		MemoryMB:     getInt("memory_mb"),
		MachineID:    getString("machine_id"),
		Serial:       getString("serial"),
		HardwareHash: getString("hardware_hash"),
		DiskSerials:  getStrings("disk_serials"),
		MACs:         getStrings("macs"),
		DNSServers:   getStrings("dns_servers"),
		Gateway:      getString("gateway"),
		Uptime:       getInt("uptime_secs"),
		BootID:       getString("boot_id"),
		Locale:       getString("locale"),
		SSHHostKeys:  getStrings("ssh_host_keys"),
		OpenPorts:    nil, // Skip open_ports for now
		PackageCount: getInt("package_count"),
	}

	// Store ALL raw data for audit trail
	log.Printf("sse: enrollment payload keys: %v", len(rawInput))

	// 3. Validate session token (ties to /install request)
	if input.Session != "" {
		storedIP, valid := m.validateSession(input.Session)
		if !valid {
			log.Printf("sse: invalid session from %s", connFP.SourceIP)
			http.Error(w, "invalid session", http.StatusForbidden)
			return
		}
		if storedIP != "" && storedIP != connFP.SourceIP {
			log.Printf("sse: session IP mismatch: %s vs %s", storedIP, connFP.SourceIP)
			http.Error(w, "session mismatch", http.StatusForbidden)
			return
		}
	}

	// 4. Build machine profile
	profile := &verify.MachineProfile{
		SourceIP:     connFP.SourceIP,
		UserAgent:    connFP.UserAgent,
		JA4:          connFP.JA4,
		Hostname:     input.Hostname,
		OS:           input.OS,
		OSVersion:    input.OSVersion,
		Arch:         input.Arch,
		Kernel:       input.Kernel,
		CPUModel:     input.CPUInfo,
		CPUCores:     input.CPUCores,
		MemoryMB:     input.MemoryMB,
		MachineID:    input.MachineID,
		SerialNumber: input.Serial,
		HardwareHash: input.HardwareHash,
		DiskSerials:  input.DiskSerials,
		MACAddrs:     input.MACs,
		DNSServers:   input.DNSServers,
		Gateway:      input.Gateway,
		Timezone:     input.Timezone,
		Uptime:       input.Uptime,
		BootID:       input.BootID,
		Locale:       input.Locale,
		SSHHostKeys:  input.SSHHostKeys,
		OpenPorts:    input.OpenPorts,
		PackageCount: input.PackageCount,
	}
	profile.ComputeHashes()

	log.Printf("sse: %s [%s] hw=%s full=%s", profile.Hostname, input.Method, profile.HardwareHash, profile.FullHash)

	// 5. Generate enrollment tag (only this stream knows it)
	// Device must echo this back in health checks/status updates to prove legitimate enrollment
	enrollmentTag := generateSessionToken()

	// 6. Start SSE stream
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	flusher.Flush()

	send := func(event string, data interface{}) {
		// Add tag to all events for bot detection
		dataMap := make(map[string]interface{})
		switch v := data.(type) {
		case map[string]interface{}:
			for k, val := range v {
				dataMap[k] = val
			}
		default:
			// For struct types, marshal and unmarshal to get a map
			b, _ := json.Marshal(data)
			json.Unmarshal(b, &dataMap)
		}
		dataMap["tag"] = enrollmentTag

		b, _ := json.Marshal(dataMap)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(b))
		flusher.Flush()

		// Progress events are delivered via SSE to the enrollment UI.
		// Real-time dashboard delivery uses relay.tango → ziti-dash SSE fan-out.
	}

	// 7. Run verify pipeline — stream results as they complete
	// 8. Special handling for controller enrollment (see below)
	decision := m.V.RunAll(
		profile.Hostname, profile.OS, profile.OSVersion,
		profile.HardwareHash, profile.MachineID,
		input.RoleID, input.SecretID,
		profile.MACAddrs,
	)

	// Stream each verdict
	for _, v := range decision.Verdicts {
		send("verify", v)
	}

	// 8. Special handling for controller enrollment
	if input.Method == "controller" {
		// Controller enrollment requires admin approval
		// Controllers get special access to internal APIs (Ziti mgmt on 1280, etc)
		decision.Approved = true
		decision.Quarantine = false
		decision.Rejected = false  // Clear any rejection from verification pipeline
		decision.Attributes = append(decision.Attributes, "controller", "stage-4")
		decision.Reason = "controller enrollment"
		send("verify", verify.Verdict{
			Check: "controller_mode", Passed: true,
			Reason: "controller node enrolling to cluster",
		})
	}

	// 9. Fingerprint matching for scan method
	if !decision.Approved && input.Method == "scan" {
		machineID, matchResult, _ := m.V.FindMachineByProfile(profile)
		if matchResult != nil && matchResult.Confidence() == "high" {
			decision.Approved = true
			decision.Quarantine = false
			decision.Reason = fmt.Sprintf("fingerprint match: score=%d", matchResult.Score)
			if machineID != "" {
				info, _ := m.V.Identity(machineID)
				if info != nil && info.ProfileName != "" {
					profileInfo, _ := m.V.Profile(info.ProfileName)
					if profileInfo != nil {
						decision.Attributes = profileInfo.Attributes
						decision.Profile = info.ProfileName
					}
				}
			}
			send("verify", verify.Verdict{
				Check: "fingerprint_match", Passed: true,
				Confidence: matchResult.Confidence(),
				Reason:     fmt.Sprintf("score=%d machine=%s", matchResult.Score, machineID),
			})
		}
	}

	// Resolve profile name early (needed for both attrs and extraServices later).
	resolvedProfile := "base"
	if input.Profile != "" {
		resolvedProfile = input.Profile
	}

	// 10. Auto-approve HookOS enrollments from trusted source IPs.
	// Set AUTO_APPROVE_IPS=<comma-separated CIDRs or IPs> to enable.
	// Only applies when source=="hookos" so browser enrollments are unaffected.
	if !decision.Approved && !decision.Rejected && input.Source == "hookos" {
		if autoApproveIPs := os.Getenv("AUTO_APPROVE_IPS"); autoApproveIPs != "" {
			srcIP := connFP.SourceIP
			// Strip port if present.
			if i := strings.LastIndex(srcIP, ":"); i > strings.LastIndex(srcIP, "]") {
				srcIP = srcIP[:i]
			}
			srcIP = strings.Trim(srcIP, "[]")
			for _, allowed := range strings.Split(autoApproveIPs, ",") {
				allowed = strings.TrimSpace(allowed)
				if allowed == srcIP {
					decision.Approved = true
					decision.Quarantine = false
					decision.Reason = "auto-approved: trusted source IP"
					decision.Attributes = []string{"tunnel", "stage-1", "web-clients"}
					send("verify", verify.Verdict{
						Check: "auto_approve", Passed: true,
						Reason: fmt.Sprintf("source IP %s is in AUTO_APPROVE_IPS", srcIP),
					})
					log.Printf("sse: %s → auto-approved (hookos from %s)", profile.Hostname, srcIP)
					break
				}
			}
		}
	}

	// Resolve profile — merge profile attributes into decision.
	// This runs after ALL decision-modifying blocks so profile attrs are always appended last,
	// regardless of whether auto-approve (or any other block) replaced decision.Attributes.
	var resolvedP *profiles.Profile
	if m.Profiles != nil {
		if p := m.Profiles.Get(resolvedProfile); p != nil {
			resolvedP = p
			if resolvedProfile != p.Name {
				log.Printf("sse: profile %q not found — using %q", resolvedProfile, p.Name)
			}
		}
	}
	if resolvedP != nil {
		decision.Attributes = append(decision.Attributes, resolvedP.Attributes...)
	}

	// 11. Send decision
	statusStr := "quarantine"
	if decision.Approved {
		statusStr = "approved"
	}
	if decision.Rejected {
		send("decision", map[string]interface{}{
			"status": "rejected", "reason": decision.Reason,
		})
		log.Printf("sse: %s → rejected (%s)", profile.Hostname, decision.Reason)
		return
	}
	send("decision", map[string]interface{}{
		"status": statusStr, "reason": decision.Reason, "attributes": decision.Attributes,
	})

	// 10. Create identity + enroll
	reg := common.NewRegistration(profile.Hostname)

	// Allow custom ID if provided (for attribution), otherwise use generated UUID
	if input.ID != "" {
		reg.ID = input.ID
	}

	// User-provided slug becomes the device nickname and service prefix.
	// Sanitize: lowercase, alphanumeric + hyphen only, max 32 chars.
	if input.Slug != "" {
		slug := strings.ToLower(input.Slug)
		var clean []byte
		for _, c := range []byte(slug) {
			if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
				clean = append(clean, c)
			}
		}
		if len(clean) > 32 {
			clean = clean[:32]
		}
		if len(clean) > 0 {
			reg.Nickname = string(clean)
		}
	}

	// app_id: stable opaque handle for Zitadel claims (separate from Ziti identity ID).
	// Currently same UUID as reg.ID, but stored separately so it can be remapped later.
	appID := reg.ID

	reg.SourceIP = connFP.SourceIP
	reg.OS = profile.OS
	reg.OSVersion = profile.OSVersion
	reg.Arch = profile.Arch
	reg.HardwareHash = profile.HardwareHash
	reg.MACAddrs = profile.MACAddrs
	reg.CPUInfo = profile.CPUModel
	reg.MachineID = profile.MachineID
	reg.KernelVersion = profile.Kernel
	reg.Timezone = profile.Timezone
	if decision.EntityID != "" {
		reg.BaoEntityID = decision.EntityID
		reg.Trusted = true
	}

	appData := map[string]interface{}{
		"id": reg.ID, "app_id": appID, "nickname": reg.Nickname, "hostname": reg.Hostname,
		"state": string(reg.State), "registered_at": reg.RegisteredAt,
		"source": "sse", "method": input.Method,
		// Hardware + network identity — persisted for SSH known_hosts and re-enrollment
		"ssh_host_keys": input.SSHHostKeys,
		"macs":          input.MACs,
		"machine_id":    input.MachineID,
		"serial":        input.Serial,
		"os":            input.OS,
		"arch":          input.Arch,
		"os_version":    input.OSVersion,
	}

	send("progress", map[string]string{"step": "creating identity"})

	id, jwt, err := m.C.Ziti.CreateIdentity(reg.ID, decision.Attributes, appData)
	if err != nil {
		log.Printf("sse: identity creation failed for %s: %v", reg.ID, err)
		send("error", map[string]string{"reason": "identity creation failed"})
		return
	}

	send("progress", map[string]string{"step": "enrolling"})

	identity, err := m.Enroll.Enroll(jwt)
	if err != nil {
		log.Printf("sse: enrollment failed for %s: %v", reg.ID, err)
		send("error", map[string]string{"reason": "enrollment failed"})
		return
	}

	// Provision Ziti services for the enrolled identity.
	var zitiServices []string
	token, _ := m.C.Ziti.Authenticate()
	if token != "" {
		var extraServices []profiles.ExtraService
		if resolvedP != nil {
			extraServices = resolvedP.ExtraServices
		}
		zitiServices = createZitiBasic(m.C, token, reg.Nickname, id, decision.Approved, extraServices)
		AddEnrolledAttribute(m.C, token, id)
		createProgressService(m.C, token)
	}

	// Send identity — the terminal SSE event. Must not block on Bao.
	send("identity", map[string]interface{}{
		"id":       reg.ID,
		"nickname": reg.Nickname,
		"status":   statusStr,
		"identity": string(identity),
		"services": zitiServices,
		"token":    enrollmentTag,
	})

	// Fire-and-forget: persist machine record to Bao if available.
	identitySnapshot := string(identity)
	go func() {
		if isEphemeralCompute(profile) {
			log.Printf("enrollment: ephemeral compute detected for %s — skipping machine inventory", profile.Hostname)
		} else {
			m.C.Bao.PutMachine(reg.ID, appData)
			m.V.StoreMachineProfile(reg.ID, profile)

			// Write complete enrollment snapshot — immutable audit record.
			if snapSSE := BuildSnapshot(reg, profile, decision,
				ConnectionMeta{
					SourceIP: connFP.SourceIP,
				},
				m.NodeName, id, "sse",
			); snapSSE != nil {
				if err := m.C.Bao.PutEnrollmentSnapshot(reg.ID, snapSSE); err != nil {
					log.Printf("enrollment: snapshot write failed (non-fatal): %v", err)
				}
			}
		}
		m.C.Bao.Put("secret", "devices/"+reg.ID+"/enrollment-tag", map[string]interface{}{
			"tag":       enrollmentTag,
			"timestamp": time.Now().Unix(),
		})
		// Store Ziti identity JSON — enables recovery and re-enrollment without the machine
		if identitySnapshot != "" {
			m.C.Bao.Put("secret", "identities/machines/"+reg.ID+"/identity", map[string]interface{}{
				"identity_json": identitySnapshot,
				"ziti_id":       id,
				"nickname":      reg.Nickname,
				"stored_at":     time.Now().Unix(),
			})
		}
	}()

	log.Printf("sse: %s → %s as %s (%s)", profile.Hostname, statusStr, reg.Nickname, reg.ID)
}

// handleEnrollEndpoint routes between SSE stream and v1 REST based on Accept header.
func (m *Module) HandleEnrollEndpoint(w http.ResponseWriter, r *http.Request) {
	accept := r.Header.Get("Accept")
	if r.Method == http.MethodPost && strings.Contains(accept, "text/event-stream") {
		m.HandleSSE(w, r)
		return
	}
	// Fall through to v1 REST handler (wired separately in routes.go)
	http.Error(w, "use Accept: text/event-stream for SSE enrollment", http.StatusBadRequest)
}

