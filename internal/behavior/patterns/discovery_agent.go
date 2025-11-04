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
	"github.com/saaga0h/jeeves-platform/internal/behavior/types"
	"github.com/saaga0h/jeeves-platform/pkg/mqtt"
)

// TimeManager interface for getting current time (real or virtual)
type TimeManager interface {
	Now() time.Time
	IsTestMode() bool
}

// DiscoveryConfig configures pattern discovery behavior
type DiscoveryConfig struct {
	Interval                      time.Duration // production: 24h, tests: triggered
	MinAnchors                    int           // minimum anchors needed (default: 10)
	LookbackHours                 int           // how far back to analyze
	TemporalGroupingEnabled       bool          // enable multi-stage clustering
	TemporalGroupingWindow        time.Duration // window size for grouping
	TemporalGroupingOverlapRatio  float64       // overlap threshold for parallelism
	UseLocationTemporalClustering bool          // NEW: use location-aware temporal clustering
}

// DiscoveryAgent orchestrates clustering and pattern interpretation
type DiscoveryAgent struct {
	config      DiscoveryConfig
	storage     *storage.AnchorStorage
	clustering  *clustering.ClusteringEngine
	interpreter *PatternInterpreter
	mqtt        mqtt.Client
	logger      *slog.Logger
	timeManager TimeManager

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
	timeManager TimeManager,
) *DiscoveryAgent {
	return &DiscoveryAgent{
		config:       config,
		storage:      storage,
		clustering:   clustering,
		interpreter:  interpreter,
		mqtt:         mqttClient,
		logger:       logger,
		timeManager:  timeManager,
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
		// Test mode: wait for explicit triggers only
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

	// Production mode: periodic execution AND MQTT triggers
	a.logger.Info("Pattern discovery agent running in production mode",
		"interval", a.config.Interval)

	ticker := time.NewTicker(a.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case trigger := <-a.testTriggers:
			// Also process MQTT triggers in production mode (for test scenarios)
			if err := a.discoverPatterns(ctx, trigger.MinAnchors, trigger.LookbackHours); err != nil {
				a.logger.Error("Pattern discovery failed", "error", err)
			}
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

// DiscoverPatternsWithLookback performs pattern discovery with the specified lookback period (for batch coordinator)
func (a *DiscoveryAgent) DiscoverPatternsWithLookback(ctx context.Context, minAnchors, lookbackHours int) (int, error) {
	if err := a.discoverPatterns(ctx, minAnchors, lookbackHours); err != nil {
		return 0, err
	}
	// TODO: Return actual count of patterns created
	return 0, nil
}

// DiscoverPatternsInWindow performs pattern discovery for anchors within a specific time window (for batch processing)
func (a *DiscoveryAgent) DiscoverPatternsInWindow(
	ctx context.Context,
	minAnchors int,
	windowStart, windowEnd time.Time,
) (int, error) {
	if err := a.discoverPatternsInWindow(ctx, minAnchors, windowStart, windowEnd); err != nil {
		return 0, err
	}
	// TODO: Return actual count of patterns created
	return 0, nil
}

// discoverPatternsInWindow performs pattern discovery from anchors within a time window
func (a *DiscoveryAgent) discoverPatternsInWindow(
	ctx context.Context,
	minAnchors int,
	windowStart, windowEnd time.Time,
) error {
	startTime := a.timeManager.Now()

	a.logger.Info("Starting pattern discovery in window",
		"min_anchors", minAnchors,
		"window_start", windowStart,
		"window_end", windowEnd)

	// Get anchors within the time window (distances will be computed in-memory during clustering)
	anchors, err := a.storage.GetAnchorsSinceInWindow(ctx, windowStart, windowEnd)
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

	a.logger.Info("Clustering anchors in window", "count", len(anchors))

	// Use the same multi-stage clustering logic as discoverPatterns
	var validClusters []*clustering.Cluster

	if a.config.TemporalGroupingEnabled {
		// STAGE 1: Temporal Grouping
		groups := GroupByTimeWindow(anchors, a.config.TemporalGroupingWindow)

		a.logger.Info("Temporal grouping complete",
			"groups_created", len(groups),
			"window_size", a.config.TemporalGroupingWindow)

		// STAGE 2 & 3: Detect parallelism and cluster adaptively
		for i, group := range groups {
			if len(group.Anchors) < minAnchors {
				continue
			}

			overlapThreshold := time.Duration(float64(a.config.TemporalGroupingWindow) * a.config.TemporalGroupingOverlapRatio)
			isParallel := DetectParallelism(group, overlapThreshold)
			locations := GetUniqueLocations(group)

			a.logger.Info("Analyzing temporal group",
				"group_index", i,
				"anchor_count", len(group.Anchors),
				"is_parallel", isParallel)

			if isParallel {
				// TWO-PHASE CLUSTERING for parallel activities
				// Phase 1: Same-location patterns (parallel activities)
				// Phase 2: Cross-location patterns (transitions/routines)

				withinLocationEpsilon := 0.15  // For same-location clusters (dist ~0.12)
				crossLocationEpsilon := 0.27   // For cross-location transitions (dist ~0.25)

				a.logger.Info("Using two-phase clustering for parallel activities",
					"within_location_epsilon", withinLocationEpsilon,
					"cross_location_epsilon", crossLocationEpsilon)

				// PHASE 1: Cluster same-location anchors
				for _, location := range locations {
					locationAnchors := FilterByLocation(group, location)
					if len(locationAnchors) < minAnchors {
						continue
					}

					anchorIDs := make([]uuid.UUID, len(locationAnchors))
					for j, anchor := range locationAnchors {
						anchorIDs[j] = anchor.ID
					}

					clusters, err := a.clustering.ClusterAnchorsWithEpsilon(ctx, anchorIDs, withinLocationEpsilon)
					if err != nil {
						a.logger.Error("Same-location clustering failed", "error", err, "location", location)
						continue
					}

					for _, cluster := range clusters {
						if !cluster.Noise && len(cluster.Members) >= minAnchors {
							validClusters = append(validClusters, cluster)
						}
					}
				}

				// PHASE 2: Cluster all anchors with higher epsilon to find cross-location patterns
				// This captures transitions and sequences across locations
				anchorIDs := make([]uuid.UUID, len(group.Anchors))
				for j, anchor := range group.Anchors {
					anchorIDs[j] = anchor.ID
				}

				crossClusters, err := a.clustering.ClusterAnchorsWithEpsilon(ctx, anchorIDs, crossLocationEpsilon)
				if err != nil {
					a.logger.Error("Cross-location clustering failed", "error", err)
				} else {
					// Use minAnchors for transitions (may be fewer than same-location patterns)
					for _, cluster := range crossClusters {
						if !cluster.Noise && len(cluster.Members) >= minAnchors {
							validClusters = append(validClusters, cluster)
						}
					}
				}
			} else {
				anchorIDs := make([]uuid.UUID, len(group.Anchors))
				for j, anchor := range group.Anchors {
					anchorIDs[j] = anchor.ID
				}

				clusters, err := a.clustering.ClusterAnchors(ctx, anchorIDs)
				if err != nil {
					a.logger.Error("Clustering failed", "error", err)
					continue
				}

				for _, cluster := range clusters {
					if !cluster.Noise && len(cluster.Members) >= minAnchors {
						validClusters = append(validClusters, cluster)
					}
				}
			}
		}
	} else {
		// Two-phase clustering without temporal grouping
		// Process all anchors together with adaptive epsilon
		withinLocationEpsilon := 0.15  // For same-location clusters
		crossLocationEpsilon := 0.27   // For cross-location transitions

		a.logger.Info("Using two-phase clustering on all anchors",
			"total_anchors", len(anchors),
			"within_location_epsilon", withinLocationEpsilon,
			"cross_location_epsilon", crossLocationEpsilon)

		locations := GetUniqueLocations(TemporalGroup{Anchors: anchors})

		// PHASE 1: Cluster same-location anchors
		for _, location := range locations {
			locationAnchors := FilterByLocation(TemporalGroup{Anchors: anchors}, location)
			if len(locationAnchors) < minAnchors {
				continue
			}

			anchorIDs := make([]uuid.UUID, len(locationAnchors))
			for j, anchor := range locationAnchors {
				anchorIDs[j] = anchor.ID
			}

			clusters, err := a.clustering.ClusterAnchorsWithEpsilon(ctx, anchorIDs, withinLocationEpsilon)
			if err != nil {
				a.logger.Error("Same-location clustering failed", "error", err, "location", location)
				continue
			}

			for _, cluster := range clusters {
				if !cluster.Noise && len(cluster.Members) >= minAnchors {
					validClusters = append(validClusters, cluster)
				}
			}
		}

		// PHASE 2: Cluster all anchors with higher epsilon for cross-location patterns
		anchorIDs := make([]uuid.UUID, len(anchors))
		for j, anchor := range anchors {
			anchorIDs[j] = anchor.ID
		}

		crossClusters, err := a.clustering.ClusterAnchorsWithEpsilon(ctx, anchorIDs, crossLocationEpsilon)
		if err != nil {
			a.logger.Error("Cross-location clustering failed", "error", err)
		} else {
			for _, cluster := range crossClusters {
				if !cluster.Noise && len(cluster.Members) >= minAnchors {
					validClusters = append(validClusters, cluster)
				}
			}
		}
	}

	a.logger.Info("Valid clusters found", "count", len(validClusters))

	if len(validClusters) == 0 {
		a.publishCompletion(0)
		return nil
	}

	// Interpret and create patterns
	patternsCreated := 0
	for _, cluster := range validClusters {
		pattern, err := a.interpreter.InterpretCluster(ctx, cluster.Members)
		if err != nil {
			a.logger.Error("Failed to interpret cluster", "error", err)
			continue
		}

		if err := a.storage.CreatePattern(ctx, pattern); err != nil {
			a.logger.Error("Failed to store pattern", "error", err)
			continue
		}

		for _, anchorID := range cluster.Members {
			if err := a.storage.UpdateAnchorPattern(ctx, anchorID, pattern.ID); err != nil {
				a.logger.Warn("Failed to update anchor pattern", "error", err)
			}
		}

		patternsCreated++
	}

	duration := time.Since(startTime)
	a.logger.Info("Pattern discovery in window completed",
		"patterns_created", patternsCreated,
		"duration", duration)

	a.publishCompletion(patternsCreated)
	return nil
}

// discoverPatterns performs pattern discovery from recent anchors
func (a *DiscoveryAgent) discoverPatterns(ctx context.Context, minAnchors, lookbackHours int) error {
	startTime := a.timeManager.Now()

	currentTime := a.timeManager.Now()
	since := currentTime.Add(-time.Duration(lookbackHours) * time.Hour)

	a.logger.Info("Starting pattern discovery",
		"min_anchors", minAnchors,
		"lookback_hours", lookbackHours,
		"current_time", currentTime,
		"since", since)

	// Get recent anchors (distances will be computed in-memory during clustering)
	anchors, err := a.storage.GetAnchorsSince(ctx, since)
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

	// NEW: Location-temporal clustering path
	if a.config.UseLocationTemporalClustering {
		return a.discoverPatternsWithLocationTemporal(ctx, anchors, minAnchors, startTime)
	}

	// Multi-stage clustering: check if temporal grouping is enabled
	var validClusters []*clustering.Cluster

	if a.config.TemporalGroupingEnabled {
		// STAGE 1: Temporal Grouping
		groups := GroupByTimeWindow(anchors, a.config.TemporalGroupingWindow)

		a.logger.Info("Temporal grouping complete",
			"groups_created", len(groups),
			"window_size", a.config.TemporalGroupingWindow)

		// STAGE 2 & 3: Detect parallelism and cluster adaptively
		for i, group := range groups {
			// Skip groups that are too small
			if len(group.Anchors) < minAnchors {
				a.logger.Debug("Skipping small temporal group",
					"group_index", i,
					"anchor_count", len(group.Anchors),
					"required", minAnchors)
				continue
			}

			// Detect parallelism
			overlapThreshold := time.Duration(float64(a.config.TemporalGroupingWindow) * a.config.TemporalGroupingOverlapRatio)
			isParallel := DetectParallelism(group, overlapThreshold)

			locations := GetUniqueLocations(group)

			a.logger.Info("Analyzing temporal group",
				"group_index", i,
				"anchor_count", len(group.Anchors),
				"unique_locations", len(locations),
				"is_parallel", isParallel,
				"locations", locations)

			if isParallel {
				// Parallel activities: cluster each location separately
				a.logger.Info("Splitting parallel group by location",
					"group_index", i,
					"locations", locations)

				for _, location := range locations {
					locationAnchors := FilterByLocation(group, location)

					if len(locationAnchors) < minAnchors {
						a.logger.Debug("Skipping location subset (too few anchors)",
							"location", location,
							"anchor_count", len(locationAnchors),
							"required", minAnchors)
						continue
					}

					a.logger.Info("Clustering location subset",
						"location", location,
						"anchor_count", len(locationAnchors))

					// Extract anchor IDs for this location
					anchorIDs := make([]uuid.UUID, len(locationAnchors))
					for j, anchor := range locationAnchors {
						anchorIDs[j] = anchor.ID
					}

					// Cluster this location
					clusters, err := a.clustering.ClusterAnchors(ctx, anchorIDs)
					if err != nil {
						a.logger.Error("Clustering failed for location",
							"location", location,
							"error", err)
						continue
					}

					// Filter valid clusters from this location
					for _, cluster := range clusters {
						if !cluster.Noise && len(cluster.Members) >= minAnchors {
							validClusters = append(validClusters, cluster)
						}
					}
				}
			} else {
				// Sequential activities: cluster normally
				a.logger.Info("Clustering sequential group",
					"group_index", i,
					"anchor_count", len(group.Anchors))

				// Extract anchor IDs
				anchorIDs := make([]uuid.UUID, len(group.Anchors))
				for j, anchor := range group.Anchors {
					anchorIDs[j] = anchor.ID
				}

				// Cluster this group
				clusters, err := a.clustering.ClusterAnchors(ctx, anchorIDs)
				if err != nil {
					a.logger.Error("Clustering failed for group",
						"group_index", i,
						"error", err)
					continue
				}

				// Filter valid clusters
				for _, cluster := range clusters {
					if !cluster.Noise && len(cluster.Members) >= minAnchors {
						validClusters = append(validClusters, cluster)
					}
				}
			}
		}

		a.logger.Info("Multi-stage clustering complete",
			"temporal_groups", len(groups),
			"valid_clusters", len(validClusters))

	} else {
		// Original single-stage clustering (backward compatibility)
		a.logger.Info("Using single-stage clustering (temporal grouping disabled)")

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
		for _, cluster := range clusters {
			if !cluster.Noise && len(cluster.Members) >= minAnchors {
				validClusters = append(validClusters, cluster)
			}
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

// discoverPatternsWithLocationTemporal uses location-aware temporal clustering
func (a *DiscoveryAgent) discoverPatternsWithLocationTemporal(
	ctx context.Context,
	anchors []*types.SemanticAnchor,
	minAnchors int,
	startTime time.Time,
) error {
	a.logger.Info("Using location-temporal clustering",
		"anchor_count", len(anchors),
		"min_anchors", minAnchors)

	// STEP 1: Location-temporal clustering
	clusterer := NewLocationTemporalClusterer()
	sequences := clusterer.ClusterByLocationTemporal(anchors)

	a.logger.Info("Location-temporal clustering complete",
		"sequences_found", len(sequences))

	// STEP 2: Semantic validation
	validator := NewSemanticValidator(a.logger)
	var validSequences []*ActivitySequence

	for _, seq := range sequences {
		valid, avgDist := validator.ValidateSequence(seq)

		if valid {
			validSequences = append(validSequences, seq)
			a.logger.Info("Sequence validated",
				"sequence_id", seq.ID,
				"anchor_count", len(seq.Anchors),
				"locations", seq.Locations,
				"cross_location", seq.IsCrossLocation,
				"avg_distance", avgDist)
		} else {
			// Try to split incoherent sequence
			a.logger.Info("Sequence validation failed, attempting split",
				"sequence_id", seq.ID,
				"avg_distance", avgDist)

			split := validator.SplitIncoherentSequence(seq)
			for _, subSeq := range split {
				subValid, subAvgDist := validator.ValidateSequence(subSeq)
				if subValid {
					validSequences = append(validSequences, subSeq)
					a.logger.Info("Sub-sequence validated after split",
						"sequence_id", subSeq.ID,
						"anchor_count", len(subSeq.Anchors),
						"avg_distance", subAvgDist)
				}
			}
		}
	}

	a.logger.Info("Semantic validation complete",
		"valid_sequences", len(validSequences))

	if len(validSequences) == 0 {
		a.logger.Info("No valid sequences found")
		a.publishCompletion(0)
		return nil
	}

	// STEP 3: Convert sequences to patterns
	patternsCreated := 0

	for _, seq := range validSequences {
		// Filter sequences that are too small
		if len(seq.Anchors) < minAnchors {
			a.logger.Debug("Skipping small sequence",
				"sequence_id", seq.ID,
				"anchor_count", len(seq.Anchors),
				"required", minAnchors)
			continue
		}

		// Extract anchor IDs for pattern interpretation
		anchorIDs := make([]uuid.UUID, len(seq.Anchors))
		for i, anchor := range seq.Anchors {
			anchorIDs[i] = anchor.ID
		}

		// Interpret sequence as a pattern
		pattern, err := a.interpreter.InterpretCluster(ctx, anchorIDs)
		if err != nil {
			a.logger.Error("Failed to interpret sequence",
				"sequence_id", seq.ID,
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
		for _, anchorID := range anchorIDs {
			if err := a.storage.UpdateAnchorPattern(ctx, anchorID, pattern.ID); err != nil {
				a.logger.Warn("Failed to update anchor pattern",
					"anchor_id", anchorID,
					"pattern_id", pattern.ID,
					"error", err)
			}
		}

		patternsCreated++

		a.logger.Info("Pattern created from sequence",
			"pattern_id", pattern.ID,
			"sequence_id", seq.ID,
			"anchor_count", len(anchorIDs),
			"locations", seq.Locations,
			"cross_location", seq.IsCrossLocation)
	}

	duration := time.Since(startTime)

	a.logger.Info("Location-temporal pattern discovery completed",
		"anchors_analyzed", len(anchors),
		"sequences_found", len(sequences),
		"valid_sequences", len(validSequences),
		"patterns_created", patternsCreated,
		"duration", duration)

	// Publish completion event
	a.publishCompletion(patternsCreated)

	return nil
}
