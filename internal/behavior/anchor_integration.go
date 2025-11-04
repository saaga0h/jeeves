package behavior

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/saaga0h/jeeves-platform/internal/behavior/anchor"
	behaviorcontext "github.com/saaga0h/jeeves-platform/internal/behavior/context"
	"github.com/saaga0h/jeeves-platform/internal/behavior/embedding"
	"github.com/saaga0h/jeeves-platform/internal/behavior/storage"
	"github.com/saaga0h/jeeves-platform/internal/behavior/types"
	"github.com/saaga0h/jeeves-platform/pkg/config"
	"github.com/saaga0h/jeeves-platform/pkg/llm"
)

// initializeAnchorCreator sets up the semantic anchor creation system.
// This should be called during agent initialization.
func (a *Agent) initializeAnchorCreator(cfg *config.Config) error {
	// Get database connection from pgClient
	// Note: This assumes pgClient has a way to get the underlying *sql.DB
	// You may need to adapt this based on your postgres.Client interface
	db, err := a.getDBConnection()
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}

	// Initialize location embedding system (dynamic LLM-based classification)
	llmClient := llm.NewOllamaClient(cfg.LLMEndpoint, a.logger)
	locationClassifier := embedding.NewLocationClassifier(llmClient, cfg.LLMModel, a.logger)
	locationStorage := embedding.NewLocationEmbeddingStorage(db, locationClassifier, a.logger)

	// Set global location embedding storage for use by semantic_embedding.go
	embedding.SetLocationEmbeddingStorage(locationStorage)

	// Preload location embeddings cache from database
	if err := locationStorage.PreloadCache(context.Background()); err != nil {
		a.logger.Warn("Failed to preload location embeddings cache", "error", err)
		// Continue anyway - embeddings will be loaded on-demand
	} else {
		a.logger.Info("Location embeddings cache preloaded", "count", locationStorage.GetCacheSize())
	}

	// Create storage layer
	anchorStorage := storage.NewAnchorStorage(db)

	// Create context gatherer
	// Note: Need to convert redis.Client interface to *redis.Client
	// We need the underlying go-redis client for ZRevRangeWithScores
	redisClient := a.getRedisClient()
	contextGatherer := behaviorcontext.NewContextGatherer(redisClient, a.logger)

	// Create anchor creator
	a.anchorCreator = anchor.NewAnchorCreator(anchorStorage, contextGatherer, a.logger)

	// Initialize progressive activity embeddings (optional feature)
	if cfg.ProgressiveActivityEmbeddings {
		llmClient := llm.NewOllamaClient(cfg.LLMEndpoint, a.logger)
		activityStorage := embedding.NewActivityEmbeddingStorage(db)
		activityLLM := embedding.NewActivityLLMEmbeddingGenerator(
			llmClient,
			cfg.LLMModel, // Reuse the same model as distance computation
			a.logger,
		)
		activityAgent := &embedding.ActivityEmbeddingAgent{
			Storage: activityStorage,
			LLM:     activityLLM,
		}
		a.anchorCreator.SetActivityEmbeddingAgent(activityAgent)
		a.logger.Info("Progressive activity embeddings enabled")
	}

	a.logger.Info("Semantic anchor system initialized with dynamic location embeddings")
	return nil
}

// createAnchorFromEvent creates a semantic anchor from an event during episode detection.
// This is called for significant events (motion ON, lighting ON) to create anchor points.
func (a *Agent) createAnchorFromEvent(ctx context.Context, event Event) error {
	// Skip if anchor creator not initialized
	if a.anchorCreator == nil {
		return nil
	}

	// Gather signals from the event
	signals := []types.ActivitySignal{
		{
			Type:       event.Type,
			Confidence: 0.8, // Default confidence
			Timestamp:  event.Timestamp,
			Value:      a.buildSignalValue(event),
		},
	}

	// Create semantic anchor
	anchor, err := a.anchorCreator.CreateAnchor(ctx, event.Location, event.Timestamp, signals)
	if err != nil {
		a.logger.Warn("Failed to create semantic anchor",
			"location", event.Location,
			"event_type", event.Type,
			"error", err)
		// Don't fail episode creation if anchor creation fails
		return nil
	}

	a.logger.Debug("Created semantic anchor",
		"anchor_id", anchor.ID,
		"location", event.Location,
		"event_type", event.Type,
		"embedding_dims", len(anchor.SemanticEmbedding.Slice()))

	return nil
}

