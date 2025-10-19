package behavior

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/saaga0h/jeeves-platform/pkg/config"
	"github.com/saaga0h/jeeves-platform/pkg/llm"
	"github.com/saaga0h/jeeves-platform/pkg/mqtt"
	"github.com/saaga0h/jeeves-platform/pkg/ontology"
	"github.com/saaga0h/jeeves-platform/pkg/postgres"
	"github.com/saaga0h/jeeves-platform/pkg/redis"
)

type Agent struct {
	mqtt     mqtt.Client
	redis    redis.Client
	pgClient postgres.Client
	cfg      *config.Config
	logger   *slog.Logger

	timeManager         *TimeManager      // NEW
	activeEpisodes      map[string]string // location → episode ID
	lastEpisodeEndTime  map[string]time.Time // location → when last episode ended
	lastOccupancyState  map[string]string // location → "occupied" | "empty"
	lastLightState      map[string]string // location → "on" | "off" | "unknown"
	stateMux            sync.RWMutex
}

func NewAgent(mqttClient mqtt.Client, redisClient redis.Client, pgClient postgres.Client, cfg *config.Config, logger *slog.Logger) (*Agent, error) {
	return &Agent{
		mqtt:               mqttClient,
		redis:              redisClient,
		pgClient:           pgClient,
		cfg:                cfg,
		logger:             logger,
		timeManager:        NewTimeManager(logger),
		activeEpisodes:     make(map[string]string),
		lastEpisodeEndTime: make(map[string]time.Time),
		lastOccupancyState: make(map[string]string),
		lastLightState:     make(map[string]string),
	}, nil
}

func (a *Agent) Start(ctx context.Context) error {
	a.logger.Info("Starting behavior agent")

	// Connect to MQTT
	if err := a.mqtt.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to MQTT: %w", err)
	}

	// NEW: Subscribe to test mode configuration
	if err := a.timeManager.ConfigureFromMQTT(a.mqtt); err != nil {
		a.logger.Warn("Failed to subscribe to test mode config", "error", err)
		// Not fatal - continue without test mode support
	}

	// Subscribe to context topics
	topics := []string{
		"automation/context/occupancy/+",
		"automation/context/lighting/+",
	}

	for _, topic := range topics {
		if err := a.mqtt.Subscribe(topic, 0, a.handleMessage); err != nil {
			return fmt.Errorf("failed to subscribe to %s: %w", topic, err)
		}
	}

	// NEW: Subscribe to manual consolidation trigger
	if err := a.mqtt.Subscribe("automation/behavior/consolidate", 0, a.handleConsolidationTrigger); err != nil {
		a.logger.Warn("Failed to subscribe to consolidation trigger", "error", err)
	}

	a.logger.Info("Subscribed to topics", "topics", topics)

	// go a.runConsolidationJob(ctx)

	// Block until context cancelled
	<-ctx.Done()
	return nil
}

func (a *Agent) Stop() error {
	a.logger.Info("Stopping behavior agent")
	a.mqtt.Disconnect()
	return a.pgClient.Disconnect()
}

// checkShouldCloseEpisode determines if episode should close based on activity context
func (a *Agent) checkShouldCloseEpisode(location string) {
	ctx := context.Background()
	now := a.timeManager.Now()

	// Check if media is playing/paused recently
	if a.hasActiveMedia(ctx, location, now) {
		a.logger.Debug("Episode kept open - active media detected",
			"location", location)

		// Schedule delayed check (10 minutes)
		go a.scheduleDelayedCheck(location, 10*time.Minute)
		return
	}

	// Check for recent manual light adjustments
	if a.hasRecentManualLighting(ctx, location, now) {
		a.logger.Debug("Episode kept open - recent manual interaction",
			"location", location)

		// Schedule delayed check (5 minutes)
		go a.scheduleDelayedCheck(location, 5*time.Minute)
		return
	}

	// No activity anchors - safe to close immediately
	a.logger.Info("No activity anchors, closing episode",
		"location", location)
	a.endEpisode(location, "occupancy_empty")
}

