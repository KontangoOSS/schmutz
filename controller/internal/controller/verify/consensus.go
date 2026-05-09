package verify

import (
	"fmt"
	"sync"
)

// Verdict is the result of a single verification function.
type Verdict struct {
	Check      string      `json:"check"`          // which check ran
	Passed     bool        `json:"passed"`         // pass/fail
	Confidence string      `json:"confidence"`     // high/medium/low (for fingerprint)
	Reason     string      `json:"reason"`         // why it failed
	Data       interface{} `json:"data,omitempty"` // any resolved data (profile, entity, etc.)
}

// Decision is the final consensus across all verdicts.
type Decision struct {
	Approved   bool      `json:"approved"`
	Quarantine bool      `json:"quarantine"`
	Rejected   bool      `json:"rejected"`
	Reason     string    `json:"reason"`
	Verdicts   []Verdict `json:"verdicts"`
	Attributes []string  `json:"attributes,omitempty"`
	EntityID   string    `json:"entity_id,omitempty"`
	Profile    string    `json:"profile,omitempty"`
	Stage      int       `json:"stage"` // 0-3, maps to service.Stage
}

// Pipeline runs verify functions in parallel and reaches consensus.
type Pipeline struct {
	mu       sync.Mutex
	verdicts []Verdict
	done     chan struct{}
	count    int
	expected int
}

// NewPipeline creates a pipeline that expects n verdicts.
func NewPipeline(expected int) *Pipeline {
	return &Pipeline{
		verdicts: make([]Verdict, 0, expected),
		done:     make(chan struct{}),
		expected: expected,
	}
}

// Submit adds a verdict. When all expected verdicts are in, signals done.
func (p *Pipeline) Submit(v Verdict) {
	p.mu.Lock()
	p.verdicts = append(p.verdicts, v)
	p.count++
	if p.count >= p.expected {
		select {
		case <-p.done:
		default:
			close(p.done)
		}
	}
	p.mu.Unlock()
}

// Wait blocks until all verdicts are submitted.
func (p *Pipeline) Wait() {
	<-p.done
}

// Consensus evaluates all verdicts and returns the final decision.
//
// Rules:
//   - Any ban check fails → rejected
//   - All checks pass + credentials/fingerprint valid → approved (stage 2+)
//   - Checks pass but no credentials/fingerprint → quarantine (stage 0)
//
// Stage assignment:
//   - Credentials with admin entity → stage 3
//   - Credentials (AppRole) → stage 2
//   - High-confidence fingerprint → stored stage (returning device)
//   - No trust signal → stage 0 (quarantine)
func (p *Pipeline) Consensus() *Decision {
	p.mu.Lock()
	defer p.mu.Unlock()

	d := &Decision{Verdicts: p.verdicts}

	var hasCredentials, hasFingerprint bool
	var credAttrs, fpAttrs []string
	var entityID, profileName string

	for _, v := range p.verdicts {
		// Any critical failure → reject immediately
		if !v.Passed && (v.Check == "banned" || v.Check == "hostname" || v.Check == "os") {
			d.Rejected = true
			d.Reason = v.Check + ": " + v.Reason
			return d
		}

		switch v.Check {
		case "credentials":
			if v.Passed {
				hasCredentials = true
				if info, ok := v.Data.(credentialData); ok {
					entityID = info.EntityID
					profileName = info.Profile
					credAttrs = info.Attributes
				}
			}
		case "fingerprint":
			if v.Passed && v.Confidence == "high" {
				hasFingerprint = true
				if attrs, ok := v.Data.([]string); ok {
					fpAttrs = attrs
				}
			}
		}
	}

	// Approved: credentials → stage 2 (contributor)
	if hasCredentials {
		d.Approved = true
		d.Stage = 2 // StageContributor
		d.Attributes = stageAttributes(d.Stage)
		if len(credAttrs) > 0 {
			d.Attributes = credAttrs // profile may override
		}
		d.EntityID = entityID
		d.Profile = profileName
		d.Reason = "trusted credentials"
		return d
	}

	// Approved: fingerprint → keep stored stage (returning device)
	if hasFingerprint {
		d.Approved = true
		d.Stage = stageFromAttrs(fpAttrs)
		if d.Stage < 1 {
			d.Stage = 1 // at least member if returning
		}
		d.Attributes = stageAttributes(d.Stage)
		d.Reason = "fingerprint match (high confidence)"
		return d
	}

	// Quarantine: stage 0
	d.Quarantine = true
	d.Stage = 0
	d.Attributes = stageAttributes(0)
	d.Reason = "no credentials or fingerprint match"
	return d
}

