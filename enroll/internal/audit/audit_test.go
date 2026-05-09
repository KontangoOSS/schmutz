package audit

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEvent_JSONShape(t *testing.T) {
	now := time.Date(2026, 5, 3, 14, 22, 11, 123_000_000, time.UTC)
	ev := Event{
		Timestamp: now,
		Actor:     "dillon-laptop",
		Action:    "identity.approve",
		Resource:  "machine-a583fdac",
		Result:    ResultOK,
		Metadata: map[string]string{
			"old_roles": "machine-a583fdac-sshhost,quarantine",
			"new_roles": "machine-a583fdac-sshhost,test",
		},
		RequestID: "req-7f3a",
	}
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["ts"] != "2026-05-03T14:22:11.123Z" {
		t.Errorf("ts = %v, want 2026-05-03T14:22:11.123Z", got["ts"])
	}
	if got["actor"] != "dillon-laptop" {
		t.Errorf("actor = %v", got["actor"])
	}
	if got["action"] != "identity.approve" {
		t.Errorf("action = %v", got["action"])
	}
	if got["result"] != "ok" {
		t.Errorf("result = %v, want 'ok'", got["result"])
	}
}

func TestActionConstants(t *testing.T) {
	cases := []struct {
		got, want string
	}{
		{ActionTokenIssue, "token.issue"},
		{ActionTokenRevoke, "token.revoke"},
		{ActionTokenExpired, "token.expired"},
		{ActionIdentityCreate, "identity.create"},
		{ActionIdentityApprove, "identity.approve"},
		{ActionIdentityDeny, "identity.deny"},
		{ActionIdentityUpdate, "identity.update"},
		{ActionIdentityDelete, "identity.delete"},
		{ActionBootstrapBaoInit, "bootstrap.bao-init"},
		{ActionBootstrapCreateBreakGlass, "bootstrap.create-break-glass"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("action constant = %q, want %q", c.got, c.want)
		}
	}
}