// hasActiveMedia checks if media is playing or recently paused
func (a *Agent) hasActiveMedia(ctx context.Context, location string, now time.Time) bool {
	mediaKey := fmt.Sprintf("sensor:media:%s", location)

	// Get most recent media event (last 15 minutes)
	lookback := now.Add(-15 * time.Minute)

	max := float64(now.UnixMilli())
	min := float64(lookback.UnixMilli())

	a.logger.Debug("Checking for active media",
		"location", location,
		"key", mediaKey,
		"now", now.Format(time.RFC3339),
		"lookback", lookback.Format(time.RFC3339),
		"max_score", max,
		"min_score", min)

	// Query in reverse order (most recent first), limit 1
	members, err := a.redis.ZRevRangeByScoreWithScores(ctx, mediaKey, max, min, 0, 1)

	if err != nil {
		a.logger.Warn("Redis query failed for media data",
			"location", location,
			"key", mediaKey,
			"error", err)
		return false
	}

	a.logger.Debug("Media query completed",
		"location", location,
		"results_count", len(members))

	if len(members) == 0 {
		a.logger.Debug("No recent media events found",
			"location", location,
			"lookback_minutes", 15)
		return false
	}

	a.logger.Debug("Media member found",
		"location", location,
		"score", members[0].Score,
		"member", members[0].Member)

	var mediaData map[string]interface{}
	if err := json.Unmarshal([]byte(members[0].Member), &mediaData); err != nil {
		a.logger.Warn("Failed to parse media data from Redis",
			"location", location,
			"error", err)
		return false
	}

	a.logger.Debug("Parsed media data",
		"location", location,
		"data", mediaData)

	state, ok := mediaData["state"].(string)
	if !ok {
		return false
	}

	// Consider "playing" or "paused" as active
	isActive := state == "playing" || state == "paused"

	a.logger.Info("Media activity check completed",
		"location", location,
		"state", state,
		"is_active", isActive,
		"timestamp", mediaData["timestamp"])

	if isActive {
		a.logger.Debug("Active media detected",
			"location", location,
			"state", state)
	}

	return isActive
}

// hasRecentManualLighting checks for recent manual light adjustments
func (a *Agent) hasRecentManualLighting(ctx context.Context, location string, now time.Time) bool {
	lightKey := fmt.Sprintf("sensor:lighting:%s", location)

	// Get most recent lighting event (last 5 minutes)
	lookback := now.Add(-5 * time.Minute)

	max := float64(now.UnixMilli())
	min := float64(lookback.UnixMilli())

	a.logger.Debug("Checking for manual lighting",
		"location", location,
		"key", lightKey,
		"now", now.Format(time.RFC3339),
		"lookback", lookback.Format(time.RFC3339),
		"max_score", max,
		"min_score", min)

	// Query in reverse order (most recent first), limit 1
	members, err := a.redis.ZRevRangeByScoreWithScores(ctx, lightKey, max, min, 0, 1)

	if err != nil {
		a.logger.Warn("Redis query failed for lighting data",
			"location", location,
			"key", lightKey,
			"error", err)
		return false
	}

	a.logger.Debug("Lighting query completed",
		"location", location,
		"results_count", len(members))

	if len(members) == 0 {
		a.logger.Debug("No recent lighting events found",
			"location", location,
			"lookback_minutes", 5)
		return false
	}

	a.logger.Debug("Lighting member found",
		"location", location,
		"score", members[0].Score,
		"member", members[0].Member)

	var lightData map[string]interface{}
	if err := json.Unmarshal([]byte(members[0].Member), &lightData); err != nil {
		a.logger.Warn("Failed to parse lighting data from Redis",
			"location", location,
			"error", err)
		return false
	}

	a.logger.Debug("Parsed lighting data",
		"location", location,
		"data", lightData)

	source, ok := lightData["source"].(string)
	if !ok {
		return false
	}

	isManual := source == "manual"

	a.logger.Info("Manual lighting check completed",
		"location", location,
		"source", source,
		"is_manual", isManual,
		"timestamp", lightData["timestamp"])

	if isManual {
		a.logger.Debug("Recent manual lighting interaction detected",
			"location", location)
	}

	return isManual
}

