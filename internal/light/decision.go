package light

import (
	"context"
	"fmt"
	"log/slog"
)

// Decision represents a lighting decision with action, settings, and reasoning
type Decision struct {
	Action     string                 // "on", "off", "maintain"
	Brightness int                    // 0-100
	ColorTemp  int                    // Kelvin (2400-5500), 0 if not applicable
	Reason     string                 // Concise reason for the decision
	Confidence float64                // 0.0-1.0
	Details    map[string]interface{} // Additional context for debugging
}

// IlluminanceAssessor provides illuminance assessment for decision making
type IlluminanceAssessor interface {
	GetIlluminanceAssessment(ctx context.Context, location, timeOfDay string) *IlluminanceAssessment
}

// MakeLightingDecision implements the core decision logic (Rules 0-4)
// Returns a Decision struct with action, brightness, colorTemp, reason, and confidence
func MakeLightingDecision(
	ctx context.Context,
	location string,
	occupancyState string,
	occupancyConfidence float64,
	analyzer IlluminanceAssessor,
	overrideManager *OverrideManager,
	logger *slog.Logger,
) *Decision {
	// Get current time of day for all calculations
	timeOfDay := getTimeOfDay()

	// Rule 0: Manual Override - Always maintain current state
	if overrideManager.CheckManualOverride(location) {
		logger.Debug("Rule 0: Manual override active",
			"location", location,
			"action", "maintain")
		return &Decision{
			Action:     "maintain",
			Reason:     "manual_override_active",
			Confidence: 1.0,
			Details: map[string]interface{}{
				"rule":        0,
				"description": "Manual override active",
			},
		}
	}

	// Rule 1: Empty Room - Turn lights off immediately
	if occupancyState == "empty" {
		logger.Debug("Rule 1: Room empty, turning lights off",
			"location", location,
			"confidence", occupancyConfidence)
		return &Decision{
			Action:     "off",
			Brightness: 0,
			ColorTemp:  0,
			Reason:     "room_empty",
			Confidence: occupancyConfidence,
			Details: map[string]interface{}{
				"rule":                "1",
				"description":         "Empty room - lights off",
				"occupancy_state":     occupancyState,
				"occupancy_confidence": occupancyConfidence,
			},
		}
	}

	// Rule 2: Uncertain Occupancy - Wait for clarity
	uncertainStates := map[string]bool{
		"likely":   true,
		"unlikely": true,
		"unknown":  true,
	}
	if uncertainStates[occupancyState] {
		logger.Debug("Rule 2: Uncertain occupancy, maintaining state",
			"location", location,
			"occupancy_state", occupancyState)
		return &Decision{
			Action:     "maintain",
			Reason:     fmt.Sprintf("awaiting_occupancy_confirmation_%s", occupancyState),
			Confidence: occupancyConfidence,
			Details: map[string]interface{}{
				"rule":                "2",
				"description":         "Uncertain occupancy - maintain",
				"occupancy_state":     occupancyState,
				"occupancy_confidence": occupancyConfidence,
			},
		}
	}

	// Rule 3: Low Confidence - Don't act on uncertain information
	if occupancyConfidence < 0.5 {
		logger.Debug("Rule 3: Low occupancy confidence, maintaining state",
			"location", location,
			"confidence", occupancyConfidence)
		return &Decision{
			Action:     "maintain",
			Reason:     "occupancy_confidence_too_low",
			Confidence: occupancyConfidence,
			Details: map[string]interface{}{
				"rule":                "3",
				"description":         "Low confidence - maintain",
				"occupancy_state":     occupancyState,
				"occupancy_confidence": occupancyConfidence,
				"threshold":           0.5,
			},
		}
	}

	// Rule 4: Occupied Room - Calculate lighting needs
	if occupancyState == "occupied" {
		// Get illuminance assessment using 3-tier fallback strategy
		assessment := analyzer.GetIlluminanceAssessment(ctx, location, timeOfDay)

		logger.Debug("Rule 4: Occupied room, calculating lighting needs",
			"location", location,
			"illuminance_state", assessment.State,
			"illuminance_lux", assessment.Lux,
			"illuminance_source", assessment.Source,
			"time_of_day", timeOfDay)

		// Determine if light is natural
		isNaturalLight := isLikelyNaturalLight(assessment.Lux, timeOfDay)

		// Calculate brightness based on illuminance conditions
		brightnessResult := calculateBrightness(
			assessment.State,
			assessment.Lux,
			isNaturalLight,
			timeOfDay,
		)

		// Calculate color temperature based on time of day
		colorTemp := calculateColorTemperature(timeOfDay)

		// Determine action (on or off)
		action := "on"
		if brightnessResult.Brightness == 0 {
			action = "off"
		}

		// Build comprehensive reason
		reason := fmt.Sprintf("occupied_%s_%s_%s",
			assessment.State,
			timeOfDay,
			assessment.Source)

		// Combine occupancy and illuminance confidence
		// Use minimum of both as we rely on both for decision
		combinedConfidence := min(occupancyConfidence, assessment.Confidence)

		logger.Debug("Rule 4: Decision calculated",
			"location", location,
			"action", action,
			"brightness", brightnessResult.Brightness,
			"color_temp", colorTemp,
			"reason", reason,
			"confidence", combinedConfidence)

		return &Decision{
			Action:     action,
			Brightness: brightnessResult.Brightness,
			ColorTemp:  colorTemp,
			Reason:     reason,
			Confidence: combinedConfidence,
			Details: map[string]interface{}{
				"rule":                "4",
				"description":         "Occupied room - calculated lighting",
				"occupancy_state":     occupancyState,
				"occupancy_confidence": occupancyConfidence,
				"brightness_reason":   brightnessResult.Reason,
				"illuminance_source":  assessment.Source,
				"illuminance_state":   assessment.State,
				"illuminance_lux":     fmt.Sprintf("%.1f", assessment.Lux),
				"illuminance_confidence": assessment.Confidence,
				"is_natural_light":    isNaturalLight,
				"time_of_day":         timeOfDay,
			},
		}
	}

	// Fallback: Unknown occupancy state - maintain
	logger.Warn("Unexpected occupancy state, maintaining current state",
		"location", location,
		"occupancy_state", occupancyState)
	return &Decision{
		Action:     "maintain",
		Reason:     fmt.Sprintf("unexpected_occupancy_state_%s", occupancyState),
		Confidence: occupancyConfidence,
		Details: map[string]interface{}{
			"rule":                "fallback",
			"description":         "Unexpected state - maintain",
			"occupancy_state":     occupancyState,
			"occupancy_confidence": occupancyConfidence,
		},
	}
}

// min returns the minimum of two float64 values
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
