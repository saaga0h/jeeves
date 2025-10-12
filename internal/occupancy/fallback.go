package occupancy

import (
	"fmt"
)

// FallbackAnalysis provides deterministic occupancy analysis when LLM is unavailable
// Implements the same decision tree as the LLM prompt
func FallbackAnalysis(abstraction *TemporalAbstraction, stabilization StabilizationResult) AnalysisResult {
	minutesSinceMotion := abstraction.CurrentState.MinutesSinceLastMotion
	motion2Min := abstraction.MotionDensity.Last2Min
	motion8Min := abstraction.MotionDensity.Last8Min

	// Pattern 1: Active Presence (motion in last 2 minutes)
	if motion2Min > 0 {
		reasoning := fmt.Sprintf("Motion in last 2min (%d events) - person actively present", motion2Min)
		if stabilization.ShouldDampen {
			reasoning += fmt.Sprintf(" (V-H stabilization: %s)", stabilization.Recommendation)
		}
		return AnalysisResult{
			Occupied:   true,
			Confidence: 0.9,
			Reasoning:  reasoning,
		}
	}

	// Pattern 2: Recent Motion (less than 5 minutes since last motion)
	if minutesSinceMotion < 5.0 {
		// Check if settling in (multiple recent motions, now quiet)
		if motion8Min >= 3 {
			reasoning := fmt.Sprintf("Multiple motions in recent past (%d in 2-8min), now quiet (%.1f min since) - person likely settled (working/reading)", motion8Min, minutesSinceMotion)
			if stabilization.ShouldDampen {
				reasoning += fmt.Sprintf(" (V-H stabilization: %s)", stabilization.Recommendation)
			}
			return AnalysisResult{
				Occupied:   true,
				Confidence: 0.75,
				Reasoning:  reasoning,
			}
		}

		// Single recent motion
		reasoning := fmt.Sprintf("Recent motion (%.1f min since) - likely still present", minutesSinceMotion)
		if stabilization.ShouldDampen {
			reasoning += fmt.Sprintf(" (V-H stabilization: %s)", stabilization.Recommendation)
		}
		return AnalysisResult{
			Occupied:   true,
			Confidence: 0.8,
			Reasoning:  reasoning,
		}
	}

	// Pattern 3: Pass-Through (single motion 5-10 minutes ago)
	if minutesSinceMotion >= 5.0 && minutesSinceMotion < 10.0 {
		totalRecent := motion2Min + motion8Min
		if totalRecent <= 1 {
			reasoning := fmt.Sprintf("Single motion event %.1f minutes ago - pass-through detected", minutesSinceMotion)
			if stabilization.ShouldDampen {
				reasoning += fmt.Sprintf(" (V-H stabilization: %s)", stabilization.Recommendation)
			}
			return AnalysisResult{
				Occupied:   false,
				Confidence: 0.75,
				Reasoning:  reasoning,
			}
		}

		// Multiple motions but old
		reasoning := fmt.Sprintf("Last motion %.1f minutes ago - person likely left", minutesSinceMotion)
		if stabilization.ShouldDampen {
			reasoning += fmt.Sprintf(" (V-H stabilization: %s)", stabilization.Recommendation)
		}
		return AnalysisResult{
			Occupied:   false,
			Confidence: 0.7,
			Reasoning:  reasoning,
		}
	}

	// Pattern 4: Extended Absence (10+ minutes)
	if minutesSinceMotion >= 10.0 {
		reasoning := fmt.Sprintf("No motion for %.1f minutes - extended absence", minutesSinceMotion)
		if stabilization.ShouldDampen {
			reasoning += fmt.Sprintf(" (V-H stabilization: %s)", stabilization.Recommendation)
		}
		confidence := 0.8
		if minutesSinceMotion >= 15.0 {
			confidence = 0.9
			reasoning = fmt.Sprintf("No motion for %.1f minutes - room clearly empty", minutesSinceMotion)
			if stabilization.ShouldDampen {
				reasoning += fmt.Sprintf(" (V-H stabilization: %s)", stabilization.Recommendation)
			}
		}
		return AnalysisResult{
			Occupied:   false,
			Confidence: confidence,
			Reasoning:  reasoning,
		}
	}

	// Default: Medium absence (should not reach here based on logic above)
	reasoning := fmt.Sprintf("%.1f minutes since motion - assuming empty", minutesSinceMotion)
	if stabilization.ShouldDampen {
		reasoning += fmt.Sprintf(" (V-H stabilization: %s)", stabilization.Recommendation)
	}
	return AnalysisResult{
		Occupied:   false,
		Confidence: 0.6,
		Reasoning:  reasoning,
	}
}
