package illuminance

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

// LocationState tracks analysis state for a location
type LocationState struct {
	LastAnalysis time.Time
	CurrentLabel string
}

// Agent represents the illuminance analysis agent
type Agent struct {
	mqtt    mqtt.Client
	redis   redis.Client
	storage *Storage
	cfg     *config.Config
	logger  *slog.Logger

	// In-memory state management
	stateMux sync.RWMutex
	states   map[string]*LocationState

	// Periodic analysis
	ticker   *time.Ticker
	stopChan chan struct{}
}

// NewAgent creates a new illuminance agent
func NewAgent(mqttClient mqtt.Client, redisClient redis.Client, cfg *config.Config, logger *slog.Logger) *Agent {
	storage := NewStorage(redisClient, cfg, logger)

	return &Agent{
		mqtt:     mqttClient,
		redis:    redisClient,
		storage:  storage,
		cfg:      cfg,
		logger:   logger,
		states:   make(map[string]*LocationState),
		stopChan: make(chan struct{}),
	}
}

// Start starts the illuminance agent and begins processing
func (a *Agent) Start(ctx context.Context) error {
	a.logger.Info("Starting illuminance agent",
		"service_name", a.cfg.ServiceName,
		"latitude", a.cfg.Latitude,
		"longitude", a.cfg.Longitude,
		"analysis_interval", a.cfg.AnalysisIntervalSec)

	// Connect to MQTT broker
	if err := a.mqtt.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to MQTT: %w", err)
	}

	// Verify Redis connection
	if err := a.redis.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping Redis: %w", err)
	}

	// Subscribe to illuminance trigger topics
	triggerTopic := "automation/sensor/illuminance/+"
	if err := a.mqtt.Subscribe(triggerTopic, 0, a.handleTrigger); err != nil {
		return fmt.Errorf("failed to subscribe to %s: %w", triggerTopic, err)
	}

	a.logger.Info("Subscribed to trigger topic", "topic", triggerTopic)

	// Start periodic analysis
	a.startPeriodicAnalysis()

	a.logger.Info("Illuminance agent started and ready")

	// Block until context is cancelled
	<-ctx.Done()
	a.logger.Info("Illuminance agent stopping")

	return nil
}

// Stop gracefully stops the illuminance agent
func (a *Agent) Stop() error {
	a.logger.Info("Stopping illuminance agent")

	// Stop periodic analysis
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

	a.logger.Info("Illuminance agent stopped")
	return nil
}

// startPeriodicAnalysis starts the periodic analysis timer
func (a *Agent) startPeriodicAnalysis() {
	interval := time.Duration(a.cfg.AnalysisIntervalSec) * time.Second
	a.ticker = time.NewTicker(interval)

	go func() {
		a.logger.Info("Starting periodic analysis", "interval_sec", a.cfg.AnalysisIntervalSec)
		for {
			select {
			case <-a.ticker.C:
				a.performPeriodicAnalysis()
			case <-a.stopChan:
				return
			}
		}
	}()
}

// performPeriodicAnalysis analyzes all locations with sufficient data
func (a *Agent) performPeriodicAnalysis() {
	ctx := context.Background()

	// Get all locations with illuminance data
	locations, err := a.storage.GetAllLocations(ctx)
	if err != nil {
		a.logger.Error("Failed to get locations", "error", err)
		return
	}

	a.logger.Debug("Performing periodic analysis", "location_count", len(locations))

	for _, location := range locations {
		// Check if recently analyzed (avoid duplicate work < 25s)
		if a.wasRecentlyAnalyzed(location, 25*time.Second) {
			a.logger.Debug("Skipping recently analyzed location",
				"location", location,
				"reason", "analyzed_less_than_25s_ago")
			continue
		}

		// Analyze this location
		a.analyzeLocation(ctx, location, "periodic")
	}
}

// handleTrigger handles MQTT trigger messages
func (a *Agent) handleTrigger(msg mqtt.Message) {
	topic := msg.Topic()

	// Extract location from topic: automation/sensor/illuminance/{location}
	parts := strings.Split(topic, "/")
	if len(parts) != 4 {
		a.logger.Warn("Invalid trigger topic format", "topic", topic)
		return
	}

	location := parts[3]

	a.logger.Debug("Received illuminance trigger", "location", location, "topic", topic)

	// Perform immediate analysis
	ctx := context.Background()
	a.analyzeLocation(ctx, location, "sensor_trigger")
}