// buildSignalValue constructs the signal value map from an event.
func (a *Agent) buildSignalValue(event Event) map[string]interface{} {
	value := map[string]interface{}{
		"type": event.Type,
	}

	// Add basic event fields
	switch event.Type {
	case "motion":
		value["state"] = event.State
	case "lighting":
		value["state"] = event.State
		value["source"] = event.Source
	case "presence":
		value["state"] = event.State
	case "media":
		value["state"] = event.State
	}

	return value
}

// getDBConnection extracts the underlying *sql.DB from the postgres client.
// The postgres.Client interface doesn't expose DB(), but the concrete
// *PostgresClient implementation does, so we need a type assertion.
func (a *Agent) getDBConnection() (*sql.DB, error) {
	// Type assert to access the DB() method
	// This is safe because we know the concrete type is *PostgresClient
	type dbGetter interface {
		DB() *sql.DB
	}

	if getter, ok := a.pgClient.(dbGetter); ok {
		db := getter.DB()
		if db == nil {
			return nil, fmt.Errorf("postgres client not connected")
		}
		return db, nil
	}

	return nil, fmt.Errorf("postgres client does not expose DB() method")
}

// getRedisClient extracts the underlying *redis.Client from the redis client interface.
// Since our redis.Client is an interface wrapping go-redis, we need to create a new
// direct connection for the context gatherer.
func (a *Agent) getRedisClient() *goredis.Client {
	// Create a new go-redis client with the same configuration
	// This is necessary because our redis.Client interface doesn't expose
	// ZRevRangeWithScores method needed by the context gatherer
	opts := &goredis.Options{
		Addr:     a.cfg.RedisAddress(),
		Password: a.cfg.RedisPassword,
		DB:       a.cfg.RedisDB,
	}

	return goredis.NewClient(opts)
}

