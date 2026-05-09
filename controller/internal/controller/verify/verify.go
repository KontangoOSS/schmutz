// Package verify is the core verification module.
// Every controller operation depends on this.
//
// It answers three questions:
//  1. Who is this? (identity resolution across Bao + Ziti)
//  2. Do I trust them? (ban check, credential validation, fingerprint matching)
//  3. What are they allowed to do? (profile, attributes, policies)
package verify

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"git.konoss.org/kore/schmutz/controller/internal/controller/common"
)

// Module holds the SDK clients. Stateless — all state is in Bao/Ziti.
type Module struct {
	C *common.Clients
}

func New(c *common.Clients) *Module {
	return &Module{C: c}
}

// --- 1. WHO IS THIS? ---

// Identity resolves a name (hostname, UUID, nickname) across both backends.
// This is the most fundamental operation — everything else builds on it.
func (m *Module) Identity(name string) (*common.IdentityInfo, error) {
	info := &common.IdentityInfo{Name: name}

	// Bao identity engine (entities)
	entity, err := m.C.Identity.GetEntity(name)
	if err == nil && entity != nil {
		info.InBao = true
		info.BaoEntityID = entity.ID
		info.BaoEntityName = entity.Name
		info.BaoMetadata = entity.Metadata
		info.BaoPolicies = entity.Policies
	}

	// Ziti overlay (identities)
	token, err := m.C.Ziti.Authenticate()
	if err == nil {
		zitiID, attrs, appData, err := m.C.Ziti.GetIdentityByName(token, name)
		if err == nil && zitiID != "" {
			info.InZiti = true
			info.ZitiID = zitiID
			info.ZitiAttributes = attrs
			info.ZitiAppData = appData
		}
	}

	// Bao KV — registered machine
	machineID, nickname, exists := m.C.Bao.LookupByHostname(name)
	if exists {
		info.IsRegistered = true
		info.MachineID = machineID
		info.Nickname = nickname
	}

	// Bao KV — known machine
	known, _ := m.C.Bao.GetKnownMachine(name)
	if known != nil {
		info.IsKnown = true
		info.KnownMachine = known
		if profileName, ok := known["profile"].(string); ok {
			info.ProfileName = profileName
			info.Profile, _ = m.C.Bao.GetProfile(profileName)
		}
	}

	info.Exists = info.InBao || info.InZiti || info.IsRegistered || info.IsKnown
	return info, nil
}

// IdentityByID resolves by Ziti ID or Bao entity ID.
func (m *Module) IdentityByID(id string) (*common.IdentityInfo, error) {
	info := &common.IdentityInfo{}

	// Bao entity by ID
	entity, err := m.C.Identity.GetEntityByID(id)
	if err == nil && entity != nil {
		info.InBao = true
		info.BaoEntityID = entity.ID
		info.BaoEntityName = entity.Name
		info.BaoMetadata = entity.Metadata
		info.Name = entity.Name
	}

	// Ziti identity (UUID is the name)
	token, err := m.C.Ziti.Authenticate()
	if err == nil {
		zitiID, attrs, appData, err := m.C.Ziti.GetIdentityByName(token, id)
		if err == nil && zitiID != "" {
			info.InZiti = true
			info.ZitiID = zitiID
			info.ZitiAttributes = attrs
			info.ZitiAppData = appData
		}
	}

	// Machine record
	machine, _ := m.C.Bao.GetMachine(id)
	if machine != nil {
		info.IsRegistered = true
		info.MachineID = id
		info.Nickname, _ = machine["nickname"].(string)
	}

	info.Exists = info.InBao || info.InZiti || info.IsRegistered
	return info, nil
}

// Machine resolves a machine by hostname, ID, or nickname.
func (m *Module) Machine(machineID, nickname string) (map[string]interface{}, error) {
	data, err := m.C.Bao.GetMachine(machineID)
	if err != nil || data == nil {
		return nil, fmt.Errorf("unknown machine")
	}
	storedNick, _ := data["nickname"].(string)
	if nickname != "" && storedNick != nickname {
		return nil, fmt.Errorf("invalid credentials")
	}
	return data, nil
}

// --- 2. DO I TRUST THEM? ---

// Banned checks if any identifier (hostname, MAC, fingerprint, machine_id) is banned.
func (m *Module) Banned(identifiers ...string) *common.BanInfo {
	info := &common.BanInfo{}
	for _, id := range identifiers {
		if id != "" && m.C.Bao.IsBanned(id) {
			info.Banned = true
			info.Identifiers = append(info.Identifiers, id)
		}
	}
	return info
}

