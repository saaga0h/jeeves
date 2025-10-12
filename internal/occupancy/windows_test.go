package occupancy

import (
	"testing"
	"time"
)

func TestGetSemanticLabel_2MinWindow(t *testing.T) {
	tests := []struct {
		name          string
		motionCount   int
		expectedLabel SemanticLabel
	}{
		{"no motion", 0, "no_motion"},
		{"recent motion", 1, "recent_motion"},
		{"active motion", 2, "active_motion"},
		{"active motion multiple", 5, "active_motion"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			label := GetSemanticLabel(2, tt.motionCount, nil)
			if label != tt.expectedLabel {
				t.Errorf("expected %s, got %s", tt.expectedLabel, label)
			}
		})
	}
}

func TestGetSemanticLabel_8MinWindow(t *testing.T) {
	tests := []struct {
		name          string
		motionCount   int
		expectedLabel SemanticLabel
	}{
		{"no motion", 0, "no_motion"},
		{"single motion", 1, "single_motion"},
		{"periodic motion", 2, "periodic_motion"},
		{"periodic motion 3", 3, "periodic_motion"},
		{"continuous activity", 4, "continuous_activity"},
		{"continuous activity 5", 5, "continuous_activity"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			label := GetSemanticLabel(6, tt.motionCount, nil) // 6 is the exclusive window size (8-2)
			if label != tt.expectedLabel {
				t.Errorf("expected %s, got %s", tt.expectedLabel, label)
			}
		})
	}
}

func TestGetSemanticLabel_20MinWindow(t *testing.T) {
	gap1Min := 0.5
	gap3Min := 3.5

	tests := []struct {
		name          string
		motionCount   int
		avgGap        *float64
		expectedLabel SemanticLabel
	}{
		{"empty", 0, nil, "empty"},
		{"brief visit", 1, nil, "brief_visit"},
		{"pass through", 2, &gap1Min, "pass_through"},
		{"intermittent presence 3", 3, nil, "intermittent_presence"},
		{"intermittent presence 4", 4, nil, "intermittent_presence"},
		{"sustained presence", 5, &gap1Min, "sustained_presence"},
		{"sustained presence no gap", 5, &gap3Min, "sustained_presence"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			label := GetSemanticLabel(12, tt.motionCount, tt.avgGap) // 12 is the exclusive window size (20-8)
			if label != tt.expectedLabel {
				t.Errorf("expected %s, got %s", tt.expectedLabel, label)
			}
		})
	}
}

