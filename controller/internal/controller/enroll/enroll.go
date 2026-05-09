// Package enroll handles the WebSocket enrollment conversation.
// One connection. Stream data. Parallel verification. Yes or no.
package enroll

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"

	"git.konoss.org/kore/schmutz/controller/internal/controller/common"
	"git.konoss.org/kore/schmutz/controller/internal/controller/profiles"
	"git.konoss.org/kore/schmutz/controller/internal/controller/verify"
	"git.konoss.org/kore/schmutz/controller/internal/service"
)

// Module handles enrollment conversations.
type Module struct {
	C             *common.Clients
	V             *verify.Module
	Enroll        *service.EnrollmentService
	Profiles      *profiles.Registry
	NodeName      string
	GitHubRelease string
	// ZitiVersion is the ziti binary version to install on enrolled machines.
	// If empty, "latest" is resolved from the GitHub releases API at enroll time.
	ZitiVersion string
	// BaoVersion is the Bao version to install on enrolled machines.
	// If empty, "2.5.2" is used as default.
	BaoVersion string
}

func New(c *common.Clients, v *verify.Module, enrollSvc *service.EnrollmentService, nodeName string) *Module {
	return &Module{C: c, V: v, Enroll: enrollSvc, NodeName: nodeName}
}

// HandleWebSocket is the HTTP handler for /api/ws/enroll.
// One connection. The controller drives the conversation.
func (m *Module) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns:     []string{"*"},
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Printf("ws: accept: %v", err)
		return
	}
	defer conn.CloseNow()

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	// What we know from the connection itself (can't be faked)
	sourceIP := r.RemoteAddr
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		sourceIP = fwd
	}
	userAgent := r.Header.Get("User-Agent")

	log.Printf("ws: connection from %s", sourceIP)

	// 0. Screen this connection too
	wsFP := verify.CaptureConnection(r)
	if proceed, reason := m.V.ScreenConnection(wsFP); !proceed {
		log.Printf("ws: rejected %s — %s", sourceIP, reason)
		conn.Close(websocket.StatusNormalClosure, "rejected")
		return
	}

	// 1. Wait for hello — learn intent + session token
	var hello map[string]interface{}
	if err := wsjson.Read(ctx, conn, &hello); err != nil {
		return
	}
	helloType, _ := hello["type"].(string)
	if helloType != "hello" {
		wsjson.Write(ctx, conn, msg("error", "expected hello"))
		return
	}
	method, _ := hello["method"].(string)
	roleID, _ := hello["role_id"].(string)
	secretID, _ := hello["secret_id"].(string)
	oidcToken, _ := hello["token"].(string)
	sessionToken, _ := hello["session"].(string)

	// Validate session token — must match the IP that requested /install
	if sessionToken != "" {
		sessionData, _ := m.C.Bao.Get("secret", "identities/sessions/token/"+sessionToken)
		if sessionData == nil {
			log.Printf("ws: invalid session token from %s", sourceIP)
			wsjson.Write(ctx, conn, msg("error", "invalid session"))
			conn.Close(websocket.StatusNormalClosure, "invalid session")
			return
		}
		storedIP, _ := sessionData["source_ip"].(string)
		if storedIP != "" && storedIP != wsFP.SourceIP {
			log.Printf("ws: session IP mismatch: expected %s got %s", storedIP, wsFP.SourceIP)
			wsjson.Write(ctx, conn, msg("error", "session mismatch"))
			conn.Close(websocket.StatusNormalClosure, "session mismatch")
			return
		}
		// Burn the token — one-time use
		m.C.Bao.Delete("secret", "identities/sessions/token/"+sessionToken)
		log.Printf("ws: session validated for %s", sourceIP)
	}

	// Correlate with previous /install request
	if prevFP, found := m.V.CorrelateConnection(wsFP.SourceIP); found {
		log.Printf("ws: correlated with install request (ja4=%s ua=%s)", prevFP.JA4, prevFP.ClientType)
	}

	// 2. Probe for everything — send all probes, collect all responses
	profile := &verify.MachineProfile{SourceIP: sourceIP, UserAgent: userAgent}

	for _, probe := range Probes {
		wsjson.Write(ctx, conn, msg("probe", probe))
		var resp map[string]interface{}
		if err := wsjson.Read(ctx, conn, &resp); err != nil {
			log.Printf("ws: probe %s read error: %v", probe, err)
			continue
		}
		applyToProfile(profile, resp)
	}

	profile.ComputeHashes()
	log.Printf("ws: %s [%s] hw=%s full=%s", profile.Hostname, method, profile.HardwareHash, profile.FullHash)

	// 3. Run verify pipeline — all checks in parallel
	decision := m.V.RunAll(
		profile.Hostname, profile.OS, profile.OSVersion,
		profile.HardwareHash, profile.MachineID,
		roleID, secretID,
		profile.MACAddrs,
	)

	// 4. Fingerprint matching — check if this is a returning device
	if !decision.Approved && method == "scan" {
		machineID, matchResult, _ := m.V.FindMachineByProfile(profile)
		if matchResult != nil && matchResult.Confidence() == "high" {
			decision.Approved = true
			decision.Quarantine = false
			decision.Reason = "fingerprint match: score=" + itoa(matchResult.Score)
			// Look up the machine's profile for attributes
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
			log.Printf("ws: %s scan matched machine %s (score=%d)", profile.Hostname, machineID, matchResult.Score)
		}
	}

	// 5. OIDC verification (if provided)
	if !decision.Approved && oidcToken != "" {
		// TODO: verify OIDC token against ZITADEL/Bao
		// For now, treat as untrusted
		log.Printf("ws: OIDC token provided but verification not yet implemented")
	}

	log.Printf("ws: %s → %s (%s) attrs=%v", profile.Hostname, status(decision), decision.Reason, decision.Attributes)

	// 6. Rejected — close
	if decision.Rejected {
		wsjson.Write(ctx, conn, map[string]interface{}{
			"type": "status", "status": "rejected", "reason": decision.Reason,
		})
		conn.Close(websocket.StatusNormalClosure, "rejected")

		// Enrollment events recorded in Bao — delivered to dashboard via relay.tango SSE.

		return
	}

	// 7. Create identity + enroll
	reg := common.NewRegistration(profile.Hostname)
	reg.SourceIP = sourceIP
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
		"id": reg.ID, "nickname": reg.Nickname, "hostname": reg.Hostname,
		"state": string(reg.State), "registered_at": reg.RegisteredAt,
		"source": "websocket", "method": method,
	}

	id, jwt, err := m.C.Ziti.CreateIdentity(reg.ID, decision.Attributes, appData)
	if err != nil {
		wsjson.Write(ctx, conn, msg("error", "identity creation failed"))
		return
	}

	identity, err := m.Enroll.Enroll(jwt)
	if err != nil {
		wsjson.Write(ctx, conn, msg("error", "enrollment failed"))
		return
	}

	// 8. Store everything (persistent nodes only — skip containers/ephemeral compute)
	if isEphemeralCompute(profile) {
		log.Printf("enrollment: ephemeral compute detected for %s — skipping machine inventory", reg.Hostname)
	} else {
		m.C.Bao.PutMachine(reg.ID, appData)
		m.V.StoreMachineProfile(reg.ID, profile)

		// Write complete enrollment snapshot — immutable audit record.
		if snap := BuildSnapshot(reg, profile, decision,
			ConnectionMeta{
				SourceIP:  sourceIP,
				UserAgent: userAgent,
			},
			m.NodeName, id, "websocket",
		); snap != nil {
			if err := m.C.Bao.PutEnrollmentSnapshot(reg.ID, snap); err != nil {
				log.Printf("enrollment: snapshot write failed (non-fatal): %v", err)
			}
		}
	}

	// Enrollment events recorded in Bao — delivered to dashboard via relay.tango SSE.

	// Provision Ziti Basic services and SSH keypair
	var zitiServices []string
	var sshPubKey string
	token, _ := m.C.Ziti.Authenticate()
	if token != "" {
		zitiServices = createZitiBasic(m.C, token, reg.Nickname, id, decision.Approved, nil)

		// Add #enrolled attribute so device can dial progress-service
		AddEnrolledAttribute(m.C, token, id)

		// Create progress-service if it doesn't exist yet (idempotent)
		createProgressService(m.C, token)
	}

	// Generate SSH keypair and store in Bao — approved and quarantine both get one
	if pub, sshErr := generateAndStoreSSHKey(m.C, reg.ID); sshErr == nil {
		sshPubKey = pub
		log.Printf("  ssh key stored for %s", reg.Nickname)
	} else {
		log.Printf("  ssh key generation failed: %v", sshErr)
	}

	// 9. Build config for the machine
	cfg := m.buildConfig()

	// 10. Send result — identity + config in one message
	enrollStatus := "quarantine"
	if decision.Approved {
		enrollStatus = "approved"
	}

	wsjson.Write(ctx, conn, map[string]interface{}{
		"type":          "identity",
		"status":        enrollStatus,
		"id":            reg.ID,
		"nickname":      reg.Nickname,
		"identity":      json.RawMessage(identity),
		"config":        cfg,
		"reason":        decision.Reason,
		"services":      zitiServices,
		"ssh_public_key": sshPubKey,
	})

	conn.Close(websocket.StatusNormalClosure, "enrolled")
	log.Printf("ws: %s → %s (%s) as %s", profile.Hostname, enrollStatus, reg.Nickname, reg.ID)
}

