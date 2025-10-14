package occupancy

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/saaga0h/jeeves-platform/pkg/config"
	"github.com/saaga0h/jeeves-platform/pkg/mqtt"
	"github.com/saaga0h/jeeves-platform/pkg/redis"
)

// Agent represents the occupancy analysis agent
type Agent struct {
	mqtt    mqtt.Client
	redis   redis.Client
	storage *Storage
	cfg     *config.Config
	logger  *slog.Logger

	// Periodic analysis
	ticker   *time.Ticker
	stopChan chan struct{}
}

// NewAgent creates a new occupancy agent
func NewAgent(mqttClient mqtt.Client, redisClient redis.Client, cfg *config.Config, logger *slog.Logger) *Agent {
	storage := NewStorage(redisClient, cfg, logger)

	return &Agent{
		mqtt:     mqttClient,
		redis:    redisClient,
		storage:  storage,
		cfg:      cfg,
		logger:   logger,
		stopChan: make(chan struct{}),
	}
}

// Start starts the occupancy agent and begins processing
func (a *Agent) Start(ctx context.Context) error {
	a.logger.Info("Starting occupancy agent",
		"service_name", a.cfg.ServiceName,
		"analysis_interval", a.cfg.OccupancyAnalysisIntervalSec)

	// Connect to MQTT broker
	if err := a.mqtt.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to MQTT: %w", err)
	}

	// Verify Redis connection
	if err := a.redis.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping Redis: %w", err)
	}

	// Subscribe to motion trigger topics
	triggerTopic := "automation/sensor/motion/+"
	if err := a.mqtt.Subscribe(triggerTopic, 0, a.handleTrigger); err != nil {
		return fmt.Errorf("failed to subscribe to %s: %w", triggerTopic, err)
	}

	a.logger.Info("Subscribed to trigger topic", "topic", triggerTopic)

	// Start periodic analysis
	a.startPeriodicAnalysis()

	a.logger.Info("Occupancy agent started and ready")

	// Block until context is cancelled
	<-ctx.Done()
	a.logger.Info("Occupancy agent stopping")

	return nil
}

// Stop gracefully stops the occupancy agent
func (a *Agent) Stop() error {
	a.logger.Info("Stopping occupancy agent")

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

	a.logger.Info("Occupancy agent stopped")
	return nil
}

