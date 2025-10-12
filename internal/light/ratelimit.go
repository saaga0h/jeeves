package light

import (
	"sync"
	"time"
)

// RateLimiter manages rate limiting for decisions per location
type RateLimiter struct {
	mu              sync.RWMutex
	lastDecisionMap map[string]time.Time
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		lastDecisionMap: make(map[string]time.Time),
	}
}

// ShouldMakeDecision checks if enough time has passed since the last decision
// Returns true if decision should be made, false if rate limited
func (rl *RateLimiter) ShouldMakeDecision(location string, minIntervalMs int) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	lastTime, exists := rl.lastDecisionMap[location]
	if !exists {
		// First decision for this location
		rl.lastDecisionMap[location] = time.Now()
		return true
	}

	elapsed := time.Since(lastTime)
	minInterval := time.Duration(minIntervalMs) * time.Millisecond

	if elapsed < minInterval {
		// Rate limited
		return false
	}

	// Update last decision time
	rl.lastDecisionMap[location] = time.Now()
	return true
}

// RecordDecision records that a decision was made for a location
// Used when bypassing rate limiting (e.g., forced decisions)
func (rl *RateLimiter) RecordDecision(location string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.lastDecisionMap[location] = time.Now()
}

// GetLastDecisionTime returns the last decision time for a location
func (rl *RateLimiter) GetLastDecisionTime(location string) (time.Time, bool) {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	lastTime, exists := rl.lastDecisionMap[location]
	return lastTime, exists
}
