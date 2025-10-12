package light

import (
	"context"
	"log/slog"
	"os"
	"testing"
)

// Mock analyzer that returns a predictable assessment
type mockAnalyzer struct {
	assessment *IlluminanceAssessment
}

func (m *mockAnalyzer) GetIlluminanceAssessment(ctx context.Context, location, timeOfDay string) *IlluminanceAssessment {
	return m.assessment
}

func TestMakeLightingDecision_Rule0_ManualOverride(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	overrideManager := NewOverrideManager()

	// Set manual override
	overrideManager.SetManualOverride("study", 30)

	analyzer := &mockAnalyzer{
		assessment: &IlluminanceAssessment{
			State:      "dark",
			Lux:        10,
			Confidence: 0.95,
			Source:     "recent_reading",
		},
	}

	decision := MakeLightingDecision(
		context.Background(),
		"study",
		"occupied",
		0.95,
		analyzer,
		overrideManager,
		logger,
	)

	if decision.Action != "maintain" {
		t.Errorf("Expected action 'maintain', got '%s'", decision.Action)
	}

	if decision.Reason != "manual_override_active" {
		t.Errorf("Expected reason 'manual_override_active', got '%s'", decision.Reason)
	}

	if decision.Confidence != 1.0 {
		t.Errorf("Expected confidence 1.0, got %f", decision.Confidence)
	}
}

func TestMakeLightingDecision_Rule1_EmptyRoom(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	overrideManager := NewOverrideManager()

	analyzer := &mockAnalyzer{
		assessment: &IlluminanceAssessment{
			State:      "dark",
			Lux:        10,
			Confidence: 0.95,
			Source:     "recent_reading",
		},
	}

	decision := MakeLightingDecision(
		context.Background(),
		"study",
		"empty",
		0.90,
		analyzer,
		overrideManager,
		logger,
	)

	if decision.Action != "off" {
		t.Errorf("Expected action 'off', got '%s'", decision.Action)
	}

	if decision.Brightness != 0 {
		t.Errorf("Expected brightness 0, got %d", decision.Brightness)
	}

	if decision.Reason != "room_empty" {
		t.Errorf("Expected reason 'room_empty', got '%s'", decision.Reason)
	}

	if decision.Confidence != 0.90 {
		t.Errorf("Expected confidence 0.90, got %f", decision.Confidence)
	}
}

func TestMakeLightingDecision_Rule2_UncertainOccupancy(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	overrideManager := NewOverrideManager()

	analyzer := &mockAnalyzer{
		assessment: &IlluminanceAssessment{
			State:      "dark",
			Lux:        10,
			Confidence: 0.95,
			Source:     "recent_reading",
		},
	}

	testCases := []struct {
		state            string
		expectedReason   string
	}{
		{"likely", "awaiting_occupancy_confirmation_likely"},
		{"unlikely", "awaiting_occupancy_confirmation_unlikely"},
		{"unknown", "awaiting_occupancy_confirmation_unknown"},
	}

	for _, tc := range testCases {
		t.Run(tc.state, func(t *testing.T) {
			decision := MakeLightingDecision(
				context.Background(),
				"study",
				tc.state,
				0.65,
				analyzer,
				overrideManager,
				logger,
			)

			if decision.Action != "maintain" {
				t.Errorf("Expected action 'maintain', got '%s'", decision.Action)
			}

			if decision.Reason != tc.expectedReason {
				t.Errorf("Expected reason '%s', got '%s'", tc.expectedReason, decision.Reason)
			}
		})
	}
}

