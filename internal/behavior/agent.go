package behavior

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/saaga0h/jeeves-platform/internal/behavior/anchor"
	"github.com/saaga0h/jeeves-platform/internal/behavior/clustering"
	"github.com/saaga0h/jeeves-platform/internal/behavior/distance"
	"github.com/saaga0h/jeeves-platform/internal/behavior/patterns"
	"github.com/saaga0h/jeeves-platform/internal/behavior/storage"
	"github.com/saaga0h/jeeves-platform/internal/behavior/types"
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

	// Semantic anchor system (optional - Phase 3)
	anchorCreator       *anchor.AnchorCreator

	// Pattern discovery system (optional - Phase 4)
	distanceAgent       *distance.ComputationAgent
	clusteringEngine    *clustering.ClusteringEngine
	patternInterpreter  *patterns.PatternInterpreter
	discoveryAgent      *patterns.DiscoveryAgent
}

// Event represents a sensor event used for episode detection and anchor creation
type Event struct {
	Location  string
	Timestamp time.Time
	Type      string // "motion", "presence", "lighting"
	State     string // "on"/"off" for motion, "occupied"/"empty" for presence
	Source    string // "manual"/"automated" for lighting events
}

func NewAgent(mqttClient mqtt.Client, redisClient redis.Client, pgClient postgres.Client, cfg *config.Config, logger *slog.Logger) (*Agent, error) {
	agent := &Agent{
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
	}

	// Initialize pattern discovery if enabled
	if cfg.PatternDiscoveryEnabled {
		// Initialize anchor creator first (required for pattern discovery)
		if err := agent.initializeAnchorCreator(cfg); err != nil {
			logger.Warn("Failed to initialize anchor creator", "error", err)
		}

		if err := agent.initializePatternDiscovery(); err != nil {
			logger.Warn("Failed to initialize pattern discovery", "error", err)
			// Not fatal - continue without pattern discovery
		}
	}

	return agent, nil
}

// initializePatternDiscovery initializes all pattern discovery components
func (a *Agent) initializePatternDiscovery() error {
	a.logger.Info("Initializing pattern discovery system",
		"strategy", a.cfg.PatternDistanceStrategy,
		"interval_hours", a.cfg.PatternDiscoveryIntervalHours,
		"epsilon", a.cfg.PatternClusteringEpsilon,
		"min_points", a.cfg.PatternClusteringMinPoints)

	// Get database connection for storage layer
	db, err := a.getDBConnection()
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}

	// Create storage instance (will be used by multiple components)
	anchorStorage := a.createAnchorStorage(db)

	// Create LLM client for pattern interpretation and distance computation
	llmClient := llm.NewOllamaClient(a.cfg.LLMEndpoint, a.logger)

	// Initialize distance computation agent
	distanceConfig := distance.ComputationConfig{
		Strategy:  a.cfg.PatternDistanceStrategy,
		Model:     a.cfg.LLMModel,
		BatchSize: a.cfg.PatternDiscoveryBatchSize,
		Interval:  time.Duration(a.cfg.PatternDiscoveryIntervalHours) * time.Hour,
	}
	a.distanceAgent = distance.NewComputationAgent(
		distanceConfig,
		anchorStorage,
		llmClient,
		a.mqtt,
		a.logger,
	)

	// Initialize clustering engine
	clusteringConfig := clustering.DBSCANConfig{
		Epsilon:   a.cfg.PatternClusteringEpsilon,
		MinPoints: a.cfg.PatternClusteringMinPoints,
	}
	a.clusteringEngine = clustering.NewClusteringEngine(
		clusteringConfig,
		anchorStorage,
		a.logger,
	)

	// Initialize pattern interpreter
	a.patternInterpreter = patterns.NewPatternInterpreter(
		anchorStorage,
		llmClient,
		a.cfg.LLMModel,
		a.logger,
	)

	// Initialize pattern discovery agent
	discoveryConfig := patterns.DiscoveryConfig{
		MinAnchors:    a.cfg.PatternMinAnchorsForDiscovery,
		LookbackHours: a.cfg.PatternLookbackHours,
		Interval:      time.Duration(a.cfg.PatternDiscoveryIntervalHours) * time.Hour,
	}
	a.discoveryAgent = patterns.NewDiscoveryAgent(
		discoveryConfig,
		anchorStorage,
		a.clusteringEngine,
		a.patternInterpreter,
		a.mqtt,
		a.logger,
	)

	a.logger.Info("Pattern discovery system initialized successfully")
	return nil
}

