package light

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/saaga0h/jeeves-platform/pkg/config"
	"github.com/saaga0h/jeeves-platform/pkg/mqtt"
	"github.com/saaga0h/jeeves-platform/pkg/redis"
)

// LocationContext tracks occupancy state and metadata for a location
type LocationContext struct {
	OccupancyState      string
	OccupancyConfidence float64
	LastUpdate          time.Time
}

// Agent represents the light automation agent
type Agent struct {
	mqtt     mqtt.Client
	redis    redis.Client
	cfg      *config.Config
	logger   *slog.Logger
	analyzer *IlluminanceAnalyzer

	// State management
	contextMux       sync.RWMutex
	locationContexts map[string]*LocationContext

	overrideManager *OverrideManager
	rateLimiter     *RateLimiter

	// Periodic decision loop
	ticker   *time.Ticker
	stopChan chan struct{}
}

// NewAgent creates a new light agent
func NewAgent(mqttClient mqtt.Client, redisClient redis.Client, cfg *config.Config, logger *slog.Logger) *Agent {
	analyzer := NewIlluminanceAnalyzer(redisClient, cfg, logger)

	return &Agent{
		mqtt:             mqttClient,
		redis:            redisClient,
		cfg:              cfg,
		logger:           logger,
		analyzer:         analyzer,
		locationContexts: make(map[string]*LocationContext),
		overrideManager:  NewOverrideManager(),
		rateLimiter:      NewRateLimiter(),
		stopChan:         make(chan struct{}),
	}
}

// Start starts the light agent and begins processing
func (a *Agent) Start(ctx context.Context) error {
	a.logger.Info("Starting light agent",
		"service_name", a.cfg.ServiceName,
		"decision_interval_sec", a.cfg.DecisionIntervalSec,
		"manual_override_minutes", a.cfg.ManualOverrideMinutes,
		"min_decision_interval_ms", a.cfg.MinDecisionIntervalMs)

	// Connect to MQTT broker
	if err := a.mqtt.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to MQTT: %w", err)
	}

	// Verify Redis connection
	if err := a.redis.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping Redis: %w", err)
	}

	// Subscribe to occupancy context
	occupancyTopic := "automation/context/occupancy/+"
	if err := a.mqtt.Subscribe(occupancyTopic, 0, a.handleOccupancyMessage); err != nil {
		return fmt.Errorf("failed to subscribe to %s: %w", occupancyTopic, err)
	}
	a.logger.Info("Subscribed to occupancy context", "topic", occupancyTopic)

	// Subscribe to illuminance context (for future use / logging)
	illuminanceTopic := "automation/context/illuminance/+"
	if err := a.mqtt.Subscribe(illuminanceTopic, 0, a.handleIlluminanceMessage); err != nil {
		return fmt.Errorf("failed to subscribe to %s: %w", illuminanceTopic, err)
	}
	a.logger.Info("Subscribed to illuminance context", "topic", illuminanceTopic)

	// Start periodic decision loop
	a.startPeriodicDecisionLoop()

	a.logger.Info("Light agent started and ready")

	// Block until context is cancelled
	<-ctx.Done()
	a.logger.Info("Light agent stopping")

	return nil
}

// Stop gracefully stops the light agent
func (a *Agent) Stop() error {
	a.logger.Info("Stopping light agent")

	// Stop periodic decision loop
	if a.ticker != nil {
		a.ticker.Stop()
	}
	close(a.stopChan)

	// Disconnect from MQTT
	a.mqtt.Disconnect()

	// Close Redis connection
	if err := a.redis.Close(); err != nil {
		a.logger.Error("Error closing Redis connection", "error", err)
		return err
	}

	a.logger.Info("Light agent stopped")
	return nil
}

// startPeriodicDecisionLoop starts the periodic decision evaluation
func (a *Agent) startPeriodicDecisionLoop() {
	interval := time.Duration(a.cfg.DecisionIntervalSec) * time.Second
	a.ticker = time.NewTicker(interval)

	go func() {
		a.logger.Info("Starting periodic decision loop", "interval_sec", a.cfg.DecisionIntervalSec)
		for {
			select {
			case <-a.ticker.C:
				a.performPeriodicDecisions()
			case <-a.stopChan:
				return
			}
		}
	}()
}