// Credentials validates Bao AppRole credentials.
// Returns entity ID and metadata on success.
func (m *Module) Credentials(roleID, secretID string) (entityID string, metadata map[string]interface{}, err error) {
	if roleID == "" || secretID == "" {
		return "", nil, fmt.Errorf("credentials required")
	}

	result, err := m.C.Bao.AppRoleLogin(roleID, secretID)
	if err != nil {
		return "", nil, fmt.Errorf("invalid credentials: %w", err)
	}

	entityID, _ = result["entity_id"].(string)
	metadata = map[string]interface{}{}

	// Enrich with entity metadata (profile, role, owner)
	if entityID != "" {
		entity, err := m.C.Identity.GetEntityByID(entityID)
		if err == nil && entity != nil {
			for k, v := range entity.Metadata {
				metadata[k] = v
			}
		}
	}

	return entityID, metadata, nil
}

// Fingerprint checks if a hardware fingerprint matches a known device.
// Returns the matched entity's profile attributes if found.
func (m *Module) Fingerprint(hardwareHash, machineID string) (matched bool, attrs []string) {
	if hardwareHash != "" {
		data, _ := m.C.Bao.Get("secret", "identities/devices/by-fingerprint/"+hardwareHash)
		if data != nil {
			return true, m.resolveEntityAttrs(data)
		}
	}
	if machineID != "" {
		data, _ := m.C.Bao.Get("secret", "identities/devices/by-machine-id/"+machineID)
		if data != nil {
			return true, m.resolveEntityAttrs(data)
		}
	}
	return false, nil
}

func (m *Module) resolveEntityAttrs(deviceData map[string]interface{}) []string {
	entityID, _ := deviceData["entity_id"].(string)
	if entityID == "" {
		return nil
	}
	entity, err := m.C.Identity.GetEntityByID(entityID)
	if err != nil || entity == nil {
		return nil
	}
	if profileName, ok := entity.Metadata["profile"]; ok {
		profile, _ := m.C.Bao.GetProfile(profileName)
		if profile != nil {
			if attrStr, ok := profile["attributes"].(string); ok {
				return strings.Split(attrStr, ",")
			}
		}
	}
	return nil
}

// OS checks if an OS version is in the Bao catalog.
func (m *Module) OS(osFamily, osVersion string) (*common.OSInfo, error) {
	distro, version := ParseOSVersion(osFamily, osVersion)
	if distro == "" || version == "" {
		// Unknown OS version is not a rejection — log and pass with unknown distro.
		return &common.OSInfo{Family: osFamily, Distro: "unknown", Version: "unknown", Supported: true}, nil
	}

	path := fmt.Sprintf("os/%s/%s/%s", osFamily, distro, version)
	data, err := m.C.Bao.Get("public", path)
	// If OS not in Bao, allow it anyway (permissive enrollment mode)
	if err != nil || data == nil {
		// Accept any parseable OS for now
		return &common.OSInfo{
			Ref: "public/" + path, Family: osFamily, Distro: distro,
			Version: version, Codename: "", Supported: true,
		}, nil
	}

	supported := false
	if s, ok := data["supported"].(string); ok {
		supported = s == "true"
	}
	if !supported {
		return nil, fmt.Errorf("OS not supported: %s/%s/%s", osFamily, distro, version)
	}

	codename, _ := data["codename"].(string)
	return &common.OSInfo{
		Ref: "public/" + path, Family: osFamily, Distro: distro,
		Version: version, Codename: codename, Supported: true,
	}, nil
}

// --- 3. WHAT ARE THEY ALLOWED TO DO? ---

// Profile resolves a profile name into the full chain (app, build, sizing).
func (m *Module) Profile(name string) (*common.ProfileInfo, error) {
	data, err := m.C.Bao.GetProfile(name)
	if err != nil || data == nil {
		return nil, fmt.Errorf("profile %q not found", name)
	}

	info := &common.ProfileInfo{Name: name, Data: data}

	// Attributes
	if attrStr, ok := data["attributes"].(string); ok {
		info.Attributes = strings.Split(attrStr, ",")
	}

	// Resolve application → build
	if appRef, ok := data["application"].(string); ok && strings.HasPrefix(appRef, "public/") {
		appPath := strings.TrimPrefix(appRef, "public/")
		info.Application, _ = m.C.Bao.Get("public", appPath)
		if buildRef, err := m.C.Bao.Get("public", appPath+"/builds/latest"); err == nil && buildRef != nil {
			if ref, ok := buildRef["ref"].(string); ok {
				info.Build, _ = m.C.Bao.Get("public", appPath+"/builds/"+ref)
			}
		}
	}

	// Resolve sizing
	if sizingRef, ok := data["sizing"].(string); ok && strings.HasPrefix(sizingRef, "public/") {
		info.Sizing, _ = m.C.Bao.Get("public", strings.TrimPrefix(sizingRef, "public/"))
	}

	return info, nil
}

