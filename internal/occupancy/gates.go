package occupancy

import (
	"time"
)

// AnalysisResult represents the output of an occupancy analysis
type AnalysisResult struct {
	Occupied   bool
	Confidence float64
	Reasoning  string
}

// ShouldUpdateOccupancy determines if an occupancy state update should be published
// Implements confidence and timing gates to prevent oscillation
func ShouldUpdateOccupancy(
	currentOccupancy *bool,
	lastStateChange *time.Time,
	analysisResult AnalysisResult,
	stabilization StabilizationResult,
) bool {
	// Gate 1: Always update initial state (first prediction)
	if currentOccupancy == nil {
		return true
	}

	// Determine if state is changing
	isStateChange := analysisResult.Occupied != *currentOccupancy

	// Gate 2: Confidence threshold (state-dependent)
	baseThreshold := 0.3 // Maintaining current state
	if isStateChange {
		baseThreshold = 0.6 // Changing state requires higher confidence
	}

	// Apply stabilization adjustment
	requiredConfidence := baseThreshold
	if stabilization.ShouldDampen {
		requiredConfidence += stabilization.StabilizationFactor
	}

	// Check confidence gate
	if analysisResult.Confidence < requiredConfidence {
		return false // Blocked by confidence gate
	}

	// Gate 3: Time hysteresis (only for state changes)
	if isStateChange && lastStateChange != nil {
		timeSinceChange := time.Since(*lastStateChange)
		if timeSinceChange < 45*time.Second {
			return false // Blocked by time hysteresis
		}
	}

	// All gates passed
	return true
}

// GetRequiredConfidence calculates the confidence threshold for a given situation
func GetRequiredConfidence(isStateChange bool, stabilization StabilizationResult) float64 {
	baseThreshold := 0.3
	if isStateChange {
		baseThreshold = 0.6
	}

	if stabilization.ShouldDampen {
		return baseThreshold + stabilization.StabilizationFactor
	}

	return baseThreshold
}
