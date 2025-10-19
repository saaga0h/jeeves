package behavior

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// BehavioralVector represents a detected sequence of location transitions
type BehavioralVector struct {
	ID              uuid.UUID
	Timestamp       time.Time
	Sequence        []VectorNode
	Context         VectorContext
	EdgeStats       map[string]EdgeStatistics
	MicroEpisodeIDs []uuid.UUID
	ScenarioName    string // For test scenarios
	QualityScore    float64
	CreatedAt       time.Time
}

// VectorNode represents one location in the sequence
type VectorNode struct {
	Location    string   `json:"location"`
	DurationSec int      `json:"duration_sec"`
	GapToNext   int      `json:"gap_to_next"` // Gap to next node in seconds
	Sensors     []string `json:"sensors"`     // Which sensors were active
}

// VectorContext provides temporal/spatial metadata
type VectorContext struct {
	TimeOfDay        string  `json:"time_of_day"`
	DayOfWeek        string  `json:"day_of_week"`
	TotalDurationSec int     `json:"total_duration_sec"`
	LocationCount    int     `json:"location_count"`
	TransitionCount  int     `json:"transition_count"`
	VirtualTime      *string `json:"virtual_time,omitempty"` // For test scenarios
}

// EdgeStatistics captures transition patterns between locations
type EdgeStatistics struct {
	GapSeconds             int     `json:"gap_seconds"`
	Frequency              float64 `json:"frequency,omitempty"`      // Historical frequency (0-1)
	TypicalGap             float64 `json:"typical_gap,omitempty"`    // Average gap in seconds
	TemporalProximityScore float64 `json:"temporal_proximity_score"` // Based on gap size
}

// ===================================================================
// Vector Detection Logic (Pure Functions)
// ===================================================================

// detectVectors finds behavioral vectors from a sequence of micro-episodes
// A vector is a sequence of 2+ location transitions with tight temporal coupling
func detectVectors(episodes []*MicroEpisode, maxGapSeconds int, logger *slog.Logger) []*BehavioralVector {
	if len(episodes) < 2 {
		logger.Debug("Not enough episodes for vector detection", "count", len(episodes))
		return nil
	}

	logger.Info("Vector detection starting",
		"episodes", len(episodes),
		"max_gap_seconds", maxGapSeconds)

	var vectors []*BehavioralVector

	// Sliding window approach: look for sequences of tightly coupled episodes
	i := 0
	vectorsFound := 0

	for i < len(episodes) {
		// Start a potential vector
		currentSequence := []*MicroEpisode{episodes[i]}

		logger.Debug("Starting vector detection from episode",
			"index", i,
			"location", episodes[i].Location,
			"started_at", episodes[i].StartedAt.Format("15:04:05"))

		// Try to extend the sequence
		j := i + 1
		for j < len(episodes) {
			prevEp := currentSequence[len(currentSequence)-1]
			currentEp := episodes[j]

			// Check if this episode can extend the vector
			if canExtendVector(prevEp, currentEp, maxGapSeconds, logger) {
				currentSequence = append(currentSequence, currentEp)
				logger.Debug("Extended vector",
					"from_location", prevEp.Location,
					"to_location", currentEp.Location,
					"gap_sec", calculateGapSeconds(prevEp, currentEp))
				j++
			} else {
				// Stop extending this vector
				logger.Debug("Cannot extend vector",
					"from_location", prevEp.Location,
					"to_location", currentEp.Location,
					"reason", "gap too large or invalid")
				break
			}
		}

		// If we have 2+ episodes in sequence, create a vector
		if len(currentSequence) >= 2 {
			vector := buildVector(currentSequence, logger)
			if vector != nil {
				vectors = append(vectors, vector)
				vectorsFound++

				logger.Info("Vector detected",
					"vector_id", vector.ID,
					"locations", buildLocationSequenceString(currentSequence),
					"duration_min", vector.Context.TotalDurationSec/60,
					"transitions", vector.Context.TransitionCount,
					"quality_score", vector.QualityScore)
			}

			// Continue from after this vector
			i = j
		} else {
			// Single episode, move to next
			logger.Debug("Single episode, not a vector",
				"location", episodes[i].Location)
			i++
		}
	}

	logger.Info("Vector detection complete",
		"vectors_found", vectorsFound)

	return vectors
}

