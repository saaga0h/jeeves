package occupancy

import (
	"testing"
	"time"
)

func TestComputeVonichHakimStabilization_Unstable(t *testing.T) {
	// Test with unstable predictions (oscillating)
	unstablePredictions := []PredictionRecord{
		{Timestamp: time.Now(), Occupied: true, Confidence: 0.8},
		{Timestamp: time.Now(), Occupied: false, Confidence: 0.7},
		{Timestamp: time.Now(), Occupied: true, Confidence: 0.75},
		{Timestamp: time.Now(), Occupied: false, Confidence: 0.8},
	}

	result := ComputeVonichHakimStabilization(unstablePredictions)

	if !result.ShouldDampen {
		t.Error("expected ShouldDampen to be true for unstable predictions")
	}

	if result.StabilizationFactor <= 0.3 {
		t.Errorf("expected StabilizationFactor > 0.3, got %f", result.StabilizationFactor)
	}

	if result.OscillationCount != 3 {
		t.Errorf("expected OscillationCount = 3, got %d", result.OscillationCount)
	}

	if result.Recommendation == "maintain_course" {
		t.Errorf("unexpected recommendation for unstable sequence: %s", result.Recommendation)
	}
}

func TestComputeVonichHakimStabilization_Stable(t *testing.T) {
	// Test with stable predictions (no oscillation)
	stablePredictions := []PredictionRecord{
		{Timestamp: time.Now(), Occupied: true, Confidence: 0.85},
		{Timestamp: time.Now(), Occupied: true, Confidence: 0.87},
		{Timestamp: time.Now(), Occupied: true, Confidence: 0.83},
		{Timestamp: time.Now(), Occupied: true, Confidence: 0.86},
	}

	result := ComputeVonichHakimStabilization(stablePredictions)

	if result.ShouldDampen {
		t.Error("expected ShouldDampen to be false for stable predictions")
	}

	if result.StabilizationFactor >= 0.2 {
		t.Errorf("expected StabilizationFactor < 0.2, got %f", result.StabilizationFactor)
	}

	if result.OscillationCount != 0 {
		t.Errorf("expected OscillationCount = 0, got %d", result.OscillationCount)
	}

	if result.Recommendation != "maintain_course" {
		t.Errorf("expected maintain_course recommendation, got %s", result.Recommendation)
	}
}

func TestComputeVonichHakimStabilization_InsufficientHistory(t *testing.T) {
	// Less than 2 predictions - no stabilization
	predictions := []PredictionRecord{
		{Timestamp: time.Now(), Occupied: true, Confidence: 0.8},
	}

	result := ComputeVonichHakimStabilization(predictions)

	if result.ShouldDampen {
		t.Error("expected ShouldDampen to be false for insufficient history")
	}

	if result.StabilizationFactor != 0.0 {
		t.Errorf("expected StabilizationFactor = 0.0, got %f", result.StabilizationFactor)
	}

	if result.Recommendation != "insufficient_history" {
		t.Errorf("expected insufficient_history recommendation, got %s", result.Recommendation)
	}
}

func TestComputeVonichHakimStabilization_HighOscillation(t *testing.T) {
	// High oscillation count should trigger bias_current_state
	highOscillation := []PredictionRecord{
		{Timestamp: time.Now(), Occupied: true, Confidence: 0.8},
		{Timestamp: time.Now(), Occupied: false, Confidence: 0.8},
		{Timestamp: time.Now(), Occupied: true, Confidence: 0.8},
		{Timestamp: time.Now(), Occupied: false, Confidence: 0.8},
		{Timestamp: time.Now(), Occupied: true, Confidence: 0.8},
		{Timestamp: time.Now(), Occupied: false, Confidence: 0.8},
	}

	result := ComputeVonichHakimStabilization(highOscillation)

	if result.OscillationCount != 5 {
		t.Errorf("expected OscillationCount = 5, got %d", result.OscillationCount)
	}

	if result.Recommendation != "bias_current_state" {
		t.Errorf("expected bias_current_state recommendation, got %s", result.Recommendation)
	}

	if !result.ShouldDampen {
		t.Error("expected ShouldDampen to be true for high oscillation")
	}
}