// scheduleDelayedCheck schedules a delayed check to close episode later
func (a *Agent) scheduleDelayedCheck(location string, delay time.Duration) {
	time.Sleep(delay)

	// Re-check if episode should close now
	a.stateMux.RLock()
	_, exists := a.activeEpisodes[location]
	currentOccupancy := a.lastOccupancyState[location]
	a.stateMux.RUnlock()

	if !exists {
		return // Episode already closed
	}

	// If still empty and no activity context, close now
	if currentOccupancy == "empty" {
		ctx := context.Background()
		now := a.timeManager.Now()

		if !a.hasActiveMedia(ctx, location, now) &&
			!a.hasRecentManualLighting(ctx, location, now) {
			a.logger.Info("Delayed check: closing episode now",
				"location", location,
				"delay_was", delay)
			a.endEpisode(location, "activity_complete")
		} else {
			a.logger.Debug("Delayed check: activity still present, keeping open",
				"location", location)
		}
	}
}

func (a *Agent) handleMessage(msg mqtt.Message) {
	topic := msg.Topic()

	if strings.Contains(topic, "/occupancy/") {
		a.handleOccupancyMessage(msg)
	} else if strings.Contains(topic, "/lighting/") {
		a.handleLightingMessage(msg)
	} else if strings.Contains(topic, "/media/") {
		a.handleMediaMessage(msg)
	}
}

func (a *Agent) handleOccupancyMessage(msg mqtt.Message) {
	// Extract location from topic first
	parts := strings.Split(msg.Topic(), "/")
	if len(parts) < 4 {
		a.logger.Error("Invalid occupancy topic format", "topic", msg.Topic())
		return
	}
	location := parts[3]

	// Try simple format first ({"state": "occupied", "confidence": 0.85})
	var simple struct {
		State      string  `json:"state"`
		Confidence float64 `json:"confidence"`
	}

	if err := json.Unmarshal(msg.Payload(), &simple); err == nil && simple.State != "" {
		a.stateMux.Lock()
		previousState := a.lastOccupancyState[location]
		currentState := simple.State
		a.lastOccupancyState[location] = currentState
		a.stateMux.Unlock()

		a.logger.Debug("Occupancy state update",
			"location", location,
			"previous", previousState,
			"current", currentState,
			"transition", fmt.Sprintf("%s→%s", previousState, currentState))

		// Detect transitions
		if previousState != "occupied" && currentState == "occupied" {
			a.startEpisode(location, "occupancy_transition")
		}

		// Check if episode should close when occupancy becomes empty
		if previousState == "occupied" && currentState == "empty" {
			a.checkShouldCloseEpisode(location)
		}
		return
	}

	// Try nested format ({"data": {"occupied": true, "confidence": 0.8, ...}})
	var nested struct {
		Data struct {
			Occupied   bool    `json:"occupied"`
			Confidence float64 `json:"confidence"`
		} `json:"data"`
	}

	if err := json.Unmarshal(msg.Payload(), &nested); err == nil {
		a.stateMux.Lock()
		previousState := a.lastOccupancyState[location]
		currentState := "empty"
		if nested.Data.Occupied {
			currentState = "occupied"
		}
		a.lastOccupancyState[location] = currentState
		a.stateMux.Unlock()

		// Detect transitions
		if previousState != "occupied" && currentState == "occupied" {
			a.startEpisode(location, "occupancy_transition")
		}

		// Check if episode should close when occupancy becomes empty
		if previousState == "occupied" && currentState == "empty" {
			a.checkShouldCloseEpisode(location)
		}
		return
	}

	a.logger.Warn("Failed to parse occupancy message in any known format",
		"topic", msg.Topic(),
		"payload", string(msg.Payload()))
}