// canExtendVector checks if nextEp can extend the vector from prevEp
func canExtendVector(prevEp, nextEp *MicroEpisode, maxGapSeconds int, logger *slog.Logger) bool {
	// Previous episode must be closed
	if prevEp.EndedAt == nil {
		logger.Debug("Previous episode not closed")
		return false
	}

	// Calculate gap
	gapSeconds := calculateGapSeconds(prevEp, nextEp)

	// Gap must be within threshold
	if gapSeconds < 0 {
		logger.Debug("Negative gap (overlapping episodes)", "gap_sec", gapSeconds)
		return false
	}

	if gapSeconds > maxGapSeconds {
		logger.Debug("Gap too large", "gap_sec", gapSeconds, "max", maxGapSeconds)
		return false
	}

	// Don't create vectors from very long episodes (likely spurious)
	prevDuration := prevEp.EndedAt.Sub(prevEp.StartedAt).Hours()
	nextDuration := time.Duration(0)
	if nextEp.EndedAt != nil {
		nextDuration = nextEp.EndedAt.Sub(nextEp.StartedAt)
	}

	if prevDuration > 6 || nextDuration.Hours() > 6 {
		logger.Debug("Episode too long",
			"prev_duration_hours", prevDuration,
			"next_duration_hours", nextDuration.Hours())
		return false
	}

	return true
}

// calculateGapSeconds returns gap between episodes in seconds
func calculateGapSeconds(prevEp, nextEp *MicroEpisode) int {
	if prevEp.EndedAt == nil {
		return -1
	}
	gap := nextEp.StartedAt.Sub(*prevEp.EndedAt)
	return int(gap.Seconds())
}

// buildVector constructs a BehavioralVector from episode sequence
func buildVector(episodes []*MicroEpisode, logger *slog.Logger) *BehavioralVector {
	if len(episodes) < 2 {
		return nil
	}

	// Build sequence nodes
	nodes := make([]VectorNode, len(episodes))
	for i, ep := range episodes {
		durationSec := 0
		if ep.EndedAt != nil {
			durationSec = int(ep.EndedAt.Sub(ep.StartedAt).Seconds())
		}

		gapToNext := 0
		if i < len(episodes)-1 {
			gapToNext = calculateGapSeconds(ep, episodes[i+1])
		}

		// Infer sensors from episode data (simplified)
		sensors := inferSensors(ep)

		nodes[i] = VectorNode{
			Location:    ep.Location,
			DurationSec: durationSec,
			GapToNext:   gapToNext,
			Sensors:     sensors,
		}
	}

	// Build context
	startTime := episodes[0].StartedAt
	endTime := episodes[len(episodes)-1].EndedAt
	if endTime == nil {
		now := time.Now()
		endTime = &now
	}
	totalDuration := int(endTime.Sub(startTime).Seconds())

	context := VectorContext{
		TimeOfDay:        categorizeTimeOfDay(startTime),
		DayOfWeek:        startTime.Weekday().String(),
		TotalDurationSec: totalDuration,
		LocationCount:    len(episodes),
		TransitionCount:  len(episodes) - 1,
	}

	// Build edge statistics
	edgeStats := buildEdgeStats(episodes, logger)

	// Calculate quality score
	qualityScore := calculateVectorQuality(nodes, edgeStats)

	// Collect episode IDs
	episodeIDs := make([]uuid.UUID, len(episodes))
	for i, ep := range episodes {
		episodeIDs[i] = ep.ID
	}

	vector := &BehavioralVector{
		ID:              uuid.New(),
		Timestamp:       startTime,
		Sequence:        nodes,
		Context:         context,
		EdgeStats:       edgeStats,
		MicroEpisodeIDs: episodeIDs,
		QualityScore:    qualityScore,
	}

	logger.Debug("Vector built",
		"id", vector.ID,
		"nodes", len(nodes),
		"quality", qualityScore)

	return vector
}

