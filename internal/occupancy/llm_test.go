package occupancy

import (
	"context"
	"log/slog"
	"math"
	"os"
	"testing"

	"github.com/saaga0h/jeeves-platform/pkg/config"
)

// Integration tests for LLM - require Ollama running locally
// Run with: go test -v ./internal/occupancy/... -run TestLLM

func getTestConfig() *config.Config {
	cfg := config.NewConfig()
	cfg.LLMEndpoint = getEnvOrDefault("LLM_ENDPOINT", "http://localhost:11434/api/generate")
	cfg.LLMModel = getEnvOrDefault("LLM_MODEL", "mixtral:8x7b")
	return cfg
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func TestLLM_ActiveMotionDetection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping LLM integration test in short mode")
	}

	cfg := getTestConfig()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

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
		TemporalPatterns: struct {
			Last2Min  SemanticLabel `json:"last_2min"`
			Last8Min  SemanticLabel `json:"last_8min"`
			Last20Min SemanticLabel `json:"last_20min"`
			Last60Min SemanticLabel `json:"last_60min"`
		}{
			Last2Min: "active_motion",
			Last8Min: "continuous_activity",
		},
		EnvironmentalSignals: struct {
			TimeOfDay string `json:"time_of_day"`
		}{
			TimeOfDay: "afternoon",
		},
	}

	stabilization := StabilizationResult{
		ShouldDampen:        false,
		StabilizationFactor: 0,
	}

	ctx := context.Background()
	result, err := AnalyzeWithLLM(ctx, "study", abstraction, stabilization, cfg, logger)

	if err != nil {
		t.Fatalf("LLM analysis failed: %v", err)
	}

	if !result.Occupied {
		t.Error("expected Occupied = true for active motion")
	}

	if result.Confidence < 0.7 {
		t.Errorf("expected Confidence >= 0.7, got %f", result.Confidence)
	}

	t.Logf("LLM result: occupied=%v, confidence=%.2f, reasoning=%s",
		result.Occupied, result.Confidence, result.Reasoning)
}

func TestLLM_PassThroughDetection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping LLM integration test in short mode")
	}

	cfg := getTestConfig()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Pass-through: 10 minutes since motion, 1 event total in 8min window
	// Note: LLM may interpret this as "settling in" which is also reasonable
	abstraction := &TemporalAbstraction{
		CurrentState: struct {
			MinutesSinceLastMotion float64 `json:"minutes_since_last_motion"`
		}{
			MinutesSinceLastMotion: 10.0,
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
		TemporalPatterns: struct {
			Last2Min  SemanticLabel `json:"last_2min"`
			Last8Min  SemanticLabel `json:"last_8min"`
			Last20Min SemanticLabel `json:"last_20min"`
			Last60Min SemanticLabel `json:"last_60min"`
		}{
			Last2Min: "no_motion",
			Last8Min: "single_motion",
		},
		EnvironmentalSignals: struct {
			TimeOfDay string `json:"time_of_day"`
		}{
			TimeOfDay: "afternoon",
		},
	}

	stabilization := StabilizationResult{
		ShouldDampen:        false,
		StabilizationFactor: 0,
	}

	ctx := context.Background()
	result, err := AnalyzeWithLLM(ctx, "hallway", abstraction, stabilization, cfg, logger)

	if err != nil {
		t.Fatalf("LLM analysis failed: %v", err)
	}

	if result.Occupied {
		t.Error("expected Occupied = false for pass-through")
	}

	if result.Confidence < 0.6 {
		t.Errorf("expected Confidence >= 0.6, got %f", result.Confidence)
	}

	t.Logf("LLM result: occupied=%v, confidence=%.2f, reasoning=%s",
		result.Occupied, result.Confidence, result.Reasoning)
}

func TestLLM_SettlingInDetection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping LLM integration test in short mode")
	}

	cfg := getTestConfig()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

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
		TemporalPatterns: struct {
			Last2Min  SemanticLabel `json:"last_2min"`
			Last8Min  SemanticLabel `json:"last_8min"`
			Last20Min SemanticLabel `json:"last_20min"`
			Last60Min SemanticLabel `json:"last_60min"`
		}{
			Last2Min: "no_motion",
			Last8Min: "continuous_activity",
		},
		EnvironmentalSignals: struct {
			TimeOfDay string `json:"time_of_day"`
		}{
			TimeOfDay: "afternoon",
		},
	}

	stabilization := StabilizationResult{
		ShouldDampen:        false,
		StabilizationFactor: 0,
	}

	ctx := context.Background()
	result, err := AnalyzeWithLLM(ctx, "study", abstraction, stabilization, cfg, logger)

	if err != nil {
		t.Fatalf("LLM analysis failed: %v", err)
	}

	if !result.Occupied {
		t.Error("expected Occupied = true for settling in pattern")
	}

	if result.Confidence < 0.5 {
		t.Errorf("expected Confidence >= 0.5, got %f", result.Confidence)
	}

	t.Logf("LLM result: occupied=%v, confidence=%.2f, reasoning=%s",
		result.Occupied, result.Confidence, result.Reasoning)
}

