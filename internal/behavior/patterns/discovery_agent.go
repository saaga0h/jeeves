package patterns

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/saaga0h/jeeves-platform/internal/behavior/clustering"
	"github.com/saaga0h/jeeves-platform/internal/behavior/storage"
	"github.com/saaga0h/jeeves-platform/pkg/mqtt"
)

// DiscoveryConfig configures pattern discovery behavior
type DiscoveryConfig struct {
	Interval      time.Duration // production: 24h, tests: triggered
	MinAnchors    int           // minimum anchors needed (default: 10)
	LookbackHours int           // how far back to analyze
}

// DiscoveryAgent orchestrates clustering and pattern interpretation
type DiscoveryAgent struct {
	config      DiscoveryConfig
	storage     *storage.AnchorStorage
	clustering  *clustering.ClusteringEngine
	interpreter *PatternInterpreter
	mqtt        mqtt.Client
	logger      *slog.Logger

	// Test mode support
	testMode     bool
	testTriggers chan TriggerEvent
}

// TriggerEvent represents a manual trigger for pattern discovery
type TriggerEvent struct {
	MinAnchors    int
	LookbackHours int
}

// NewDiscoveryAgent creates a new pattern discovery agent
func NewDiscoveryAgent(
	config DiscoveryConfig,
	storage *storage.AnchorStorage,
	clustering *clustering.ClusteringEngine,
	interpreter *PatternInterpreter,
	mqttClient mqtt.Client,
	logger *slog.Logger,
) *DiscoveryAgent {
	return &DiscoveryAgent{
		config:       config,
		storage:      storage,
		clustering:   clustering,
		interpreter:  interpreter,
		mqtt:         mqttClient,
		logger:       logger,
		testTriggers: make(chan TriggerEvent, 10),
	}
}

// EnableTestMode switches to test mode (trigger-based instead of interval-based)
func (a *DiscoveryAgent) EnableTestMode() {
	a.testMode = true
}

// Start begins the pattern discovery agent
func (a *DiscoveryAgent) Start(ctx context.Context) error {
	// Subscribe to trigger events for test mode
	if err := a.mqtt.Subscribe("automation/behavior/discover_patterns", 0, a.handleTrigger); err != nil {
		return fmt.Errorf("failed to subscribe to triggers: %w", err)
	}

	if a.testMode {
		// Test mode: wait for explicit triggers
		a.logger.Info("Pattern discovery agent running in test mode")
		for {
			select {
			case trigger := <-a.testTriggers:
				if err := a.discoverPatterns(ctx, trigger.MinAnchors, trigger.LookbackHours); err != nil {
					a.logger.Error("Pattern discovery failed", "error", err)
				}
			case <-ctx.Done():
				return nil
			}
		}
	}

	// Production mode: periodic execution
	a.logger.Info("Pattern discovery agent running in production mode",
		"interval", a.config.Interval)

	ticker := time.NewTicker(a.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := a.discoverPatterns(ctx, a.config.MinAnchors, a.config.LookbackHours); err != nil {
				a.logger.Error("Pattern discovery failed", "error", err)
			}
		case <-ctx.Done():
			return nil
		}
	}
}

func (a *DiscoveryAgent) handleTrigger(msg mqtt.Message) {
	var trigger struct {
		MinAnchors    int `json:"min_anchors"`
		LookbackHours int `json:"lookback_hours"`
	}

	if err := json.Unmarshal(msg.Payload(), &trigger); err != nil {
		a.logger.Error("Failed to parse trigger", "error", err)
		return
	}

	a.logger.Info("Received pattern discovery trigger",
		"topic", msg.Topic(),
		"min_anchors", trigger.MinAnchors,
		"lookback_hours", trigger.LookbackHours)

	a.testTriggers <- TriggerEvent{
		MinAnchors:    trigger.MinAnchors,
		LookbackHours: trigger.LookbackHours,
	}
}

// discoverPatterns performs pattern discovery from recent anchors
func (a *DiscoveryAgent) discoverPatterns(ctx context.Context, minAnchors, lookbackHours int) error {
	startTime := time.Now()

	a.logger.Info("Starting pattern discovery",
		"min_anchors", minAnchors,
		"lookback_hours", lookbackHours)

	// Get recent anchors with computed distances
	since := time.Now().Add(-time.Duration(lookbackHours) * time.Hour)
	anchors, err := a.storage.GetAnchorsWithDistances(ctx, since)
	if err != nil {
		return fmt.Errorf("failed to get anchors: %w", err)
	}

	if len(anchors) < minAnchors {
		a.logger.Info("Insufficient anchors for pattern discovery",
			"found", len(anchors),
			"required", minAnchors)
		a.publishCompletion(0)
		return nil
	}

	a.logger.Info("Clustering anchors", "count", len(anchors))

	// Extract anchor IDs
	anchorIDs := make([]uuid.UUID, len(anchors))
	for i, anchor := range anchors {
		anchorIDs[i] = anchor.ID
	}

	// Perform clustering
	clusters, err := a.clustering.ClusterAnchors(ctx, anchorIDs)
	if err != nil {
		return fmt.Errorf("clustering failed: %w", err)
	}

	// Filter out noise cluster and small clusters
	var validClusters []*clustering.Cluster
	for _, cluster := range clusters {
		if !cluster.Noise && len(cluster.Members) >= minAnchors {
			validClusters = append(validClusters, cluster)
		}
	}

	a.logger.Info("Valid clusters found", "count", len(validClusters))

	if len(validClusters) == 0 {
		a.logger.Info("No valid clusters found")
		a.publishCompletion(0)
		return nil
	}

	// Interpret each cluster as a pattern
	patternsCreated := 0

	for _, cluster := range validClusters {
		pattern, err := a.interpreter.InterpretCluster(ctx, cluster.Members)
		if err != nil {
			a.logger.Error("Failed to interpret cluster",
				"cluster_id", cluster.ID,
				"error", err)
			continue
		}

		// Store pattern
		if err := a.storage.CreatePattern(ctx, pattern); err != nil {
			a.logger.Error("Failed to store pattern",
				"pattern_id", pattern.ID,
				"error", err)
			continue
		}

		// Update anchors to reference this pattern
		for _, anchorID := range cluster.Members {
			if err := a.storage.UpdateAnchorPattern(ctx, anchorID, pattern.ID); err != nil {
				a.logger.Warn("Failed to update anchor pattern",
					"anchor_id", anchorID,
					"pattern_id", pattern.ID,
					"error", err)
			}
		}

		patternsCreated++
	}

	duration := time.Since(startTime)

	a.logger.Info("Pattern discovery completed",
		"anchors_analyzed", len(anchors),
		"clusters_found", len(validClusters),
		"patterns_created", patternsCreated,
		"duration", duration)

	// Publish completion event
	a.publishCompletion(patternsCreated)

	return nil
}

func (a *DiscoveryAgent) publishCompletion(patternsCreated int) {
	payload := map[string]interface{}{
		"patterns_created": patternsCreated,
		"timestamp":        time.Now().Format(time.RFC3339),
	}

	payloadBytes, _ := json.Marshal(payload)
	if err := a.mqtt.Publish("automation/behavior/patterns/discovered", 0, false, payloadBytes); err != nil {
		a.logger.Error("Failed to publish completion", "error", err)
	} else {
		a.logger.Info("Published pattern discovery completion",
			"patterns_created", patternsCreated)
	}
}
