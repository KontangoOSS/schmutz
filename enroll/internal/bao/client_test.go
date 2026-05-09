package bao

import (
	"testing"
	"time"
)

func TestTokenRecord_IsExpired(t *testing.T) {
	now := time.Now()
	past := now.Add(-1 * time.Hour)
	future := now.Add(1 * time.Hour)

	r := &TokenRecord{ExpiresAt: future}
	if r.IsExpired(now) {
		t.Error("future-expiring record should not be expired now")
	}
	r.ExpiresAt = past
	if !r.IsExpired(now) {
		t.Error("past-expiring record should be expired")
	}
}

func TestTokenRecord_IsConsumed(t *testing.T) {
	r := &TokenRecord{}
	if r.IsConsumed() {
		t.Error("fresh record should not be consumed")
	}
	now := time.Now()
	r.ConsumedAt = &now
	if !r.IsConsumed() {
		t.Error("record with ConsumedAt should be consumed")
	}
}
