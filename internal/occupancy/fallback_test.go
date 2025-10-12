package occupancy

import (
	"strings"
	"testing"
)

func TestFallbackAnalysis_ActiveMotion(t *testing.T) {
	// Pattern 1: Active presence (motion in last 2 min)
	abstraction := &TemporalAbstraction{
		CurrentState: struct {
			MinutesSinceLastMotion float64 `json:"minutes_since_last_motion"`
		}{
			MinutesSinceLastMotion: 0.5,
		},
		MotionDensity: struct {
			Last2Min  int `json:"last_2min"`
			Last8Min  int `json:"last_8min"`
			Last20Min int `json:"last_20min"`
			Last60Min int `json:"last_60min"`
		}{
			Last2Min: 2,
			Last8Min: 3,
		},
	}

	stabilization := StabilizationResult{
		ShouldDampen:        false,
		StabilizationFactor: 0,
	}

	result := FallbackAnalysis(abstraction, stabilization)

	if !result.Occupied {
		t.Error("expected Occupied = true for active motion")
	}

	if result.Confidence != 0.9 {
		t.Errorf("expected Confidence = 0.9, got %f", result.Confidence)
	}

	if !strings.Contains(result.Reasoning, "Motion in last 2min") {
		t.Errorf("expected reasoning to mention 'Motion in last 2min', got: %s", result.Reasoning)
	}
}

func TestFallbackAnalysis_SettlingIn(t *testing.T) {
	// Pattern 3: Settling in (multiple recent motions, now quiet < 5 min)
	abstraction := &TemporalAbstraction{
		CurrentState: struct {
			MinutesSinceLastMotion float64 `json:"minutes_since_last_motion"`
		}{
			MinutesSinceLastMotion: 3.0,
		},
		MotionDensity: struct {
			Last2Min  int `json:"last_2min"`
			Last8Min  int `json:"last_8min"`
			Last20Min int `json:"last_20min"`
			Last60Min int `json:"last_60min"`
		}{
			Last2Min: 0,
			Last8Min: 4,
		},
	}

	stabilization := StabilizationResult{
		ShouldDampen:        false,
		StabilizationFactor: 0,
	}

	result := FallbackAnalysis(abstraction, stabilization)

	if !result.Occupied {
		t.Error("expected Occupied = true for settling in pattern")
	}

	if result.Confidence != 0.75 {
		t.Errorf("expected Confidence = 0.75, got %f", result.Confidence)
	}

	if !strings.Contains(result.Reasoning, "settled") {
		t.Errorf("expected reasoning to mention 'settled', got: %s", result.Reasoning)
	}
}

func TestFallbackAnalysis_PassThrough(t *testing.T) {
	// Pattern 2: Pass-through (single motion 5-10 minutes ago)
	abstraction := &TemporalAbstraction{
		CurrentState: struct {
			MinutesSinceLastMotion float64 `json:"minutes_since_last_motion"`
		}{
			MinutesSinceLastMotion: 6.0,
		},
		MotionDensity: struct {
			Last2Min  int `json:"last_2min"`
			Last8Min  int `json:"last_8min"`
			Last20Min int `json:"last_20min"`
			Last60Min int `json:"last_60min"`
		}{
			Last2Min: 0,
			Last8Min: 1,
		},
	}

	stabilization := StabilizationResult{
		ShouldDampen:        false,
		StabilizationFactor: 0,
	}

	result := FallbackAnalysis(abstraction, stabilization)

	if result.Occupied {
		t.Error("expected Occupied = false for pass-through")
	}

	if result.Confidence < 0.7 {
		t.Errorf("expected Confidence >= 0.7, got %f", result.Confidence)
	}

	if !strings.Contains(result.Reasoning, "Single motion") {
		t.Errorf("expected reasoning to mention 'Single motion', got: %s", result.Reasoning)
	}
}

func TestFallbackAnalysis_ExtendedAbsence(t *testing.T) {
	// Pattern 4: Extended absence (no motion for 15+ min)
	abstraction := &TemporalAbstraction{
		CurrentState: struct {
			MinutesSinceLastMotion float64 `json:"minutes_since_last_motion"`
		}{
			MinutesSinceLastMotion: 20.0,
		},
		MotionDensity: struct {
			Last2Min  int `json:"last_2min"`
			Last8Min  int `json:"last_8min"`
			Last20Min int `json:"last_20min"`
			Last60Min int `json:"last_60min"`
		}{
			Last2Min: 0,
			Last8Min: 0,
		},
	}

	stabilization := StabilizationResult{
		ShouldDampen:        false,
		StabilizationFactor: 0,
	}

	result := FallbackAnalysis(abstraction, stabilization)

	if result.Occupied {
		t.Error("expected Occupied = false for extended absence")
	}

	if result.Confidence < 0.8 {
		t.Errorf("expected Confidence >= 0.8, got %f", result.Confidence)
	}

	if !strings.Contains(result.Reasoning, "No motion") {
		t.Errorf("expected reasoning to mention 'No motion', got: %s", result.Reasoning)
	}
}

