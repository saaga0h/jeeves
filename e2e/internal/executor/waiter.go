package executor

import (
	"sync"
	"time"
)

var (
	lastEventTime time.Time
	eventMutex    sync.Mutex
)

// WaitUntil waits until a specific time relative to start, with optional time scaling
func WaitUntil(startTime time.Time, targetSeconds int, timeScale int) {
	if timeScale < 1 {
		timeScale = 1 // Default to no scaling
	}

	// Scale the target time
	scaledSeconds := targetSeconds / timeScale
	targetTime := startTime.Add(time.Duration(scaledSeconds) * time.Second)
	now := time.Now()

	eventMutex.Lock()
	defer eventMutex.Unlock()

	// Calculate how long to wait
	var sleepDuration time.Duration

	if now.Before(targetTime) {
		// We're on schedule - wait until target time
		sleepDuration = targetTime.Sub(now)
	} else {
		// We're behind schedule - ensure minimum spacing from last event
		// This prevents all events from bunching together if test runner falls behind
		// Use 3 seconds minimum to ensure proper virtual time spacing at 60x scale
		// (3 real seconds * 60 = 180 virtual seconds = 3 virtual minutes)
		minDelay := 3 * time.Second
		if !lastEventTime.IsZero() {
			timeSinceLastEvent := now.Sub(lastEventTime)
			if timeSinceLastEvent < minDelay {
				sleepDuration = minDelay - timeSinceLastEvent
			}
		}
	}

	if sleepDuration > 0 {
		time.Sleep(sleepDuration)
	}

	// Update last event time to now + sleep duration
	lastEventTime = time.Now()
}

// GetElapsed returns elapsed seconds since start
func GetElapsed(startTime time.Time) float64 {
	return time.Since(startTime).Seconds()
}
