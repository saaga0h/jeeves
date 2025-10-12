package illuminance

import (
	"testing"
	"time"
)

func TestLuxToLabel(t *testing.T) {
	tests := []struct {
		lux      float64
		expected string
	}{
		{0, "dark"},
		{5, "dark"},
		{10, "dark"},
		{11, "dim"},
		{50, "dim"},
		{51, "moderate"},
		{200, "moderate"},
		{201, "bright"},
		{500, "bright"},
		{501, "very_bright"},
		{10000, "very_bright"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := LuxToLabel(tt.lux)
			if result != tt.expected {
				t.Errorf("LuxToLabel(%.1f) = %s, want %s", tt.lux, result, tt.expected)
			}
		})
	}
}

func TestCalculateTrend(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		readings []IlluminanceReading
		expected string
	}{
		{
			name:     "insufficient data",
			readings: []IlluminanceReading{{Timestamp: now, Lux: 100}},
			expected: "unknown",
		},
		{
			name: "brightening trend",
			readings: []IlluminanceReading{
				{Timestamp: now.Add(-5 * time.Minute), Lux: 100},
				{Timestamp: now.Add(-4 * time.Minute), Lux: 110},
				{Timestamp: now.Add(-3 * time.Minute), Lux: 130},
				{Timestamp: now.Add(-2 * time.Minute), Lux: 150},
				{Timestamp: now.Add(-1 * time.Minute), Lux: 180},
			},
			expected: "brightening",
		},
		{
			name: "dimming trend",
			readings: []IlluminanceReading{
				{Timestamp: now.Add(-5 * time.Minute), Lux: 200},
				{Timestamp: now.Add(-4 * time.Minute), Lux: 180},
				{Timestamp: now.Add(-3 * time.Minute), Lux: 150},
				{Timestamp: now.Add(-2 * time.Minute), Lux: 120},
				{Timestamp: now.Add(-1 * time.Minute), Lux: 100},
			},
			expected: "dimming",
		},
		{
			name: "stable trend",
			readings: []IlluminanceReading{
				{Timestamp: now.Add(-5 * time.Minute), Lux: 100},
				{Timestamp: now.Add(-4 * time.Minute), Lux: 105},
				{Timestamp: now.Add(-3 * time.Minute), Lux: 98},
				{Timestamp: now.Add(-2 * time.Minute), Lux: 102},
				{Timestamp: now.Add(-1 * time.Minute), Lux: 103},
			},
			expected: "stable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateTrend(tt.readings)
			if result != tt.expected {
				t.Errorf("CalculateTrend() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestCalculateStability(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		readings []IlluminanceReading
		avg      float64
		expected string
	}{
		{
			name:     "insufficient data",
			readings: []IlluminanceReading{{Timestamp: now, Lux: 100}},
			avg:      100,
			expected: "unknown",
		},
		{
			name: "stable readings",
			readings: []IlluminanceReading{
				{Timestamp: now.Add(-5 * time.Minute), Lux: 100},
				{Timestamp: now.Add(-4 * time.Minute), Lux: 102},
				{Timestamp: now.Add(-3 * time.Minute), Lux: 98},
				{Timestamp: now.Add(-2 * time.Minute), Lux: 101},
				{Timestamp: now.Add(-1 * time.Minute), Lux: 99},
			},
			avg:      100,
			expected: "stable",
		},
		{
			name: "variable readings",
			readings: []IlluminanceReading{
				{Timestamp: now.Add(-5 * time.Minute), Lux: 100},
				{Timestamp: now.Add(-4 * time.Minute), Lux: 140},
				{Timestamp: now.Add(-3 * time.Minute), Lux: 80},
				{Timestamp: now.Add(-2 * time.Minute), Lux: 130},
				{Timestamp: now.Add(-1 * time.Minute), Lux: 90},
			},
			avg:      108,
			expected: "variable",
		},
		{
			name: "volatile readings",
			readings: []IlluminanceReading{
				{Timestamp: now.Add(-5 * time.Minute), Lux: 50},
				{Timestamp: now.Add(-4 * time.Minute), Lux: 200},
				{Timestamp: now.Add(-3 * time.Minute), Lux: 30},
				{Timestamp: now.Add(-2 * time.Minute), Lux: 180},
				{Timestamp: now.Add(-1 * time.Minute), Lux: 40},
			},
			avg:      100,
			expected: "volatile",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateStability(tt.readings, tt.avg)
			if result != tt.expected {
				t.Errorf("CalculateStability() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestGetTimeOfDay(t *testing.T) {
	tests := []struct {
		hour     int
		expected string
	}{
		{0, "night"},
		{4, "night"},
		{5, "early_morning"},
		{6, "early_morning"},
		{7, "morning"},
		{9, "morning"},
		{10, "midday"},
		{13, "midday"},
		{14, "afternoon"},
		{16, "afternoon"},
		{17, "evening"},
		{19, "evening"},
		{20, "late_evening"},
		{21, "late_evening"},
		{22, "night"},
		{23, "night"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := GetTimeOfDay(tt.hour)
			if result != tt.expected {
				t.Errorf("GetTimeOfDay(%d) = %s, want %s", tt.hour, result, tt.expected)
			}
		})
	}
}

func TestDetermineLightSource(t *testing.T) {
	tests := []struct {
		name                  string
		lux                   float64
		isDaytime             bool
		theoreticalOutdoorLux float64
		expected              []string
	}{
		{
			name:                  "no light",
			lux:                   5,
			isDaytime:             false,
			theoreticalOutdoorLux: 0,
			expected:              []string{"none"},
		},
		{
			name:                  "natural light daytime",
			lux:                   300,
			isDaytime:             true,
			theoreticalOutdoorLux: 50000,
			expected:              []string{"natural"},
		},
		{
			name:                  "mixed light daytime",
			lux:                   800,
			isDaytime:             true,
			theoreticalOutdoorLux: 50000,
			expected:              []string{"natural", "mixed"},
		},
		{
			name:                  "artificial light nighttime",
			lux:                   100,
			isDaytime:             false,
			theoreticalOutdoorLux: 0,
			expected:              []string{"artificial"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetermineLightSource(tt.lux, tt.isDaytime, tt.theoreticalOutdoorLux)
			if len(result) != len(tt.expected) {
				t.Errorf("DetermineLightSource() = %v, want %v", result, tt.expected)
				return
			}
			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("DetermineLightSource() = %v, want %v", result, tt.expected)
					return
				}
			}
		})
	}
}

func TestCompareToTypical(t *testing.T) {
	tests := []struct {
		name       string
		currentLux float64
		timeOfDay  string
		expected   string
	}{
		{
			name:       "well below typical",
			currentLux: 50,
			timeOfDay:  "midday",
			expected:   "well_below_typical",
		},
		{
			name:       "below typical",
			currentLux: 250,
			timeOfDay:  "midday",
			expected:   "below_typical",
		},
		{
			name:       "near typical",
			currentLux: 380,
			timeOfDay:  "midday",
			expected:   "near_typical",
		},
		{
			name:       "above typical",
			currentLux: 600,
			timeOfDay:  "midday",
			expected:   "above_typical",
		},
		{
			name:       "well above typical",
			currentLux: 1000,
			timeOfDay:  "midday",
			expected:   "well_above_typical",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CompareToTypical(tt.currentLux, tt.timeOfDay)
			if result != tt.expected {
				t.Errorf("CompareToTypical(%.1f, %s) = %s, want %s",
					tt.currentLux, tt.timeOfDay, result, tt.expected)
			}
		})
	}
}