// startPeriodicAnalysis starts the periodic analysis timer
func (a *Agent) startPeriodicAnalysis() {
	interval := time.Duration(a.cfg.OccupancyAnalysisIntervalSec) * time.Second
	a.ticker = time.NewTicker(interval)

	go func() {
		a.logger.Info("Starting periodic occupancy analysis", "interval_sec", a.cfg.OccupancyAnalysisIntervalSec)
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

// performPeriodicAnalysis analyzes all locations with motion history
func (a *Agent) performPeriodicAnalysis() {
	ctx := context.Background()

	// Get all locations with motion data
	locations, err := a.storage.GetAllLocations(ctx)
	if err != nil {
		a.logger.Error("Failed to get locations", "error", err)
		return
	}

	a.logger.Debug("Performing periodic occupancy analysis", "location_count", len(locations))

	for _, location := range locations {
		// Check if location has motion history
		if !a.storage.HasMotionHistory(ctx, location) {
			a.logger.Debug("Skipping location without motion history", "location", location)
			continue
		}

		// Get temporal state
		state, err := a.storage.GetTemporalState(ctx, location)
		if err != nil {
			a.logger.Warn("Failed to get temporal state", "location", location, "error", err)
			continue
		}

		// Check rate limiting (skip if analyzed < 25s ago)
		if state.LastAnalysis != nil && time.Since(*state.LastAnalysis) < 25*time.Second {
			a.logger.Debug("Skipping recently analyzed location",
				"location", location,
				"reason", "analyzed_less_than_25s_ago")
			continue
		}

		// Update last analysis timestamp
		if err := a.storage.UpdateLastAnalysis(ctx, location); err != nil {
			a.logger.Warn("Failed to update last analysis", "location", location, "error", err)
		}

		// Analyze this location
		a.analyzeLocation(ctx, location, "vonich_hakim_stabilized")
	}
}

// handleTrigger handles MQTT motion trigger messages
func (a *Agent) handleTrigger(msg mqtt.Message) {
	topic := msg.Topic()

	// Extract location from topic: automation/sensor/motion/{location}
	parts := strings.Split(topic, "/")
	if len(parts) != 4 {
		a.logger.Warn("Invalid trigger topic format", "topic", topic)
		return
	}

	location := parts[3]

	a.logger.Debug("Received motion trigger", "location", location, "topic", topic)

	ctx := context.Background()

	// Check for recent motion (< 2 minutes)
	now := time.Now()
	recentMotionCount, err := a.storage.GetMotionCountInWindow(ctx, location, now.Add(-Window2Min), now)
	if err != nil {
		a.logger.Warn("Failed to check recent motion", "location", location, "error", err)
		return
	}

	// If no recent motion, ignore trigger
	if recentMotionCount == 0 {
		a.logger.Debug("Ignoring trigger - no recent motion (< 2 min)", "location", location)
		return
	}

	// Get current occupancy state
	state, err := a.storage.GetTemporalState(ctx, location)
	if err != nil {
		a.logger.Warn("Failed to get temporal state", "location", location, "error", err)
		// Continue with analysis anyway
		state = &TemporalState{}
	}

	// FAST PATH: If currentOccupancy is null AND recent motion exists
	if state.CurrentOccupancy == nil && recentMotionCount > 0 {
		a.logger.Info("Fast path triggered - initial motion detected",
			"location", location,
			"motion_count", recentMotionCount)

		// Fast path: occupied=true, confidence=0.9, skip LLM
		result := AnalysisResult{
			Occupied:   true,
			Confidence: 0.9,
			Reasoning:  fmt.Sprintf("Initial motion detected (%d events in last 2 minutes)", recentMotionCount),
		}

		// Create prediction record
		prediction := PredictionRecord{
			Timestamp:            now,
			Occupied:             result.Occupied,
			Confidence:           result.Confidence,
			Reasoning:            result.Reasoning,
			StabilizationApplied: false,
		}

		// Add to history
		if err := a.storage.AddPredictionHistory(ctx, location, prediction); err != nil {
			a.logger.Warn("Failed to add prediction to history", "location", location, "error", err)
		}

		// Update occupancy
		if err := a.storage.UpdateOccupancy(ctx, location, result.Occupied); err != nil {
			a.logger.Error("Failed to update occupancy", "location", location, "error", err)
			return
		}

		// Publish context
		minutesSince, _ := a.storage.GetMinutesSinceLastMotion(ctx, location, now)
		if err := a.publishContext(location, result, "initial_motion", minutesSince, recentMotionCount, 0); err != nil {
			a.logger.Error("Failed to publish context", "location", location, "error", err)
			return
		}

		a.logger.Info("Fast path occupancy published",
			"location", location,
			"occupied", result.Occupied,
			"confidence", result.Confidence)

		return
	}

	// FULL ANALYSIS PATH
	a.logger.Debug("Running full analysis for motion trigger", "location", location)
	a.analyzeLocation(ctx, location, "immediate_vonich_hakim_analysis")
}

// analyzeLocation performs complete occupancy analysis for a location
func (a *Agent) analyzeLocation(ctx context.Context, location string, method string) {
	now := time.Now()

	a.logger.Info("analyzeLocation: STARTING analysis",
		"location", location,
		"method", method,
		"timestamp", now.Format(time.RFC3339))

	// Generate temporal abstraction
	abstraction, err := GenerateTemporalAbstraction(ctx, location, a.storage, now)
	if err != nil {
		a.logger.Error("Failed to generate temporal abstraction",
			"location", location,
			"error", err)
		return
	}

	// Get temporal state
	state, err := a.storage.GetTemporalState(ctx, location)
	if err != nil {
		a.logger.Warn("Failed to get temporal state", "location", location, "error", err)
		state = &TemporalState{}
	}

	// Compute Vonich-Hakim stabilization
	stabilization := ComputeVonichHakimStabilization(state.PredictionHistory)

	a.logger.Debug("Stabilization computed",
		"location", location,
		"factor", stabilization.StabilizationFactor,
		"should_dampen", stabilization.ShouldDampen,
		"recommendation", stabilization.Recommendation)

	// Analyze with LLM (with fallback)
	result := AnalyzeWithFallback(ctx, location, abstraction, stabilization, a.cfg, a.logger)

	a.logger.Info("analyzeLocation: Analysis complete",
		"location", location,
		"occupied", result.Occupied,
		"confidence", result.Confidence,
		"reasoning", result.Reasoning)

	// Apply gates
	shouldUpdate := ShouldUpdateOccupancy(
		state.CurrentOccupancy,
		state.LastStateChange,
		result,
		stabilization,
	)

	a.logger.Info("analyzeLocation: Gate decision",
		"location", location,
		"should_update", shouldUpdate,
		"current_occupancy", state.CurrentOccupancy,
		"new_occupied", result.Occupied,
		"confidence", result.Confidence)

	if shouldUpdate {
		// Create prediction record
		prediction := PredictionRecord{
			Timestamp:            now,
			Occupied:             result.Occupied,
			Confidence:           result.Confidence,
			Reasoning:            result.Reasoning,
			StabilizationApplied: stabilization.ShouldDampen,
		}

		// Add to prediction history
		if err := a.storage.AddPredictionHistory(ctx, location, prediction); err != nil {
			a.logger.Warn("Failed to add prediction to history", "location", location, "error", err)
		}

		// Update occupancy state
		if err := a.storage.UpdateOccupancy(ctx, location, result.Occupied); err != nil {
			a.logger.Error("Failed to update occupancy", "location", location, "error", err)
			return
		}

		// Publish context message
		minutesSince := abstraction.CurrentState.MinutesSinceLastMotion
		motion2Min := abstraction.MotionDensity.Last2Min
		motion8Min := abstraction.MotionDensity.Last8Min

		if err := a.publishContext(location, result, method, minutesSince, motion2Min, motion8Min); err != nil {
			a.logger.Error("Failed to publish context", "location", location, "error", err)
			return
		}

		a.logger.Info("Occupancy analysis published",
			"location", location,
			"occupied", result.Occupied,
			"confidence", result.Confidence,
			"method", method)
	} else {
		a.logger.Debug("Occupancy update blocked by gates",
			"location", location,
			"occupied", result.Occupied,
			"confidence", result.Confidence,
			"current_occupancy", state.CurrentOccupancy,
			"stabilization_dampening", stabilization.ShouldDampen)
	}
}

// publishContext publishes the occupancy context message to MQTT
func (a *Agent) publishContext(
	location string,
	result AnalysisResult,
	method string,
	minutesSinceMotion float64,
	motion2Min int,
	motion8Min int,
) error {
	// Determine state string
	state := "empty"
	if result.Occupied {
		state = "occupied"
	}

	// Build message string
	message := fmt.Sprintf("Room is %s (confidence: %.2f)", state, result.Confidence)

	// Build context message
	contextMsg := map[string]interface{}{
		"source":    "temporal-occupancy-agent",
		"type":      "occupancy",
		"location":  location,
		"state":     state,
		"message":   message,
		"data": map[string]interface{}{
			"occupied":             result.Occupied,
			"confidence":           result.Confidence,
			"reasoning":            result.Reasoning,
			"method":               method,
			"minutes_since_motion": minutesSinceMotion,
			"motion_last_2min":     motion2Min,
			"motion_last_8min":     motion8Min,
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}

	// Serialize to JSON
	payload, err := json.Marshal(contextMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal context message: %w", err)
	}

	// Publish to MQTT
	topic := fmt.Sprintf("automation/context/occupancy/%s", location)
	if err := a.mqtt.Publish(topic, 0, false, payload); err != nil {
		return fmt.Errorf("failed to publish to MQTT: %w", err)
	}

	a.logger.Debug("Published context message",
		"topic", topic,
		"state", state,
		"confidence", result.Confidence)

	return nil
}