// buildEdgeStats creates edge statistics for transitions
func buildEdgeStats(episodes []*MicroEpisode, logger *slog.Logger) map[string]EdgeStatistics {
	stats := make(map[string]EdgeStatistics)

	for i := 0; i < len(episodes)-1; i++ {
		fromLoc := episodes[i].Location
		toLoc := episodes[i+1].Location
		edgeKey := fmt.Sprintf("%s->%s", fromLoc, toLoc)

		gapSec := calculateGapSeconds(episodes[i], episodes[i+1])

		// Temporal proximity: closer = higher score (exponential decay)
		// 0 seconds = 1.0, 60 seconds = ~0.5, 300 seconds = ~0.2
		proximityScore := 1.0
		if gapSec > 0 {
			proximityScore = 1.0 / (1.0 + float64(gapSec)/60.0)
		}

		stats[edgeKey] = EdgeStatistics{
			GapSeconds:             gapSec,
			TemporalProximityScore: proximityScore,
			// Frequency and TypicalGap will be populated from historical data later
		}

		logger.Debug("Edge statistics",
			"edge", edgeKey,
			"gap_sec", gapSec,
			"proximity_score", proximityScore)
	}

	return stats
}

// calculateVectorQuality scores the vector based on transition tightness
// Higher score = tighter transitions, more likely a real behavioral pattern
func calculateVectorQuality(nodes []VectorNode, edgeStats map[string]EdgeStatistics) float64 {
	if len(nodes) < 2 {
		return 0.0
	}

	// Average the temporal proximity scores
	totalScore := 0.0
	edgeCount := 0

	for _, stats := range edgeStats {
		totalScore += stats.TemporalProximityScore
		edgeCount++
	}

	if edgeCount == 0 {
		return 0.0
	}

	avgScore := totalScore / float64(edgeCount)

	// Bonus for longer sequences (more transitions = more confident pattern)
	lengthBonus := 1.0
	if len(nodes) >= 3 {
		lengthBonus = 1.1
	}
	if len(nodes) >= 4 {
		lengthBonus = 1.2
	}

	quality := avgScore * lengthBonus
	if quality > 1.0 {
		quality = 1.0
	}

	return quality
}

// inferSensors determines which sensors were active for an episode
func inferSensors(ep *MicroEpisode) []string {
	sensors := []string{}

	// Based on trigger type, infer primary sensor
	switch ep.TriggerType {
	case "occupancy_transition":
		sensors = append(sensors, "motion")
	case "manual_lighting":
		sensors = append(sensors, "lighting")
	default:
		// Default to motion for backwards compatibility
		sensors = append(sensors, "motion")
	}

	// Check for manual actions (indicates additional lighting interactions)
	// Only add "lighting" if not already the primary sensor
	if len(ep.ManualActions) > 0 && ep.TriggerType != "manual_lighting" {
		sensors = append(sensors, "lighting")
	}

	// Could add more sensor inference logic here
	// e.g., media interactions, temperature changes, etc.

	return sensors
}

// buildLocationSequenceString creates a readable sequence string
func buildLocationSequenceString(episodes []*MicroEpisode) string {
	if len(episodes) == 0 {
		return ""
	}

	result := episodes[0].Location
	for i := 1; i < len(episodes); i++ {
		result += " → " + episodes[i].Location
	}
	return result
}

// ===================================================================
// Vector Enrichment (can be called later to add historical context)
// ===================================================================

// enrichVectorWithHistory updates vector with historical edge frequency data
// This would query historical vectors from database and compute statistics
func enrichVectorWithHistory(vector *BehavioralVector, historicalEdges map[string]EdgeStatistics) {
	for edgeKey, stats := range vector.EdgeStats {
		if historical, exists := historicalEdges[edgeKey]; exists {
			stats.Frequency = historical.Frequency
			stats.TypicalGap = historical.TypicalGap
			vector.EdgeStats[edgeKey] = stats
		}
	}

	// Recalculate quality score with historical data
	// Could weight frequency into the score here
}
