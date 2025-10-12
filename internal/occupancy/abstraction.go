package occupancy

import (
	"context"
	"time"
)

// TemporalAbstraction represents a multi-scale semantic interpretation of motion data
type TemporalAbstraction struct {
	CurrentState struct {
		MinutesSinceLastMotion float64 `json:"minutes_since_last_motion"`
	} `json:"current_state"`

	TemporalPatterns struct {
		Last2Min  SemanticLabel `json:"last_2min"`
		Last8Min  SemanticLabel `json:"last_8min"`
		Last20Min SemanticLabel `json:"last_20min"`
		Last60Min SemanticLabel `json:"last_60min"`
	} `json:"temporal_patterns"`

	MotionDensity struct {
		Last2Min  int `json:"last_2min"`
		Last8Min  int `json:"last_8min"`  // Exclusive: 2-8 min window
		Last20Min int `json:"last_20min"` // Exclusive: 8-20 min window
		Last60Min int `json:"last_60min"` // Exclusive: 20-60 min window
	} `json:"motion_density"`

	EnvironmentalSignals struct {
		TimeOfDay string `json:"time_of_day"`
	} `json:"environmental_signals"`
}

// DataProvider interface abstracts data access for testability
type DataProvider interface {
	GetMotionCountInWindow(ctx context.Context, location string, start, end time.Time) (int, error)
	GetMotionEventsInWindow(ctx context.Context, location string, start, end time.Time) ([]time.Time, error)
	GetMinutesSinceLastMotion(ctx context.Context, location string, referenceTime time.Time) (float64, error)
}

// GenerateTemporalAbstraction builds a complete temporal abstraction for a location
func GenerateTemporalAbstraction(
	ctx context.Context,
	location string,
	dataProvider DataProvider,
	analysisTime time.Time,
) (*TemporalAbstraction, error) {
	// Query cumulative motion counts
	count2Min, err := dataProvider.GetMotionCountInWindow(ctx, location, analysisTime.Add(-Window2Min), analysisTime)
	if err != nil {
		return nil, err
	}

	count8Min, err := dataProvider.GetMotionCountInWindow(ctx, location, analysisTime.Add(-Window8Min), analysisTime)
	if err != nil {
		return nil, err
	}

	count20Min, err := dataProvider.GetMotionCountInWindow(ctx, location, analysisTime.Add(-Window20Min), analysisTime)
	if err != nil {
		return nil, err
	}

	count60Min, err := dataProvider.GetMotionCountInWindow(ctx, location, analysisTime.Add(-Window60Min), analysisTime)
	if err != nil {
		return nil, err
	}

	// Get timestamps for gap analysis
	timestamps2Min, _ := dataProvider.GetMotionEventsInWindow(ctx, location, analysisTime.Add(-Window2Min), analysisTime)
	timestamps8Min, _ := dataProvider.GetMotionEventsInWindow(ctx, location, analysisTime.Add(-Window8Min), analysisTime)
	timestamps20Min, _ := dataProvider.GetMotionEventsInWindow(ctx, location, analysisTime.Add(-Window20Min), analysisTime)
	timestamps60Min, _ := dataProvider.GetMotionEventsInWindow(ctx, location, analysisTime.Add(-Window60Min), analysisTime)

	// Calculate exclusive windows
	windows := CalculateExclusiveWindows(
		count2Min, count8Min, count20Min, count60Min,
		timestamps2Min, timestamps8Min, timestamps20Min, timestamps60Min,
	)

	// Get minutes since last motion
	minutesSinceMotion, err := dataProvider.GetMinutesSinceLastMotion(ctx, location, analysisTime)
	if err != nil {
		minutesSinceMotion = 999.0 // Default to large value on error
	}

	// Build abstraction
	abstraction := &TemporalAbstraction{}

	// Current state
	abstraction.CurrentState.MinutesSinceLastMotion = minutesSinceMotion

	// Temporal patterns (semantic labels)
	abstraction.TemporalPatterns.Last2Min = windows[0].SemanticLabel
	abstraction.TemporalPatterns.Last8Min = windows[1].SemanticLabel
	abstraction.TemporalPatterns.Last20Min = windows[2].SemanticLabel
	abstraction.TemporalPatterns.Last60Min = windows[3].SemanticLabel

	// Motion density (exclusive counts)
	abstraction.MotionDensity.Last2Min = windows[0].MotionCount
	abstraction.MotionDensity.Last8Min = windows[1].MotionCount
	abstraction.MotionDensity.Last20Min = windows[2].MotionCount
	abstraction.MotionDensity.Last60Min = windows[3].MotionCount

	// Environmental signals
	abstraction.EnvironmentalSignals.TimeOfDay = GetTimeOfDay(analysisTime)

	return abstraction, nil
}