func TestMakeLightingDecision_Rule3_LowConfidence(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	overrideManager := NewOverrideManager()

	analyzer := &mockAnalyzer{
		assessment: &IlluminanceAssessment{
			State:      "dark",
			Lux:        10,
			Confidence: 0.95,
			Source:     "recent_reading",
		},
	}

	decision := MakeLightingDecision(
		context.Background(),
		"study",
		"occupied",
		0.45, // Below 0.5 threshold
		analyzer,
		overrideManager,
		logger,
	)

	if decision.Action != "maintain" {
		t.Errorf("Expected action 'maintain', got '%s'", decision.Action)
	}

	if decision.Reason != "occupancy_confidence_too_low" {
		t.Errorf("Expected reason 'occupancy_confidence_too_low', got '%s'", decision.Reason)
	}

	if decision.Confidence != 0.45 {
		t.Errorf("Expected confidence 0.45, got %f", decision.Confidence)
	}
}

func TestMakeLightingDecision_Rule4_OccupiedDark(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	overrideManager := NewOverrideManager()

	analyzer := &mockAnalyzer{
		assessment: &IlluminanceAssessment{
			State:      "dark",
			Lux:        3.0,
			Confidence: 0.95,
			Source:     "recent_reading",
		},
	}

	decision := MakeLightingDecision(
		context.Background(),
		"study",
		"occupied",
		0.95,
		analyzer,
		overrideManager,
		logger,
	)

	if decision.Action != "on" {
		t.Errorf("Expected action 'on', got '%s'", decision.Action)
	}

	// Dark conditions during active hours should give 80% brightness
	if decision.Brightness != 80 && decision.Brightness != 50 {
		t.Errorf("Expected brightness 80 or 50 (depending on time), got %d", decision.Brightness)
	}

	// Color temp should be set
	if decision.ColorTemp == 0 {
		t.Errorf("Expected non-zero color temp, got 0")
	}

	// Confidence should be minimum of occupancy and illuminance
	expectedConfidence := 0.95
	if decision.Confidence != expectedConfidence {
		t.Errorf("Expected confidence %f, got %f", expectedConfidence, decision.Confidence)
	}
}

func TestMakeLightingDecision_Rule4_OccupiedDim(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	overrideManager := NewOverrideManager()

	analyzer := &mockAnalyzer{
		assessment: &IlluminanceAssessment{
			State:      "dim",
			Lux:        30.0,
			Confidence: 0.85,
			Source:     "recent_reading",
		},
	}

	decision := MakeLightingDecision(
		context.Background(),
		"study",
		"occupied",
		0.90,
		analyzer,
		overrideManager,
		logger,
	)

	if decision.Action != "on" {
		t.Errorf("Expected action 'on', got '%s'", decision.Action)
	}

	// Dim conditions should give 40-60% brightness depending on time
	if decision.Brightness < 40 || decision.Brightness > 60 {
		t.Errorf("Expected brightness between 40-60, got %d", decision.Brightness)
	}

	// Confidence should be minimum of occupancy and illuminance
	expectedConfidence := 0.85 // Min of 0.90 and 0.85
	if decision.Confidence != expectedConfidence {
		t.Errorf("Expected confidence %f, got %f", expectedConfidence, decision.Confidence)
	}
}

func TestMakeLightingDecision_Rule4_OccupiedBright(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	overrideManager := NewOverrideManager()

	analyzer := &mockAnalyzer{
		assessment: &IlluminanceAssessment{
			State:      "bright",
			Lux:        650.0,
			Confidence: 0.95,
			Source:     "recent_reading",
		},
	}

	decision := MakeLightingDecision(
		context.Background(),
		"study",
		"occupied",
		0.95,
		analyzer,
		overrideManager,
		logger,
	)

	// Bright conditions should result in off or very low brightness
	if decision.Action != "off" && decision.Brightness > 10 {
		t.Errorf("Expected action 'off' or brightness <= 10, got action '%s' brightness %d",
			decision.Action, decision.Brightness)
	}
}