// --- Profile builder from probe responses ---

func applyToProfile(p *verify.MachineProfile, data map[string]interface{}) {
	s := func(key string) string { v, _ := data[key].(string); return v }
	i := func(key string) int {
		if v, ok := data[key].(float64); ok {
			return int(v)
		}
		return 0
	}

	switch s("type") {
	case "os":
		p.Hostname = s("hostname")
		p.OS = s("os")
		p.OSVersion = s("os_version")
		p.Arch = s("arch")
		p.Kernel = s("kernel")
		if tz := s("timezone"); tz != "" {
			p.Timezone = tz
		}

	case "hardware":
		p.CPUModel = s("cpu_info")
		p.CPUCores = i("cpu_cores")
		p.MemoryMB = i("memory_mb")
		p.MachineID = s("machine_id")
		p.SerialNumber = s("serial")
		p.HardwareHash = s("hardware_hash")
		if tz := s("timezone"); tz != "" {
			p.Timezone = tz
		}
		if disks, ok := data["disk_serials"].([]interface{}); ok {
			for _, d := range disks {
				if ds, ok := d.(string); ok {
					p.DiskSerials = append(p.DiskSerials, ds)
				}
			}
		}

	case "network":
		if macs, ok := data["macs"].([]interface{}); ok {
			for _, mac := range macs {
				if ms, ok := mac.(string); ok {
					p.MACAddrs = append(p.MACAddrs, ms)
				}
			}
		}
		if ifaces, ok := data["interfaces"].([]interface{}); ok {
			for _, iface := range ifaces {
				if im, ok := iface.(map[string]interface{}); ok {
					ni := verify.NetworkInterface{
						Name: im["name"].(string),
					}
					if mac, ok := im["mac"].(string); ok {
						ni.MAC = mac
					}
					if up, ok := im["up"].(bool); ok {
						ni.Up = up
					}
					if ips, ok := im["ips"].([]interface{}); ok {
						for _, ip := range ips {
							if ips, ok := ip.(string); ok {
								ni.IPs = append(ni.IPs, ips)
							}
						}
					}
					p.Interfaces = append(p.Interfaces, ni)
				}
			}
		}
		if dns, ok := data["dns_servers"].([]interface{}); ok {
			for _, d := range dns {
				if ds, ok := d.(string); ok {
					p.DNSServers = append(p.DNSServers, ds)
				}
			}
		}
		p.Gateway = s("gateway")

	case "system":
		p.Uptime = i("uptime_secs")
		p.BootID = s("boot_id")
		p.Locale = s("locale")
		p.PackageCount = i("package_count")
		if keys, ok := data["ssh_host_keys"].([]interface{}); ok {
			for _, k := range keys {
				if ks, ok := k.(string); ok {
					p.SSHHostKeys = append(p.SSHHostKeys, ks)
				}
			}
		}
		if ports, ok := data["open_ports"].([]interface{}); ok {
			for _, port := range ports {
				if pf, ok := port.(float64); ok {
					p.OpenPorts = append(p.OpenPorts, int(pf))
				}
			}
		}
	}
}

