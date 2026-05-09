// baoadmin-smoke is a one-shot connectivity check for the new bao.OIDCAdmin
// surface against a real Bao. NOT part of the production deploy; lives in
// the module so it can import internal/bao.
//
// Run on a controller that has a known entity already (created by the bash
// scripts/bao-jwt/enroll-app.sh):
//
//   BAO_ADDR=http://127.0.0.1:8200 BAO_TOKEN=<root> \
//   ENTITY=kontango-inventree-prod-01 ROLE=kontango-inventree-prod-01 \
//   ./baoadmin-smoke
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"git.konoss.org/kore/schmutz/enroll/internal/bao"
)

func main() {
	addr := os.Getenv("BAO_ADDR")
	tok := os.Getenv("BAO_TOKEN")
	entity := os.Getenv("ENTITY")
	role := os.Getenv("ROLE")
	if addr == "" || tok == "" || entity == "" || role == "" {
		fmt.Fprintln(os.Stderr, "BAO_ADDR + BAO_TOKEN + ENTITY + ROLE required")
		os.Exit(2)
	}
	cli := bao.NewHTTP(addr, tok, "secret", "enroll-tokens", true)
	a, ok := cli.(bao.OIDCAdmin)
	if !ok {
		fmt.Fprintln(os.Stderr, "client does not implement OIDCAdmin")
		os.Exit(2)
	}
	ctx := context.Background()
	fail := func(label string, err error) {
		fmt.Fprintf(os.Stderr, "FAIL %s: %v\n", label, err)
		os.Exit(1)
	}

	ent, err := a.LookupEntityByName(ctx, entity)
	if err != nil {
		fail("lookup existing", err)
	}
	fmt.Printf("OK lookup existing: id=%s tenant=%s app=%s ziti_identity=%s\n",
		ent.ID, ent.Metadata["tenant"], ent.Metadata["app"], ent.Metadata["ziti_identity"])

	if _, err := a.LookupEntityByName(ctx, "no-such-entity-xyz-9876"); err != bao.ErrNotFound {
		fail("lookup missing", fmt.Errorf("expected ErrNotFound, got %v", err))
	}
	fmt.Println("OK lookup missing: ErrNotFound as expected")

	acc, err := a.AuthAccessor(ctx, "approle/")
	if err != nil {
		fail("auth accessor", err)
	}
	fmt.Printf("OK approle accessor: %s\n", acc)

	rid, err := a.ReadApproleRoleID(ctx, role)
	if err != nil {
		fail("read role-id", err)
	}
	fmt.Printf("OK role_id: %s...\n", rid[:12])

	wrap, ttl, err := a.WrapApproleSecretID(ctx, role, 5*time.Minute)
	if err != nil {
		fail("wrap secret-id", err)
	}
	fmt.Printf("OK wrap_token: %s... ttl=%ds\n", wrap[:8], ttl)
	fmt.Println("ALL CHECKS PASSED")
}
