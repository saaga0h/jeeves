package occupancy

import (
	"testing"
	"time"
)

func TestShouldUpdateOccupancy_InitialState(t *testing.T) {
	// Gate 1: Always update initial state
	result := AnalysisResult{
		Occupied:   true,
		Confidence: 0.5,
		Reasoning:  "First motion",
	}

	stabilization := StabilizationResult{
		ShouldDampen:        false,
		StabilizationFactor: 0,
	}

	shouldUpdate := ShouldUpdateOccupancy(nil, nil, result, stabilization)

	if !shouldUpdate {
		t.Error("expected initial state to always update")
	}
}

func TestShouldUpdateOccupancy_HighConfidenceChange(t *testing.T) {
	// State change with high confidence should pass
	currentOccupancy := true
	lastChange := time.Now().Add(-2 * time.Minute)

	result := AnalysisResult{
		Occupied:   false, // Changing state
		Confidence: 0.85,
		Reasoning:  "Extended absence",
	}

	stabilization := StabilizationResult{
		ShouldDampen:        false,
		StabilizationFactor: 0,
	}

	shouldUpdate := ShouldUpdateOccupancy(&currentOccupancy, &lastChange, result, stabilization)

	if !shouldUpdate {
		t.Error("expected high confidence state change to pass")
	}
}

func TestShouldUpdateOccupancy_LowConfidenceBlocked(t *testing.T) {
	// State change with low confidence should be blocked
	currentOccupancy := true
	lastChange := time.Now().Add(-2 * time.Minute)

	result := AnalysisResult{
		Occupied:   false, // Changing state
		Confidence: 0.5,   // Too low (need 0.6)
		Reasoning:  "Uncertain",
	}

	stabilization := StabilizationResult{
		ShouldDampen:        false,
		StabilizationFactor: 0,
	}

	shouldUpdate := ShouldUpdateOccupancy(&currentOccupancy, &lastChange, result, stabilization)

	if shouldUpdate {
		t.Error("expected low confidence state change to be blocked")
	}
}

func TestShouldUpdateOccupancy_StabilizationRaisesThreshold(t *testing.T) {
	// Stabilization factor raises required confidence
	currentOccupancy := true
	lastChange := time.Now().Add(-2 * time.Minute)

	result := AnalysisResult{
		Occupied:   false,
		Confidence: 0.7, // Would normally pass (> 0.6), but with stabilization...
		Reasoning:  "Absence",
	}

	stabilization := StabilizationResult{
		ShouldDampen:        true,
		StabilizationFactor: 0.3, // Required = 0.6 + 0.3 = 0.9
	}

	shouldUpdate := ShouldUpdateOccupancy(&currentOccupancy, &lastChange, result, stabilization)

	if shouldUpdate {
		t.Error("expected stabilization to block update (0.7 < 0.9)")
	}
}

func TestShouldUpdateOccupancy_MaintainingState(t *testing.T) {
	// Maintaining same state has lower threshold (0.3)
	currentOccupancy := true
	lastChange := time.Now().Add(-2 * time.Minute)

	result := AnalysisResult{
		Occupied:   true, // Same state
		Confidence: 0.4,  // Would fail for state change, but OK for maintaining
		Reasoning:  "Still occupied",
	}

	stabilization := StabilizationResult{
		ShouldDampen:        false,
		StabilizationFactor: 0,
	}

	shouldUpdate := ShouldUpdateOccupancy(&currentOccupancy, &lastChange, result, stabilization)

	if !shouldUpdate {
		t.Error("expected maintaining state with 0.4 confidence to pass")
	}
}

func TestShouldUpdateOccupancy_TimeHysteresis(t *testing.T) {
	// Time hysteresis blocks rapid state changes
	currentOccupancy := true
	lastChange := time.Now().Add(-30 * time.Second) // Too recent

	result := AnalysisResult{
		Occupied:   false,
		Confidence: 0.9, // High confidence
		Reasoning:  "Empty",
	}

	stabilization := StabilizationResult{
		ShouldDampen:        false,
		StabilizationFactor: 0,
	}

	shouldUpdate := ShouldUpdateOccupancy(&currentOccupancy, &lastChange, result, stabilization)

	if shouldUpdate {
		t.Error("expected time hysteresis to block update (< 45s)")
	}
}

func TestShouldUpdateOccupancy_TimeHysteresisPasses(t *testing.T) {
	// Time hysteresis allows change after 45 seconds
	currentOccupancy := true
	lastChange := time.Now().Add(-60 * time.Second) // Enough time passed

	result := AnalysisResult{
		Occupied:   false,
		Confidence: 0.7,
		Reasoning:  "Empty",
	}

	stabilization := StabilizationResult{
		ShouldDampen:        false,
		StabilizationFactor: 0,
	}

	shouldUpdate := ShouldUpdateOccupancy(&currentOccupancy, &lastChange, result, stabilization)

	if !shouldUpdate {
		t.Error("expected time hysteresis to pass after 60s")
	}
}

func TestGetRequiredConfidence(t *testing.T) {
	tests := []struct {
		name                string
		isStateChange       bool
		stabilizationFactor float64
		expectedMin         float64
	}{
		{"maintaining state, no stabilization", false, 0.0, 0.3},
		{"changing state, no stabilization", true, 0.0, 0.6},
		{"maintaining state, with stabilization", false, 0.2, 0.5},
		{"changing state, with stabilization", true, 0.3, 0.9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stabilization := StabilizationResult{
				ShouldDampen:        tt.stabilizationFactor > 0,
				StabilizationFactor: tt.stabilizationFactor,
			}

			required := GetRequiredConfidence(tt.isStateChange, stabilization)

			// Use tolerance for floating point comparison
			tolerance := 0.001
			if required < tt.expectedMin-tolerance || required > tt.expectedMin+tolerance {
				t.Errorf("expected %f, got %f", tt.expectedMin, required)
			}
		})
	}
}

func TestShouldUpdateOccupancy_NoLastChangeTime(t *testing.T) {
	// First state change (no previous lastStateChange)
	currentOccupancy := true

	result := AnalysisResult{
		Occupied:   false,
		Confidence: 0.7,
		Reasoning:  "Empty",
	}

	stabilization := StabilizationResult{
		ShouldDampen:        false,
		StabilizationFactor: 0,
	}

	shouldUpdate := ShouldUpdateOccupancy(&currentOccupancy, nil, result, stabilization)

	if !shouldUpdate {
		t.Error("expected state change with nil lastStateChange to pass")
	}
}

func TestShouldUpdateOccupancy_HighStabilizationBlocksAll(t *testing.T) {
	// Very high stabilization factor should block even high confidence
	currentOccupancy := true
	lastChange := time.Now().Add(-2 * time.Minute)

	result := AnalysisResult{
		Occupied:   false,
		Confidence: 0.95,
		Reasoning:  "Empty",
	}

	stabilization := StabilizationResult{
		ShouldDampen:        true,
		StabilizationFactor: 0.5, // Required = 0.6 + 0.5 = 1.1 (impossible)
	}

	shouldUpdate := ShouldUpdateOccupancy(&currentOccupancy, &lastChange, result, stabilization)

	if shouldUpdate {
		t.Error("expected very high stabilization to block even 0.95 confidence")
	}
}
