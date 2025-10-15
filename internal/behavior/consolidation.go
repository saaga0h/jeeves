package behavior

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/saaga0h/jeeves-platform/pkg/mqtt"
)

// shouldMergeEpisodes determines if two episodes can be merged
func shouldMergeEpisodes(ep1, ep2 *MicroEpisode, maxGapMinutes int) bool {
	// Must be same location
	if ep1.Location != ep2.Location {
		return false
	}

	// First episode must be closed
	if ep1.EndedAt == nil {
		return false
	}

	// Calculate gap between episodes
	gapMinutes := ep2.StartedAt.Sub(*ep1.EndedAt).Minutes()

	if gapMinutes > float64(maxGapMinutes) {
		return false
	}

	// Same trigger type (for now - can be more sophisticated later)
	if ep1.TriggerType != ep2.TriggerType {
		return false
	}

	return true
}

// mergeMicroEpisodes creates a macro-episode from multiple micro-episodes
func mergeMicroEpisodes(episodes []*MicroEpisode) *MacroEpisode {
	if len(episodes) == 0 {
		return nil
	}

	startTime := episodes[0].StartedAt
	endTime := episodes[len(episodes)-1].EndedAt
	if endTime == nil {
		endTime = &time.Time{}
		*endTime = time.Now()
	}

	durationMinutes := int(endTime.Sub(startTime).Minutes())

	// Collect unique locations
	locationMap := make(map[string]bool)
	for _, ep := range episodes {
		locationMap[ep.Location] = true
	}
	locations := make([]string, 0, len(locationMap))
	for loc := range locationMap {
		locations = append(locations, loc)
	}

	// Pattern type (simplified - use first episode's trigger)
	patternType := episodes[0].TriggerType

	// Collect micro-episode IDs
	microIDs := make([]uuid.UUID, len(episodes))
	for i, ep := range episodes {
		microIDs[i] = ep.ID
	}

	// Count manual actions (from JSONB field)
	manualActionCount := 0
	for _, ep := range episodes {
		manualActionCount += len(ep.ManualActions)
	}

	// Generate summary
	summary := fmt.Sprintf("%s session at %s for %d minutes with %d manual adjustments",
		patternType, locations[0], durationMinutes, manualActionCount)

	// Semantic tags
	tags := []string{patternType}
	tags = append(tags, locations...)
	if durationMinutes > 60 {
		tags = append(tags, "extended_session")
	} else {
		tags = append(tags, "short_session")
	}
	if manualActionCount > 0 {
		tags = append(tags, "with_manual_adjustments")
	} else {
		tags = append(tags, "automated")
	}

	contextFeatures := map[string]interface{}{
		"manual_action_count": manualActionCount,
		"location_count":      len(locations),
		"micro_episode_count": len(episodes),
	}

	return &MacroEpisode{
		ID:              uuid.New(),
		PatternType:     patternType,
		StartTime:       startTime,
		EndTime:         *endTime,
		DurationMinutes: durationMinutes,
		Locations:       locations,
		MicroEpisodeIDs: microIDs,
		Summary:         summary,
		SemanticTags:    tags,
		ContextFeatures: contextFeatures,
		CreatedAt:       time.Now(),
	}
}