// --- Input validation ---

var (
	HostnameRe  = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,62}$`)
	MacAddrRe   = regexp.MustCompile(`^([0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}$`)
	HexRe       = regexp.MustCompile(`^[0-9a-fA-F]+$`)
	PrintableRe = regexp.MustCompile(`^[\x20-\x7E]+$`)
	ValidOS     = map[string]bool{"linux": true, "darwin": true, "windows": true}
	ValidArch   = map[string]bool{"amd64": true, "arm64": true, "arm": true, "386": true, "": true}
)

// Hostname validates a hostname string.
func (m *Module) Hostname(hostname string) error {
	if !HostnameRe.MatchString(strings.TrimSpace(hostname)) {
		return fmt.Errorf("invalid hostname")
	}
	return nil
}

// Timezone validates and normalizes a timezone string.
func (m *Module) Timezone(tz string) string {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		return ""
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return ""
	}
	return tz
}

// SanitizeMACs validates and normalizes MAC addresses.
func (m *Module) SanitizeMACs(macs []string) []string {
	if len(macs) > 16 {
		macs = macs[:16]
	}
	result := make([]string, 0, len(macs))
	for _, mac := range macs {
		mac = strings.TrimSpace(strings.ToLower(mac))
		if MacAddrRe.MatchString(mac) {
			result = append(result, mac)
		}
	}
	return result
}

// SanitizeString trims and validates a printable string.
func (m *Module) SanitizeString(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" || !PrintableRe.MatchString(s) {
		return ""
	}
	if len(s) > max {
		return s[:max]
	}
	return s
}

// SanitizeHex trims and validates a hex string.
func (m *Module) SanitizeHex(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" || !HexRe.MatchString(s) {
		return ""
	}
	if len(s) > max {
		return s[:max]
	}
	return s
}

// --- OS version parsing ---

// ParseOSVersion extracts distro and version from an OS version string.
func ParseOSVersion(osFamily, version string) (distro, ver string) {
	patterns := []struct {
		prefix string
		distro string
	}{
		{"Ubuntu ", "ubuntu"},
		{"Debian GNU/Linux ", "debian"},
		{"Proxmox VE ", "proxmox"},
		{"Rocky Linux ", "rocky"},
		{"Alpine Linux ", "alpine"},
		{"Talos ", "talos"},
		{"macOS ", "macos"},
		{"Fedora Linux ", "fedora"},
		{"Arch Linux", "arch"},
	}

	for _, p := range patterns {
		if strings.Contains(version, p.prefix) || strings.HasPrefix(version, p.prefix) {
			rest := strings.TrimPrefix(version, p.prefix)
			parts := strings.Fields(rest)
			if len(parts) > 0 {
				ver = strings.TrimRight(parts[0], ".")
				if idx := strings.Index(ver, "("); idx > 0 {
					ver = strings.TrimSpace(ver[:idx])
				}
				// Normalize point releases: 24.04.3 → 24.04, 12.8 → 12
				// Keep only major.minor for Ubuntu/Alpine, major for Debian/Rocky
				dotParts := strings.Split(ver, ".")
				switch p.distro {
				case "ubuntu", "alpine", "talos":
					if len(dotParts) > 2 {
						ver = dotParts[0] + "." + dotParts[1]
					}
				case "debian", "rocky", "proxmox":
					ver = dotParts[0]
				}
			}
			if p.distro == "arch" {
				ver = "rolling"
			}
			return p.distro, ver
		}
	}

	// Windows
	if strings.Contains(version, "Windows") {
		if strings.Contains(version, "Server") {
			for _, p := range strings.Fields(version) {
				if len(p) == 4 && p[0] >= '2' {
					return "server", p
				}
			}
		} else {
			for _, p := range strings.Fields(version) {
				if p == "10" || p == "11" {
					return "desktop", p
				}
			}
		}
	}

	return "", ""
}
