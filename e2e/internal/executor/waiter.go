package executor

import (
	"time"
)

// WaitUntil waits until a specific time relative to start
func WaitUntil(startTime time.Time, targetSeconds int) {
	targetTime := startTime.Add(time.Duration(targetSeconds) * time.Second)
	now := time.Now()

	if now.Before(targetTime) {
		time.Sleep(targetTime.Sub(now))
	}
}

// GetElapsed returns elapsed seconds since start
func GetElapsed(startTime time.Time) float64 {
	return time.Since(startTime).Seconds()
}