// performPeriodicDecisions evaluates all tracked locations
func (a *Agent) performPeriodicDecisions() {
	ctx := context.Background()

	// Get all locations we're tracking
	a.contextMux.RLock()
	locations := make([]string, 0, len(a.locationContexts))
	for location := range a.locationContexts {
		locations = append(locations, location)
	}
	a.contextMux.RUnlock()

	a.logger.Debug("Performing periodic decisions", "location_count", len(locations))

	for _, location := range locations {
		// Get context
		a.contextMux.RLock()
		context, exists := a.locationContexts[location]
		a.contextMux.RUnlock()

		if !exists {
			continue
		}

		// Evaluate lighting need with rate limiting
		a.evaluateLightingNeed(ctx, location, context.OccupancyState, context.OccupancyConfidence, false)
	}

	// Cleanup expired overrides periodically
	cleaned := a.overrideManager.CleanupExpiredOverrides()
	if cleaned > 0 {
		a.logger.Debug("Cleaned up expired overrides", "count", cleaned)
	}
}

// handleOccupancyMessage handles incoming occupancy context messages
func (a *Agent) handleOccupancyMessage(msg mqtt.Message) {
	topic := msg.Topic()
	payload := msg.Payload()

	// Extract location from topic: automation/context/occupancy/{location}
	parts := strings.Split(topic, "/")
	if len(parts) != 4 {
		a.logger.Warn("Invalid occupancy topic format", "topic", topic)
		return
	}
	location := parts[3]

	// Parse message
	var occupancyMsg struct {
		State      string  `json:"state"`
		Confidence float64 `json:"confidence"`
		Timestamp  string  `json:"timestamp"`
	}

	if err := json.Unmarshal(payload, &occupancyMsg); err != nil {
		a.logger.Error("Failed to parse occupancy message",
			"location", location,
			"error", err)
		return
	}

	a.logger.Debug("Received occupancy context",
		"location", location,
		"state", occupancyMsg.State,
		"confidence", occupancyMsg.Confidence)

	// Check if state changed
	stateChanged := false
	a.contextMux.RLock()
	if prevContext, exists := a.locationContexts[location]; exists {
		stateChanged = prevContext.OccupancyState != occupancyMsg.State
	} else {
		// First time seeing this location - consider it a state change
		stateChanged = true
	}
	a.contextMux.RUnlock()

	// Update context
	a.contextMux.Lock()
	a.locationContexts[location] = &LocationContext{
		OccupancyState:      occupancyMsg.State,
		OccupancyConfidence: occupancyMsg.Confidence,
		LastUpdate:          time.Now(),
	}
	a.contextMux.Unlock()

	// If state changed, trigger immediate decision (force bypass rate limit)
	if stateChanged {
		a.logger.Info("Occupancy state changed, triggering immediate decision",
			"location", location,
			"new_state", occupancyMsg.State,
			"confidence", occupancyMsg.Confidence)

		ctx := context.Background()
		a.evaluateLightingNeed(ctx, location, occupancyMsg.State, occupancyMsg.Confidence, true)
	}
}

// handleIlluminanceMessage handles incoming illuminance context messages
// Currently just logs - illuminance data is read from Redis instead
func (a *Agent) handleIlluminanceMessage(msg mqtt.Message) {
	topic := msg.Topic()
	payload := msg.Payload()

	// Extract location from topic
	parts := strings.Split(topic, "/")
	if len(parts) != 4 {
		a.logger.Warn("Invalid illuminance topic format", "topic", topic)
		return
	}
	location := parts[3]

	// Parse message for logging
	var illuminanceMsg struct {
		State string                 `json:"state"`
		Data  map[string]interface{} `json:"data"`
	}

	if err := json.Unmarshal(payload, &illuminanceMsg); err != nil {
		a.logger.Debug("Received illuminance context (unparsed)",
			"location", location,
			"topic", topic)
		return
	}

	a.logger.Debug("Received illuminance context",
		"location", location,
		"state", illuminanceMsg.State)
	// Note: We don't take action here - illuminance is read from Redis during decision making
}

