package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

// zitiIdentityPath returns the Ziti identity to use for data plane operations.
//
// Resolution order:
//  1. agent.json's ziti_identity_path — explicit operator override.
//  2. /etc/ziti/machine-*.json — operational tunnel identity from ziti-edge-tunnel.
//  3. <schmutzDir>/identity.json — hub-enrolled identity.
//
// The Bao identity (schmutzDir/identity.json) is used separately for the
// bao-jwt flow regardless of which identity is chosen here.
func (a *Agent) zitiIdentityPath() string {
	identityPath, _ := a.root.IdentityPath()
	schmutzDir := filepath.Dir(identityPath)

	if pinned := readPinnedZitiIdentityPath(filepath.Join(schmutzDir, "agent.json")); pinned != "" {
		return pinned
	}
	return resolveZitiIdentityPath(schmutzDir)
}

// readPinnedZitiIdentityPath does a permissive read of agent.json, looking
// only for a ziti_identity_path field. Any error (file missing, malformed
// JSON, field empty) returns "" to fall through to auto-detect. baojwt's
// LoadAgentConfig is too strict here — it validates the full Bao bundle,
// which is not yet present on hosts that haven't completed bao-enroll.
func readPinnedZitiIdentityPath(agentJSONPath string) string {
	data, err := os.ReadFile(agentJSONPath)
	if err != nil {
		return ""
	}
	var cfg struct {
		ZitiIdentityPath string `json:"ziti_identity_path"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ""
	}
	return cfg.ZitiIdentityPath
}

// StartService creates a ZitiHost for the given ServiceRequest, binds all
// listed Ziti services, and registers the host with the agent manager.
//
// Uses the operational Ziti identity (auto-detected) for the data plane,
// not the Bao identity. On LAN hosts this is /etc/ziti/machine-*.json;
// on cloud hosts it falls back to /etc/schmutz/identity.json.
func (a *Agent) StartService(req *ServiceRequest) error {
	host, err := NewZitiHost(a.zitiIdentityPath())
	if err != nil {
		return fmt.Errorf("start service %s: %w", req.Name, err)
	}

	if err := host.BindServices(req.Services); err != nil {
		return fmt.Errorf("start service %s: %w", req.Name, err)
	}

	svc := &service{
		name:    req.Name,
		request: req,
		host:    host,
		agent:   a,
	}

	go svc.monitor()
	a.addService <- svc
	return nil
}

// StartHTTPService creates a ZitiHost, binds the named Ziti service, and
// serves the given HTTP handler directly on the Ziti listener.
// Used by the embedded gateway to expose schmutz-api.{zone} over the overlay.
func (a *Agent) StartHTTPService(serviceName string, handler http.Handler) error {
	host, err := NewZitiHost(a.zitiIdentityPath())
	if err != nil {
		return fmt.Errorf("start http service %s: %w", serviceName, err)
	}
	if err := host.BindHTTPService(serviceName, handler); err != nil {
		return fmt.Errorf("start http service %s: %w", serviceName, err)
	}
	svc := &service{
		name:  serviceName,
		host:  host,
		agent: a,
	}
	go svc.monitor()
	a.addService <- svc
	return nil
}
