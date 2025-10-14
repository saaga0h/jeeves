package executor

import (
	"time"
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

	if now.Before(targetTime) {
		time.Sleep(targetTime.Sub(now))
	}
}

// GetElapsed returns elapsed seconds since start
func GetElapsed(startTime time.Time) float64 {
	return time.Since(startTime).Seconds()
}
