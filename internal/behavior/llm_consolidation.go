package behavior

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/saaga0h/jeeves-platform/pkg/config"
	"github.com/saaga0h/jeeves-platform/pkg/llm"
)

// ===================================================================
// LLM Consolidation Input/Output Types
// ===================================================================

// ConsolidationInput is the input for LLM analysis
type ConsolidationInput struct {
	Episodes []*MicroEpisode      `json:"episodes"`
	Context  ConsolidationContext `json:"context"`
}

// ConsolidationContext provides temporal and spatial context
type ConsolidationContext struct {
	TimeOfDay        string    `json:"time_of_day"`
	DayOfWeek        string    `json:"day_of_week"`
	LocationSequence string    `json:"location_sequence"`
	TotalDuration    int       `json:"total_duration_min"`
	Gaps             []float64 `json:"gaps_hours"`
}

// ConsolidationOutput is the structured LLM response
type ConsolidationOutput struct {
	ShouldMerge bool    `json:"should_merge"`
	PatternType *string `json:"pattern_type"`
	MacroName   string  `json:"macro_name,omitempty"`
	Confidence  float64 `json:"confidence"`
	Reasoning   string  `json:"reasoning"`
}

// ===================================================================
// LLM Analyzer Implementation
// ===================================================================

// ConsolidationAnalyzer implements llm.Analyzer for episode consolidation
type ConsolidationAnalyzer struct {
	cfg *config.Config
}

// NewConsolidationAnalyzer creates a new analyzer
func NewConsolidationAnalyzer(cfg *config.Config) *ConsolidationAnalyzer {
	return &ConsolidationAnalyzer{cfg: cfg}
}

// BuildPrompt creates the LLM prompt from episode data
func (a *ConsolidationAnalyzer) BuildPrompt(input ConsolidationInput) string {
	episodes := input.Episodes
	ctx := input.Context

	// Build episode data for prompt
	episodeData := make([]map[string]interface{}, len(episodes))
	for i, ep := range episodes {
		duration := 0
		if ep.EndedAt != nil {
			duration = int(ep.EndedAt.Sub(ep.StartedAt).Minutes())
		}

		episodeData[i] = map[string]interface{}{
			"location": ep.Location,
			"start":    ep.StartedAt.Format("15:04"),
			"end":      ep.EndedAt.Format("15:04"),
			"duration": duration,
		}
	}

	data := map[string]interface{}{
		"episodes": episodeData,
		"context": map[string]interface{}{
			"time_of_day":    ctx.TimeOfDay,
			"day_of_week":    ctx.DayOfWeek,
			"sequence":       ctx.LocationSequence,
			"total_duration": ctx.TotalDuration,
			"gaps":           ctx.Gaps,
		},
	}

	jsonData, _ := json.MarshalIndent(data, "", "  ")

	return fmt.Sprintf(`Analyze these behavioral episodes to determine if they represent a SINGLE continuous activity pattern or SEPARATE unrelated activities.

IMPORTANT: It is PERFECTLY ACCEPTABLE to say should_merge=false. Many episodes are naturally separate activities and should NOT be merged.

Red flags that indicate SEPARATE activities (do NOT merge):
- Gaps > 4 hours (likely sleep, work, or different activity)
- Overnight gaps (crossing sleep period)
- Illogical location sequences
- Very different activity contexts

Consider:
1. Temporal proximity - Are gaps < 2 hours?
2. Location sequence - Does the flow make sense?
3. Time of day - Does it cross major boundaries?
4. Duration patterns - Quick transitions vs. long gaps

Examples:
✓ MERGE: bedroom(8:00)→kitchen(8:15)→dining(8:35) - Morning routine
✗ DON'T MERGE: bedroom(22:00)→[8h gap]→kitchen(7:00) - Sleep in between
✗ DON'T MERGE: study(14:00)→[6h gap]→living_room(20:00) - Different activities

Data:
%s

Respond ONLY with valid JSON (no markdown, no explanation):
{
  "should_merge": true/false,
  "pattern_type": "morning_routine" | "meal_preparation" | "work_session" | "entertainment" | "evening_routine" | null,
  "macro_name": "human readable name",
  "confidence": 0.0-1.0,
  "reasoning": "explanation"
}`, jsonData)
}