// createAnchorsDirectlyFromSensorEvents creates semantic anchors directly from sensor events
// stored in Redis, bypassing episode consolidation. This runs in parallel with the old
// episode-based approach to enable testing anchor-first pattern discovery.
//
// Anchor creation rules:
// - Motion: Create anchor if >2 minutes since last motion anchor in same location
// - Lighting: Always create anchor for state changes (on/off)
// - Media: Always create anchor for state changes (play/stop)
func (a *Agent) createAnchorsDirectlyFromSensorEvents(ctx context.Context, sinceTime time.Time, virtualNow time.Time, locations []string) (int, error) {
	if a.anchorCreator == nil {
		a.logger.Debug("Anchor creator not initialized, skipping direct anchor creation")
		return 0, nil
	}

	a.logger.Info("Creating anchors directly from sensor events",
		"since", sinceTime.Format(time.RFC3339),
		"until", virtualNow.Format(time.RFC3339),
		"locations", locations)

	// Gather all sensor events from Redis
	var allEvents []Event

	// Gather motion sensor events
	for _, loc := range locations {
		key := fmt.Sprintf("sensor:motion:%s", loc)
		minScore := float64(sinceTime.UnixMilli())
		maxScore := float64(virtualNow.UnixMilli())

		members, err := a.redis.ZRangeByScoreWithScores(ctx, key, minScore, maxScore)
		if err != nil {
			a.logger.Debug("No motion data for location", "location", loc, "error", err)
			continue
		}

		for _, member := range members {
			var motionData struct {
				Timestamp string `json:"timestamp"`
				State     string `json:"state"`
			}
			if err := json.Unmarshal([]byte(member.Member), &motionData); err != nil {
				continue
			}

			ts, _ := time.Parse(time.RFC3339, motionData.Timestamp)
			allEvents = append(allEvents, Event{
				Location:  loc,
				Timestamp: ts,
				Type:      "motion",
				State:     motionData.State,
			})
		}
	}

	// Gather lighting sensor events
	for _, loc := range locations {
		key := fmt.Sprintf("sensor:lighting:%s", loc)
		minScore := float64(sinceTime.UnixMilli())
		maxScore := float64(virtualNow.UnixMilli())

		members, err := a.redis.ZRangeByScoreWithScores(ctx, key, minScore, maxScore)
		if err != nil {
			a.logger.Debug("No lighting data for location", "location", loc, "error", err)
			continue
		}

		for _, member := range members {
			var lightingData struct {
				Timestamp string `json:"timestamp"`
				State     string `json:"state"`
				Source    string `json:"source"`
			}
			if err := json.Unmarshal([]byte(member.Member), &lightingData); err != nil {
				continue
			}

			ts, _ := time.Parse(time.RFC3339, lightingData.Timestamp)
			allEvents = append(allEvents, Event{
				Location:  loc,
				Timestamp: ts,
				Type:      "lighting",
				State:     lightingData.State,
				Source:    lightingData.Source,
			})
		}
	}

	// Gather media sensor events
	for _, loc := range locations {
		key := fmt.Sprintf("sensor:media:%s", loc)
		minScore := float64(sinceTime.UnixMilli())
		maxScore := float64(virtualNow.UnixMilli())

		members, err := a.redis.ZRangeByScoreWithScores(ctx, key, minScore, maxScore)
		if err != nil {
			a.logger.Debug("No media data for location", "location", loc, "error", err)
			continue
		}

		for _, member := range members {
			var mediaData struct {
				Timestamp string `json:"timestamp"`
				State     string `json:"state"`
			}
			if err := json.Unmarshal([]byte(member.Member), &mediaData); err != nil {
				continue
			}

			ts, _ := time.Parse(time.RFC3339, mediaData.Timestamp)
			allEvents = append(allEvents, Event{
				Location:  loc,
				Timestamp: ts,
				Type:      "media",
				State:     mediaData.State,
			})
		}
	}

	a.logger.Info("Gathered sensor events for direct anchor creation",
		"total_events", len(allEvents))

	// Sort events by timestamp
	// Note: Using custom sort instead of sort.Slice to avoid issues
	for i := 0; i < len(allEvents); i++ {
		for j := i + 1; j < len(allEvents); j++ {
			if allEvents[j].Timestamp.Before(allEvents[i].Timestamp) {
				allEvents[i], allEvents[j] = allEvents[j], allEvents[i]
			}
		}
	}

	// Track last significant state per location to avoid redundant anchors
	// Redis would be better for production, but using in-memory for now
	lastMotionAnchor := make(map[string]time.Time)
	lastLightingState := make(map[string]map[string]interface{}) // location -> {state, brightness}
	lastMediaState := make(map[string]string)                     // location -> state (playing/stopped)

	minMotionGap := 5 * time.Minute // Motion: Only create if >5 min gap

	anchorsCreated := 0

	for _, event := range allEvents {
		shouldCreateAnchor := false

		// Decide if this event should create an anchor
		switch event.Type {
		case "motion":
			if event.State == "on" {
				// Only create anchor if:
				// 1. First motion in location, OR
				// 2. >5 minutes since last motion anchor
				lastTime, exists := lastMotionAnchor[event.Location]
				if !exists || event.Timestamp.Sub(lastTime) > minMotionGap {
					shouldCreateAnchor = true
					lastMotionAnchor[event.Location] = event.Timestamp
				}
			}

		case "lighting":
			// Only create anchor for lighting STATE changes (on/off)
			// Skip redundant events with same state
			lastState, exists := lastLightingState[event.Location]
			if !exists {
				// First lighting event in location
				shouldCreateAnchor = true
				lastLightingState[event.Location] = a.buildSignalValue(event)
			} else {
				// Only create anchor if state actually changed
				if lastState["state"] != event.State {
					shouldCreateAnchor = true
					lastLightingState[event.Location] = a.buildSignalValue(event)
				}
			}

		case "media":
			// Only create anchor for state changes (playing ↔ stopped)
			// Skip redundant "still playing" events
			lastState, exists := lastMediaState[event.Location]
			if !exists || lastState != event.State {
				shouldCreateAnchor = true
				lastMediaState[event.Location] = event.State
			}
		}

		if !shouldCreateAnchor {
			continue
		}

		// Build signals for this anchor
		signals := []types.ActivitySignal{
			{
				Type:       event.Type,
				Confidence: 0.8,
				Timestamp:  event.Timestamp,
				Value:      a.buildSignalValue(event),
			},
		}

		// Create the anchor
		anchor, err := a.anchorCreator.CreateAnchor(ctx, event.Location, event.Timestamp, signals)
		if err != nil {
			a.logger.Warn("Failed to create direct anchor",
				"location", event.Location,
				"event_type", event.Type,
				"timestamp", event.Timestamp.Format(time.RFC3339),
				"error", err)
			continue
		}

		anchorsCreated++
		a.logger.Debug("Created direct anchor",
			"anchor_id", anchor.ID,
			"location", event.Location,
			"event_type", event.Type,
			"timestamp", event.Timestamp.Format(time.RFC3339))
	}

	a.logger.Info("Direct anchor creation completed",
		"anchors_created", anchorsCreated,
		"events_processed", len(allEvents))

	return anchorsCreated, nil
}