func (a *Agent) startEpisode(location, triggerType string) {
	now := a.timeManager.Now() // Changed from time.Now()

	a.stateMux.Lock()
	_, exists := a.activeEpisodes[location]
	lastEndTime, hasRecentEnd := a.lastEpisodeEndTime[location]
	a.stateMux.Unlock()

	// Check if episode already active
	if exists {
		a.logger.Debug("Episode already active, skipping duplicate creation",
			"location", location,
			"trigger_type", triggerType)
		return
	}

	// Check if an episode was recently closed (within last 10 minutes)
	// This prevents occupancy agent predictions from creating spurious episodes
	if hasRecentEnd {
		timeSinceLastEpisode := now.Sub(lastEndTime)
		if timeSinceLastEpisode < 10*time.Minute {
			a.logger.Debug("Episode recently closed, skipping spurious creation",
				"location", location,
				"trigger_type", triggerType,
				"time_since_last", timeSinceLastEpisode)
			return
		}
	}

	// Create episode with virtual time
	episode := ontology.NewEpisode(
		ontology.Activity{
			Type: "adl:Present",
			Name: "Present",
		},
		ontology.Location{
			Type: "saref:Room",
			ID:   fmt.Sprintf("urn:room:%s", location),
			Name: location,
		},
	)

	// Override the timestamp with virtual time
	episode.StartedAt = now

	// Add trigger type to the JSON-LD
	episodeJSON, _ := json.Marshal(episode)
	var episodeMap map[string]interface{}
	json.Unmarshal(episodeJSON, &episodeMap)
	episodeMap["jeeves:triggerType"] = triggerType
	jsonld, _ := json.Marshal(episodeMap)

	var id string
	err := a.pgClient.QueryRow(context.Background(),
		"INSERT INTO behavioral_episodes (jsonld) VALUES ($1) RETURNING id",
		jsonld,
	).Scan(&id)

	if err != nil {
		a.logger.Error("Failed to create episode", "error", err)
		return
	}

	a.stateMux.Lock()
	a.activeEpisodes[location] = id
	a.stateMux.Unlock()

	a.logger.Info("Episode started", "location", location, "id", id, "trigger_type", triggerType)

	// Publish event
	a.publishEpisodeEvent("started", map[string]interface{}{
		"location":     location,
		"trigger_type": triggerType,
	})
}

func (a *Agent) endEpisode(location string, reason string) {
	a.stateMux.Lock()
	id, exists := a.activeEpisodes[location]
	a.stateMux.Unlock()

	if !exists {
		return
	}

	now := a.timeManager.Now() // Changed from time.Now()

	_, err := a.pgClient.Exec(context.Background(),
		"UPDATE behavioral_episodes SET jsonld = jsonb_set(jsonld, '{jeeves:endedAt}', to_jsonb($1::text)) WHERE id = $2",
		now.Format(time.RFC3339),
		id,
	)

	if err != nil {
		a.logger.Error("Failed to end episode", "error", err)
		return
	}

	a.stateMux.Lock()
	delete(a.activeEpisodes, location)
	a.lastEpisodeEndTime[location] = now
	a.stateMux.Unlock()

	a.logger.Info("Episode ended", "location", location, "id", id, "ended_at", now.Format(time.RFC3339))

	// Publish event
	a.publishEpisodeEvent("closed", map[string]interface{}{
		"location":   location,
		"end_reason": reason,
	})
}

func (a *Agent) publishEpisodeEvent(eventType string, data map[string]interface{}) {
	topic := fmt.Sprintf("automation/behavior/episode/%s", eventType)
	payload, _ := json.Marshal(data)
	a.mqtt.Publish(topic, 0, false, payload)
}

