package patterns

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/saaga0h/jeeves-platform/internal/behavior/storage"
	"github.com/saaga0h/jeeves-platform/internal/behavior/types"
	"github.com/saaga0h/jeeves-platform/pkg/llm"
)

// PatternInterpreter uses LLM to interpret clusters as behavioral patterns
type PatternInterpreter struct {
	storage *storage.AnchorStorage
	llm     llm.Client
	logger  *slog.Logger
}

// NewPatternInterpreter creates a new pattern interpreter
func NewPatternInterpreter(
	storage *storage.AnchorStorage,
	llmClient llm.Client,
	logger *slog.Logger,
) *PatternInterpreter {
	return &PatternInterpreter{
		storage: storage,
		llm:     llmClient,
		logger:  logger,
	}
}

// InterpretCluster asks LLM to identify behavioral pattern from cluster
func (p *PatternInterpreter) InterpretCluster(
	ctx context.Context,
	anchorIDs []uuid.UUID,
) (*types.BehavioralPattern, error) {

	// Load anchor details
	anchors, err := p.storage.GetAnchorsByIDs(ctx, anchorIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to load anchors: %w", err)
	}

	if len(anchors) == 0 {
		return nil, fmt.Errorf("no anchors found for interpretation")
	}

	// Build prompt
	prompt := p.buildInterpretationPrompt(anchors)

	// Ask LLM
	req := llm.GenerateRequest{
		Prompt: prompt,
		Format: "json", // Request JSON response
	}

	response, err := p.llm.Generate(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("LLM request failed: %w", err)
	}

	// Parse response
	var llmResult struct {
		PatternType            string   `json:"pattern_type"`
		Name                   string   `json:"name"`
		Confidence             float64  `json:"confidence"`
		TypicalDurationMinutes *int     `json:"typical_duration_minutes"`
		KeyCharacteristics     []string `json:"key_characteristics"`
	}

	if err := json.Unmarshal([]byte(response.Response), &llmResult); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response: %w", err)
	}

	// Create pattern
	pattern := &types.BehavioralPattern{
		ID:                     uuid.New(),
		Name:                   llmResult.Name,
		PatternType:            llmResult.PatternType,
		Weight:                 0.1, // initial weight
		Observations:           len(anchorIDs),
		TypicalDurationMinutes: llmResult.TypicalDurationMinutes,
		Context:                p.extractCommonContext(anchors),
		FirstSeen:              p.findEarliestTimestamp(anchors),
		LastSeen:               p.findLatestTimestamp(anchors),
		CreatedAt:              time.Now(),
		UpdatedAt:              time.Now(),
	}

	p.logger.Info("Pattern interpreted",
		"pattern_id", pattern.ID,
		"name", pattern.Name,
		"pattern_type", pattern.PatternType,
		"anchors", len(anchorIDs),
		"confidence", llmResult.Confidence)

	return pattern, nil
}

func (p *PatternInterpreter) buildInterpretationPrompt(anchors []*types.SemanticAnchor) string {
	// Build anchor summary
	anchorSummary := ""
	for i, anchor := range anchors {
		if i < 10 { // Limit to first 10 for prompt size
			anchorSummary += fmt.Sprintf("\nAnchor %d: %s @ %s (%s, %s, %s)",
				i+1,
				anchor.Location,
				anchor.Timestamp.Format("15:04"),
				getContextValue(anchor.Context, "time_of_day"),
				getContextValue(anchor.Context, "day_type"),
				getContextValue(anchor.Context, "season"))
		}
	}

	if len(anchors) > 10 {
		anchorSummary += fmt.Sprintf("\n... and %d more anchors", len(anchors)-10)
	}

	// Extract common patterns
	locations := p.extractUniqueValues(anchors, "location")
	timesOfDay := p.extractUniqueContextValues(anchors, "time_of_day")
	dayTypes := p.extractUniqueContextValues(anchors, "day_type")

	return fmt.Sprintf(`Analyze this cluster of behavioral anchors and identify the pattern they represent.

Anchors in cluster (%d total):%s

Common characteristics:
- Locations: %v
- Times of day: %v
- Day types: %v

These anchors were grouped together because they have small semantic distance in behavioral space.

What behavioral pattern does this cluster represent?

Consider:
- Is this a routine (morning_routine, evening_wind_down)?
- Is this an activity type (meal_preparation, work_session, leisure_time)?
- Is this a transition (waking_up, going_to_bed)?
- Are there multiple interpretations (concurrent activities)?

Respond with ONLY valid JSON (no markdown):
{
  "pattern_type": "morning_routine" | "meal_preparation" | "work_session" | "leisure" | "transition" | etc,
  "name": "Human-readable pattern name",
  "confidence": 0.0-1.0,
  "typical_duration_minutes": estimated_duration or null,
  "key_characteristics": ["characteristic1", "characteristic2"]
}`,
		len(anchors),
		anchorSummary,
		locations,
		timesOfDay,
		dayTypes)
}

func (p *PatternInterpreter) extractUniqueValues(anchors []*types.SemanticAnchor, field string) []string {
	seen := make(map[string]bool)
	var unique []string

	for _, anchor := range anchors {
		var value string
		switch field {
		case "location":
			value = anchor.Location
		}

		if !seen[value] {
			seen[value] = true
			unique = append(unique, value)
		}
	}

	return unique
}

func (p *PatternInterpreter) extractUniqueContextValues(anchors []*types.SemanticAnchor, key string) []string {
	seen := make(map[string]bool)
	var unique []string

	for _, anchor := range anchors {
		if value, ok := anchor.Context[key].(string); ok {
			if !seen[value] {
				seen[value] = true
				unique = append(unique, value)
			}
		}
	}

	return unique
}

func (p *PatternInterpreter) extractCommonContext(anchors []*types.SemanticAnchor) map[string]interface{} {
	// Find most common context values
	context := make(map[string]interface{})

	// Most common time of day
	timeOfDay := p.mostCommon(anchors, "time_of_day")
	if timeOfDay != "" {
		context["typical_time_of_day"] = timeOfDay
	}

	// Most common day type
	dayType := p.mostCommon(anchors, "day_type")
	if dayType != "" {
		context["typical_day_type"] = dayType
	}

	return context
}

func (p *PatternInterpreter) mostCommon(anchors []*types.SemanticAnchor, key string) string {
	counts := make(map[string]int)

	for _, anchor := range anchors {
		if value, ok := anchor.Context[key].(string); ok {
			counts[value]++
		}
	}

	var maxValue string
	maxCount := 0
	for value, count := range counts {
		if count > maxCount {
			maxCount = count
			maxValue = value
		}
	}

	return maxValue
}

func (p *PatternInterpreter) findEarliestTimestamp(anchors []*types.SemanticAnchor) time.Time {
	if len(anchors) == 0 {
		return time.Now()
	}

	earliest := anchors[0].Timestamp
	for _, anchor := range anchors[1:] {
		if anchor.Timestamp.Before(earliest) {
			earliest = anchor.Timestamp
		}
	}

	return earliest
}

func (p *PatternInterpreter) findLatestTimestamp(anchors []*types.SemanticAnchor) time.Time {
	if len(anchors) == 0 {
		return time.Now()
	}

	latest := anchors[0].Timestamp
	for _, anchor := range anchors[1:] {
		if anchor.Timestamp.After(latest) {
			latest = anchor.Timestamp
		}
	}

	return latest
}

// getContextValue safely extracts string value from context map
func getContextValue(context map[string]interface{}, key string) string {
	if val, ok := context[key].(string); ok {
		return val
	}
	return "unknown"
}
