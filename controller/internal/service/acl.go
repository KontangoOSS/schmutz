package service

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// Stage represents a trust level in the network.
type Stage int

const (
	StageEdge        Stage = 0 // quarantine — SSH to self only
	StageMember      Stage = 1 // core services — monitoring, logging
	StageContributor Stage = 2 // app services — hosting, DB access
	StageAdmin       Stage = 3 // fabric — Ziti mgmt, Bao, controllers
)

func (s Stage) String() string {
	switch s {
	case StageEdge:
		return "edge"
	case StageMember:
		return "member"
	case StageContributor:
		return "contributor"
	case StageAdmin:
		return "admin"
	default:
		return "unknown"
	}
}

func ParseStage(s string) (Stage, error) {
	switch strings.ToLower(s) {
	case "edge", "0":
		return StageEdge, nil
	case "member", "1":
		return StageMember, nil
	case "contributor", "2":
		return StageContributor, nil
	case "admin", "3":
		return StageAdmin, nil
	default:
		return -1, fmt.Errorf("unknown stage: %q", s)
	}
}

// StageAttributes returns the canonical Ziti attributes for a stage.
// These are the ONLY attributes an identity should have (plus #host-* roles).
func StageAttributes(s Stage) []string {
	base := []string{"tunnel", fmt.Sprintf("stage-%d", int(s))}
	switch s {
	case StageAdmin:
		base = append(base, "admin-users", "ssh-clients", "web-clients", "infra-hosts")
	case StageContributor:
		base = append(base, "web-clients")
	case StageMember:
		base = append(base, "web-clients")
	case StageEdge:
		base = append(base, "quarantine")
	}
	return base
}

// StageFromAttributes extracts the stage from a set of Ziti attributes.
// Returns StageEdge if no stage attribute is found.
func StageFromAttributes(attrs []string) Stage {
	for _, a := range attrs {
		switch a {
		case "stage-3":
			return StageAdmin
		case "stage-2":
			return StageContributor
		case "stage-1":
			return StageMember
		case "stage-0":
			return StageEdge
		}
	}
	// Legacy: infer from old attributes
	for _, a := range attrs {
		if a == "admin-users" {
			return StageAdmin
		}
	}
	return StageEdge
}

// HostRolesFromAttributes extracts #host-<nickname> roles from attributes.
func HostRolesFromAttributes(attrs []string) []string {
	var roles []string
	for _, a := range attrs {
		if strings.HasPrefix(a, "host-") {
			roles = append(roles, strings.TrimPrefix(a, "host-"))
		}
	}
	return roles
}

// AccessReport is the full access picture for an identity.
type AccessReport struct {
	Identity   string   `json:"identity"`
	ZitiID     string   `json:"ziti_id"`
	Stage      Stage    `json:"stage"`
	StageName  string   `json:"stage_name"`
	CanDial    []string `json:"can_dial"`
	CanBind    []string `json:"can_bind"`
	HostRoles  []string `json:"host_roles"`
	Attributes []string `json:"attributes"`
}

// ACLService evaluates and enforces access across Ziti and Bao.
type ACLService struct {
	ziti  *ZitiService
	store *StoreService
}

func NewACLService(ziti *ZitiService, store *StoreService) *ACLService {
	return &ACLService{ziti: ziti, store: store}
}

// StageOf returns the current stage for a Ziti identity.
func (a *ACLService) StageOf(token, identityNameOrID string) (Stage, string, error) {
	id, attrs, _, err := a.ziti.GetIdentityByName(token, identityNameOrID)
	if err != nil {
		return StageEdge, "", err
	}
	return StageFromAttributes(attrs), id, nil
}

// Promote raises an identity to a higher stage.
func (a *ACLService) Promote(token, identityNameOrID string, to Stage, by string) error {
	id, attrs, _, err := a.ziti.GetIdentityByName(token, identityNameOrID)
	if err != nil {
		return fmt.Errorf("identity %q: %w", identityNameOrID, err)
	}

	current := StageFromAttributes(attrs)
	if to <= current {
		return fmt.Errorf("identity already at stage %d (%s), cannot promote to %d (%s)",
			current, current, to, to)
	}

	return a.setStage(token, id, identityNameOrID, attrs, to, by, "promote")
}

// Demote drops an identity to a lower stage.
func (a *ACLService) Demote(token, identityNameOrID string, to Stage, reason string) error {
	id, attrs, _, err := a.ziti.GetIdentityByName(token, identityNameOrID)
	if err != nil {
		return fmt.Errorf("identity %q: %w", identityNameOrID, err)
	}

	current := StageFromAttributes(attrs)
	if to >= current {
		return fmt.Errorf("identity at stage %d (%s), cannot demote to %d (%s)",
			current, current, to, to)
	}

	return a.setStage(token, id, identityNameOrID, attrs, to, reason, "demote")
}

