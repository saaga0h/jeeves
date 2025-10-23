package anchor

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	behaviorcontext "github.com/saaga0h/jeeves-platform/internal/behavior/context"
	"github.com/saaga0h/jeeves-platform/internal/behavior/embedding"
	"github.com/saaga0h/jeeves-platform/internal/behavior/storage"
	"github.com/saaga0h/jeeves-platform/internal/behavior/types"
)

// AnchorCreator creates semantic anchors from observed activity events.
type AnchorCreator struct {
	storage         *storage.AnchorStorage
	contextGatherer *behaviorcontext.ContextGatherer
	logger          *slog.Logger

	// Track last anchor per location for linking
	lastAnchors    map[string]uuid.UUID
	lastAnchorsMux sync.RWMutex
}

// NewAnchorCreator creates a new anchor creator instance.
func NewAnchorCreator(
	storage *storage.AnchorStorage,
	contextGatherer *behaviorcontext.ContextGatherer,
	logger *slog.Logger,
) *AnchorCreator {
	return &AnchorCreator{
		storage:         storage,
		contextGatherer: contextGatherer,
		logger:          logger,
		lastAnchors:     make(map[string]uuid.UUID),
	}
}

// CreateAnchor creates a semantic anchor from observed activity signals.
// This is the main entry point for anchor creation.
func (c *AnchorCreator) CreateAnchor(
	ctx context.Context,
	location string,
	timestamp time.Time,
	signals []types.ActivitySignal,
) (*types.SemanticAnchor, error) {

	// Gather semantic context dimensions
	semanticContext, err := c.contextGatherer.GatherContext(ctx, location, timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to gather context: %w", err)
	}

	// Compute semantic embedding (128-dimensional vector)
	embeddingVec, err := embedding.ComputeSemanticEmbedding(
		location,
		timestamp,
		semanticContext,
		signals,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to compute embedding: %w", err)
	}

	// Create anchor structure
	anchor := &types.SemanticAnchor{
		ID:                uuid.New(),
		Timestamp:         timestamp,
		Location:          location,
		SemanticEmbedding: embeddingVec,
		Context:           semanticContext,
		Signals:           signals,
		CreatedAt:         time.Now(),
	}

	// Link to previous anchor in this location (creates graph structure)
	c.lastAnchorsMux.Lock()
	if lastID, exists := c.lastAnchors[location]; exists {
		anchor.PrecedingAnchorID = &lastID
		// Note: We could update the previous anchor's FollowingAnchorID here,
		// but that would require an additional database UPDATE.
		// For now, we can traverse the graph using PrecedingAnchorID.
	}
	c.lastAnchors[location] = anchor.ID
	c.lastAnchorsMux.Unlock()

	// Store anchor in database
	if err := c.storage.CreateAnchor(ctx, anchor); err != nil {
		return nil, fmt.Errorf("failed to store anchor: %w", err)
	}

	c.logger.Info("Created semantic anchor",
		"id", anchor.ID,
		"location", location,
		"timestamp", timestamp.Format(time.RFC3339),
		"signals", len(signals),
		"context_keys", len(semanticContext))

	// Detect and store multiple interpretations (parallel activities)
	interpretations := c.detectInterpretations(anchor)
	if len(interpretations) > 0 {
		c.logger.Info("Detected activity interpretations",
			"anchor_id", anchor.ID,
			"count", len(interpretations))

		for _, interp := range interpretations {
			if err := c.storage.CreateInterpretation(ctx, &interp); err != nil {
				c.logger.Error("Failed to store interpretation",
					"anchor_id", anchor.ID,
					"activity", interp.ActivityType,
					"error", err)
				// Don't fail the whole operation, just log
			}
		}
	}

	return anchor, nil
}