// ParseResponse parses the LLM's JSON response
func (a *ConsolidationAnalyzer) ParseResponse(response string) (ConsolidationOutput, error) {
	var output ConsolidationOutput
	if err := json.Unmarshal([]byte(response), &output); err != nil {
		return ConsolidationOutput{}, fmt.Errorf("failed to parse JSON: %w", err)
	}
	return output, nil
}

// Validate checks if output meets constraints
func (a *ConsolidationAnalyzer) Validate(output ConsolidationOutput) error {
	if output.Confidence < 0.0 || output.Confidence > 1.0 {
		return fmt.Errorf("confidence must be 0.0-1.0, got %.2f", output.Confidence)
	}

	if output.ShouldMerge && output.PatternType == nil {
		return fmt.Errorf("pattern_type required when should_merge is true")
	}

	if output.Reasoning == "" {
		return fmt.Errorf("reasoning is required")
	}

	return nil
}

// ===================================================================
// LLM Consolidation Logic (called by agent)
// ===================================================================

// consolidateWithLLM is a PURE FUNCTION that uses LLM to consolidate complex patterns
// Takes episodes and config, returns macro-episodes
func consolidateWithLLM(
	ctx context.Context,
	episodes []*MicroEpisode,
	llmClient llm.Client,
	cfg *config.Config,
	logger *slog.Logger,
	now time.Time,
) ([]*MacroEpisode, error) {
	if len(episodes) < 2 {
		return nil, nil
	}

	logger.Info("LLM consolidation starting",
		"episodes", len(episodes),
		"model", cfg.LLMModel)

	// Group episodes into time windows
	windows := groupByTimeWindow(episodes, 2*time.Hour)

	logger.Info("Episodes grouped into time windows",
		"windows", len(windows))

	var macros []*MacroEpisode
	windowsProcessed := 0
	windowsSkipped := 0
	windowsMerged := 0

	for windowIdx, window := range windows {
		logger.Debug("Processing window",
			"window_index", windowIdx,
			"episodes", len(window))

		if len(window) < 2 {
			logger.Debug("Window too small, skipping", "window_index", windowIdx)
			windowsSkipped++
			continue
		}

		// Skip if all same location
		if allSameLocation(window) {
			logger.Debug("Window has same location, skipping LLM",
				"window_index", windowIdx,
				"location", window[0].Location)
			windowsSkipped++
			continue
		}

		// Prepare input
		input := ConsolidationInput{
			Episodes: window,
			Context: ConsolidationContext{
				TimeOfDay:        categorizeTimeOfDay(window[0].StartedAt),
				DayOfWeek:        window[0].StartedAt.Weekday().String(),
				LocationSequence: buildLocationSequence(window),
				TotalDuration:    calculateTotalDuration(window),
				Gaps:             calculateGaps(window),
			},
		}

		logger.Info("Calling LLM for window analysis",
			"window_index", windowIdx,
			"time_of_day", input.Context.TimeOfDay,
			"sequence", input.Context.LocationSequence,
			"total_duration_min", input.Context.TotalDuration)

		// Call LLM
		analyzer := NewConsolidationAnalyzer(cfg)
		output, err := llm.Analyze(ctx, llmClient, analyzer, cfg.LLMModel, input, logger)
		if err != nil {
			logger.Error("LLM analysis failed for window",
				"window_index", windowIdx,
				"error", err)
			continue
		}

		logger.Info("LLM response received",
			"window_index", windowIdx,
			"should_merge", output.ShouldMerge,
			"pattern_type", output.PatternType,
			"confidence", output.Confidence,
			"reasoning", output.Reasoning)

		// Validate
		isValid := validateLLMResult(output, window, cfg.ConsolidationMaxGapMinutes, cfg.LLMMinConfidence, logger)

		if !isValid {
			logger.Warn("LLM result rejected by validation",
				"window_index", windowIdx)
			windowsProcessed++
			continue
		}

		// Create macro if LLM says merge
		if output.ShouldMerge {
			macro := createMacroFromLLM(window, output, now)
			macros = append(macros, macro)
			windowsMerged++

			logger.Info("LLM macro-episode created",
				"window_index", windowIdx,
				"macro_id", macro.ID,
				"pattern", macro.PatternType,
				"locations", macro.Locations)
		}

		windowsProcessed++
	}

	logger.Info("LLM consolidation complete",
		"windows_total", len(windows),
		"windows_processed", windowsProcessed,
		"windows_skipped", windowsSkipped,
		"windows_merged", windowsMerged,
		"macros_created", len(macros))

	return macros, nil
}

