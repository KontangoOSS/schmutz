package bao

import "time"

type TokenRecord struct {
	Slug             string     `json:"slug"`
	RoleAttributes   []string   `json:"role_attributes"`
	ExpiresAt        time.Time  `json:"expires_at"`
	IssuedBy         string     `json:"issued_by"`
	ConsumedAt       *time.Time `json:"consumed_at,omitempty"`
	ConsumedBy       string     `json:"consumed_by,omitempty"`
	ConsumedIdentity string     `json:"consumed_identity,omitempty"`
}

func (r *TokenRecord) IsExpired(now time.Time) bool {
	return now.After(r.ExpiresAt)
}

func (r *TokenRecord) IsConsumed() bool {
	return r.ConsumedAt != nil && !r.ConsumedAt.IsZero()
}
