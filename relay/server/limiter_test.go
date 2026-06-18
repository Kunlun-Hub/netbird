package server

import (
	"testing"
	"time"
)

func TestAccountRateLimiterAggregatesByAccount(t *testing.T) {
	limiter := newAccountRateLimiter()
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)

	if wait := limiter.reserve("account-a", 8, 1_000_000, now); wait != 0 {
		t.Fatalf("expected initial burst to pass, got wait %s", wait)
	}
	if wait := limiter.reserve("account-a", 8, 1, now); wait <= 0 {
		t.Fatalf("expected same account to be limited after shared bucket exhaustion, got %s", wait)
	}
	if wait := limiter.reserve("account-b", 8, 1, now); wait != 0 {
		t.Fatalf("expected another account to have an independent bucket, got %s", wait)
	}
}