// createAnchorStorage creates a new AnchorStorage instance from a database connection
func (a *Agent) createAnchorStorage(db *sql.DB) *storage.AnchorStorage {
	return storage.NewAnchorStorage(db)
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

	// Subscribe ONLY to consolidation trigger (no real-time episode creation)
	if err := a.mqtt.Subscribe("automation/behavior/consolidate", 0, a.handleConsolidationTrigger); err != nil {
		return fmt.Errorf("failed to subscribe to consolidation trigger: %w", err)
	}

	a.logger.Info("Behavior agent subscribed to consolidation trigger only",
		"note", "Episodes will be created during consolidation from Redis sensor data")

	// Start pattern discovery agents if enabled
	if a.cfg.PatternDiscoveryEnabled {
		a.logger.Info("Starting pattern discovery agents")

		// Start distance computation agent
		if a.distanceAgent != nil {
			go func() {
				if err := a.distanceAgent.Start(ctx); err != nil {
					a.logger.Error("Distance computation agent error", "error", err)
				}
			}()
		}

		// Start pattern discovery agent
		if a.discoveryAgent != nil {
			go func() {
				if err := a.discoveryAgent.Start(ctx); err != nil {
					a.logger.Error("Pattern discovery agent error", "error", err)
				}
			}()
		}
	}

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

// createEpisodesFromSensors creates episodes by analyzing sensor data in Redis
// Uses location transitions (motion/presence) to detect episode boundaries
func (a *Agent) createEpisodesFromSensors(ctx context.Context, sinceTime time.Time, location string) (int, error) {
	virtualNow := a.timeManager.Now()

	// Get all locations to process
	locations := []string{"bedroom", "bathroom", "kitchen", "dining_room", "hallway", "study", "living_room"}
	if location != "" && location != "universe" {
		locations = []string{location}
	}

	// Collect all motion/presence events across all locations
	var allEvents []Event

	// Gather motion sensor events from all locations
	for _, loc := range locations {
		key := fmt.Sprintf("sensor:motion:%s", loc)

		// Query Redis for motion events in the time range
		// Collector now stores virtual timestamps (from timeManager.Now().UnixMilli())
		// so this query will correctly filter by virtual time in test scenarios
		minScore := float64(sinceTime.UnixMilli())
		maxScore := float64(virtualNow.UnixMilli())

		members, err := a.redis.ZRangeByScoreWithScores(ctx, key, minScore, maxScore)
		if err != nil {
			a.logger.Debug("No motion data for location", "location", loc, "error", err)
			continue
		}

		a.logger.Debug("Retrieved motion data from Redis",
			"location", loc,
			"count", len(members),
			"time_range", fmt.Sprintf("%s to %s", sinceTime.Format("15:04:05"), virtualNow.Format("15:04:05")))

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

	// TODO: Add presence sensor events when available

	// Gather lighting sensor events from all locations
	// Lighting events help detect occupancy in rooms without motion sensors (e.g., dining room)
	for _, loc := range locations {
		key := fmt.Sprintf("sensor:lighting:%s", loc)

		minScore := float64(sinceTime.UnixMilli())
		maxScore := float64(virtualNow.UnixMilli())

		members, err := a.redis.ZRangeByScoreWithScores(ctx, key, minScore, maxScore)
		if err != nil {
			a.logger.Debug("No lighting data for location", "location", loc, "error", err)
			continue
		}

		a.logger.Debug("Retrieved lighting data from Redis",
			"location", loc,
			"count", len(members),
			"time_range", fmt.Sprintf("%s to %s", sinceTime.Format("15:04:05"), virtualNow.Format("15:04:05")))

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

	// Sort all events by timestamp
	sort.Slice(allEvents, func(i, j int) bool {
		return allEvents[i].Timestamp.Before(allEvents[j].Timestamp)
	})

	// Detect episodes using location transitions AND temporal gaps
	// Key insights:
	// 1. Motion in new location ENDS previous episode and STARTS new one
	// 2. Large gap (>5min) in same location also ends episode and starts new one
	const maxGapMinutes = 5
	var currentLocation string
	var episodeStart time.Time
	var lastEventTime time.Time
	episodesCreated := 0

	for _, event := range allEvents {
		// Process motion ON and lighting ON events (episode starts)
		if (event.Type == "motion" && event.State == "on") || (event.Type == "lighting" && event.State == "on") {
			// Check if we need to close current episode
			shouldCloseEpisode := false
			closeReason := ""
			var episodeEndTime time.Time

			if currentLocation != "" {
				if currentLocation != event.Location {
					// Location transition - close previous episode
					// End time is when new location activity detected (person has moved)
					shouldCloseEpisode = true
					closeReason = fmt.Sprintf("%s_transition", event.Type)
					episodeEndTime = event.Timestamp
				} else {
					// Same location - check for temporal gap
					gap := event.Timestamp.Sub(lastEventTime)
					if gap > maxGapMinutes*time.Minute {
						// Temporal gap - end at last event before gap
						shouldCloseEpisode = true
						closeReason = "temporal_gap"
						episodeEndTime = lastEventTime
						a.logger.Debug("Temporal gap detected in same location",
							"location", currentLocation,
							"gap_minutes", int(gap.Minutes()),
							"previous_event", lastEventTime.Format("15:04:05"),
							"current_event", event.Timestamp.Format("15:04:05"))
					}
				}
			}

			// Close previous episode if needed
			if shouldCloseEpisode {
				if err := a.createEpisodeInDB(ctx, currentLocation, episodeStart, episodeEndTime, closeReason); err != nil {
					a.logger.Error("Failed to create episode",
						"location", currentLocation,
						"error", err)
				} else {
					episodesCreated++
					a.logger.Info("Episode created",
						"location", currentLocation,
						"start", episodeStart.Format(time.RFC3339),
						"end", episodeEndTime.Format(time.RFC3339),
						"duration_min", int(episodeEndTime.Sub(episodeStart).Minutes()),
						"reason", closeReason)
				}
			}

			// Start new episode if transitioning or after gap
			if currentLocation == "" || shouldCloseEpisode {
				currentLocation = event.Location
				episodeStart = event.Timestamp
			}

			lastEventTime = event.Timestamp
		} else if event.Type == "lighting" && event.State == "off" && event.Source == "manual" {
			// Manual lighting OFF - explicit episode end for current location
			// Automated lighting OFF events are ignored (status updates, not occupancy changes)
			if currentLocation == event.Location {
				if err := a.createEpisodeInDB(ctx, currentLocation, episodeStart, event.Timestamp, "lighting_off"); err != nil {
					a.logger.Error("Failed to create episode from lighting off",
						"location", currentLocation,
						"error", err)
				} else {
					episodesCreated++
					a.logger.Info("Episode created from manual lighting off",
						"location", currentLocation,
						"start", episodeStart.Format(time.RFC3339),
						"end", event.Timestamp.Format(time.RFC3339),
						"duration_min", int(event.Timestamp.Sub(episodeStart).Minutes()),
						"source", event.Source)
				}
				// Clear current location since episode ended
				currentLocation = ""
				episodeStart = time.Time{}
			}
		}
	}

	// Close final episode if exists
	if currentLocation != "" {
		if err := a.createEpisodeInDB(ctx, currentLocation, episodeStart, virtualNow, "motion_transition"); err != nil {
			a.logger.Error("Failed to create final episode",
				"location", currentLocation,
				"error", err)
		} else {
			episodesCreated++
			a.logger.Info("Final episode created",
				"location", currentLocation,
				"start", episodeStart.Format(time.RFC3339),
				"end", virtualNow.Format(time.RFC3339))
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

// createAnchorsFromEpisodes creates semantic anchors from behavioral episodes
func (a *Agent) createAnchorsFromEpisodes(ctx context.Context, sinceTime time.Time, location string) (int, error) {
	if a.anchorCreator == nil {
		a.logger.Debug("Anchor creator not initialized, skipping anchor creation")
		return 0, nil
	}

	// Query episodes created since sinceTime
	query := `
		SELECT id, jsonld
		FROM behavioral_episodes
		WHERE (jsonld->>'jeeves:startedAt')::timestamptz >= $1
		AND ($2 = 'universe' OR (jsonld->'adl:activity'->'adl:location'->>'name') = $2)
		ORDER BY (jsonld->>'jeeves:startedAt')::timestamptz ASC
	`

	rows, err := a.pgClient.Query(ctx, query, sinceTime, location)
	if err != nil {
		return 0, fmt.Errorf("failed to query episodes: %w", err)
	}
	defer rows.Close()

	anchorsCreated := 0

	for rows.Next() {
		var episodeID string
		var jsonldData []byte

		if err := rows.Scan(&episodeID, &jsonldData); err != nil {
			a.logger.Warn("Failed to scan episode", "error", err)
			continue
		}

		// Parse episode JSON
		var episode map[string]interface{}
		if err := json.Unmarshal(jsonldData, &episode); err != nil {
			a.logger.Warn("Failed to parse episode", "episode_id", episodeID, "error", err)
			continue
		}

		// Extract location and timestamp from episode JSON-LD structure
		// Path: adl:activity -> adl:location -> name
		var locationName string
		if activity, ok := episode["adl:activity"].(map[string]interface{}); ok {
			if location, ok := activity["adl:location"].(map[string]interface{}); ok {
				locationName, _ = location["name"].(string)
			}
		}

		if locationName == "" {
			a.logger.Warn("Episode missing location", "episode_id", episodeID)
			continue
		}

		timestampStr, ok := episode["jeeves:startedAt"].(string)
		if !ok {
			a.logger.Warn("Episode missing timestamp", "episode_id", episodeID)
			continue
		}
		timestamp, err := time.Parse(time.RFC3339, timestampStr)
		if err != nil {
			a.logger.Warn("Failed to parse timestamp", "episode_id", episodeID, "error", err)
			continue
		}

		// Gather sensor signals from Redis for this episode
		signals := a.gatherSignalsForEpisode(ctx, locationName, timestamp)

		if len(signals) == 0 {
			a.logger.Debug("No signals found for episode, skipping anchor",
				"episode_id", episodeID,
				"location", locationName)
			continue
		}

		// Create semantic anchor
		anchor, err := a.anchorCreator.CreateAnchor(ctx, locationName, timestamp, signals)
		if err != nil {
			a.logger.Warn("Failed to create anchor",
				"episode_id", episodeID,
				"location", locationName,
				"error", err)
			continue
		}

		anchorsCreated++
		a.logger.Debug("Anchor created",
			"anchor_id", anchor.ID,
			"episode_id", episodeID,
			"location", locationName,
			"timestamp", timestamp.Format(time.RFC3339))
	}

	if err := rows.Err(); err != nil {
		return anchorsCreated, fmt.Errorf("error iterating episodes: %w", err)
	}

	a.logger.Info("Anchor creation from episodes completed",
		"anchors_created", anchorsCreated,
		"since", sinceTime.Format(time.RFC3339))

	return anchorsCreated, nil
}

// gatherSignalsForEpisode retrieves sensor signals for an episode from Redis
func (a *Agent) gatherSignalsForEpisode(ctx context.Context, location string, timestamp time.Time) []types.ActivitySignal {
	signals := []types.ActivitySignal{}

	// Get motion signal (look back 5 minutes before episode start)
	motionKey := fmt.Sprintf("sensor:motion:%s", location)
	lookback := timestamp.Add(-5 * time.Minute)

	members, err := a.redis.ZRangeByScoreWithScores(ctx, motionKey,
		float64(lookback.UnixMilli()),
		float64(timestamp.UnixMilli()))

	if err == nil && len(members) > 0 {
		signals = append(signals, types.ActivitySignal{
			Type:       "motion",
			Confidence: 0.8,
			Timestamp:  timestamp,
			Value: map[string]interface{}{
				"state": "detected",
			},
		})
	}

	// Get lighting signal
	lightingKey := fmt.Sprintf("sensor:lighting:%s", location)
	members, err = a.redis.ZRangeByScoreWithScores(ctx, lightingKey,
		float64(lookback.UnixMilli()),
		float64(timestamp.UnixMilli()))

	if err == nil && len(members) > 0 {
		// Parse the most recent lighting event
		var lightData map[string]interface{}
		if err := json.Unmarshal([]byte(members[len(members)-1].Member), &lightData); err == nil {
			signals = append(signals, types.ActivitySignal{
				Type:       "lighting",
				Confidence: 0.7,
				Timestamp:  timestamp,
				Value: map[string]interface{}{
					"state":  lightData["state"],
					"source": lightData["source"],
				},
			})
		}
	}

	// Get media signal if available
	mediaKey := fmt.Sprintf("sensor:media:%s", location)
	members, err = a.redis.ZRangeByScoreWithScores(ctx, mediaKey,
		float64(lookback.UnixMilli()),
		float64(timestamp.UnixMilli()))

	if err == nil && len(members) > 0 {
		var mediaData map[string]interface{}
		if err := json.Unmarshal([]byte(members[len(members)-1].Member), &mediaData); err == nil {
			signals = append(signals, types.ActivitySignal{
				Type:       "media",
				Confidence: 0.9,
				Timestamp:  timestamp,
				Value: map[string]interface{}{
					"state":      mediaData["state"],
					"media_type": mediaData["media_type"],
				},
			})
		}
	}

	return signals
}

func (a *Agent) performConsolidation(ctx context.Context, sinceTime time.Time, location string) error {
	a.logger.Info("=== CONSOLIDATION ORCHESTRATION START ===",
		"since", sinceTime.Format(time.RFC3339),
		"location", location,
		"virtual_time", a.timeManager.Now().Format(time.RFC3339))

	// STEP 0: Create episodes from Redis sensor data
	a.logger.Info("--- PHASE 0: EPISODE CREATION FROM SENSORS ---")
	episodesCreated, err := a.createEpisodesFromSensors(ctx, sinceTime, location)
	if err != nil {
		a.logger.Error("Failed to create episodes from sensors", "error", err)
		// Continue anyway - work with existing episodes
	} else {
		a.logger.Info("Episodes created from sensor data",
			"count", episodesCreated,
			"since", sinceTime.Format(time.RFC3339))
	}

	// STEP 0.5: Create semantic anchors from episodes
	if a.anchorCreator != nil {
		a.logger.Info("--- PHASE 0.5: SEMANTIC ANCHOR CREATION ---")
		anchorsCreated, err := a.createAnchorsFromEpisodes(ctx, sinceTime, location)
		if err != nil {
			a.logger.Error("Failed to create anchors from episodes", "error", err)
		} else {
			a.logger.Info("Semantic anchors created successfully",
				"count", anchorsCreated,
				"since", sinceTime.Format(time.RFC3339))
		}
	}

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