// setStage is the core stage transition. Updates Ziti attributes + Bao metadata.
func (a *ACLService) setStage(token, zitiID, name string, currentAttrs []string, to Stage, byOrReason, action string) error {
	// Preserve host roles — stages change, host bindings don't
	hostRoles := HostRolesFromAttributes(currentAttrs)

	// Build new attribute set
	newAttrs := StageAttributes(to)
	for _, hr := range hostRoles {
		newAttrs = append(newAttrs, "host-"+hr)
	}

	// Update Ziti identity
	if err := a.ziti.UpdateIdentity(token, zitiID, newAttrs); err != nil {
		return fmt.Errorf("update identity: %w", err)
	}

	// Update Bao entity metadata
	meta := map[string]interface{}{
		"stage":      int(to),
		"stage_name": to.String(),
		"host_roles": hostRoles,
		"updated_at": time.Now().Unix(),
	}
	if action == "promote" {
		meta["promoted_at"] = time.Now().Unix()
		meta["promoted_by"] = byOrReason
	} else {
		meta["demoted_at"] = time.Now().Unix()
		meta["demote_reason"] = byOrReason
	}
	a.store.Put("secret", "identities/acl/"+zitiID, meta)

	log.Printf("acl: %s %s → stage-%d (%s) by=%s", action, name, to, to, byOrReason)
	return nil
}

// GrantHost gives a machine the #host-<nickname> attribute.
func (a *ACLService) GrantHost(token, identityNameOrID, serviceNickname string) error {
	id, attrs, _, err := a.ziti.GetIdentityByName(token, identityNameOrID)
	if err != nil {
		return err
	}

	hostAttr := "host-" + serviceNickname
	for _, a := range attrs {
		if a == hostAttr {
			return nil // already has it
		}
	}

	attrs = append(attrs, hostAttr)
	return a.ziti.UpdateIdentity(token, id, attrs)
}

// RevokeHost removes a machine's host role.
func (a *ACLService) RevokeHost(token, identityNameOrID, serviceNickname string) error {
	id, attrs, _, err := a.ziti.GetIdentityByName(token, identityNameOrID)
	if err != nil {
		return err
	}

	hostAttr := "host-" + serviceNickname
	newAttrs := make([]string, 0, len(attrs))
	for _, a := range attrs {
		if a != hostAttr {
			newAttrs = append(newAttrs, a)
		}
	}

	return a.ziti.UpdateIdentity(token, id, newAttrs)
}

// Audit builds the full access report for an identity.
func (a *ACLService) Audit(token, identityNameOrID string) (*AccessReport, error) {
	id, attrs, _, err := a.ziti.GetIdentityByName(token, identityNameOrID)
	if err != nil {
		return nil, err
	}

	stage := StageFromAttributes(attrs)
	hostRoles := HostRolesFromAttributes(attrs)

	report := &AccessReport{
		Identity:   identityNameOrID,
		ZitiID:     id,
		Stage:      stage,
		StageName:  stage.String(),
		HostRoles:  hostRoles,
		Attributes: attrs,
	}

	// Resolve what this stage can dial
	switch {
	case stage >= StageAdmin:
		report.CanDial = []string{"#quarantine-services", "#core-services", "#web-services", "#db-services", "#fabric-services", "#ssh-services"}
	case stage >= StageContributor:
		report.CanDial = []string{"#quarantine-services", "#core-services", "#web-services", "#db-services"}
	case stage >= StageMember:
		report.CanDial = []string{"#quarantine-services", "#core-services"}
	default:
		report.CanDial = []string{"#quarantine-services"}
	}

	// Resolve what this identity can bind
	for _, hr := range hostRoles {
		report.CanBind = append(report.CanBind, "ssh-"+hr, "app-"+hr)
	}

	return report, nil
}

// Summary returns stage info for all identities.
func (a *ACLService) Summary(token string) ([]map[string]interface{}, error) {
	identities, err := a.ziti.ListIdentities(token, 500)
	if err != nil {
		return nil, err
	}

	var result []map[string]interface{}
	for _, ident := range identities {
		// Skip honeypot/test identities
		if strings.Contains(ident.Name, "honeypot@") || strings.Contains(ident.Name, "fresh@") ||
			strings.Contains(ident.Name, "test@") || strings.Contains(ident.Name, "join@") {
			continue
		}
		// Skip routers
		if ident.Type != nil {
			if t, ok := ident.Type["name"]; ok && t == "Router" {
				continue
			}
		}

		stage := StageFromAttributes(ident.RoleAttributes)
		hostRoles := HostRolesFromAttributes(ident.RoleAttributes)

		result = append(result, map[string]interface{}{
			"name":       ident.Name,
			"id":         ident.ID,
			"stage":      int(stage),
			"stage_name": stage.String(),
			"host_roles": hostRoles,
			"attributes": ident.RoleAttributes,
			"online":     ident.HasEdgeRouter,
		})
	}

	return result, nil
}

// --- Canonical Policy Sync ---