// createEpisodesFromRedis creates episodes by analyzing occupancy data in Redis
func (a *Agent) createEpisodesFromRedis(ctx context.Context, sinceTime time.Time, location string) (int, error) {
	// Use real time for Redis queries since predictions are stored with wall-clock time
	// Virtual time is only used for episode timestamps
	now := time.Now()
	virtualNow := a.timeManager.Now()

	// Get all locations to process
	locations := []string{"bedroom", "bathroom", "kitchen", "dining_room", "hallway", "study", "living_room"}
	if location != "" && location != "universe" {
		locations = []string{location}
	}

	episodesCreated := 0

	for _, loc := range locations {
		// Query Redis for occupancy predictions (stored as a list)
		key := fmt.Sprintf("predictions:%s", loc)

		a.logger.Debug("Querying Redis for occupancy predictions",
			"location", loc,
			"key", key,
			"since", sinceTime.Format(time.RFC3339),
			"now", now.Format(time.RFC3339))

		// Get all predictions from the list
		predictions, err := a.redis.LRange(ctx, key, 0, -1)
		if err != nil {
			a.logger.Warn("Failed to query Redis for predictions",
				"location", loc,
				"error", err)
			continue
		}

		if len(predictions) == 0 {
			a.logger.Debug("No prediction data found in Redis",
				"location", loc)
			continue
		}

		a.logger.Debug("Found prediction events in Redis",
			"location", loc,
			"count", len(predictions))

		// Parse predictions and detect transitions
		// Note: predictions are stored newest first, so we need to reverse
		var lastState string
		var episodeStart *time.Time

		// Process in chronological order (reverse the list)
		for i := len(predictions) - 1; i >= 0; i-- {
			var predData struct {
				Timestamp  string  `json:"timestamp"`
				Occupied   bool    `json:"occupied"`
				Confidence float64 `json:"confidence"`
			}

			if err := json.Unmarshal([]byte(predictions[i]), &predData); err != nil {
				a.logger.Warn("Failed to parse prediction data", "error", err)
				continue
			}

			eventTime, err := time.Parse(time.RFC3339, predData.Timestamp)
			if err != nil {
				a.logger.Warn("Failed to parse timestamp", "error", err, "timestamp", predData.Timestamp)
				continue
			}

			// NOTE: For test scenarios, we process ALL predictions regardless of timestamp
			// because predictions use wall-clock time while scenarios use virtual time.
			// In production, we'd want to filter by time window.
			// TODO: Make occupancy agent use virtual time in test mode

			// Get state
			var currentState string
			if predData.Occupied {
				currentState = "occupied"
			} else {
				currentState = "empty"
			}

			// Detect transition to occupied (start episode)
			if lastState != "occupied" && currentState == "occupied" {
				episodeStart = &eventTime
				a.logger.Debug("Detected episode start",
					"location", loc,
					"time", eventTime.Format(time.RFC3339))
			}

			// Detect transition to empty (end episode)
			if lastState == "occupied" && currentState == "empty" && episodeStart != nil {
				// Create episode in database
				if err := a.createEpisodeInDB(ctx, loc, *episodeStart, eventTime, "occupancy_transition"); err != nil {
					a.logger.Error("Failed to create episode in DB",
						"location", loc,
						"error", err)
				} else {
					episodesCreated++
					a.logger.Info("Episode created from Redis data",
						"location", loc,
						"start", episodeStart.Format(time.RFC3339),
						"end", eventTime.Format(time.RFC3339))
				}
				episodeStart = nil
			}

			lastState = currentState
		}

		// Handle unclosed episode (still occupied at end of window)
		if episodeStart != nil {
			if err := a.createEpisodeInDB(ctx, loc, *episodeStart, virtualNow, "occupancy_transition"); err != nil {
				a.logger.Error("Failed to create unclosed episode in DB",
					"location", loc,
					"error", err)
			} else {
				episodesCreated++
				a.logger.Info("Unclosed episode created from Redis data",
					"location", loc,
					"start", episodeStart.Format(time.RFC3339),
					"end", "ongoing")
			}
		}
	}

	return episodesCreated, nil
}