// --- Helpers ---

func msg(msgType, value string) map[string]string {
	if msgType == "probe" {
		return map[string]string{"type": "probe", "probe": value}
	}
	return map[string]string{"type": msgType, "reason": value}
}

func status(d *verify.Decision) string {
	if d.Rejected {
		return "rejected"
	}
	if d.Approved {
		return "approved"
	}
	return "quarantine"
}

func itoa(n int) string {
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

func (m *Module) buildConfig() map[string]interface{} {
	controllerInfo, _ := m.C.Bao.Get("secret", "infra/controllers/"+m.NodeName)
	var hosts []string
	if controllerInfo != nil {
		if addr, ok := controllerInfo["tango_addr"].(string); ok {
			hosts = append(hosts, addr)
		}
		if domain, ok := controllerInfo["domain"].(string); ok {
			hosts = append(hosts, domain)
		}
	}
	zitiVer := m.ZitiVersion
	if zitiVer == "" {
		zitiVer = "latest"
	}
	downloadBase := "https://github.com/openziti/ziti/releases/latest/download"
	if zitiVer != "latest" {
		downloadBase = "https://github.com/openziti/ziti/releases/download/v" + zitiVer
	}
	return map[string]interface{}{
		"hosts": hosts,
		"tunnel": map[string]interface{}{
			"binary":   "ziti",
			"version":  zitiVer,
			"download": downloadBase,
		},
	}
}

func createZitiBasic(c *common.Clients, token, nickname, identityID string, approved bool, extraServices []profiles.ExtraService) []string {
	interceptTypeID := c.Ziti.GetConfigTypeID(token, "intercept.v1")
	hostTypeID := c.Ziti.GetConfigTypeID(token, "host.v1")
	if interceptTypeID == "" || hostTypeID == "" {
		log.Printf("ziti-basic: could not get config type IDs for %s", nickname)
		return nil
	}

	// Add #host-<nickname> to the identity so bind policies match
	c.Ziti.UpdateIdentityAddAttr(token, identityID, "host-"+nickname)

	svcDialAttrs := []string{"#stage-2", "#stage-3"}
	if !approved {
		svcDialAttrs = []string{}
	}

	var created []string

	type svcDef struct {
		name  string
		port  int
		attrs []string
		dial  []string
	}

	// Base services — always provisioned for every device.
	// Telemetry flows over Ziti via telemetry.tango (TCP frames) — no per-device NATS service needed.
	services := []svcDef{
		{name: "ssh", port: 22, attrs: []string{"ssh-" + nickname, "ssh-services"}, dial: []string{"#clients"}},
		{name: "http", port: 80, attrs: []string{"http-" + nickname, "web-services"}, dial: svcDialAttrs},
		{name: "https", port: 443, attrs: []string{"https-" + nickname, "web-services"}, dial: svcDialAttrs},
		{name: "docs", port: 8080, attrs: []string{"docs-" + nickname, "docs-services"}, dial: svcDialAttrs},
	}

	// Extra services from profile
	for _, es := range extraServices {
		services = append(services, svcDef{
			name:  es.Name,
			port:  es.Port,
			attrs: []string{es.Name + "-" + nickname, es.Name + "-services"},
			dial:  svcDialAttrs,
		})
	}

	for _, svc := range services {
		svcName := svc.name + "-" + nickname
		interceptID, _ := c.Ziti.CreateConfig(token, svcName+"-intercept", interceptTypeID, map[string]interface{}{
			"protocols":  []string{"tcp"},
			"addresses":  []string{nickname + ".tango"},
			"portRanges": []map[string]int{{"low": svc.port, "high": svc.port}},
		})
		hostID, _ := c.Ziti.CreateConfig(token, svcName+"-host", hostTypeID, map[string]interface{}{
			"protocol": "tcp",
			"address":  "localhost",
			"port":     svc.port,
		})
		if interceptID == "" || hostID == "" {
			log.Printf("ziti-basic: failed to create configs for %s", svcName)
			continue
		}
		svcID, _ := c.Ziti.CreateService(token, svcName, []string{interceptID, hostID}, svc.attrs)
		if svcID == "" {
			log.Printf("ziti-basic: failed to create service %s", svcName)
			continue
		}
		c.Ziti.CreateServicePolicy(token, svcName+"-bind", "Bind", "AnyOf", []string{"#host-" + nickname}, []string{"@" + svcID})
		c.Ziti.CreateServicePolicy(token, svcName+"-dial", "Dial", "AnyOf", svc.dial, []string{"@" + svcID})
		created = append(created, svcName)
		log.Printf("  ziti-basic: %s → %s.tango:%d (host.v1)", svcName, nickname, svc.port)
	}

	return created
}

// generateAndStoreSSHKey creates an ed25519 keypair and stores it in Bao at
// secret/devices/<slug>/ssh. The public key is returned for adding to authorized_keys.
func generateAndStoreSSHKey(c *common.Clients, slug string) (pubKey string, err error) {
	pub, priv, err := genED25519Key()
	if err != nil {
		return "", err
	}
	if err := c.Bao.Put("secret", "devices/"+slug+"/ssh", map[string]interface{}{
		"public_key":  pub,
		"private_key": priv,
	}); err != nil {
		return "", err
	}
	return pub, nil
}