// consolidateMicroEpisodes performs the consolidation process
func (a *Agent) consolidateMicroEpisodes(ctx context.Context, sinceTime time.Time, location string) error {
	a.logger.Info("Starting consolidation",
		"since", sinceTime,
		"location", location)

	// Get unconsolidated episodes
	episodes, err := a.getUnconsolidatedEpisodes(ctx, sinceTime, location)
	if err != nil {
		return fmt.Errorf("failed to get unconsolidated episodes: %w", err)
	}

	a.logger.Info("Found unconsolidated episodes", "count", len(episodes))

	if len(episodes) == 0 {
		a.logger.Info("No episodes to consolidate")
		return nil
	}

	// Group by location
	byLocation := make(map[string][]*MicroEpisode)
	for _, ep := range episodes {
		byLocation[ep.Location] = append(byLocation[ep.Location], ep)
	}

	macroEpisodesCreated := 0
	maxGapMinutes := a.cfg.ConsolidationMaxGapMinutes

	// Process each location
	for loc, locationEpisodes := range byLocation {
		a.logger.Debug("Processing location", "location", loc, "episodes", len(locationEpisodes))

		// Sort by start time
		sortEpisodesByStartTime(locationEpisodes)

		currentGroup := []*MicroEpisode{locationEpisodes[0]}

		for i := 1; i < len(locationEpisodes); i++ {
			prevEpisode := locationEpisodes[i-1]
			currentEpisode := locationEpisodes[i]

			if shouldMergeEpisodes(prevEpisode, currentEpisode, maxGapMinutes) {
				currentGroup = append(currentGroup, currentEpisode)
			} else {
				// Save current group if it has multiple episodes
				if len(currentGroup) > 1 {
					macroEpisode := mergeMicroEpisodes(currentGroup)
					if err := a.createMacroEpisode(ctx, macroEpisode); err != nil {
						a.logger.Error("Failed to create macro-episode", "error", err)
					} else {
						macroEpisodesCreated++
					}
				}

				// Start new group
				currentGroup = []*MicroEpisode{currentEpisode}
			}
		}

		// Handle last group
		if len(currentGroup) > 1 {
			macroEpisode := mergeMicroEpisodes(currentGroup)
			if err := a.createMacroEpisode(ctx, macroEpisode); err != nil {
				a.logger.Error("Failed to create macro-episode", "error", err)
			} else {
				macroEpisodesCreated++
			}
		}
	}

	a.logger.Info("Consolidation completed",
		"micro_episodes_processed", len(episodes),
		"macro_episodes_created", macroEpisodesCreated)

	// Publish consolidation result
	a.publishConsolidationResult(macroEpisodesCreated, len(episodes))

	return nil
}

// Helper: sort episodes by start time
func sortEpisodesByStartTime(episodes []*MicroEpisode) {
	sort.Slice(episodes, func(i, j int) bool {
		return episodes[i].StartedAt.Before(episodes[j].StartedAt)
	})
}

// publishConsolidationResult publishes consolidation metrics
func (a *Agent) publishConsolidationResult(macroCreated, microProcessed int) {
	topic := "automation/behavior/consolidation/completed"

	payload := map[string]interface{}{
		"timestamp":                a.timeManager.Now().Format(time.RFC3339),
		"macro_episodes_created":   macroCreated,
		"micro_episodes_processed": microProcessed,
	}

	payloadBytes, _ := json.Marshal(payload)
	a.mqtt.Publish(topic, 0, false, payloadBytes)
}

// handleConsolidationTrigger handles manual consolidation requests
func (a *Agent) handleConsolidationTrigger(msg mqtt.Message) {
	var trigger struct {
		Action        string `json:"action"`
		LookbackHours int    `json:"lookback_hours"`
		Location      string `json:"location"`
	}

	if err := json.Unmarshal(msg.Payload(), &trigger); err != nil {
		a.logger.Error("Failed to parse consolidation trigger", "error", err)
		return
	}

	if trigger.Action != "consolidate" {
		a.logger.Warn("Unknown consolidation action", "action", trigger.Action)
		return
	}

	lookbackHours := trigger.LookbackHours
	if lookbackHours == 0 {
		lookbackHours = a.cfg.ConsolidationLookbackHours
	}

	now := a.timeManager.Now()
	sinceTime := now.Add(-time.Duration(lookbackHours) * time.Hour)

	a.logger.Info("Manual consolidation triggered",
		"lookback_hours", lookbackHours,
		"location", trigger.Location,
		"virtual_time", now)

	ctx := context.Background()
	if err := a.consolidateMicroEpisodes(ctx, sinceTime, trigger.Location); err != nil {
		a.logger.Error("Manual consolidation failed", "error", err)
	}
}

// runConsolidationJob runs periodic consolidation in the background
func (a *Agent) runConsolidationJob(ctx context.Context) {
	interval := time.Duration(a.cfg.ConsolidationIntervalHours) * time.Hour

	a.logger.Info("Starting consolidation job",
		"interval", interval,
		"lookback", a.cfg.ConsolidationLookbackHours)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			now := a.timeManager.Now() // Virtual time aware!
			sinceTime := now.Add(-time.Duration(a.cfg.ConsolidationLookbackHours) * time.Hour)

			a.logger.Info("Running periodic consolidation", "virtual_time", now)

			if err := a.consolidateMicroEpisodes(ctx, sinceTime, ""); err != nil {
				a.logger.Error("Periodic consolidation failed", "error", err)
			}

		case <-ctx.Done():
			a.logger.Info("Consolidation job stopping")
			return
		}
	}
}