// CanonicalPolicy defines a Ziti service policy that the controller owns.
type CanonicalPolicy struct {
	Name          string
	Type          string // Bind or Dial
	Semantic      string // AnyOf
	IdentityRoles []string
	ServiceRoles  []string
}

// CanonicalPolicies returns the 13 policies that define the ACL model.
func CanonicalPolicies() []CanonicalPolicy {
	return []CanonicalPolicy{
		// --- Dial policies: who can access what ---

		// All stages can reach quarantine (the floor)
		{
			Name: "stage-0-dial", Type: "Dial", Semantic: "AnyOf",
			IdentityRoles: []string{"#stage-0", "#stage-1", "#stage-2", "#stage-3"},
			ServiceRoles:  []string{"#quarantine-services"},
		},
		// Stage 1+ can reach core (monitoring, logging, catalog)
		{
			Name: "stage-1-dial", Type: "Dial", Semantic: "AnyOf",
			IdentityRoles: []string{"#stage-1", "#stage-2", "#stage-3"},
			ServiceRoles:  []string{"#core-services"},
		},
		// Stage 2+ can reach web services (app UIs and APIs)
		{
			Name: "stage-2-dial", Type: "Dial", Semantic: "AnyOf",
			IdentityRoles: []string{"#stage-2", "#stage-3"},
			ServiceRoles:  []string{"#web-services"},
		},
		// Stage 2+ can reach DB (MFA enforced via posture check, not here)
		{
			Name: "db-dial", Type: "Dial", Semantic: "AnyOf",
			IdentityRoles: []string{"#stage-2", "#stage-3"},
			ServiceRoles:  []string{"#db-services"},
		},
		// Stage 3 only: fabric (Ziti mgmt, Bao, controllers)
		{
			Name: "stage-3-dial", Type: "Dial", Semantic: "AnyOf",
			IdentityRoles: []string{"#stage-3"},
			ServiceRoles:  []string{"#fabric-services"},
		},
		// Stage 3 only: SSH to any machine
		{
			Name: "ssh-dial", Type: "Dial", Semantic: "AnyOf",
			IdentityRoles: []string{"#stage-3"},
			ServiceRoles:  []string{"#ssh-services"},
		},
		// Edge proxies: Caddy dials web + fabric (for transport ziti)
		{
			Name: "edge-dial", Type: "Dial", Semantic: "AnyOf",
			IdentityRoles: []string{"#do-caddy"},
			ServiceRoles:  []string{"#web-services", "#fabric-services"},
		},
		// Devices dial their own services
		{
			Name: "device-dial", Type: "Dial", Semantic: "AnyOf",
			IdentityRoles: []string{"#device"},
			ServiceRoles:  []string{"#device-services"},
		},

		// --- Bind policies: who hosts what ---

		// All tunnels bind web + DB + quarantine services
		{
			Name: "tango-bind", Type: "Bind", Semantic: "AnyOf",
			IdentityRoles: []string{"#tunnel"},
			ServiceRoles:  []string{"#web-services", "#db-services", "#quarantine-services"},
		},
		// All tunnels bind SSH
		{
			Name: "ssh-bind", Type: "Bind", Semantic: "AnyOf",
			IdentityRoles: []string{"#tunnel"},
			ServiceRoles:  []string{"#ssh-services"},
		},
		// Infra hosts bind fabric services
		{
			Name: "infra-bind", Type: "Bind", Semantic: "AnyOf",
			IdentityRoles: []string{"#infra-hosts"},
			ServiceRoles:  []string{"#fabric-services"},
		},
		// K8s cluster binds core services (monitoring stack)
		{
			Name: "k8s-bind", Type: "Bind", Semantic: "AnyOf",
			IdentityRoles: []string{"#k8s-hosts"},
			ServiceRoles:  []string{"#core-services"},
		},
		// Devices bind their own services
		{
			Name: "device-bind", Type: "Bind", Semantic: "AnyOf",
			IdentityRoles: []string{"#device"},
			ServiceRoles:  []string{"#device-services"},
		},
	}
}

// SyncPolicies ensures all canonical policies exist in Ziti.
// Creates missing ones, updates existing ones that don't match.
// Idempotent — safe to call on every startup.
func (a *ACLService) SyncPolicies(token string) error {
	for _, cp := range CanonicalPolicies() {
		existingID, err := a.ziti.FindByName(token, "service-policies", cp.Name)
		if err != nil {
			log.Printf("acl: sync check %s: %v", cp.Name, err)
			continue
		}

		if existingID == "" {
			// Create
			err := a.ziti.CreateServicePolicy(token, cp.Name, cp.Type, cp.Semantic,
				cp.IdentityRoles, cp.ServiceRoles)
			if err != nil {
				log.Printf("acl: create policy %s: %v", cp.Name, err)
			} else {
				log.Printf("acl: created policy %s", cp.Name)
			}
		} else {
			log.Printf("acl: policy %s exists (%s)", cp.Name, existingID)
		}
	}
	return nil
}