// detectInterpretations identifies possible concurrent activities at this anchor.
// This enables detection of parallel activities (e.g., watching TV while eating).
func (c *AnchorCreator) detectInterpretations(anchor *types.SemanticAnchor) []types.ActivityInterpretation {
	var interpretations []types.ActivityInterpretation

	// Detect media activity
	for _, signal := range anchor.Signals {
		if signal.Type == "media" {
			if state, ok := signal.Value["state"].(string); ok && state == "playing" {
				evidence := []string{"media_playing"}

				// Add media type to evidence if available
				if mediaType, ok := signal.Value["type"].(string); ok {
					evidence = append(evidence, fmt.Sprintf("media_type:%s", mediaType))
				}

				interpretations = append(interpretations, types.ActivityInterpretation{
					AnchorID:     anchor.ID,
					ActivityType: "watching_media",
					Confidence:   0.9,
					Evidence:     evidence,
				})
			}
		}
	}

	// Detect reading activity (bright manual lighting, stationary)
	if lighting, ok := anchor.Context["lighting_state"].(map[string]interface{}); ok {
		if brightness, ok := lighting["brightness"].(float64); ok && brightness > 70 {
			if source, ok := lighting["source"].(string); ok && source == "manual" {
				// Check for low motion (reading = stationary)
				motionCount := 0
				for _, signal := range anchor.Signals {
					if signal.Type == "motion" {
						motionCount++
					}
				}

				confidence := 0.7
				if motionCount <= 1 {
					confidence = 0.8 // Higher confidence if stationary
				}

				interpretations = append(interpretations, types.ActivityInterpretation{
					AnchorID:     anchor.ID,
					ActivityType: "reading",
					Confidence:   confidence,
					Evidence:     []string{"bright_manual_light", fmt.Sprintf("motion_count:%d", motionCount)},
				})
			}
		}
	}

	// Detect cooking activity (frequent motion in kitchen)
	if anchor.Location == "kitchen" {
		motionCount := 0
		totalConfidence := 0.0

		for _, signal := range anchor.Signals {
			if signal.Type == "motion" {
				motionCount++
				totalConfidence += signal.Confidence
			}
		}

		if motionCount >= 3 {
			avgConfidence := totalConfidence / float64(motionCount)
			interpretations = append(interpretations, types.ActivityInterpretation{
				AnchorID:     anchor.ID,
				ActivityType: "cooking",
				Confidence:   min(avgConfidence, 0.9),
				Evidence:     []string{"frequent_motion", "kitchen_location", fmt.Sprintf("motion_events:%d", motionCount)},
			})
		}
	}

	// Detect sleeping (bedroom at night, minimal motion)
	if anchor.Location == "bedroom" {
		if timeOfDay, ok := anchor.Context["time_of_day"].(string); ok {
			if timeOfDay == "night" || anchor.Context["household_mode"].(string) == "sleeping" {
				motionCount := 0
				for _, signal := range anchor.Signals {
					if signal.Type == "motion" {
						motionCount++
					}
				}

				// Low motion at night in bedroom = likely sleeping
				if motionCount <= 1 {
					interpretations = append(interpretations, types.ActivityInterpretation{
						AnchorID:     anchor.ID,
						ActivityType: "sleeping",
						Confidence:   0.85,
						Evidence:     []string{"bedroom_location", "night_time", "low_motion"},
					})
				}
			}
		}
	}

	// Detect dining (dining_room with lighting, moderate motion)
	if anchor.Location == "dining_room" {
		hasLighting := false
		for _, signal := range anchor.Signals {
			if signal.Type == "lighting" {
				if state, ok := signal.Value["state"].(string); ok && state == "on" {
					hasLighting = true
					break
				}
			}
		}

		if hasLighting {
			// Check time of day for meal times
			timeOfDay := anchor.Context["time_of_day"].(string)
			confidence := 0.7

			// Higher confidence during typical meal times
			if timeOfDay == "morning" || timeOfDay == "evening" {
				confidence = 0.85
			}

			interpretations = append(interpretations, types.ActivityInterpretation{
				AnchorID:     anchor.ID,
				ActivityType: "dining",
				Confidence:   confidence,
				Evidence:     []string{"dining_room_location", "lighting_on", fmt.Sprintf("time:%s", timeOfDay)},
			})
		}
	}

	// Detect working (office/desk location during active hours)
	if anchor.Location == "office" || anchor.Location == "desk" || anchor.Location == "study" {
		if householdMode, ok := anchor.Context["household_mode"].(string); ok && householdMode == "active" {
			// Check for sustained presence (multiple signals over time)
			if len(anchor.Signals) >= 2 {
				interpretations = append(interpretations, types.ActivityInterpretation{
					AnchorID:     anchor.ID,
					ActivityType: "working",
					Confidence:   0.75,
					Evidence:     []string{"work_location", "active_hours", "sustained_presence"},
				})
			}
		}
	}

	return interpretations
}

// CreateAnchorFromEvent is a convenience method for creating anchors from single events.
// This is useful for quick integration with existing event handlers.
func (c *AnchorCreator) CreateAnchorFromEvent(
	ctx context.Context,
	location string,
	eventType string,
	eventData map[string]interface{},
	timestamp time.Time,
) (*types.SemanticAnchor, error) {

	// Convert event to signal
	signal := types.ActivitySignal{
		Type:       eventType,
		Value:      eventData,
		Confidence: 0.8, // Default confidence
		Timestamp:  timestamp,
	}

	// Extract confidence if provided
	if conf, ok := eventData["confidence"].(float64); ok {
		signal.Confidence = conf
	}

	return c.CreateAnchor(ctx, location, timestamp, []types.ActivitySignal{signal})
}

// min returns the smaller of two float64 values
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