// ===================================================================
// Helper Functions (pure functions, no Agent dependency)
// ===================================================================

// groupByTimeWindow groups episodes into time windows
func groupByTimeWindow(episodes []*MicroEpisode, windowSize time.Duration) [][]*MicroEpisode {
	if len(episodes) == 0 {
		return nil
	}

	var windows [][]*MicroEpisode
	currentWindow := []*MicroEpisode{episodes[0]}
	windowStart := episodes[0].StartedAt

	for i := 1; i < len(episodes); i++ {
		ep := episodes[i]

		if ep.StartedAt.Sub(windowStart) < windowSize {
			currentWindow = append(currentWindow, ep)
		} else {
			if len(currentWindow) > 0 {
				windows = append(windows, currentWindow)
			}
			currentWindow = []*MicroEpisode{ep}
			windowStart = ep.StartedAt
		}
	}

	if len(currentWindow) > 0 {
		windows = append(windows, currentWindow)
	}

	return windows
}

// allSameLocation checks if all episodes are in same location
func allSameLocation(episodes []*MicroEpisode) bool {
	if len(episodes) == 0 {
		return true
	}
	first := episodes[0].Location
	for _, ep := range episodes[1:] {
		if ep.Location != first {
			return false
		}
	}
	return true
}

// categorizeTimeOfDay returns time of day category
func categorizeTimeOfDay(t time.Time) string {
	hour := t.Hour()
	switch {
	case hour >= 5 && hour < 12:
		return "morning"
	case hour >= 12 && hour < 17:
		return "afternoon"
	case hour >= 17 && hour < 21:
		return "evening"
	default:
		return "night"
	}
}

// buildLocationSequence creates location flow string
func buildLocationSequence(episodes []*MicroEpisode) string {
	if len(episodes) == 0 {
		return ""
	}

	locs := make([]string, len(episodes))
	for i, ep := range episodes {
		locs[i] = ep.Location
	}
	return strings.Join(locs, " → ")
}

// calculateTotalDuration returns total duration in minutes
func calculateTotalDuration(episodes []*MicroEpisode) int {
	if len(episodes) == 0 {
		return 0
	}

	start := episodes[0].StartedAt
	end := episodes[len(episodes)-1].EndedAt

	if end == nil {
		now := time.Now()
		end = &now
	}

	return int(end.Sub(start).Minutes())
}

// calculateGaps returns gaps between episodes in hours
func calculateGaps(episodes []*MicroEpisode) []float64 {
	gaps := make([]float64, 0, len(episodes)-1)
	for i := 0; i < len(episodes)-1; i++ {
		if episodes[i].EndedAt != nil {
			gap := episodes[i+1].StartedAt.Sub(*episodes[i].EndedAt).Hours()
			gaps = append(gaps, gap)
		}
	}
	return gaps
}

