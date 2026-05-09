package beacon_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"git.konoss.org/kore/schmutz/neverland/internal/beacon"
)

func TestShortCode_Deterministic(t *testing.T) {
	u, _ := uuid.Parse("a1276864-8e8a-4900-b3a5-1dbb865efb4c")
	const want = "B92T-ZQYK"
	got := beacon.ShortCode(u)
	if got != want {
		t.Fatalf("ShortCode regression: want %q, got %q", want, got)
	}
	// re-call to confirm determinism within a single test run
	if beacon.ShortCode(u) != want {
		t.Fatalf("non-deterministic")
	}
}

func TestShortCode_Format(t *testing.T) {
	u := uuid.New()
	c := beacon.ShortCode(u)
	if len(c) != 9 {
		t.Fatalf("expected 9 chars (XXXX-XXXX), got %d: %q", len(c), c)
	}
	if !strings.Contains(c, "-") {
		t.Fatalf("expected dash separator, got %q", c)
	}
	if strings.ContainsAny(c, "0OoIl") {
		t.Fatalf("ambiguous chars not allowed in short code: %q", c)
	}
}

func TestShortCode_DifferentUUIDsDifferentCodes(t *testing.T) {
	a := beacon.ShortCode(uuid.New())
	b := beacon.ShortCode(uuid.New())
	if a == b {
		t.Fatalf("collision on random uuids: %q", a)
	}
}
