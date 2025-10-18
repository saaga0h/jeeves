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

	timeManager        *TimeManager      // NEW
	activeEpisodes     map[string]string // location → episode ID
	lastOccupancyState map[string]string // location → "occupied" | "empty"
	stateMux           sync.RWMutex
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
		lastOccupancyState: make(map[string]string),
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
		"automation/manual/light/+",
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
			a.startEpisode(location)
		}

		// CHANGED: Don't blindly close - check activity context
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
			a.startEpisode(location)
		}

		// CHANGED: Don't blindly close - check activity context
		if previousState == "occupied" && currentState == "empty" {
			a.checkShouldCloseEpisode(location)
		}
		return
	}

	a.logger.Warn("Failed to parse occupancy message in any known format",
		"topic", msg.Topic(),
		"payload", string(msg.Payload()))
}

func (a *Agent) startEpisode(location string) {
	now := a.timeManager.Now() // Changed from time.Now()

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

	// Store in Postgres
	jsonld, _ := json.Marshal(episode)

	var id string
	err := a.pgClient.QueryRow(context.Background(),
		"INSERT INTO behavioral_episodes (jsonld) VALUES ($1) RETURNING id",
		jsonld,
	).Scan(&id)

	if err != nil {
		a.logger.Error("Failed to create episode", "error", err)
		return
	}

	a.activeEpisodes[location] = id
	a.logger.Info("Episode started", "location", location, "id", id)

	// Publish event
	a.publishEpisodeEvent("started", map[string]interface{}{
		"location":     location,
		"trigger_type": "occupancy_transition",
	})
}

func (a *Agent) endEpisode(location string, reason string) {
	id, exists := a.activeEpisodes[location]
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

	delete(a.activeEpisodes, location)
	a.logger.Info("Episode ended", "location", location, "id", id)

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

func (a *Agent) performConsolidation(ctx context.Context, sinceTime time.Time, location string) error {
	a.logger.Info("=== CONSOLIDATION ORCHESTRATION START ===",
		"since", sinceTime.Format(time.RFC3339),
		"location", location,
		"virtual_time", a.timeManager.Now().Format(time.RFC3339))

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

// extractLocation extracts location from MQTT topic
func extractLocation(topic string) string {
	parts := strings.Split(topic, "/")
	if len(parts) >= 4 {
		return parts[3]
	}
	return ""
}

// Stubs for other handlers (implement in later iterations)
func (a *Agent) handleLightingMessage(msg mqtt.Message) {
	// TODO: Track manual adjustments
}

func (a *Agent) handleMediaMessage(msg mqtt.Message) {
	// TODO: Detect media activity for inference
}