func TestComputeVonichHakimStabilization_ModerateDampening(t *testing.T) {
	// Moderate instability - wider variance
	moderatePredictions := []PredictionRecord{
		{Timestamp: time.Now(), Occupied: true, Confidence: 0.9},
		{Timestamp: time.Now(), Occupied: true, Confidence: 0.5},
		{Timestamp: time.Now(), Occupied: false, Confidence: 0.8},
		{Timestamp: time.Now(), Occupied: false, Confidence: 0.6},
	}

	result := ComputeVonichHakimStabilization(moderatePredictions)

	// Should have moderate dampening
	if result.StabilizationFactor < 0.15 || result.StabilizationFactor > 0.4 {
		t.Errorf("expected StabilizationFactor between 0.15 and 0.4, got %f", result.StabilizationFactor)
	}

	if result.Recommendation != "moderate_dampening" {
		t.Errorf("expected moderate_dampening recommendation, got %s", result.Recommendation)
	}
}

func TestCalculateConfidenceVariance_Consistent(t *testing.T) {
	// All same confidence
	predictions := []PredictionRecord{
		{Confidence: 0.8},
		{Confidence: 0.8},
		{Confidence: 0.8},
		{Confidence: 0.8},
	}

	varianceFactor := calculateConfidenceVariance(predictions)

	if varianceFactor != 0.0 {
		t.Errorf("expected variance factor 0.0 for identical confidences, got %f", varianceFactor)
	}
}

func TestCalculateConfidenceVariance_HighVariance(t *testing.T) {
	// Wide variance in confidence
	predictions := []PredictionRecord{
		{Confidence: 0.3},
		{Confidence: 0.9},
		{Confidence: 0.4},
		{Confidence: 0.95},
	}

	varianceFactor := calculateConfidenceVariance(predictions)

	// Should have variance > 0.15 (actual is around 0.168)
	if varianceFactor < 0.15 {
		t.Errorf("expected variance factor >0.15, got %f", varianceFactor)
	}

	// Should be capped at 0.4
	if varianceFactor > 0.4 {
		t.Errorf("expected variance factor capped at 0.4, got %f", varianceFactor)
	}
}

func TestCalculateOscillationCount(t *testing.T) {
	tests := []struct {
		name           string
		predictions    []PredictionRecord
		expectedFlips  int
	}{
		{
			"no flips",
			[]PredictionRecord{
				{Occupied: true},
				{Occupied: true},
				{Occupied: true},
			},
			0,
		},
		{
			"one flip",
			[]PredictionRecord{
				{Occupied: true},
				{Occupied: true},
				{Occupied: false},
			},
			1,
		},
		{
			"two flips",
			[]PredictionRecord{
				{Occupied: true},
				{Occupied: false},
				{Occupied: false},
				{Occupied: true},
			},
			2,
		},
		{
			"alternating",
			[]PredictionRecord{
				{Occupied: true},
				{Occupied: false},
				{Occupied: true},
				{Occupied: false},
			},
			3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flips := calculateOscillationCount(tt.predictions)
			if flips != tt.expectedFlips {
				t.Errorf("expected %d flips, got %d", tt.expectedFlips, flips)
			}
		})
	}
}

func TestGetRecommendation(t *testing.T) {
	tests := []struct {
		name                string
		stabilizationFactor float64
		oscillationCount    int
		expected            string
	}{
		{"maintain course", 0.1, 1, "maintain_course"},
		{"moderate dampening", 0.2, 2, "moderate_dampening"},
		{"high dampening", 0.35, 2, "high_dampening"},
		{"bias current state", 0.1, 3, "bias_current_state"},
		{"bias over high", 0.4, 5, "bias_current_state"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := getRecommendation(tt.stabilizationFactor, tt.oscillationCount)
			if rec != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, rec)
			}
		})
	}
}