func TestLLM_ExtendedAbsence(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping LLM integration test in short mode")
	}

	cfg := getTestConfig()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

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
		TemporalPatterns: struct {
			Last2Min  SemanticLabel `json:"last_2min"`
			Last8Min  SemanticLabel `json:"last_8min"`
			Last20Min SemanticLabel `json:"last_20min"`
			Last60Min SemanticLabel `json:"last_60min"`
		}{
			Last2Min: "no_motion",
			Last8Min: "no_motion",
		},
		EnvironmentalSignals: struct {
			TimeOfDay string `json:"time_of_day"`
		}{
			TimeOfDay: "afternoon",
		},
	}

	stabilization := StabilizationResult{
		ShouldDampen:        false,
		StabilizationFactor: 0,
	}

	ctx := context.Background()
	result, err := AnalyzeWithLLM(ctx, "study", abstraction, stabilization, cfg, logger)

	if err != nil {
		t.Fatalf("LLM analysis failed: %v", err)
	}

	if result.Occupied {
		t.Error("expected Occupied = false for extended absence")
	}

	if result.Confidence < 0.7 {
		t.Errorf("expected Confidence >= 0.7, got %f", result.Confidence)
	}

	t.Logf("LLM result: occupied=%v, confidence=%.2f, reasoning=%s",
		result.Occupied, result.Confidence, result.Reasoning)
}

func TestLLM_FallbackConsistency(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping LLM integration test in short mode")
	}

	cfg := getTestConfig()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Test that LLM matches fallback for clear cases
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
		TemporalPatterns: struct {
			Last2Min  SemanticLabel `json:"last_2min"`
			Last8Min  SemanticLabel `json:"last_8min"`
			Last20Min SemanticLabel `json:"last_20min"`
			Last60Min SemanticLabel `json:"last_60min"`
		}{
			Last2Min: "no_motion",
			Last8Min: "no_motion",
		},
		EnvironmentalSignals: struct {
			TimeOfDay string `json:"time_of_day"`
		}{
			TimeOfDay: "afternoon",
		},
	}

	stabilization := StabilizationResult{
		ShouldDampen:        false,
		StabilizationFactor: 0,
	}

	ctx := context.Background()
	llmResult, err := AnalyzeWithLLM(ctx, "study", abstraction, stabilization, cfg, logger)
	if err != nil {
		t.Fatalf("LLM analysis failed: %v", err)
	}

	fallbackResult := FallbackAnalysis(abstraction, stabilization)

	if llmResult.Occupied != fallbackResult.Occupied {
		t.Errorf("LLM and fallback disagree on occupancy: LLM=%v, fallback=%v",
			llmResult.Occupied, fallbackResult.Occupied)
	}

	// Confidence should be within reasonable range (±0.15)
	confidenceDiff := math.Abs(llmResult.Confidence - fallbackResult.Confidence)
	if confidenceDiff > 0.15 {
		t.Errorf("LLM and fallback confidence differ too much: LLM=%.2f, fallback=%.2f, diff=%.2f",
			llmResult.Confidence, fallbackResult.Confidence, confidenceDiff)
	}

	t.Logf("LLM result: occupied=%v, confidence=%.2f",
		llmResult.Occupied, llmResult.Confidence)
	t.Logf("Fallback result: occupied=%v, confidence=%.2f",
		fallbackResult.Occupied, fallbackResult.Confidence)
}

func TestLLM_Determinism(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping LLM integration test in short mode")
	}

	cfg := getTestConfig()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

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
			Last8Min: 2,
		},
		TemporalPatterns: struct {
			Last2Min  SemanticLabel `json:"last_2min"`
			Last8Min  SemanticLabel `json:"last_8min"`
			Last20Min SemanticLabel `json:"last_20min"`
			Last60Min SemanticLabel `json:"last_60min"`
		}{
			Last2Min: "no_motion",
			Last8Min: "periodic_motion",
		},
		EnvironmentalSignals: struct {
			TimeOfDay string `json:"time_of_day"`
		}{
			TimeOfDay: "afternoon",
		},
	}

	stabilization := StabilizationResult{
		ShouldDampen:        false,
		StabilizationFactor: 0,
	}

	// Run 3 times and expect consistent results (temperature is 0.1)
	ctx := context.Background()
	var results []AnalysisResult

	for i := 0; i < 3; i++ {
		result, err := AnalyzeWithLLM(ctx, "study", abstraction, stabilization, cfg, logger)
		if err != nil {
			t.Fatalf("LLM analysis %d failed: %v", i+1, err)
		}
		results = append(results, result)
		t.Logf("Run %d: occupied=%v, confidence=%.2f", i+1, result.Occupied, result.Confidence)
	}

	// All results should have same occupancy state
	firstOccupied := results[0].Occupied
	for i, result := range results[1:] {
		if result.Occupied != firstOccupied {
			t.Errorf("Run %d: occupancy mismatch (expected %v, got %v)",
				i+2, firstOccupied, result.Occupied)
		}
	}

	// Confidence should be relatively stable (within 0.15)
	firstConfidence := results[0].Confidence
	for i, result := range results[1:] {
		confidenceDiff := math.Abs(result.Confidence - firstConfidence)
		if confidenceDiff > 0.15 {
			t.Errorf("Run %d: confidence drift too high (%.2f vs %.2f, diff=%.2f)",
				i+2, firstConfidence, result.Confidence, confidenceDiff)
		}
	}
}

