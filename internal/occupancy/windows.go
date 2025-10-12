package occupancy

import (
	"math"
	"time"
)

// SemanticLabel represents a human-understandable pattern in a time window
type SemanticLabel string

// Time window duration constants
const (
	Window2Min  = 2 * time.Minute
	Window8Min  = 8 * time.Minute
	Window20Min = 20 * time.Minute
	Window60Min = 60 * time.Minute
)

// GetSemanticLabel generates a semantic label for a time window based on motion count and pattern
func GetSemanticLabel(windowMinutes int, motionCount int, avgGapMinutes *float64) SemanticLabel {
	switch windowMinutes {
	case 2: // 0-2 minute window (immediate activity)
		return getLabelFor2MinWindow(motionCount)
	case 6: // 2-8 minute window (recent activity) - exclusive window size is 6 minutes
		return getLabelFor8MinWindow(motionCount)
	case 12: // 8-20 minute window (medium-term) - exclusive window size is 12 minutes
		return getLabelFor20MinWindow(motionCount, avgGapMinutes)
	case 40: // 20-60 minute window (long-term) - exclusive window size is 40 minutes
		return getLabelFor60MinWindow(motionCount)
	default:
		return "unknown"
	}
}

// getLabelFor2MinWindow returns labels for 0-2 minute window
func getLabelFor2MinWindow(motionCount int) SemanticLabel {
	if motionCount >= 2 {
		return "active_motion"
	}
	if motionCount == 1 {
		return "recent_motion"
	}
	return "no_motion"
}

// getLabelFor8MinWindow returns labels for 2-8 minute window (exclusive)
func getLabelFor8MinWindow(motionCount int) SemanticLabel {
	if motionCount >= 4 {
		return "continuous_activity"
	}
	if motionCount >= 2 {
		return "periodic_motion"
	}
	if motionCount == 1 {
		return "single_motion"
	}
	return "no_motion"
}

// getLabelFor20MinWindow returns labels for 8-20 minute window (exclusive)
func getLabelFor20MinWindow(motionCount int, avgGapMinutes *float64) SemanticLabel {
	if motionCount == 0 {
		return "empty"
	}
	if motionCount == 1 {
		return "brief_visit"
	}
	if motionCount == 2 && avgGapMinutes != nil && *avgGapMinutes < 1.0 {
		return "pass_through"
	}
	if motionCount >= 3 && motionCount <= 4 {
		return "intermittent_presence"
	}
	if motionCount >= 5 && avgGapMinutes != nil && *avgGapMinutes < 3.0 {
		return "sustained_presence"
	}
	if motionCount >= 5 {
		return "sustained_presence"
	}
	return "intermittent_presence"
}

// getLabelFor60MinWindow returns labels for 20-60 minute window (exclusive)
func getLabelFor60MinWindow(motionCount int) SemanticLabel {
	if motionCount == 0 {
		return "unused"
	}
	if motionCount <= 2 {
		return "minimal_use"
	}
	if motionCount <= 9 {
		return "sporadic_use"
	}
	return "regular_use"
}

// GetTimeOfDay returns the time period based on the current hour
func GetTimeOfDay(t time.Time) string {
	hour := t.Hour()

	if hour >= 6 && hour < 9 {
		return "early_morning"
	}
	if hour >= 9 && hour < 12 {
		return "morning"
	}
	if hour >= 12 && hour < 14 {
		return "midday"
	}
	if hour >= 14 && hour < 18 {
		return "afternoon"
	}
	if hour >= 18 && hour < 22 {
		return "evening"
	}
	return "night"
}

// CalculateAverageGap calculates the average time gap between consecutive events in minutes
func CalculateAverageGap(timestamps []time.Time) *float64 {
	if len(timestamps) < 2 {
		return nil
	}

	var totalGapSeconds float64
	count := 0

	for i := 1; i < len(timestamps); i++ {
		gap := timestamps[i].Sub(timestamps[i-1]).Seconds()
		totalGapSeconds += gap
		count++
	}

	if count == 0 {
		return nil
	}

	avgGapMinutes := totalGapSeconds / 60.0 / float64(count)
	return &avgGapMinutes
}

// MotionWindowData represents motion data for an exclusive time window
type MotionWindowData struct {
	MotionCount    int
	AvgGapMinutes  *float64
	SemanticLabel  SemanticLabel
	WindowDuration int // in minutes
}

// CalculateExclusiveWindows calculates motion counts for exclusive time windows
// Takes cumulative counts and returns exclusive window data
func CalculateExclusiveWindows(count2Min, count8Min, count20Min, count60Min int,
	timestamps2Min, timestamps8Min, timestamps20Min, timestamps60Min []time.Time) []MotionWindowData {

	// Calculate exclusive counts
	exclusive2Min := count2Min
	exclusive8Min := count8Min - count2Min
	exclusive20Min := count20Min - count8Min
	exclusive60Min := count60Min - count20Min

	// Calculate average gaps for each exclusive window
	gap2Min := CalculateAverageGap(timestamps2Min)
	gap8Min := CalculateAverageGap(getExclusiveTimestamps(timestamps8Min, timestamps2Min))
	gap20Min := CalculateAverageGap(getExclusiveTimestamps(timestamps20Min, timestamps8Min))
	gap60Min := CalculateAverageGap(getExclusiveTimestamps(timestamps60Min, timestamps20Min))

	return []MotionWindowData{
		{
			MotionCount:    exclusive2Min,
			AvgGapMinutes:  gap2Min,
			SemanticLabel:  GetSemanticLabel(2, exclusive2Min, gap2Min),
			WindowDuration: 2,
		},
		{
			MotionCount:    exclusive8Min,
			AvgGapMinutes:  gap8Min,
			SemanticLabel:  GetSemanticLabel(6, exclusive8Min, gap8Min), // window size is 6 (8-2)
			WindowDuration: 6,
		},
		{
			MotionCount:    exclusive20Min,
			AvgGapMinutes:  gap20Min,
			SemanticLabel:  GetSemanticLabel(12, exclusive20Min, gap20Min), // window size is 12 (20-8)
			WindowDuration: 12,
		},
		{
			MotionCount:    exclusive60Min,
			AvgGapMinutes:  gap60Min,
			SemanticLabel:  GetSemanticLabel(40, exclusive60Min, gap60Min), // window size is 40 (60-20)
			WindowDuration: 40,
		},
	}
}

// getExclusiveTimestamps filters timestamps to return only those not in the excluding set
func getExclusiveTimestamps(all []time.Time, exclude []time.Time) []time.Time {
	if len(exclude) == 0 {
		return all
	}

	// Create a map of timestamps to exclude for O(1) lookup
	excludeMap := make(map[int64]bool)
	for _, t := range exclude {
		excludeMap[t.UnixNano()] = true
	}

	// Filter timestamps
	var result []time.Time
	for _, t := range all {
		if !excludeMap[t.UnixNano()] {
			result = append(result, t)
		}
	}

	return result
}

// RoundToMinutes rounds a duration to the nearest minute with one decimal place
func RoundToMinutes(d time.Duration) float64 {
	minutes := d.Minutes()
	return math.Round(minutes*10) / 10
}