func TestMakeLightingDecision_Rule4_LowIlluminanceConfidence(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	overrideManager := NewOverrideManager()

	analyzer := &mockAnalyzer{
		assessment: &IlluminanceAssessment{
			State:      "dim",
			Lux:        30.0,
			Confidence: 0.50, // Low illuminance confidence
			Source:     "time_based_default",
		},
	}

	decision := MakeLightingDecision(
		context.Background(),
		"study",
		"occupied",
		0.95, // High occupancy confidence
		analyzer,
		overrideManager,
		logger,
	)

	// Should still make a decision but with lower combined confidence
	if decision.Action != "on" {
		t.Errorf("Expected action 'on', got '%s'", decision.Action)
	}

	// Confidence should be minimum of both
	expectedConfidence := 0.50
	if decision.Confidence != expectedConfidence {
		t.Errorf("Expected confidence %f, got %f", expectedConfidence, decision.Confidence)
	}
}

func TestMakeLightingDecision_ReasonFormat(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	overrideManager := NewOverrideManager()

	analyzer := &mockAnalyzer{
		assessment: &IlluminanceAssessment{
			State:      "dark",
			Lux:        10.0,
			Confidence: 0.95,
			Source:     "recent_reading",
		},
	}

	decision := MakeLightingDecision(
		context.Background(),
		"study",
		"occupied",
		0.95,
		analyzer,
		overrideManager,
		logger,
	)

	// Reason should follow format: occupied_{state}_{timeOfDay}_{source}
	// Example: "occupied_dark_morning_recent_reading"
	if len(decision.Reason) < 10 {
		t.Errorf("Expected detailed reason, got '%s'", decision.Reason)
	}

	// Check that reason contains expected components
	if decision.Reason[:8] != "occupied" {
		t.Errorf("Expected reason to start with 'occupied', got '%s'", decision.Reason)
	}
}

func TestMakeLightingDecision_DetailsIncluded(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	overrideManager := NewOverrideManager()

	analyzer := &mockAnalyzer{
		assessment: &IlluminanceAssessment{
			State:      "dim",
			Lux:        45.0,
			Confidence: 0.85,
			Source:     "historical_pattern",
		},
	}

	decision := MakeLightingDecision(
		context.Background(),
		"study",
		"occupied",
		0.90,
		analyzer,
		overrideManager,
		logger,
	)

	// Check that details are populated
	if decision.Details == nil {
		t.Fatal("Expected Details to be populated, got nil")
	}

	// Check for key detail fields
	if _, exists := decision.Details["illuminance_source"]; !exists {
		t.Error("Expected illuminance_source in details")
	}

	if _, exists := decision.Details["illuminance_state"]; !exists {
		t.Error("Expected illuminance_state in details")
	}

	if _, exists := decision.Details["time_of_day"]; !exists {
		t.Error("Expected time_of_day in details")
	}

	if _, exists := decision.Details["brightness_reason"]; !exists {
		t.Error("Expected brightness_reason in details")
	}
}

func TestOverrideManager_BasicOperations(t *testing.T) {
	om := NewOverrideManager()

	// Initially no override
	if om.CheckManualOverride("study") {
		t.Error("Expected no override initially")
	}

	// Set override
	om.SetManualOverride("study", 30)

	// Should now be active
	if !om.CheckManualOverride("study") {
		t.Error("Expected override to be active")
	}

	// Clear override
	cleared := om.ClearManualOverride("study")
	if !cleared {
		t.Error("Expected clear to return true")
	}

	// Should no longer be active
	if om.CheckManualOverride("study") {
		t.Error("Expected override to be inactive after clear")
	}
}

func TestRateLimiter_BasicOperations(t *testing.T) {
	rl := NewRateLimiter()

	// First decision should be allowed
	if !rl.ShouldMakeDecision("study", 1000) {
		t.Error("Expected first decision to be allowed")
	}

	// Immediate second decision should be blocked
	if rl.ShouldMakeDecision("study", 1000) {
		t.Error("Expected second immediate decision to be blocked")
	}

	// Different location should be allowed
	if !rl.ShouldMakeDecision("bedroom", 1000) {
		t.Error("Expected decision for different location to be allowed")
	}
}
