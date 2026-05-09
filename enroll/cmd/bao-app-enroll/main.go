// bao-app-enroll is the operator-side first-time enrollment tool for an
// app (tenant, app, deployment). It does the full Stage-1 provisioning:
//
//   1. (Default) Mints a fresh ziti identity via the controller's ziti
//      management API. Returns the identity name + a one-time-use
//      enrollment JWT for the operator to ship to the agent.
//
//   2. Creates / upserts the Bao identity-engine entity carrying the
//      tenant + app + deployment + flavor + ziti_identity metadata.
//
//   3. Writes the ziti-identity → entity-name index entry that the
//      controller's /api/bao-bundle handler reads to resolve callers.
//
//   4. (Optional) Writes the per-deployment substrate spec to
//      <tenant>/secret/apps/<app>/<deployment>/substrate. If the spec's
//      ziti_identity field is empty, the freshly-minted identity name is
//      stamped in. If non-empty, it must match.
//
// Usage:
//
//	# Common case: mint a new identity, write substrate
//	bao-app-enroll \
//	  --tenant kontango --app inventree --deployment prod-01 \
//	  --flavor app-host \
//	  --substrate /path/to/inventree-prod-01.yaml
//
//	# Late-binding case: identity already exists (bring-existing)
//	bao-app-enroll \
//	  --tenant kontango --app inventree --deployment prod-01 \
//	  --flavor app-host \
//	  --ziti-identity machine-f6f769f1
//
// Env required: BAO_ADDR, BAO_TOKEN (admin), ZITI_API, ZITI_USERNAME,
// ZITI_PASSWORD.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"git.konoss.org/kore/schmutz/enroll/internal/bao"
	"git.konoss.org/kore/schmutz/shared"
	"git.konoss.org/kore/schmutz/enroll/internal/ziti"
)