// createEpisodeInDB inserts an episode directly into the database
func (a *Agent) createEpisodeInDB(ctx context.Context, location string, startTime, endTime time.Time, triggerType string) error {
	episode := ontology.NewEpisode(
		ontology.Activity{
			Type: "adl:Present",
			Name: "Present",
		},
		ontology.Location{
			Type: "saref:Room",
			ID:   fmt.Sprintf("urn:room:%s", location),
			Name: location,
		},
	)

	episode.StartedAt = startTime

	// Build JSON-LD with both start and end times
	episodeJSON, _ := json.Marshal(episode)
	var episodeMap map[string]interface{}
	json.Unmarshal(episodeJSON, &episodeMap)
	episodeMap["jeeves:triggerType"] = triggerType
	episodeMap["jeeves:endedAt"] = endTime.Format(time.RFC3339)
	jsonld, _ := json.Marshal(episodeMap)

	_, err := a.pgClient.Exec(ctx,
		"INSERT INTO behavioral_episodes (jsonld) VALUES ($1)",
		jsonld,
	)

	return err
}

func (a *Agent) performConsolidation(ctx context.Context, sinceTime time.Time, location string) error {
	a.logger.Info("=== CONSOLIDATION ORCHESTRATION START ===",
		"since", sinceTime.Format(time.RFC3339),
		"location", location,
		"virtual_time", a.timeManager.Now().Format(time.RFC3339))

	// STEP 0: Create episodes from Redis occupancy data
	// NOTE: This is temporarily disabled - episode creation from predictions needs more work
	// TODO: Re-enable once occupancy agent uses virtual time in tests
	a.logger.Info("--- PHASE 0: EPISODE CREATION FROM REDIS (SKIPPED) ---")
	a.logger.Warn("Episode creation from Redis is temporarily disabled - episodes should be created in real-time or via different mechanism")

	// STEP 1: Get unconsolidated episodes from database
	episodes, err := a.getUnconsolidatedEpisodes(ctx, sinceTime, location)
	if err != nil {
		a.logger.Error("Failed to get unconsolidated episodes", "error", err)
		return fmt.Errorf("failed to get unconsolidated episodes: %w", err)
	}

	if len(episodes) == 0 {
		a.logger.Info("No episodes to consolidate - orchestration complete")
		return nil
	}

	// Log what we found
	a.logger.Info("Episodes retrieved for consolidation",
		"count", len(episodes),
		"time_range", fmt.Sprintf("%s to %s",
			episodes[0].StartedAt.Format("15:04:05"),
			episodes[len(episodes)-1].StartedAt.Format("15:04:05")))

	// Log episode details for debugging
	for i, ep := range episodes {
		duration := "ongoing"
		if ep.EndedAt != nil {
			duration = fmt.Sprintf("%.1fm", ep.EndedAt.Sub(ep.StartedAt).Minutes())
		}
		a.logger.Debug("Episode details",
			"index", i,
			"id", ep.ID,
			"location", ep.Location,
			"started", ep.StartedAt.Format("15:04:05"),
			"duration", duration,
			"trigger", ep.TriggerType)
	}

	// NEW: STEP 1.5: DETECT BEHAVIORAL VECTORS
	a.logger.Info("--- PHASE 0: VECTOR DETECTION ---")

	// Detect vectors with max 300 second (5 minute) gaps
	maxGapSeconds := 300
	vectors := detectVectors(episodes, maxGapSeconds, a.logger)

	a.logger.Info("Vector detection completed",
		"vectors_detected", len(vectors),
		"max_gap_seconds", maxGapSeconds)

	// Store vectors in database
	vectorsStored := 0
	for i, vector := range vectors {
		a.logger.Debug("Storing vector",
			"index", i,
			"id", vector.ID,
			"sequence_length", len(vector.Sequence),
			"quality_score", vector.QualityScore)

		if err := a.storeVector(ctx, vector); err != nil {
			a.logger.Error("Failed to store vector",
				"error", err,
				"vector_id", vector.ID)
		} else {
			vectorsStored++

			// Log vector details for debugging
			locations := make([]string, len(vector.Sequence))
			for j, node := range vector.Sequence {
				locations[j] = node.Location
			}

			a.logger.Info("Vector stored successfully",
				"vector_id", vector.ID,
				"locations", fmt.Sprintf("%v", locations),
				"time_of_day", vector.Context.TimeOfDay,
				"duration_min", vector.Context.TotalDurationSec/60,
				"quality", vector.QualityScore)
		}
	}

	a.logger.Info("Vector storage completed",
		"vectors_stored", vectorsStored,
		"vectors_failed", len(vectors)-vectorsStored)

	totalMacrosCreated := 0

	// STEP 2: Rule-based consolidation
	a.logger.Info("--- PHASE 1: RULE-BASED CONSOLIDATION ---")

	ruleMacros := consolidateMicroEpisodesRuleBased(episodes, a.cfg.ConsolidationMaxGapMinutes, a.logger)

	a.logger.Info("Rule-based consolidation completed",
		"macros_generated", len(ruleMacros),
		"max_gap_minutes", a.cfg.ConsolidationMaxGapMinutes)

	// Store rule-based macros
	for i, macro := range ruleMacros {
		a.logger.Debug("Storing rule-based macro",
			"index", i,
			"id", macro.ID,
			"pattern", macro.PatternType,
			"locations", macro.Locations,
			"duration_min", macro.DurationMinutes,
			"micro_count", len(macro.MicroEpisodeIDs))

		if err := a.createMacroEpisode(ctx, macro); err != nil {
			a.logger.Error("Failed to create rule-based macro-episode",
				"error", err,
				"macro_id", macro.ID)
		} else {
			totalMacrosCreated++
			a.logger.Info("Rule-based macro-episode stored",
				"macro_id", macro.ID,
				"summary", macro.Summary)
		}
	}

	// STEP 3: Get remaining episodes for LLM
	a.logger.Info("--- PHASE 2: LLM CONSOLIDATION ---")

	remainingEpisodes, err := a.getUnconsolidatedEpisodes(ctx, sinceTime, location)
	if err != nil {
		a.logger.Error("Failed to get remaining episodes for LLM", "error", err)
	} else {
		a.logger.Info("Remaining episodes after rule-based consolidation",
			"count", len(remainingEpisodes))

		if len(remainingEpisodes) >= 2 {
			// Create LLM client
			llmClient := llm.NewOllamaClient(a.cfg.LLMEndpoint, a.logger)

			// Check LLM health
			healthCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			if err := llmClient.Health(healthCtx); err != nil {
				a.logger.Warn("LLM not available, skipping LLM consolidation",
					"error", err,
					"endpoint", a.cfg.LLMEndpoint)
			} else {
				a.logger.Info("LLM available, starting LLM consolidation",
					"endpoint", a.cfg.LLMEndpoint,
					"model", a.cfg.LLMModel,
					"min_confidence", a.cfg.LLMMinConfidence)

				// LLM consolidation
				llmMacros, err := consolidateWithLLM(
					ctx,
					remainingEpisodes,
					llmClient,
					a.cfg,
					a.logger,
					a.timeManager.Now(),
				)

				if err != nil {
					a.logger.Error("LLM consolidation failed", "error", err)
				} else {
					a.logger.Info("LLM consolidation completed",
						"macros_generated", len(llmMacros))

					// Store LLM macros
					for i, macro := range llmMacros {
						a.logger.Debug("Storing LLM macro",
							"index", i,
							"id", macro.ID,
							"pattern", macro.PatternType,
							"locations", macro.Locations,
							"duration_min", macro.DurationMinutes,
							"micro_count", len(macro.MicroEpisodeIDs))

						if err := a.createMacroEpisode(ctx, macro); err != nil {
							a.logger.Error("Failed to create LLM macro-episode",
								"error", err,
								"macro_id", macro.ID)
						} else {
							totalMacrosCreated++
							a.logger.Info("LLM macro-episode stored",
								"macro_id", macro.ID,
								"summary", macro.Summary)
						}
					}
				}
			}
		} else {
			a.logger.Info("Not enough remaining episodes for LLM consolidation",
				"count", len(remainingEpisodes),
				"required", 2)
		}
	}

	// STEP 4: Final summary
	a.logger.Info("=== CONSOLIDATION ORCHESTRATION COMPLETE ===",
		"total_episodes_input", len(episodes),
		"vectors_detected", len(vectors),
		"vectors_stored", vectorsStored,
		"rule_based_macros", len(ruleMacros),
		"total_macros_created", totalMacrosCreated)

	a.publishConsolidationResult(totalMacrosCreated, len(episodes))

	return nil
}