// analyzeLocation performs complete analysis for a location
func (a *Agent) analyzeLocation(ctx context.Context, location string, trigger string) {
	// Get data summary from Redis
	summary, err := a.storage.GetIlluminanceSummary(ctx, location)
	if err != nil {
		a.logger.Error("Failed to get illuminance summary",
			"location", location,
			"error", err)
		return
	}

	// Check if we have any data
	if summary.LatestReading == nil {
		a.logger.Debug("No illuminance data available for location",
			"location", location,
			"reason", "no_latest_reading")
		return
	}

	// Log data strategy
	if summary.HasSufficientData {
		a.logger.Debug("Using Redis sensor data for analysis",
			"location", location,
			"reading_count", len(summary.LastHour),
			"data_source", "redis_sensor_data")
	} else {
		a.logger.Debug("Using daylight fallback for analysis",
			"location", location,
			"reading_count", len(summary.LastHour),
			"data_source", "daylight_calculation",
			"reason", "insufficient_data")
	}

	// Generate illuminance abstraction
	abstraction, err := GenerateIlluminanceAbstraction(summary, a.cfg.Latitude, a.cfg.Longitude)
	if err != nil {
		a.logger.Error("Failed to generate abstraction",
			"location", location,
			"error", err)
		return
	}

	// Get or create state for this location
	state := a.getOrCreateState(location)

	// Determine if we should publish
	newLabel := abstraction.Current.Label
	shouldPublish := a.shouldPublish(location, state, newLabel, trigger)

	if shouldPublish {
		// Publish context message
		if err := a.publishContext(location, abstraction); err != nil {
			a.logger.Error("Failed to publish context",
				"location", location,
				"error", err)
			return
		}

		// Update state
		a.updateState(location, newLabel)

		a.logger.Info("Illuminance analysis published",
			"location", location,
			"label", newLabel,
			"lux", fmt.Sprintf("%.1f", abstraction.Current.Lux),
			"trigger", trigger,
			"data_source", abstraction.DataSource)
	} else {
		a.logger.Debug("Skipping publish - no state change and within throttle period",
			"location", location,
			"current_label", newLabel,
			"previous_label", state.CurrentLabel)
	}
}

// getOrCreateState retrieves or creates state for a location
func (a *Agent) getOrCreateState(location string) *LocationState {
	a.stateMux.Lock()
	defer a.stateMux.Unlock()

	if state, exists := a.states[location]; exists {
		return state
	}

	// Create new state
	state := &LocationState{
		LastAnalysis: time.Time{},
		CurrentLabel: "",
	}
	a.states[location] = state

	return state
}

// updateState updates the state for a location
func (a *Agent) updateState(location string, newLabel string) {
	a.stateMux.Lock()
	defer a.stateMux.Unlock()

	if state, exists := a.states[location]; exists {
		state.LastAnalysis = time.Now()
		state.CurrentLabel = newLabel
	}
}

// wasRecentlyAnalyzed checks if a location was analyzed within the given duration
func (a *Agent) wasRecentlyAnalyzed(location string, within time.Duration) bool {
	a.stateMux.RLock()
	defer a.stateMux.RUnlock()

	if state, exists := a.states[location]; exists {
		return time.Since(state.LastAnalysis) < within
	}

	return false
}

// shouldPublish determines if a context message should be published
func (a *Agent) shouldPublish(location string, state *LocationState, newLabel string, trigger string) bool {
	// Condition 1: State changed (label changed)
	if state.CurrentLabel != newLabel && state.CurrentLabel != "" {
		a.logger.Debug("Publishing due to state change",
			"location", location,
			"old_label", state.CurrentLabel,
			"new_label", newLabel)
		return true
	}

	// Condition 2: Periodic update needed (> 5 minutes since last)
	if !state.LastAnalysis.IsZero() && time.Since(state.LastAnalysis) > 5*time.Minute {
		a.logger.Debug("Publishing due to periodic update",
			"location", location,
			"time_since_last", time.Since(state.LastAnalysis))
		return true
	}

	// Condition 3: Sensor trigger (immediate analysis)
	if trigger == "sensor_trigger" {
		a.logger.Debug("Publishing due to sensor trigger", "location", location)
		return true
	}

	// First time analyzing this location
	if state.CurrentLabel == "" {
		a.logger.Debug("Publishing initial state", "location", location)
		return true
	}

	return false
}

// publishContext publishes the illuminance context message to MQTT
func (a *Agent) publishContext(location string, abstraction *IlluminanceAbstraction) error {
	// Build context message
	contextMsg := map[string]interface{}{
		"source":    "illuminance-agent",
		"type":      "illuminance",
		"location":  location,
		"state":     abstraction.Current.Label,
		"message":   fmt.Sprintf("Illuminance is %s (%.0f lux)", abstraction.Current.Label, abstraction.Current.Lux),
		"data": map[string]interface{}{
			"current_lux":             abstraction.Current.Lux,
			"current_label":           abstraction.Current.Label,
			"trend":                   abstraction.TemporalAnalysis.Trend10Min,
			"stability":               abstraction.TemporalAnalysis.Stability,
			"avg_2min":                abstraction.Statistics.Avg2Min,
			"avg_10min":               abstraction.Statistics.Avg10Min,
			"min_10min":               abstraction.Statistics.Min10Min,
			"max_10min":               abstraction.Statistics.Max10Min,
			"relative_to_typical":     abstraction.Context.RelativeToTypical,
			"likely_sources":          abstraction.Context.LikelySources,
			"is_daytime":              abstraction.Daylight.IsDaytime,
			"theoretical_outdoor_lux": abstraction.Daylight.TheoreticalOutdoorLux,
			"time_of_day":             abstraction.Context.TimeOfDay,
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}

	// Serialize to JSON
	payload, err := json.Marshal(contextMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal context message: %w", err)
	}

	// Publish to MQTT
	topic := fmt.Sprintf("automation/context/illuminance/%s", location)
	if err := a.mqtt.Publish(topic, 0, false, payload); err != nil {
		return fmt.Errorf("failed to publish to MQTT: %w", err)
	}

	a.logger.Debug("Published context message",
		"topic", topic,
		"label", abstraction.Current.Label,
		"lux", abstraction.Current.Lux)

	return nil
}

// GetStateCount returns the number of tracked locations (for health check)
func (a *Agent) GetStateCount() int {
	a.stateMux.RLock()
	defer a.stateMux.RUnlock()
	return len(a.states)
}
