package app

import (
	"testing"
	"time"
)

func newTestRateLimiter(limit int) *RateLimiter {
	return &RateLimiter{
		requests: make(map[string][]time.Time),
		refunds:  make(map[string][]time.Time),
		limit:    limit,
		window:   time.Hour,
		enabled:  true,
	}
}

func TestRateLimiter_RefundReturnsSlot(t *testing.T) {
	rl := newTestRateLimiter(3)
	rl.isAllowed("ip")
	rl.isAllowed("ip")
	if got := rl.remaining("ip"); got != 1 {
		t.Fatalf("remaining after 2 requests = %d, want 1", got)
	}
	if !rl.refund("ip") {
		t.Fatal("refund should have succeeded")
	}
	if got := rl.remaining("ip"); got != 2 {
		t.Errorf("remaining after refund = %d, want 2", got)
	}
}

func TestRateLimiter_RefundCapped(t *testing.T) {
	rl := newTestRateLimiter(10)
	// Each attempt records a slot then gets refunded; only maxExpiredRefunds succeed.
	granted := 0
	for range maxExpiredRefunds + 3 {
		rl.isAllowed("ip")
		if rl.refund("ip") {
			granted++
		}
	}
	if granted != maxExpiredRefunds {
		t.Errorf("granted refunds = %d, want %d", granted, maxExpiredRefunds)
	}
}

func TestRateLimiter_RefundNoChargeOrDisabled(t *testing.T) {
	rl := newTestRateLimiter(3)
	if rl.refund("ip") {
		t.Error("refund with no recorded request should return false")
	}

	disabled := &RateLimiter{enabled: false}
	if disabled.refund("ip") {
		t.Error("refund on a disabled limiter should return false")
	}
}
