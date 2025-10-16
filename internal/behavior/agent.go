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
		timeManager:        NewTimeManager(logger), // NEW
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
		"automation/context/lighting/+",
		"automation/media/+/+", // New: media events
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

	// NEW: Start automatic consolidation job
	go a.runConsolidationJob(ctx)

	// Block until context cancelled
	<-ctx.Done()
	return nil
}

func (a *Agent) Stop() error {
	a.logger.Info("Stopping behavior agent")
	a.mqtt.Disconnect()
	return a.pgClient.Disconnect()
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
		defer a.stateMux.Unlock()

		previousState := a.lastOccupancyState[location]
		currentState := simple.State

		a.lastOccupancyState[location] = currentState

		a.logger.Debug("Occupancy state update",
			"location", location,
			"previous", previousState,
			"current", currentState,
			"transition", fmt.Sprintf("%s→%s", previousState, currentState))

		// Detect transitions
		if previousState != "occupied" && currentState == "occupied" {
			a.startEpisode(location)
		}

		if previousState == "occupied" && currentState == "empty" {
			a.endEpisode(location, "occupancy_empty")
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
		defer a.stateMux.Unlock()

		previousState := a.lastOccupancyState[location]
		currentState := "empty"
		if nested.Data.Occupied {
			currentState = "occupied"
		}

		a.lastOccupancyState[location] = currentState

		// Detect transitions
		if previousState != "occupied" && currentState == "occupied" {
			a.startEpisode(location)
		}

		if previousState == "occupied" && currentState == "empty" {
			a.endEpisode(location, "occupancy_empty")
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
		"rule_based_macros", len(ruleMacros),
		"total_macros_created", totalMacrosCreated)

	a.publishConsolidationResult(totalMacrosCreated, len(episodes))

	return nil
}

// Stubs for other handlers (implement in later iterations)
func (a *Agent) handleLightingMessage(msg mqtt.Message) {
	// TODO: Track manual adjustments
}

func (a *Agent) handleMediaMessage(msg mqtt.Message) {
	// TODO: Detect media activity for inference
}
