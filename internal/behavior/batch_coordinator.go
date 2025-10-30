package behavior

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/saaga0h/jeeves-platform/internal/behavior/distance"
	"github.com/saaga0h/jeeves-platform/internal/behavior/patterns"
	"github.com/saaga0h/jeeves-platform/pkg/config"
	"github.com/saaga0h/jeeves-platform/pkg/mqtt"
)

// BatchCoordinator manages sliding window batch processing
type BatchCoordinator struct {
	config            *config.Config
	distanceAgent     *distance.ComputationAgent
	discoveryAgent    *patterns.DiscoveryAgent
	mqtt              mqtt.Client
	logger            *slog.Logger
	lastBatchEnd      time.Time
	schedulerStopChan chan struct{}
}

// NewBatchCoordinator creates a new batch coordinator
func NewBatchCoordinator(
	cfg *config.Config,
	distanceAgent *distance.ComputationAgent,
	discoveryAgent *patterns.DiscoveryAgent,
	mqttClient mqtt.Client,
	logger *slog.Logger,
) *BatchCoordinator {
	return &BatchCoordinator{
		config:            cfg,
		distanceAgent:     distanceAgent,
		discoveryAgent:    discoveryAgent,
		mqtt:              mqttClient,
		logger:            logger.With("component", "batch_coordinator"),
		schedulerStopChan: make(chan struct{}),
	}
}

// Start begins automatic batch scheduling if enabled and subscribes to MQTT triggers
func (bc *BatchCoordinator) Start(ctx context.Context) error {
	if !bc.config.BatchProcessingEnabled {
		bc.logger.Info("Batch processing disabled, using traditional approach")
		return nil
	}

	// Subscribe to MQTT trigger for manual batch processing
	if err := bc.mqtt.Subscribe("automation/behavior/process_batch", 0, bc.handleBatchTrigger); err != nil {
		return fmt.Errorf("failed to subscribe to batch trigger topic: %w", err)
	}

	bc.logger.Info("Subscribed to automation/behavior/process_batch")

	if !bc.config.BatchScheduleEnabled {
		bc.logger.Info("Batch scheduling disabled, waiting for MQTT triggers")
		return nil
	}

	bc.logger.Info("Starting automatic batch scheduler",
		"interval", bc.config.BatchScheduleInterval,
		"batch_duration", bc.config.BatchDuration,
		"overlap", bc.config.BatchOverlap)

	go bc.schedulerLoop(ctx)
	return nil
}

// handleBatchTrigger handles MQTT messages to trigger batch processing
func (bc *BatchCoordinator) handleBatchTrigger(msg mqtt.Message) {
	var trigger struct {
		BatchEnd          string `json:"batch_end"`           // RFC3339 timestamp
		BatchDurationHours float64 `json:"batch_duration_hours"`
		OverlapMinutes    int    `json:"overlap_minutes"`
	}

	if err := json.Unmarshal(msg.Payload(), &trigger); err != nil {
		bc.logger.Error("Failed to parse batch trigger", "error", err)
		return
	}

	batchEnd, err := time.Parse(time.RFC3339, trigger.BatchEnd)
	if err != nil {
		bc.logger.Error("Failed to parse batch_end timestamp", "error", err, "batch_end", trigger.BatchEnd)
		return
	}

	batchDuration := time.Duration(trigger.BatchDurationHours * float64(time.Hour))
	overlapDuration := time.Duration(trigger.OverlapMinutes) * time.Minute

	bc.logger.Info("Received batch processing trigger",
		"batch_end", batchEnd,
		"batch_duration", batchDuration,
		"overlap_duration", overlapDuration)

	// Process batch in background
	go func() {
		ctx := context.Background()
		if err := bc.ProcessBatchFromMessage(ctx, batchEnd, batchDuration, overlapDuration); err != nil {
			bc.logger.Error("Batch processing failed", "error", err)
		}
	}()
}

// Stop halts the automatic batch scheduler
func (bc *BatchCoordinator) Stop() {
	if bc.config.BatchScheduleEnabled {
		close(bc.schedulerStopChan)
		bc.logger.Info("Stopped batch scheduler")
	}
}

// schedulerLoop runs batches on a fixed schedule
func (bc *BatchCoordinator) schedulerLoop(ctx context.Context) {
	ticker := time.NewTicker(bc.config.BatchScheduleInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-bc.schedulerStopChan:
			return
		case <-ticker.C:
			if err := bc.ProcessBatch(ctx, time.Now()); err != nil {
				bc.logger.Error("Failed to process scheduled batch", "error", err)
			}
		}
	}
}

