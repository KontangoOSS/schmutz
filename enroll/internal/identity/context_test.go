package identity

import (
	"context"
	"testing"
)

func TestCaller_HasRole(t *testing.T) {
	c := Caller{Name: "dillon-laptop", RoleAttributes: []string{"admins"}}
	if !c.HasRole("admins") {
		t.Error("expected HasRole(admins) true")
	}
	if c.HasRole("admins-break-glass") {
		t.Error("expected HasRole(admins-break-glass) false")
	}
}

func TestCaller_RoundTripContext(t *testing.T) {
	want := Caller{Name: "alice", RoleAttributes: []string{"admins", "admins-break-glass"}}
	ctx := WithCaller(context.Background(), want)
	got, ok := FromContext(ctx)
	if !ok {
		t.Fatal("FromContext returned ok=false")
	}
	if got.Name != want.Name {
		t.Errorf("Name = %q, want %q", got.Name, want.Name)
	}
	if len(got.RoleAttributes) != 2 {
		t.Fatalf("RoleAttributes len=%d", len(got.RoleAttributes))
	}
}

func TestFromContext_Missing(t *testing.T) {
	if _, ok := FromContext(context.Background()); ok {
		t.Error("expected ok=false for empty context")
	}
}
