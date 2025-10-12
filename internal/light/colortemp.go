package light

// calculateColorTemperature returns the appropriate color temperature based on time of day
// Follows circadian rhythm principles - warmer light at night, cooler during day
func calculateColorTemperature(timeOfDay string) int {
	colorTempMap := map[string]int{
		"early_morning": 3000, // Warm start
		"morning":       4500, // Neutral
		"midday":        5500, // Cool/daylight
		"afternoon":     4500, // Neutral
		"evening":       2700, // Warm
		"late_evening":  2500, // Very warm
		"night":         2400, // Ultra warm for sleep
	}

	if temp, exists := colorTempMap[timeOfDay]; exists {
		return temp
	}

	// Default to neutral if unknown time of day
	return 4000
}
