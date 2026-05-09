package admin

import (
	"errors"
	"testing"
)

func TestIsBreakGlassIdentity(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"break-glass-admin", true},
		{"BREAK-GLASS-ADMIN", false}, // case-sensitive — this is a Ziti name
		{"machine-a583fdac", false},
		{"break-glass", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsBreakGlassIdentity(c.name); got != c.want {
			t.Errorf("IsBreakGlassIdentity(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestEnsureNotBreakGlass(t *testing.T) {
	if err := EnsureNotBreakGlass("machine-a583fdac"); err != nil {
		t.Errorf("regular identity should pass: %v", err)
	}
	err := EnsureNotBreakGlass("break-glass-admin")
	if !errors.Is(err, ErrBreakGlassProtected) {
		t.Errorf("got %v, want ErrBreakGlassProtected", err)
	}
}