// validateLLMResult applies domain safety checks
func validateLLMResult(
	output ConsolidationOutput,
	episodes []*MicroEpisode,
	maxGapMinutes int,
	minConfidence float64,
	logger *slog.Logger,
) bool {
	// Check confidence threshold
	if output.Confidence < minConfidence {
		logger.Info("LLM confidence too low",
			"confidence", output.Confidence,
			"min", minConfidence)
		return false
	}

	// Check for excessive gaps
	for i := 0; i < len(episodes)-1; i++ {
		if episodes[i].EndedAt != nil {
			gap := episodes[i+1].StartedAt.Sub(*episodes[i].EndedAt).Hours()
			maxGapHours := float64(maxGapMinutes) / 60.0
			if gap > maxGapHours {
				logger.Warn("LLM suggested merge but gap too large",
					"gap_hours", gap,
					"max_hours", maxGapHours)
				return false
			}
		}
	}

	// Check for sleep boundary crossing
	if crossesSleepBoundary(episodes) && output.ShouldMerge {
		logger.Warn("LLM suggested merge across sleep boundary - rejecting")
		return false
	}

	return output.ShouldMerge
}

// crossesSleepBoundary checks if episodes cross likely sleep period
func crossesSleepBoundary(episodes []*MicroEpisode) bool {
	for i := 0; i < len(episodes)-1; i++ {
		if episodes[i].EndedAt == nil {
			continue
		}

		endHour := episodes[i].EndedAt.Hour()
		startHour := episodes[i+1].StartedAt.Hour()

		// Crossing 00:00-06:00 window with gap > 4h
		if endHour >= 22 || endHour <= 2 {
			gap := episodes[i+1].StartedAt.Sub(*episodes[i].EndedAt).Hours()
			if gap > 4 && startHour >= 6 && startHour <= 10 {
				return true
			}
		}
	}
	return false
}

// createMacroFromLLM creates macro-episode from LLM analysis
func createMacroFromLLM(episodes []*MicroEpisode, output ConsolidationOutput, now time.Time) *MacroEpisode {
	patternType := "occupancy_transition"
	if output.PatternType != nil {
		patternType = *output.PatternType
	}

	// Collect unique locations
	locationMap := make(map[string]bool)
	for _, ep := range episodes {
		locationMap[ep.Location] = true
	}
	locations := make([]string, 0, len(locationMap))
	for loc := range locationMap {
		locations = append(locations, loc)
	}

	// Collect episode IDs
	microIDs := make([]uuid.UUID, len(episodes))
	for i, ep := range episodes {
		microIDs[i] = ep.ID
	}

	startTime := episodes[0].StartedAt
	endTime := episodes[len(episodes)-1].EndedAt
	if endTime == nil {
		endTime = &now
	}
	duration := int(endTime.Sub(startTime).Minutes())

	// Build summary
	summary := output.Reasoning
	if output.MacroName != "" {
		summary = fmt.Sprintf("%s: %s", output.MacroName, output.Reasoning)
	}

	// Build semantic tags
	tags := []string{patternType, "llm_consolidated"}
	tags = append(tags, locations...)
	tags = append(tags, categorizeTimeOfDay(startTime))

	if output.PatternType != nil {
		tags = append(tags, *output.PatternType)
	}

	// Context features with LLM metadata
	contextFeatures := map[string]interface{}{
		"llm_confidence":       output.Confidence,
		"llm_pattern_type":     output.PatternType,
		"llm_reasoning":        output.Reasoning,
		"consolidation_method": "llm",
		"location_count":       len(locations),
		"micro_episode_count":  len(episodes),
		"time_of_day":          categorizeTimeOfDay(startTime),
		"day_of_week":          startTime.Weekday().String(),
	}

	return &MacroEpisode{
		ID:              uuid.New(),
		PatternType:     patternType,
		StartTime:       startTime,
		EndTime:         *endTime,
		DurationMinutes: duration,
		Locations:       locations,
		MicroEpisodeIDs: microIDs,
		Summary:         summary,
		SemanticTags:    tags,
		ContextFeatures: contextFeatures,
		CreatedAt:       now,
	}
}