// ProcessBatch executes a single batch: distance computation + pattern detection
func (bc *BatchCoordinator) ProcessBatch(ctx context.Context, batchEnd time.Time) error {
	batchID := uuid.New().String()

	// Calculate time windows
	batchStart := batchEnd.Add(-bc.config.BatchDuration)
	overlapStart := batchStart.Add(-bc.config.BatchOverlap)

	// Handle cold start (first batch has no overlap)
	if bc.lastBatchEnd.IsZero() {
		overlapStart = batchStart
		bc.logger.Info("Cold start: running first batch without overlap")
	}

	bc.logger.Info("Starting batch processing",
		"batch_id", batchID,
		"overlap_start", overlapStart,
		"batch_start", batchStart,
		"batch_end", batchEnd,
		"total_window", batchEnd.Sub(overlapStart))

	// Step 1: Compute distances for the batch window
	if err := bc.computeDistancesForWindow(ctx, batchID, overlapStart, batchEnd); err != nil {
		return fmt.Errorf("distance computation failed: %w", err)
	}

	// Step 2: Discover patterns for the batch window
	if err := bc.discoverPatternsForWindow(ctx, batchID, overlapStart, batchEnd); err != nil {
		return fmt.Errorf("pattern discovery failed: %w", err)
	}

	// Update last batch end for next iteration
	bc.lastBatchEnd = batchEnd

	bc.logger.Info("Batch processing complete", "batch_id", batchID)

	return nil
}

// ProcessBatchFromMessage processes a batch triggered by MQTT message
// Allows test scenarios to explicitly trigger batches with specific time windows
func (bc *BatchCoordinator) ProcessBatchFromMessage(
	ctx context.Context,
	batchEnd time.Time,
	batchDuration time.Duration,
	overlapDuration time.Duration,
) error {
	batchID := uuid.New().String()

	batchStart := batchEnd.Add(-batchDuration)
	overlapStart := batchStart.Add(-overlapDuration)

	bc.logger.Info("Processing batch from MQTT trigger",
		"batch_id", batchID,
		"overlap_start", overlapStart,
		"batch_start", batchStart,
		"batch_end", batchEnd)

	if err := bc.computeDistancesForWindow(ctx, batchID, overlapStart, batchEnd); err != nil {
		return fmt.Errorf("distance computation failed: %w", err)
	}

	if err := bc.discoverPatternsForWindow(ctx, batchID, overlapStart, batchEnd); err != nil {
		return fmt.Errorf("pattern discovery failed: %w", err)
	}

	return nil
}

// computeDistancesForWindow computes distances for anchors in the time window
func (bc *BatchCoordinator) computeDistancesForWindow(
	ctx context.Context,
	batchID string,
	windowStart, windowEnd time.Time,
) error {
	startTime := time.Now()

	bc.logger.Info("Computing distances for batch window",
		"batch_id", batchID,
		"window_start", windowStart,
		"window_end", windowEnd)

	// Get anchor pairs that need distances within this window
	// The distanceAgent will handle the actual computation
	// We'll modify GetAnchorsNeedingDistances to accept time window parameters

	// For now, trigger distance computation with lookback that covers the window
	lookbackHours := int(windowEnd.Sub(windowStart).Hours()) + 1

	if err := bc.distanceAgent.ComputeDistancesWithLookback(ctx, lookbackHours); err != nil {
		return fmt.Errorf("failed to compute distances: %w", err)
	}

	elapsed := time.Since(startTime)
	bc.logger.Info("Distance computation complete",
		"batch_id", batchID,
		"elapsed", elapsed)

	return nil
}

// discoverPatternsForWindow discovers patterns for anchors in the time window
func (bc *BatchCoordinator) discoverPatternsForWindow(
	ctx context.Context,
	batchID string,
	windowStart, windowEnd time.Time,
) error {
	startTime := time.Now()

	bc.logger.Info("Discovering patterns for batch window",
		"batch_id", batchID,
		"window_start", windowStart,
		"window_end", windowEnd)

	// Discover patterns using the time window
	patternsCreated, err := bc.discoveryAgent.DiscoverPatternsInWindow(
		ctx,
		bc.config.PatternMinAnchorsForDiscovery,
		windowStart,
		windowEnd,
	)
	if err != nil {
		return fmt.Errorf("failed to discover patterns: %w", err)
	}

	elapsed := time.Since(startTime)
	bc.logger.Info("Pattern discovery complete",
		"batch_id", batchID,
		"patterns_created", patternsCreated,
		"elapsed", elapsed)

	return nil
}
