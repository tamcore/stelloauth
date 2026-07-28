package app

import (
	"log"
	"os"
	"strconv"
	"sync"
	"time"
)

const maxExpiredRefunds = 2

type RateLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
	refunds  map[string][]time.Time
	limit    int
	window   time.Duration
	enabled  bool
}

var rateLimiter *RateLimiter

func initRateLimiter() {
	limitStr := os.Getenv("RATE_LIMIT_COUNT")
	durationStr := os.Getenv("RATE_LIMIT_DURATION")

	if limitStr == "" || durationStr == "" {
		log.Printf("Rate limiting disabled (RATE_LIMIT_COUNT and RATE_LIMIT_DURATION not set)")
		rateLimiter = &RateLimiter{enabled: false}
		return
	}

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		log.Printf("Invalid RATE_LIMIT_COUNT, rate limiting disabled")
		rateLimiter = &RateLimiter{enabled: false}
		return
	}

	duration, err := time.ParseDuration(durationStr)
	if err != nil || duration <= 0 {
		log.Printf("Invalid RATE_LIMIT_DURATION, rate limiting disabled")
		rateLimiter = &RateLimiter{enabled: false}
		return
	}

	log.Printf("Rate limiting enabled: %d requests per %s", limit, duration)
	rateLimiter = &RateLimiter{
		requests: make(map[string][]time.Time),
		refunds:  make(map[string][]time.Time),
		limit:    limit,
		window:   duration,
		enabled:  true,
	}
}

func (rl *RateLimiter) validInWindow(ts []time.Time, now time.Time) []time.Time {
	cutoff := now.Add(-rl.window)
	var valid []time.Time
	for _, t := range ts {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	return valid
}

func (rl *RateLimiter) refund(ip string) bool {
	if !rl.enabled {
		return false
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	grantedRefunds := rl.validInWindow(rl.refunds[ip], now)
	if len(grantedRefunds) >= maxExpiredRefunds {
		rl.refunds[ip] = grantedRefunds
		return false
	}

	reqs := rl.validInWindow(rl.requests[ip], now)
	if len(reqs) == 0 {
		rl.requests[ip] = reqs
		return false
	}

	rl.requests[ip] = reqs[:len(reqs)-1]
	rl.refunds[ip] = append(grantedRefunds, now)
	return true
}

func (rl *RateLimiter) isAllowed(ip string) bool {
	if !rl.enabled {
		return true
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	valid := rl.validInWindow(rl.requests[ip], now)
	if len(valid) >= rl.limit {
		rl.requests[ip] = valid
		return false
	}

	rl.requests[ip] = append(valid, now)
	return true
}

func (rl *RateLimiter) remaining(ip string) int {
	if !rl.enabled {
		return -1
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	return rl.limit - len(rl.validInWindow(rl.requests[ip], time.Now()))
}