// evaluateLightingNeed makes a lighting decision and publishes if needed
func (a *Agent) evaluateLightingNeed(ctx context.Context, location string, occupancyState string, occupancyConfidence float64, forceDecision bool) {
	// Check rate limiting (unless forced)
	if !forceDecision {
		if !a.rateLimiter.ShouldMakeDecision(location, a.cfg.MinDecisionIntervalMs) {
			a.logger.Debug("Rate limited, skipping decision",
				"location", location,
				"min_interval_ms", a.cfg.MinDecisionIntervalMs)
			return
		}
	} else {
		// Record the forced decision to reset rate limit timer
		a.rateLimiter.RecordDecision(location)
	}

	// Make lighting decision
	decision := MakeLightingDecision(
		ctx,
		location,
		occupancyState,
		occupancyConfidence,
		a.analyzer,
		a.overrideManager,
		a.logger,
	)

	// If action is "maintain", don't publish anything
	if decision.Action == "maintain" {
		a.logger.Debug("Decision is maintain, no command published",
			"location", location,
			"reason", decision.Reason)
		return
	}

	// Publish lighting command
	if err := a.publishLightingCommand(location, decision); err != nil {
		a.logger.Error("Failed to publish lighting command",
			"location", location,
			"error", err)
		return
	}

	a.logger.Info("Lighting decision published",
		"location", location,
		"action", decision.Action,
		"brightness", decision.Brightness,
		"color_temp", decision.ColorTemp,
		"reason", decision.Reason,
		"confidence", decision.Confidence)
}

// publishLightingCommand publishes both command and context messages
func (a *Agent) publishLightingCommand(location string, decision *Decision) error {
	timestamp := time.Now().Format(time.RFC3339)

	// Build command message
	commandMsg := map[string]interface{}{
		"action":     decision.Action,
		"brightness": decision.Brightness,
		"reason":     decision.Reason,
		"confidence": decision.Confidence,
		"timestamp":  timestamp,
	}

	// Include color_temp only if non-zero
	if decision.ColorTemp > 0 {
		commandMsg["color_temp"] = decision.ColorTemp
	} else {
		commandMsg["color_temp"] = nil
	}

	// Publish command
	commandTopic := fmt.Sprintf("automation/command/light/%s", location)
	commandPayload, err := json.Marshal(commandMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal command message: %w", err)
	}

	if err := a.mqtt.Publish(commandTopic, 0, false, commandPayload); err != nil {
		return fmt.Errorf("failed to publish command to %s: %w", commandTopic, err)
	}

	a.logger.Debug("Published lighting command", "topic", commandTopic)

	// Build context message
	contextMsg := map[string]interface{}{
		"source":     "light-agent",
		"type":       "lighting",
		"location":   location,
		"state":      decision.Action,
		"brightness": decision.Brightness,
		"reason":     decision.Reason,
		"confidence": decision.Confidence,
		"timestamp":  timestamp,
	}

	// Include color_temp only if non-zero
	if decision.ColorTemp > 0 {
		contextMsg["color_temp"] = decision.ColorTemp
	} else {
		contextMsg["color_temp"] = nil
	}

	// Add illuminating flag for "on" state
	if decision.Action == "on" {
		contextMsg["illuminating"] = true
		contextMsg["automated"] = true
	}

	// Publish context
	contextTopic := fmt.Sprintf("automation/context/lighting/%s", location)
	contextPayload, err := json.Marshal(contextMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal context message: %w", err)
	}

	if err := a.mqtt.Publish(contextTopic, 0, false, contextPayload); err != nil {
		return fmt.Errorf("failed to publish context to %s: %w", contextTopic, err)
	}

	a.logger.Debug("Published lighting context", "topic", contextTopic)

	return nil
}

// GetLocationCount returns the number of tracked locations (for health check)
func (a *Agent) GetLocationCount() int {
	a.contextMux.RLock()
	defer a.contextMux.RUnlock()
	return len(a.locationContexts)
}

// GetOverrideManager returns the override manager for API access
func (a *Agent) GetOverrideManager() *OverrideManager {
	return a.overrideManager
}

// GetLocationContext returns the context for a specific location
func (a *Agent) GetLocationContext(location string) (*LocationContext, bool) {
	a.contextMux.RLock()
	defer a.contextMux.RUnlock()
	context, exists := a.locationContexts[location]
	return context, exists
}

// ForceDecision forces an immediate decision for a location (for API)
func (a *Agent) ForceDecision(location string) (*Decision, error) {
	ctx := context.Background()

	// Get context
	a.contextMux.RLock()
	locationContext, exists := a.locationContexts[location]
	a.contextMux.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no context available for location: %s", location)
	}

	// Make decision
	decision := MakeLightingDecision(
		ctx,
		location,
		locationContext.OccupancyState,
		locationContext.OccupancyConfidence,
		a.analyzer,
		a.overrideManager,
		a.logger,
	)

	// Publish if action is not maintain
	if decision.Action != "maintain" {
		if err := a.publishLightingCommand(location, decision); err != nil {
			return decision, fmt.Errorf("failed to publish command: %w", err)
		}
	}

	return decision, nil
}
