package audit

import (
	"context"
	"time"
)

type Result string

const (
	ResultOK    Result = "ok"
	ResultError Result = "error"
)

const (
	ActionTokenIssue   = "token.issue"
	ActionTokenRevoke  = "token.revoke"
	ActionTokenExpired = "token.expired"

	ActionIdentityCreate  = "identity.create"
	ActionIdentityApprove = "identity.approve"
	ActionIdentityDeny    = "identity.deny"
	ActionIdentityUpdate  = "identity.update"
	ActionIdentityDelete  = "identity.delete"

	ActionBootstrapBaoInit           = "bootstrap.bao-init"
	ActionBootstrapBaoDistributeKeys = "bootstrap.bao-distribute-keys"
	ActionBootstrapBaoJoinPeer       = "bootstrap.bao-join-peer"
	ActionBootstrapApplyEnrollPolicy = "bootstrap.apply-enroll-policy"
	ActionBootstrapCreateBreakGlass  = "bootstrap.create-break-glass"
)

type Event struct {
	Timestamp time.Time         `json:"ts"`
	Actor     string            `json:"actor"`
	Action    string            `json:"action"`
	Resource  string            `json:"resource,omitempty"`
	Result    Result            `json:"result"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	RequestID string            `json:"request_id,omitempty"`
}

// MarshalJSON renders Timestamp in RFC3339 with millisecond precision (matching spec).
func (e Event) MarshalJSON() ([]byte, error) {
	type alias Event
	return marshalWithTS(alias(e), e.Timestamp)
}

type Logger interface {
	Record(ctx context.Context, ev Event) error
}