func main() {
	tenant := flag.String("tenant", "", "tenant slug (required)")
	app := flag.String("app", "", "app slug (required)")
	deployment := flag.String("deployment", "", "deployment slug, e.g. prod-01 (required)")
	flavor := flag.String("flavor", "app-host", "flavor tag (free-form)")
	zitiIDFlag := flag.String("ziti-identity", "",
		"OPTIONAL: existing ziti identity name (e.g. machine-f6f769f1). "+
			"If omitted, a fresh identity is minted via ziti and its enrollment JWT printed.")
	substratePath := flag.String("substrate", "", "optional path to a substrate yaml/json file")
	jwtOut := flag.String("jwt-out", "",
		"OPTIONAL: write the new identity's enrollment JWT to this file (mode 0600). "+
			"If omitted, the JWT is printed to stdout. Ignored when --ziti-identity is provided.")
	flag.Parse()

	if *tenant == "" || *app == "" || *deployment == "" {
		fmt.Fprintln(os.Stderr, "tenant, app, deployment all required")
		flag.Usage()
		os.Exit(2)
	}

	addr := os.Getenv("BAO_ADDR")
	tok := os.Getenv("BAO_TOKEN")
	if addr == "" || tok == "" {
		fmt.Fprintln(os.Stderr, "BAO_ADDR + BAO_TOKEN required")
		os.Exit(2)
	}
	zitiAPI := os.Getenv("ZITI_API")
	zitiUser := os.Getenv("ZITI_USERNAME")
	zitiPass := os.Getenv("ZITI_PASSWORD")
	if *zitiIDFlag == "" && (zitiAPI == "" || zitiUser == "" || zitiPass == "") {
		fmt.Fprintln(os.Stderr,
			"ZITI_API + ZITI_USERNAME + ZITI_PASSWORD required when --ziti-identity is omitted")
		os.Exit(2)
	}

	ctx := context.Background()

	// -- Resolve ziti identity: generate or look up existing. --
	var (
		zitiID  string
		zitiJWT string
	)
	if *zitiIDFlag == "" {
		zc := ziti.NewHTTP(zitiAPI, zitiUser, zitiPass, true)
		newName := generateMachineName()
		// Role attrs: bao-clients lets it dial bao.tango (and
		// ziti-base.tango via the same dial policy); host-<app> is a
		// declared marker the operator can use later for app-scoped
		// service policies; quarantine matches the existing controller
		// convention for newly-minted identities.
		roleAttrs := []string{"bao-clients", "host-" + *app, "quarantine"}
		tags := map[string]string{
			"tenant":     *tenant,
			"app":        *app,
			"deployment": *deployment,
			"flavor":     *flavor,
		}
		res, err := zc.CreateBareIdentity(ctx, newName, roleAttrs, tags)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ziti: create identity %q: %v\n", newName, err)
			os.Exit(1)
		}
		zitiID = res.IdentityName
		zitiJWT = res.JWT
		fmt.Printf("ziti: minted identity name=%s id=%s attrs=%v\n",
			zitiID, res.IdentityID, roleAttrs)
	} else {
		zitiID = *zitiIDFlag
		zc := ziti.NewHTTP(zitiAPI, zitiUser, zitiPass, true)
		ident, err := zc.GetIdentity(ctx, zitiID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ziti: identity %q lookup failed: %v\n", zitiID, err)
			os.Exit(1)
		}
		fmt.Printf("ziti: using existing identity name=%s id=%s attrs=%v\n",
			ident.Name, ident.ID, ident.RoleAttributes)
	}

	// -- Validate substrate file FIRST if provided. The validator runs
	//    BEFORE any Bao write so an invalid spec doesn't leave half-applied
	//    state. If the spec's ziti_identity is empty, fill in our resolved
	//    one (operator authored a substrate without knowing the identity
	//    name yet). --
	var substrateJSON []byte
	if *substratePath != "" {
		spec, err := loadSubstrate(*substratePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "substrate: %v\n", err)
			os.Exit(1)
		}
		if spec.ZitiIdentity == "" {
			spec.ZitiIdentity = zitiID
		}
		if err := validateSubstrate(spec, *tenant, *app, *deployment, zitiID); err != nil {
			fmt.Fprintf(os.Stderr, "substrate: %v\n", err)
			os.Exit(1)
		}
		substrateJSON, err = json.MarshalIndent(spec, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "substrate: marshal: %v\n", err)
			os.Exit(1)
		}
	}

	// -- Bao writes: entity, ziti-index, optionally substrate. --
	cli := bao.NewHTTP(addr, tok, "secret", "enroll-tokens", true)
	admin, ok := cli.(bao.OIDCAdmin)
	if !ok {
		fmt.Fprintln(os.Stderr, "client does not implement OIDCAdmin (build mismatch)")
		os.Exit(2)
	}
	kv := cli // root-namespace KV

	entityName := fmt.Sprintf("%s-%s-%s", *tenant, *app, *deployment)
	md := map[string]string{
		"tenant":        *tenant,
		"app":           *app,
		"deployment":    *deployment,
		"flavor":        *flavor,
		"ziti_identity": zitiID,
	}
	ent, err := admin.UpsertEntity(ctx, entityName, md)
	if err != nil {
		fmt.Fprintf(os.Stderr, "upsert entity: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("entity: id=%s name=%s\n", ent.ID, ent.Name)

	idxBody, _ := json.Marshal(map[string]string{"entity": entityName})
	indexPath := fmt.Sprintf("schmutz/ziti-index/%s", zitiID)
	if err := kv.WriteJSON(ctx, indexPath, idxBody); err != nil {
		fmt.Fprintf(os.Stderr, "write index: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("index: %s -> %s\n", indexPath, entityName)

	if substrateJSON != nil {
		nsKV := bao.NewNamespacedKV(addr, tok, true)
		path := fmt.Sprintf("apps/%s/%s/substrate", *app, *deployment)
		if err := nsKV.WriteJSON(ctx, *tenant+"/", "secret", path, substrateJSON); err != nil {
			fmt.Fprintf(os.Stderr, "write substrate: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("substrate: %s/secret/data/%s (%d bytes)\n",
			strings.TrimRight(*tenant, "/"), path, len(substrateJSON))
	}

	// -- Print or write the enrollment JWT. Only when we minted. --
	if zitiJWT != "" {
		if *jwtOut != "" {
			if err := os.WriteFile(*jwtOut, []byte(zitiJWT), 0600); err != nil {
				fmt.Fprintf(os.Stderr, "write jwt-out: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("jwt: written to %s (mode 0600)\n", *jwtOut)
		} else {
			fmt.Println("jwt: (paste into the agent at install time)")
			fmt.Println(zitiJWT)
		}
	}

	fmt.Println("OK — agent for", zitiID, "can now call /api/bao-bundle")
}

// generateMachineName produces a name matching the existing controller
// convention: `machine-` + 8 lowercase hex chars. Mirrors the helper in
// cmd/enroll-server/main.go; kept inline here so this binary doesn't
// depend on enroll-server.
func generateMachineName() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return "machine-" + hex.EncodeToString(b)
}

// loadSubstrate reads a substrate spec from disk. yaml or json by extension.
func loadSubstrate(path string) (*shared.Schmutz, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var spec shared.Schmutz
	if strings.HasSuffix(path, ".json") {
		if err := json.Unmarshal(raw, &spec); err != nil {
			return nil, fmt.Errorf("parse %s as json: %w", path, err)
		}
	} else {
		if err := yaml.Unmarshal(raw, &spec); err != nil {
			return nil, fmt.Errorf("parse %s as yaml: %w", path, err)
		}
	}
	if spec.Version == 0 {
		spec.Version = shared.SchmutzSchemaVersion
	}
	return &spec, nil
}

// validateSubstrate runs internal/substrate's validator and additionally
// requires the spec's identity fields match the flags the operator
// passed (or the freshly-minted ziti identity).
func validateSubstrate(spec *shared.Schmutz, tenant, app, deployment, zitiID string) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	if err := spec.MatchesPath(tenant, app, deployment); err != nil {
		return err
	}
	if spec.ZitiIdentity != zitiID {
		return fmt.Errorf(
			"ziti_identity %q in substrate file doesn't match resolved identity %q",
			spec.ZitiIdentity, zitiID,
		)
	}
	return nil
}