func TestFallbackAnalysis_RecentMotion(t *testing.T) {
	// Recent motion (< 5 min) with single event
	abstraction := &TemporalAbstraction{
		CurrentState: struct {
			MinutesSinceLastMotion float64 `json:"minutes_since_last_motion"`
		}{
			MinutesSinceLastMotion: 3.5,
		},
		MotionDensity: struct {
			Last2Min  int `json:"last_2min"`
			Last8Min  int `json:"last_8min"`
			Last20Min int `json:"last_20min"`
			Last60Min int `json:"last_60min"`
		}{
			Last2Min: 0,
			Last8Min: 1,
		},
	}

	stabilization := StabilizationResult{
		ShouldDampen:        false,
		StabilizationFactor: 0,
	}

	result := FallbackAnalysis(abstraction, stabilization)

	if !result.Occupied {
		t.Error("expected Occupied = true for recent motion")
	}

	if result.Confidence != 0.8 {
		t.Errorf("expected Confidence = 0.8, got %f", result.Confidence)
	}
}

func TestFallbackAnalysis_WithStabilization(t *testing.T) {
	// Active motion with high stabilization dampening
	abstraction := &TemporalAbstraction{
		CurrentState: struct {
			MinutesSinceLastMotion float64 `json:"minutes_since_last_motion"`
		}{
			MinutesSinceLastMotion: 0.5,
		},
		MotionDensity: struct {
			Last2Min  int `json:"last_2min"`
			Last8Min  int `json:"last_8min"`
			Last20Min int `json:"last_20min"`
			Last60Min int `json:"last_60min"`
		}{
			Last2Min: 2,
		},
	}

	stabilization := StabilizationResult{
		ShouldDampen:        true,
		StabilizationFactor: 0.4,
		Recommendation:      "high_dampening",
	}

	result := FallbackAnalysis(abstraction, stabilization)

	if !result.Occupied {
		t.Error("expected Occupied = true")
	}

	// Base confidence 0.9, but reasoning should mention stabilization
	if !strings.Contains(result.Reasoning, "V-H stabilization") {
		t.Errorf("expected reasoning to mention stabilization, got: %s", result.Reasoning)
	}

	if !strings.Contains(result.Reasoning, "high_dampening") {
		t.Errorf("expected reasoning to mention 'high_dampening', got: %s", result.Reasoning)
	}
}

func TestFallbackAnalysis_MediumAbsence(t *testing.T) {
	// 8 minutes since motion
	abstraction := &TemporalAbstraction{
		CurrentState: struct {
			MinutesSinceLastMotion float64 `json:"minutes_since_last_motion"`
		}{
			MinutesSinceLastMotion: 8.0,
		},
		MotionDensity: struct {
			Last2Min  int `json:"last_2min"`
			Last8Min  int `json:"last_8min"`
			Last20Min int `json:"last_20min"`
			Last60Min int `json:"last_60min"`
		}{
			Last2Min: 0,
			Last8Min: 2,
		},
	}

	stabilization := StabilizationResult{
		ShouldDampen:        false,
		StabilizationFactor: 0,
	}

	result := FallbackAnalysis(abstraction, stabilization)

	if result.Occupied {
		t.Error("expected Occupied = false for medium absence")
	}

	if result.Confidence != 0.7 {
		t.Errorf("expected Confidence = 0.7, got %f", result.Confidence)
	}
}

func TestFallbackAnalysis_VeryLongAbsence(t *testing.T) {
	// 15+ minutes gets higher confidence
	abstraction := &TemporalAbstraction{
		CurrentState: struct {
			MinutesSinceLastMotion float64 `json:"minutes_since_last_motion"`
		}{
			MinutesSinceLastMotion: 18.0,
		},
		MotionDensity: struct {
			Last2Min  int `json:"last_2min"`
			Last8Min  int `json:"last_8min"`
			Last20Min int `json:"last_20min"`
			Last60Min int `json:"last_60min"`
		}{
			Last2Min: 0,
			Last8Min: 0,
		},
	}

	stabilization := StabilizationResult{
		ShouldDampen:        false,
		StabilizationFactor: 0,
	}

	result := FallbackAnalysis(abstraction, stabilization)

	if result.Occupied {
		t.Error("expected Occupied = false")
	}

	if result.Confidence != 0.9 {
		t.Errorf("expected Confidence = 0.9 for 15+ min absence, got %f", result.Confidence)
	}

	if !strings.Contains(result.Reasoning, "clearly empty") {
		t.Errorf("expected reasoning to say 'clearly empty', got: %s", result.Reasoning)
	}
}
