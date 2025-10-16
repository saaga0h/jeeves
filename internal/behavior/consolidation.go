package behavior

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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

	// CRITICAL: Don't merge episodes longer than 6 hours - likely spurious
	ep1Duration := ep1.EndedAt.Sub(ep1.StartedAt).Hours()
	ep2Duration := ep2.EndedAt.Sub(ep2.StartedAt).Hours()

	if ep1Duration > 6 || ep2Duration > 6 {
		return false // Skip abnormally long episodes
	}

	// Calculate gap between episodes
	gapMinutes := ep2.StartedAt.Sub(*ep1.EndedAt).Minutes()

	if gapMinutes > float64(maxGapMinutes) {
		return false
	}

	// Don't merge if gap is negative (overlapping) or too small (< 1 min)
	if gapMinutes < 1 {
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

// consolidateMicroEpisodesRuleBased is a PURE FUNCTION that creates macro-episodes
// from micro-episodes using simple rule-based logic (same location, small gaps)
func consolidateMicroEpisodesRuleBased(episodes []*MicroEpisode, maxGapMinutes int, logger *slog.Logger) []*MacroEpisode {
	if len(episodes) == 0 {
		return nil
	}

	logger.Debug("Rule-based consolidation starting",
		"episodes", len(episodes),
		"max_gap_minutes", maxGapMinutes)

	var macros []*MacroEpisode

	// Group by location
	byLocation := make(map[string][]*MicroEpisode)
	for _, ep := range episodes {
		byLocation[ep.Location] = append(byLocation[ep.Location], ep)
	}

	logger.Debug("Episodes grouped by location",
		"locations", len(byLocation))

	// Process each location
	for location, locationEpisodes := range byLocation {
		logger.Debug("Processing location",
			"location", location,
			"episodes", len(locationEpisodes))

		if len(locationEpisodes) < 2 {
			logger.Debug("Skipping location - insufficient episodes",
				"location", location)
			continue
		}

		// Sort by start time
		sortEpisodesByStartTime(locationEpisodes)

		currentGroup := []*MicroEpisode{locationEpisodes[0]}
		groupsProcessed := 0

		for i := 1; i < len(locationEpisodes); i++ {
			prevEpisode := currentGroup[len(currentGroup)-1]
			currentEpisode := locationEpisodes[i]

			canMerge := shouldMergeEpisodes(prevEpisode, currentEpisode, maxGapMinutes)

			if canMerge {
				gap := currentEpisode.StartedAt.Sub(*prevEpisode.EndedAt).Minutes()
				logger.Debug("Merging episodes",
					"location", location,
					"prev_id", prevEpisode.ID,
					"current_id", currentEpisode.ID,
					"gap_minutes", fmt.Sprintf("%.1f", gap))
				currentGroup = append(currentGroup, currentEpisode)
			} else {
				// Save current group if it has multiple episodes
				if len(currentGroup) > 1 {
					macro := mergeMicroEpisodes(currentGroup)
					if macro != nil {
						logger.Debug("Created macro from group",
							"location", location,
							"micro_count", len(currentGroup),
							"duration_min", macro.DurationMinutes)
						macros = append(macros, macro)
						groupsProcessed++
					}
				} else {
					logger.Debug("Group too small, not consolidating",
						"location", location,
						"episode_id", currentGroup[0].ID)
				}

				// Start new group
				currentGroup = []*MicroEpisode{currentEpisode}
			}
		}

		// Handle last group
		if len(currentGroup) > 1 {
			macro := mergeMicroEpisodes(currentGroup)
			if macro != nil {
				logger.Debug("Created macro from final group",
					"location", location,
					"micro_count", len(currentGroup),
					"duration_min", macro.DurationMinutes)
				macros = append(macros, macro)
				groupsProcessed++
			}
		}

		logger.Debug("Location processing complete",
			"location", location,
			"groups_processed", groupsProcessed)
	}

	logger.Debug("Rule-based consolidation complete",
		"macros_created", len(macros))

	return macros
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
	if err := a.performConsolidation(ctx, sinceTime, trigger.Location); err != nil {
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
			now := a.timeManager.Now()
			sinceTime := now.Add(-time.Duration(a.cfg.ConsolidationLookbackHours) * time.Hour)

			a.logger.Info("Running periodic consolidation", "virtual_time", now)

			if err := a.performConsolidation(ctx, sinceTime, ""); err != nil {
				a.logger.Error("Periodic consolidation failed", "error", err)
			}

		case <-ctx.Done():
			a.logger.Info("Consolidation job stopping")
			return
		}
	}
}
