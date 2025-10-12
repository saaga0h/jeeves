package illuminance

import (
	"math"
	"time"
)

// WindowStats contains statistical analysis of a time window
type WindowStats struct {
	AverageLux float64
	MinLux     float64
	MaxLux     float64
	Count      int
	Trend      string
	Stability  string
	Label      string
}

// AnalyzeWindow performs statistical analysis on illuminance readings within a time window
func AnalyzeWindow(readings []IlluminanceReading, windowMinutes int, now time.Time) *WindowStats {
	if len(readings) == 0 {
		return &WindowStats{
			AverageLux: 0,
			MinLux:     0,
			MaxLux:     0,
			Count:      0,
			Trend:      "unknown",
			Stability:  "unknown",
			Label:      "unknown",
		}
	}

	// Filter readings to only include those within the window
	cutoff := now.Add(-time.Duration(windowMinutes) * time.Minute)
	var filteredReadings []IlluminanceReading
	for _, reading := range readings {
		if reading.Timestamp.After(cutoff) {
			filteredReadings = append(filteredReadings, reading)
		}
	}

	if len(filteredReadings) == 0 {
		return &WindowStats{
			AverageLux: 0,
			MinLux:     0,
			MaxLux:     0,
			Count:      0,
			Trend:      "unknown",
			Stability:  "unknown",
			Label:      "unknown",
		}
	}

	// Calculate basic statistics
	var sum, min, max float64
	min = filteredReadings[0].Lux
	max = filteredReadings[0].Lux

	for _, reading := range filteredReadings {
		sum += reading.Lux
		if reading.Lux < min {
			min = reading.Lux
		}
		if reading.Lux > max {
			max = reading.Lux
		}
	}

	avg := sum / float64(len(filteredReadings))

	// Calculate trend (if we have enough readings)
	trend := CalculateTrend(filteredReadings)

	// Calculate stability (coefficient of variation)
	stability := CalculateStability(filteredReadings, avg)

	// Convert average to semantic label
	label := LuxToLabel(avg)

	return &WindowStats{
		AverageLux: avg,
		MinLux:     min,
		MaxLux:     max,
		Count:      len(filteredReadings),
		Trend:      trend,
		Stability:  stability,
		Label:      label,
	}
}

// CalculateTrend detects if illuminance is brightening, dimming, or stable
func CalculateTrend(readings []IlluminanceReading) string {
	if len(readings) < 3 {
		return "unknown"
	}

	// Split readings into two halves and compare averages
	mid := len(readings) / 2
	firstHalf := readings[:mid]
	secondHalf := readings[mid:]

	var firstSum, secondSum float64
	for _, r := range firstHalf {
		firstSum += r.Lux
	}
	for _, r := range secondHalf {
		secondSum += r.Lux
	}

	firstAvg := firstSum / float64(len(firstHalf))
	secondAvg := secondSum / float64(len(secondHalf))

	// Calculate percentage change
	if firstAvg == 0 {
		if secondAvg > 0 {
			return "brightening"
		}
		return "stable"
	}

	percentChange := ((secondAvg - firstAvg) / firstAvg) * 100

	// Threshold: 20% change
	if percentChange > 20 {
		return "brightening"
	} else if percentChange < -20 {
		return "dimming"
	}

	return "stable"
}

// CalculateStability measures volatility using coefficient of variation
func CalculateStability(readings []IlluminanceReading, avg float64) string {
	if len(readings) < 2 || avg == 0 {
		return "unknown"
	}

	// Calculate standard deviation
	var sumSquaredDiff float64
	for _, reading := range readings {
		diff := reading.Lux - avg
		sumSquaredDiff += diff * diff
	}

	variance := sumSquaredDiff / float64(len(readings))
	stdDev := math.Sqrt(variance)

	// Coefficient of variation
	cv := stdDev / avg

	// Thresholds from spec
	if cv > 0.5 {
		return "volatile"
	} else if cv > 0.2 {
		return "variable"
	}

	return "stable"
}

// LuxToLabel converts a lux value to a semantic label
func LuxToLabel(lux float64) string {
	if lux <= 10 {
		return "dark"
	} else if lux <= 50 {
		return "dim"
	} else if lux <= 200 {
		return "moderate"
	} else if lux <= 500 {
		return "bright"
	}
	return "very_bright"
}

// DetermineLightSource infers the source of illumination
func DetermineLightSource(lux float64, isDaytime bool, theoreticalOutdoorLux float64) []string {
	sources := []string{}

	if lux <= 10 {
		return []string{"none"}
	}

	if isDaytime && lux > 100 {
		sources = append(sources, "natural")
		// If indoor lux is very high during daytime, likely mixed with artificial
		if lux > 500 {
			sources = append(sources, "mixed")
		}
	} else if !isDaytime && lux > 50 {
		sources = append(sources, "artificial")
	}

	if len(sources) == 0 {
		sources = append(sources, "unknown")
	}

	return sources
}

// GetTimeOfDay returns the semantic time period based on hour
func GetTimeOfDay(hour int) string {
	switch {
	case hour >= 5 && hour < 7:
		return "early_morning"
	case hour >= 7 && hour < 10:
		return "morning"
	case hour >= 10 && hour < 14:
		return "midday"
	case hour >= 14 && hour < 17:
		return "afternoon"
	case hour >= 17 && hour < 20:
		return "evening"
	case hour >= 20 && hour < 22:
		return "late_evening"
	default:
		return "night"
	}
}

// GetTypicalLuxForTimeOfDay returns reference illuminance values
func GetTypicalLuxForTimeOfDay(timeOfDay string) float64 {
	typical := map[string]float64{
		"night":         0,
		"early_morning": 200,
		"morning":       300,
		"midday":        400,
		"afternoon":     350,
		"evening":       250,
		"late_evening":  150,
	}

	if val, ok := typical[timeOfDay]; ok {
		return val
	}
	return 200 // Default fallback
}

// CompareToTypical compares current lux to typical values for time of day
func CompareToTypical(currentLux float64, timeOfDay string) string {
	typical := GetTypicalLuxForTimeOfDay(timeOfDay)

	if typical == 0 {
		// Nighttime - any light is above typical
		if currentLux <= 10 {
			return "near_typical"
		}
		return "above_typical"
	}

	ratio := currentLux / typical

	if ratio < 0.5 {
		return "well_below_typical"
	} else if ratio < 0.7 {
		return "below_typical"
	} else if ratio <= 1.3 {
		return "near_typical"
	} else if ratio <= 2.0 {
		return "above_typical"
	}

	return "well_above_typical"
}