func (a *Agent) handleLightingMessage(msg mqtt.Message) {
	// Extract location from topic
	parts := strings.Split(msg.Topic(), "/")
	if len(parts) < 4 {
		a.logger.Error("Invalid lighting topic format", "topic", msg.Topic())
		return
	}
	location := parts[3]

	// Parse lighting data
	var lightData struct {
		State      string  `json:"state"`      // "on" | "off"
		Brightness *int    `json:"brightness"` // 0-100
		ColorTemp  *int    `json:"color_temp"` // Kelvin
		Source     string  `json:"source"`     // "manual" | "automated"
		Timestamp  *string `json:"timestamp"`
	}

	if err := json.Unmarshal(msg.Payload(), &lightData); err != nil {
		a.logger.Warn("Failed to parse lighting message",
			"topic", msg.Topic(),
			"payload", string(msg.Payload()),
			"error", err)
		return
	}

	a.logger.Debug("Lighting message received",
		"location", location,
		"state", lightData.State,
		"source", lightData.Source,
		"brightness", lightData.Brightness)

	// Only process manual lighting events for episode creation
	if lightData.Source != "manual" {
		a.logger.Debug("Ignoring automated lighting event", "location", location)
		return
	}

	// Track light state transitions
	a.stateMux.Lock()
	previousState := a.lastLightState[location]
	currentState := lightData.State
	a.lastLightState[location] = currentState
	_, hasActiveEpisode := a.activeEpisodes[location]
	a.stateMux.Unlock()

	a.logger.Debug("Light state transition",
		"location", location,
		"previous", previousState,
		"current", currentState,
		"has_active_episode", hasActiveEpisode)

	// Handle light turned ON manually
	if previousState != "on" && currentState == "on" {
		// Only create episode if no active episode exists
		if !hasActiveEpisode {
			a.logger.Info("Creating light-based episode",
				"location", location,
				"brightness", lightData.Brightness,
				"color_temp", lightData.ColorTemp)
			a.startEpisode(location, "manual_lighting")
		} else {
			a.logger.Debug("Light turned on, but episode already active",
				"location", location)
		}
	}

	// Handle light turned OFF manually
	if previousState == "on" && currentState == "off" {
		if hasActiveEpisode {
			// For light-based episodes, schedule delayed closure (more patient than motion)
			// Check if this episode was created by lighting
			go a.scheduleLightBasedClosure(location, 5*time.Minute)
		}
	}
}

// scheduleLightBasedClosure schedules delayed closure for light-based episodes
func (a *Agent) scheduleLightBasedClosure(location string, delay time.Duration) {
	a.logger.Debug("Scheduling light-based episode closure",
		"location", location,
		"delay", delay)

	time.Sleep(delay)

	// Check if episode should still be closed
	a.stateMux.RLock()
	_, exists := a.activeEpisodes[location]
	currentLightState := a.lastLightState[location]
	a.stateMux.RUnlock()

	if !exists {
		a.logger.Debug("Episode already closed during delay",
			"location", location)
		return // Episode already closed
	}

	// If light is still off after delay period, close the episode
	if currentLightState == "off" {
		a.logger.Info("Closing light-based episode after delay",
			"location", location,
			"delay_was", delay)
		a.endEpisode(location, "lighting_off_delay")
	} else {
		a.logger.Debug("Light turned back on during delay, keeping episode open",
			"location", location)
	}
}

func (a *Agent) handleMediaMessage(msg mqtt.Message) {
	// TODO: Detect media activity for inference
}
