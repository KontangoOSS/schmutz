package service

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
)

// EnrollmentService handles the enrollment proxy and CA patching.
type EnrollmentService struct {
	ZitiBin      string // path to ziti binary
	CABundlePath string // path to full CA bundle PEM
	// PublicZtAPI overrides the ztAPI address in enrolled identities.
	// Set this to the public-facing controller address (e.g. "https://ctrl-1.kontango.io:1280/edge/client/v1")
	// so that new machines can reach the controller before the overlay is up.
	PublicZtAPI string
	// PublicZtAPIs is a comma-separated list of all controller addresses for HA.
	// When set, enableHa is set to true and ztAPIs is populated so enrolled machines
	// can fail over across controllers automatically.
	PublicZtAPIs string
}

// Enroll runs `ziti edge enroll` with the given JWT and returns the identity JSON
// with the CA bundle patched to include all cluster intermediates.
func (e *EnrollmentService) Enroll(jwtToken string) ([]byte, error) {
	// Write JWT to temp file
	tmp, err := os.CreateTemp("", "enroll-*.jwt")
	if err != nil {
		return nil, fmt.Errorf("create temp: %w", err)
	}
	tmp.WriteString(jwtToken)
	tmp.Close()
	defer os.Remove(tmp.Name())

	outFile := tmp.Name() + ".json"
	defer os.Remove(outFile)

	// Run enrollment
	args := []string{"edge", "enroll", "-j", tmp.Name(), "-o", outFile, "-a", "EC"}
	if e.CABundlePath != "" {
		args = append(args, "--ca", e.CABundlePath)
	}
	cmd := exec.Command(e.ZitiBin, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("enroll failed: %s", string(out))
	}

	identity, err := os.ReadFile(outFile)
	if err != nil {
		return nil, fmt.Errorf("read identity: %w", err)
	}

	// Patch CA bundle
	identity = e.patchCABundle(identity)

	// Patch ztAPI to the public address so new machines can bootstrap.
	identity = e.patchZtAPI(identity)

	return identity, nil
}

// patchZtAPI replaces the ztAPI in the identity with the public-facing address
// and sets ztAPIs + enableHa for HA clusters.
func (e *EnrollmentService) patchZtAPI(identity []byte) []byte {
	if e.PublicZtAPI == "" {
		return identity
	}

	var doc map[string]interface{}
	if json.Unmarshal(identity, &doc) != nil {
		return identity
	}

	old, _ := doc["ztAPI"].(string)
	doc["ztAPI"] = e.PublicZtAPI

	// Enable HA mode if multiple controller addresses are configured.
	if e.PublicZtAPIs != "" {
		apis := strings.Split(e.PublicZtAPIs, ",")
		trimmed := make([]string, 0, len(apis))
		for _, a := range apis {
			if t := strings.TrimSpace(a); t != "" {
				trimmed = append(trimmed, t)
			}
		}
		if len(trimmed) > 1 {
			doc["ztAPIs"] = trimmed
			doc["enableHa"] = true
			log.Printf("  patched ztAPI: %s → %s (HA, %d controllers)", old, e.PublicZtAPI, len(trimmed))
		}
	} else {
		log.Printf("  patched ztAPI: %s → %s", old, e.PublicZtAPI)
	}

	patched, err := json.Marshal(doc)
	if err != nil {
		return identity
	}
	return patched
}

// patchCABundle replaces the identity's CA with the full cluster CA chain.
func (e *EnrollmentService) patchCABundle(identity []byte) []byte {
	if e.CABundlePath == "" {
		return identity
	}
	fullCA, err := os.ReadFile(e.CABundlePath)
	if err != nil {
		return identity
	}

	var doc map[string]interface{}
	if json.Unmarshal(identity, &doc) != nil {
		return identity
	}

	idSection, ok := doc["id"].(map[string]interface{})
	if !ok {
		return identity
	}

	idSection["ca"] = string(fullCA)
	patched, err := json.Marshal(doc)
	if err != nil {
		return identity
	}

	log.Printf("  patched CA bundle (%d certs)", strings.Count(string(fullCA), "BEGIN CERTIFICATE"))
	return patched
}