func TestLLM_WithStabilization(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping LLM integration test in short mode")
	}

	cfg := getTestConfig()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

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
			Last8Min: 1,
		},
		TemporalPatterns: struct {
			Last2Min  SemanticLabel `json:"last_2min"`
			Last8Min  SemanticLabel `json:"last_8min"`
			Last20Min SemanticLabel `json:"last_20min"`
			Last60Min SemanticLabel `json:"last_60min"`
		}{
			Last2Min: "no_motion",
			Last8Min: "single_motion",
		},
		EnvironmentalSignals: struct {
			TimeOfDay string `json:"time_of_day"`
		}{
			TimeOfDay: "afternoon",
		},
	}

	stabilization := StabilizationResult{
		ShouldDampen:        true,
		StabilizationFactor: 0.4,
		Recommendation:      "high_dampening",
		OscillationCount:    5,
	}

	ctx := context.Background()
	result, err := AnalyzeWithLLM(ctx, "study", abstraction, stabilization, cfg, logger)

	if err != nil {
		t.Fatalf("LLM analysis failed: %v", err)
	}

	// Reasoning should mention stabilization
	if result.Reasoning == "" {
		t.Error("expected reasoning to be populated")
	}

	// The reasoning should include V-H stabilization info
	t.Logf("LLM result with stabilization: occupied=%v, confidence=%.2f, reasoning=%s",
		result.Occupied, result.Confidence, result.Reasoning)
}

func TestAnalyzeWithFallback_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping LLM integration test in short mode")
	}

	cfg := getTestConfig()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

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
		TemporalPatterns: struct {
			Last2Min  SemanticLabel `json:"last_2min"`
			Last8Min  SemanticLabel `json:"last_8min"`
			Last20Min SemanticLabel `json:"last_20min"`
			Last60Min SemanticLabel `json:"last_60min"`
		}{
			Last2Min: "active_motion",
		},
		EnvironmentalSignals: struct {
			TimeOfDay string `json:"time_of_day"`
		}{
			TimeOfDay: "afternoon",
		},
	}

	stabilization := StabilizationResult{
		ShouldDampen:        false,
		StabilizationFactor: 0,
	}

	ctx := context.Background()
	result := AnalyzeWithFallback(ctx, "study", abstraction, stabilization, cfg, logger)

	if !result.Occupied {
		t.Error("expected Occupied = true")
	}

	if result.Confidence < 0.7 {
		t.Errorf("expected Confidence >= 0.7, got %f", result.Confidence)
	}

	t.Logf("AnalyzeWithFallback result: occupied=%v, confidence=%.2f, reasoning=%s",
		result.Occupied, result.Confidence, result.Reasoning)
}

func TestAnalyzeWithFallback_LLMFailure(t *testing.T) {
	// Test that fallback works when LLM is unreachable
	cfg := config.NewConfig()
	cfg.LLMEndpoint = "http://localhost:9999/nonexistent" // Invalid endpoint
	cfg.LLMModel = "test"

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

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
		TemporalPatterns: struct {
			Last2Min  SemanticLabel `json:"last_2min"`
			Last8Min  SemanticLabel `json:"last_8min"`
			Last20Min SemanticLabel `json:"last_20min"`
			Last60Min SemanticLabel `json:"last_60min"`
		}{
			Last2Min: "active_motion",
		},
		EnvironmentalSignals: struct {
			TimeOfDay string `json:"time_of_day"`
		}{
			TimeOfDay: "afternoon",
		},
	}

	stabilization := StabilizationResult{
		ShouldDampen:        false,
		StabilizationFactor: 0,
	}

	ctx := context.Background()
	result := AnalyzeWithFallback(ctx, "study", abstraction, stabilization, cfg, logger)

	// Should still return valid result via fallback
	if !result.Occupied {
		t.Error("expected fallback to return Occupied = true for active motion")
	}

	if result.Confidence != 0.9 {
		t.Errorf("expected fallback confidence = 0.9, got %f", result.Confidence)
	}

	t.Logf("Fallback result: occupied=%v, confidence=%.2f, reasoning=%s",
		result.Occupied, result.Confidence, result.Reasoning)
}