func TestGetSemanticLabel_60MinWindow(t *testing.T) {
	tests := []struct {
		name          string
		motionCount   int
		expectedLabel SemanticLabel
	}{
		{"unused", 0, "unused"},
		{"minimal use 1", 1, "minimal_use"},
		{"minimal use 2", 2, "minimal_use"},
		{"sporadic use 3", 3, "sporadic_use"},
		{"sporadic use 9", 9, "sporadic_use"},
		{"regular use", 10, "regular_use"},
		{"regular use 20", 20, "regular_use"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			label := GetSemanticLabel(40, tt.motionCount, nil) // 40 is the exclusive window size (60-20)
			if label != tt.expectedLabel {
				t.Errorf("expected %s, got %s", tt.expectedLabel, label)
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
		{3, "night"},
		{6, "early_morning"},
		{8, "early_morning"},
		{9, "morning"},
		{11, "morning"},
		{12, "midday"},
		{13, "midday"},
		{14, "afternoon"},
		{17, "afternoon"},
		{18, "evening"},
		{21, "evening"},
		{22, "night"},
		{23, "night"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			testTime := time.Date(2024, 1, 1, tt.hour, 0, 0, 0, time.UTC)
			result := GetTimeOfDay(testTime)
			if result != tt.expected {
				t.Errorf("hour %d: expected %s, got %s", tt.hour, tt.expected, result)
			}
		})
	}
}

func TestCalculateAverageGap(t *testing.T) {
	t.Run("no timestamps", func(t *testing.T) {
		result := CalculateAverageGap([]time.Time{})
		if result != nil {
			t.Errorf("expected nil, got %v", result)
		}
	})

	t.Run("single timestamp", func(t *testing.T) {
		timestamps := []time.Time{time.Now()}
		result := CalculateAverageGap(timestamps)
		if result != nil {
			t.Errorf("expected nil, got %v", result)
		}
	})

	t.Run("two timestamps 1 minute apart", func(t *testing.T) {
		now := time.Now()
		timestamps := []time.Time{
			now.Add(-1 * time.Minute),
			now,
		}
		result := CalculateAverageGap(timestamps)
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if *result < 0.9 || *result > 1.1 {
			t.Errorf("expected ~1.0 minutes, got %f", *result)
		}
	})

	t.Run("multiple timestamps", func(t *testing.T) {
		now := time.Now()
		timestamps := []time.Time{
			now.Add(-6 * time.Minute),
			now.Add(-4 * time.Minute), // 2 min gap
			now.Add(-2 * time.Minute), // 2 min gap
			now,                       // 2 min gap
		}
		result := CalculateAverageGap(timestamps)
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		// Average gap should be 2 minutes
		if *result < 1.9 || *result > 2.1 {
			t.Errorf("expected ~2.0 minutes, got %f", *result)
		}
	})
}

func TestCalculateExclusiveWindows(t *testing.T) {
	now := time.Now()

	// Create timestamps for different windows
	timestamps2Min := []time.Time{
		now.Add(-1 * time.Minute),
		now.Add(-90 * time.Second),
	}

	timestamps8Min := []time.Time{
		now.Add(-1 * time.Minute),
		now.Add(-90 * time.Second),
		now.Add(-5 * time.Minute),
		now.Add(-6 * time.Minute),
		now.Add(-7 * time.Minute),
	}

	timestamps20Min := []time.Time{
		now.Add(-1 * time.Minute),
		now.Add(-90 * time.Second),
		now.Add(-5 * time.Minute),
		now.Add(-6 * time.Minute),
		now.Add(-7 * time.Minute),
		now.Add(-15 * time.Minute),
	}

	timestamps60Min := []time.Time{
		now.Add(-1 * time.Minute),
		now.Add(-90 * time.Second),
		now.Add(-5 * time.Minute),
		now.Add(-6 * time.Minute),
		now.Add(-7 * time.Minute),
		now.Add(-15 * time.Minute),
		now.Add(-45 * time.Minute),
		now.Add(-50 * time.Minute),
	}

	// Cumulative counts
	count2Min := 2
	count8Min := 5
	count20Min := 6
	count60Min := 8

	windows := CalculateExclusiveWindows(
		count2Min, count8Min, count20Min, count60Min,
		timestamps2Min, timestamps8Min, timestamps20Min, timestamps60Min,
	)

	// Test exclusive counts
	if windows[0].MotionCount != 2 {
		t.Errorf("0-2 min window: expected 2, got %d", windows[0].MotionCount)
	}
	if windows[1].MotionCount != 3 {
		t.Errorf("2-8 min window: expected 3, got %d", windows[1].MotionCount)
	}
	if windows[2].MotionCount != 1 {
		t.Errorf("8-20 min window: expected 1, got %d", windows[2].MotionCount)
	}
	if windows[3].MotionCount != 2 {
		t.Errorf("20-60 min window: expected 2, got %d", windows[3].MotionCount)
	}

	// Test window durations
	if windows[0].WindowDuration != 2 {
		t.Errorf("window 0 duration: expected 2, got %d", windows[0].WindowDuration)
	}
	if windows[1].WindowDuration != 6 {
		t.Errorf("window 1 duration: expected 6, got %d", windows[1].WindowDuration)
	}
	if windows[2].WindowDuration != 12 {
		t.Errorf("window 2 duration: expected 12, got %d", windows[2].WindowDuration)
	}
	if windows[3].WindowDuration != 40 {
		t.Errorf("window 3 duration: expected 40, got %d", windows[3].WindowDuration)
	}

	// Test semantic labels
	if windows[0].SemanticLabel != "active_motion" {
		t.Errorf("window 0 label: expected active_motion, got %s", windows[0].SemanticLabel)
	}
	if windows[1].SemanticLabel != "periodic_motion" {
		t.Errorf("window 1 label: expected periodic_motion, got %s", windows[1].SemanticLabel)
	}
}

func TestRoundToMinutes(t *testing.T) {
	tests := []struct {
		duration time.Duration
		expected float64
	}{
		{30 * time.Second, 0.5},
		{90 * time.Second, 1.5},
		{2 * time.Minute, 2.0},
		{2*time.Minute + 33*time.Second, 2.6},
		{10 * time.Minute, 10.0},
	}

	for _, tt := range tests {
		t.Run(tt.duration.String(), func(t *testing.T) {
			result := RoundToMinutes(tt.duration)
			if result != tt.expected {
				t.Errorf("expected %.1f, got %.1f", tt.expected, result)
			}
		})
	}
}