// stageAttributes returns canonical Ziti attributes for a stage number.
func stageAttributes(stage int) []string {
	base := []string{"tunnel", fmt.Sprintf("stage-%d", stage)}
	switch stage {
	case 3:
		base = append(base, "admin-users", "ssh-clients", "web-clients", "infra-hosts")
	case 2:
		base = append(base, "web-clients")
	case 1:
		base = append(base, "web-clients")
	case 0:
		base = append(base, "quarantine")
	}
	return base
}

// stageFromAttrs extracts the stage number from a set of attributes.
func stageFromAttrs(attrs []string) int {
	for _, a := range attrs {
		switch a {
		case "stage-3":
			return 3
		case "stage-2":
			return 2
		case "stage-1":
			return 1
		case "stage-0":
			return 0
		}
	}
	// Legacy: infer from old attributes
	for _, a := range attrs {
		if a == "admin-users" {
			return 3
		}
	}
	return 0
}

// credentialData is the resolved data from a successful credential check.
type credentialData struct {
	EntityID   string
	Profile    string
	Attributes []string
}

// --- Convenience: run the full pipeline for a registration ---

// RunAll executes all verify checks in parallel and returns the consensus.
func (m *Module) RunAll(hostname, os, osVersion, hardwareHash, machineID, roleID, secretID string, macs []string) *Decision {
	checks := 4 // hostname, os, banned, fingerprint
	if roleID != "" {
		checks++
	}
	pipe := NewPipeline(checks)

	// All checks run in parallel
	go func() {
		err := m.Hostname(hostname)
		pipe.Submit(Verdict{
			Check: "hostname", Passed: err == nil,
			Reason: errStr(err),
		})
	}()

	go func() {
		osInfo, err := m.OS(os, osVersion)
		pipe.Submit(Verdict{
			Check: "os", Passed: err == nil,
			Reason: errStr(err), Data: osInfo,
		})
	}()

	go func() {
		ids := append([]string{hostname, hardwareHash}, macs...)
		ban := m.Banned(ids...)
		pipe.Submit(Verdict{
			Check: "banned", Passed: !ban.Banned,
			Reason: joinStrs(ban.Identifiers),
		})
	}()

	go func() {
		matched, attrs := m.Fingerprint(hardwareHash, machineID)
		confidence := ""
		if matched {
			confidence = "high"
		}
		pipe.Submit(Verdict{
			Check: "fingerprint", Passed: matched,
			Confidence: confidence, Data: attrs,
		})
	}()

	if roleID != "" {
		go func() {
			entityID, metadata, err := m.Credentials(roleID, secretID)
			v := Verdict{Check: "credentials", Passed: err == nil, Reason: errStr(err)}
			if err == nil {
				var attrs []string
				var profileName string
				if pn, ok := metadata["profile"].(string); ok {
					profileName = pn
					profile, err := m.Profile(pn)
					if err == nil {
						attrs = profile.Attributes
					}
				}
				v.Data = credentialData{
					EntityID: entityID, Profile: profileName, Attributes: attrs,
				}
			}
			pipe.Submit(v)
		}()
	}

	pipe.Wait()
	return pipe.Consensus()
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func joinStrs(strs []string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for _, s := range strs[1:] {
		result += ", " + s
	}
	return result
}
