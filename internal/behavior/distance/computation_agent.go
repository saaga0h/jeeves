package distance

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/pgvector/pgvector-go"

	"github.com/saaga0h/jeeves-platform/internal/behavior/storage"
	"github.com/saaga0h/jeeves-platform/internal/behavior/types"
	"github.com/saaga0h/jeeves-platform/pkg/llm"
	"github.com/saaga0h/jeeves-platform/pkg/mqtt"
)

// TimeManager interface for getting current time (real or virtual)
type TimeManager interface {
	Now() time.Time
	IsTestMode() bool
}

// ComputationConfig configures distance computation behavior
type ComputationConfig struct {
	Strategy      string        // "llm_first", "learned_first", "vector_first", "hybrid", "progressive_learned"
	Model         string        // LLM model name (e.g., "mixtral:8x7b")
	Interval      time.Duration // production: 6h, tests: triggered
	BatchSize     int           // default: 100
	LookbackHours int           // how far back to compute distances
}

// ComputationAgent computes semantic distances between anchor pairs
type ComputationAgent struct {
	config      ComputationConfig
	storage     *storage.AnchorStorage
	llm         llm.Client
	mqtt        mqtt.Client
	logger      *slog.Logger
	timeManager TimeManager

	// Test mode support
	testMode     bool
	testTriggers chan TriggerEvent

	// Learned distance patterns
	learnedDistances map[string]float64
	learnedMutex     sync.RWMutex

	// Progressive learned tracking
	patternObservations map[string][]float64 // Track multiple observations for confidence
	uncertainQueue      [][2]*types.SemanticAnchor
	totalComputations   int // Track how many computations we've done
}

// TriggerEvent represents a manual trigger for distance computation
type TriggerEvent struct {
	LookbackHours int
}

// NewComputationAgent creates a new distance computation agent
func NewComputationAgent(
	config ComputationConfig,
	storage *storage.AnchorStorage,
	llmClient llm.Client,
	mqttClient mqtt.Client,
	logger *slog.Logger,
	timeManager TimeManager,
) *ComputationAgent {
	return &ComputationAgent{
		config:              config,
		storage:             storage,
		llm:                 llmClient,
		mqtt:                mqttClient,
		logger:              logger,
		timeManager:         timeManager,
		testTriggers:        make(chan TriggerEvent, 10),
		learnedDistances:    make(map[string]float64),
		patternObservations: make(map[string][]float64),
		uncertainQueue:      make([][2]*types.SemanticAnchor, 0),
	}
}

// EnableTestMode switches to test mode (trigger-based instead of interval-based)
func (a *ComputationAgent) EnableTestMode() {
	a.testMode = true
}

// Start begins the distance computation agent
func (a *ComputationAgent) Start(ctx context.Context) error {
	// Subscribe to trigger events for test mode
	if err := a.mqtt.Subscribe("automation/behavior/compute_distances", 0, a.handleTrigger); err != nil {
		return fmt.Errorf("failed to subscribe to triggers: %w", err)
	}

	// Load learned distances from storage
	if err := a.loadLearnedDistances(ctx); err != nil {
		a.logger.Warn("Failed to load learned distances", "error", err)
	}

	if a.testMode {
		// Test mode: wait for explicit triggers only
		a.logger.Info("Distance computation agent running in test mode")
		for {
			select {
			case trigger := <-a.testTriggers:
				if err := a.computeDistances(ctx, trigger.LookbackHours); err != nil {
					a.logger.Error("Distance computation failed", "error", err)
				}
			case <-ctx.Done():
				return nil
			}
		}
	}

	// Production mode: periodic execution AND MQTT triggers
	a.logger.Info("Distance computation agent running in production mode",
		"interval", a.config.Interval)

	ticker := time.NewTicker(a.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case trigger := <-a.testTriggers:
			// Also process MQTT triggers in production mode (for test scenarios)
			if err := a.computeDistances(ctx, trigger.LookbackHours); err != nil {
				a.logger.Error("Distance computation failed", "error", err)
			}
		case <-ticker.C:
			if err := a.computeDistances(ctx, a.config.LookbackHours); err != nil {
				a.logger.Error("Distance computation failed", "error", err)
			}
		case <-ctx.Done():
			return nil
		}
	}
}

func (a *ComputationAgent) handleTrigger(msg mqtt.Message) {
	var trigger struct {
		LookbackHours int `json:"lookback_hours"`
	}

	if err := json.Unmarshal(msg.Payload(), &trigger); err != nil {
		a.logger.Error("Failed to parse trigger", "error", err)
		return
	}

	a.logger.Info("Received distance computation trigger",
		"topic", msg.Topic(),
		"lookback_hours", trigger.LookbackHours)

	a.testTriggers <- TriggerEvent{LookbackHours: trigger.LookbackHours}
}

// ComputeDistancesWithLookback is a public method for triggering distance computation (used by batch coordinator)
func (a *ComputationAgent) ComputeDistancesWithLookback(ctx context.Context, lookbackHours int) error {
	return a.computeDistances(ctx, lookbackHours)
}

// computeDistances performs batch distance computation
func (a *ComputationAgent) computeDistances(ctx context.Context, lookbackHours int) error {
	startTime := a.timeManager.Now()

	a.logger.Info("Starting distance computation",
		"lookback_hours", lookbackHours,
		"strategy", a.config.Strategy,
		"batch_size", a.config.BatchSize)

	// Get anchor pairs needing distances
	since := a.timeManager.Now().Add(-time.Duration(lookbackHours) * time.Hour)
	pairs, err := a.storage.GetAnchorsNeedingDistances(ctx, a.config.BatchSize)
	if err != nil {
		return fmt.Errorf("failed to get anchor pairs: %w", err)
	}

	if len(pairs) == 0 {
		a.logger.Info("No anchor pairs need distance computation")
		a.publishCompletion(0)
		return nil
	}

	a.logger.Info("Computing distances", "pairs", len(pairs))

	// Compute distances for each pair
	distancesComputed := 0

	for _, pair := range pairs {
		// Load both anchors
		anchor1, err := a.storage.GetAnchor(ctx, pair[0])
		if err != nil {
			a.logger.Warn("Failed to load anchor",
				"anchor_id", pair[0],
				"error", err)
			continue
		}

		anchor2, err := a.storage.GetAnchor(ctx, pair[1])
		if err != nil {
			a.logger.Warn("Failed to load anchor",
				"anchor_id", pair[1],
				"error", err)
			continue
		}

		// Skip if outside lookback window
		if anchor1.Timestamp.Before(since) && anchor2.Timestamp.Before(since) {
			continue
		}

		// Compute distance using configured strategy
		distance, source, err := a.computeDistance(ctx, anchor1, anchor2)
		if err != nil {
			a.logger.Warn("Failed to compute distance",
				"anchor1", anchor1.ID,
				"anchor2", anchor2.ID,
				"error", err)
			continue
		}

		// Store distance
		distanceRecord := &types.AnchorDistance{
			Anchor1ID:  pair[0],
			Anchor2ID:  pair[1],
			Distance:   distance,
			Source:     source,
			ComputedAt: a.timeManager.Now(),
		}

		if err := a.storage.StoreDistance(ctx, distanceRecord); err != nil {
			a.logger.Error("Failed to store distance", "error", err)
			continue
		}

		distancesComputed++
	}

	duration := time.Since(startTime)

	a.logger.Info("Distance computation completed",
		"pairs_processed", len(pairs),
		"distances_computed", distancesComputed,
		"duration", duration)

	// Publish completion event (for tests)
	a.publishCompletion(distancesComputed)

	return nil
}

// computeDistance calculates semantic distance using configured strategy
func (a *ComputationAgent) computeDistance(
	ctx context.Context,
	anchor1, anchor2 *types.SemanticAnchor,
) (distance float64, source string, err error) {

	switch a.config.Strategy {
	case "llm_first":
		// Tests: Always use LLM to build learned library
		return a.computeLLMDistance(ctx, anchor1, anchor2)

	case "learned_first":
		// Production: Check learned patterns first
		if dist := a.getLearnedDistance(anchor1, anchor2); dist != nil {
			return *dist, "learned", nil
		}

		// Not learned, try LLM
		if dist, src, err := a.computeLLMDistance(ctx, anchor1, anchor2); err == nil {
			// Learn this distance for future
			a.learnDistance(anchor1, anchor2, dist)
			return dist, src, nil
		}

		// LLM failed, fallback to vector
		return a.computeVectorDistance(anchor1, anchor2)

	case "vector_first":
		// Fast path: always use vector distance
		return a.computeVectorDistance(anchor1, anchor2)

	case "hybrid":
		// Hybrid: Use vector for most cases, LLM for ambiguous ones
		return a.computeHybridDistance(ctx, anchor1, anchor2)

	case "progressive_learned":
		// Progressive: Build learned model through strategic LLM sampling
		return a.computeProgressiveLearnedDistance(ctx, anchor1, anchor2)

	default:
		return 0, "", fmt.Errorf("unknown strategy: %s", a.config.Strategy)
	}
}

// computeVectorDistance calculates cosine distance between embeddings
func (a *ComputationAgent) computeVectorDistance(
	anchor1, anchor2 *types.SemanticAnchor,
) (float64, string, error) {

	// Cosine distance = 1 - cosine_similarity
	distance := cosineDist(anchor1.SemanticEmbedding, anchor2.SemanticEmbedding)

	a.logger.Debug("Computed vector distance",
		"anchor1", anchor1.ID,
		"anchor2", anchor2.ID,
		"distance", distance)

	return distance, "vector", nil
}

// cosineDist computes cosine distance between two normalized vectors
func cosineDist(v1, v2 pgvector.Vector) float64 {
	// Compute dot product
	var dot float64
	for i := 0; i < len(v1.Slice()); i++ {
		dot += float64(v1.Slice()[i]) * float64(v2.Slice()[i])
	}

	// Vectors are normalized, so cosine_similarity = dot_product
	// cosine_distance = 1 - cosine_similarity
	// Clamp to [0, 1] to handle floating point errors
	distance := 1.0 - dot
	return math.Max(0, math.Min(1, distance))
}

// computeHybridDistance uses vector for clear cases, LLM for ambiguous ones
func (a *ComputationAgent) computeHybridDistance(
	ctx context.Context,
	anchor1, anchor2 *types.SemanticAnchor,
) (float64, string, error) {

	// Always compute vector distance first (it's fast)
	vectorDist := cosineDist(anchor1.SemanticEmbedding, anchor2.SemanticEmbedding)

	sameLocation := anchor1.Location == anchor2.Location

	// CASE 1: Same location
	if sameLocation {
		// Very similar (< 0.15) - clearly same activity pattern
		if vectorDist < 0.15 {
			a.logger.Debug("Hybrid: same location, very similar - using vector",
				"anchor1", anchor1.ID, "anchor2", anchor2.ID,
				"vector_dist", vectorDist)
			return vectorDist, "vector", nil
		}

		// Very different (> 0.35) - clearly different activities
		if vectorDist > 0.35 {
			a.logger.Debug("Hybrid: same location, very different - using vector",
				"anchor1", anchor1.ID, "anchor2", anchor2.ID,
				"vector_dist", vectorDist)
			return vectorDist, "vector", nil
		}

		// Borderline case (0.15-0.35) - use LLM for semantic understanding
		// This handles cases like "same location, different time of day"
		a.logger.Debug("Hybrid: same location, borderline - using LLM",
			"anchor1", anchor1.ID, "anchor2", anchor2.ID,
			"vector_dist", vectorDist)
		return a.computeLLMDistance(ctx, anchor1, anchor2)
	}

	// CASE 2: Different locations
	// Very different (> 0.5) - clearly parallel/unrelated activities
	if vectorDist > 0.5 {
		a.logger.Debug("Hybrid: different locations, clearly different - using vector",
			"anchor1", anchor1.ID, "anchor2", anchor2.ID,
			"vector_dist", vectorDist)
		return vectorDist, "vector", nil
	}

	// Might be sequential routine (e.g., kitchen → dining_room)
	// Use LLM to understand routine flow
	if isAdjacentLocations(anchor1.Location, anchor2.Location) {
		a.logger.Debug("Hybrid: adjacent locations, potential routine - using LLM",
			"anchor1", anchor1.ID, "anchor2", anchor2.ID,
			"locations", anchor1.Location+"/"+anchor2.Location)
		return a.computeLLMDistance(ctx, anchor1, anchor2)
	}

	// Default: use vector for different locations
	a.logger.Debug("Hybrid: different locations - using vector",
		"anchor1", anchor1.ID, "anchor2", anchor2.ID,
		"vector_dist", vectorDist)
	return vectorDist, "vector", nil
}

// isAdjacentLocations checks if two locations are typically part of sequential routines
func isAdjacentLocations(loc1, loc2 string) bool {
	// Define location pairs that often appear in routines
	adjacentPairs := map[string][]string{
		"bedroom":     {"bathroom", "kitchen"},
		"bathroom":    {"bedroom", "kitchen"},
		"kitchen":     {"dining_room", "bedroom", "bathroom"},
		"dining_room": {"kitchen", "living_room"},
		"living_room": {"dining_room", "study"},
		"study":       {"living_room"},
	}

	if neighbors, ok := adjacentPairs[loc1]; ok {
		for _, neighbor := range neighbors {
			if neighbor == loc2 {
				return true
			}
		}
	}

	if neighbors, ok := adjacentPairs[loc2]; ok {
		for _, neighbor := range neighbors {
			if neighbor == loc1 {
				return true
			}
		}
	}

	return false
}

// computeProgressiveLearnedDistance implements progressive learning strategy
func (a *ComputationAgent) computeProgressiveLearnedDistance(
	ctx context.Context,
	anchor1, anchor2 *types.SemanticAnchor,
) (float64, string, error) {
	a.learnedMutex.Lock()
	a.totalComputations++
	currentTotal := a.totalComputations
	a.learnedMutex.Unlock()

	// PHASE 1: Initial seeding (first ~50 computations)
	// Use strategic sampling to build learned model
	if currentTotal <= 50 {
		if a.shouldSampleForLearning(anchor1, anchor2) {
			a.logger.Debug("Progressive: Phase 1 - LLM seeding",
				"computation", currentTotal,
				"anchor1", anchor1.ID,
				"anchor2", anchor2.ID)

			dist, _, err := a.computeLLMDistance(ctx, anchor1, anchor2)
			if err == nil {
				a.recordObservation(anchor1, anchor2, dist)
				return dist, "llm_seed", nil
			}
			// Fallback to vector if LLM fails
			a.logger.Warn("LLM failed during seeding, using vector", "error", err)
			return a.computeVectorDistance(anchor1, anchor2)
		}

		// Not selected for sampling, use vector
		dist, _, err := a.computeVectorDistance(anchor1, anchor2)
		return dist, "vector", err
	}

	// PHASE 2: Learned model usage (51-400 computations)
	// Use learned patterns with confidence scoring
	if currentTotal <= 400 {
		if dist, confidence := a.getLearnedDistanceWithConfidence(anchor1, anchor2); dist != nil {
			if confidence >= 0.7 {
				// High confidence - use learned distance
				a.logger.Debug("Progressive: Phase 2 - high confidence learned",
					"computation", currentTotal,
					"confidence", confidence,
					"distance", *dist)
				return *dist, "learned", nil
			}

			// Medium confidence - queue for verification but use learned for now
			a.logger.Debug("Progressive: Phase 2 - medium confidence, queuing",
				"computation", currentTotal,
				"confidence", confidence)
			a.queueForVerification(anchor1, anchor2)
			return *dist, "learned_uncertain", nil
		}

		// No learned pattern - compute with LLM and learn
		a.logger.Debug("Progressive: Phase 2 - new pattern, using LLM",
			"computation", currentTotal)
		dist, _, err := a.computeLLMDistance(ctx, anchor1, anchor2)
		if err == nil {
			a.recordObservation(anchor1, anchor2, dist)
			return dist, "llm", nil
		}

		// Fallback to vector
		return a.computeVectorDistance(anchor1, anchor2)
	}

	// PHASE 3: Verification phase (400+ computations)
	// Process queued uncertain cases with LLM
	if currentTotal > 400 && len(a.uncertainQueue) > 0 {
		// Check if current pair is in uncertain queue
		if a.isInUncertainQueue(anchor1, anchor2) {
			a.logger.Debug("Progressive: Phase 3 - verifying queued pair",
				"computation", currentTotal,
				"queue_size", len(a.uncertainQueue))
			dist, _, err := a.computeLLMDistance(ctx, anchor1, anchor2)
			if err == nil {
				a.recordObservation(anchor1, anchor2, dist)
				a.removeFromUncertainQueue(anchor1, anchor2)
				return dist, "llm_verify", nil
			}
		}
	}

	// Default: use learned with fallback to vector
	if dist := a.getLearnedDistance(anchor1, anchor2); dist != nil {
		return *dist, "learned", nil
	}

	return a.computeVectorDistance(anchor1, anchor2)
}

// shouldSampleForLearning determines if a pair should be sampled for LLM learning
func (a *ComputationAgent) shouldSampleForLearning(anchor1, anchor2 *types.SemanticAnchor) bool {
	// Strategic sampling to cover diverse patterns:
	// 1. Different location pairs (kitchen→dining, bedroom→bathroom, etc.)
	// 2. Time boundaries (morning→afternoon, etc.)
	// 3. Same location, different times
	// 4. Adjacent locations (for routine flows)
	// 5. Concurrent activities (same time, different locations)

	key := generatePatternKey(anchor1, anchor2)

	a.learnedMutex.RLock()
	_, alreadySampled := a.patternObservations[key]
	a.learnedMutex.RUnlock()

	// Don't resample same pattern during seeding
	if alreadySampled {
		return false
	}

	// Sample all unique patterns we encounter
	return true
}

// recordObservation adds an observation to the learned model
func (a *ComputationAgent) recordObservation(anchor1, anchor2 *types.SemanticAnchor, distance float64) {
	key := generatePatternKey(anchor1, anchor2)

	a.learnedMutex.Lock()
	defer a.learnedMutex.Unlock()

	if _, exists := a.patternObservations[key]; !exists {
		a.patternObservations[key] = make([]float64, 0)
	}

	a.patternObservations[key] = append(a.patternObservations[key], distance)

	// Update learned distance (average of observations)
	sum := 0.0
	for _, d := range a.patternObservations[key] {
		sum += d
	}
	a.learnedDistances[key] = sum / float64(len(a.patternObservations[key]))

	a.logger.Debug("Recorded observation",
		"key", key,
		"distance", distance,
		"observations", len(a.patternObservations[key]),
		"avg", a.learnedDistances[key])
}

// getLearnedDistanceWithConfidence returns learned distance and confidence score
func (a *ComputationAgent) getLearnedDistanceWithConfidence(
	anchor1, anchor2 *types.SemanticAnchor,
) (*float64, float64) {
	key := generatePatternKey(anchor1, anchor2)

	a.learnedMutex.RLock()
	defer a.learnedMutex.RUnlock()

	observations, exists := a.patternObservations[key]
	if !exists || len(observations) == 0 {
		return nil, 0.0
	}

	// Calculate confidence based on:
	// 1. Number of observations (more = higher confidence)
	// 2. Variance of observations (lower variance = higher confidence)

	numObs := len(observations)
	if numObs == 0 {
		return nil, 0.0
	}

	// Calculate average
	sum := 0.0
	for _, d := range observations {
		sum += d
	}
	avg := sum / float64(numObs)

	// Calculate variance
	variance := 0.0
	for _, d := range observations {
		diff := d - avg
		variance += diff * diff
	}
	variance = variance / float64(numObs)
	stdDev := math.Sqrt(variance)

	// Confidence scoring:
	// - 1 observation: 0.5 confidence
	// - 2 observations: 0.7 confidence
	// - 3+ observations: 0.9 confidence
	// - Reduce by variance (high variance = lower confidence)

	obsConfidence := 0.5
	if numObs >= 3 {
		obsConfidence = 0.9
	} else if numObs == 2 {
		obsConfidence = 0.7
	}

	// Variance penalty: if stdDev > 0.2, reduce confidence
	variancePenalty := math.Min(stdDev*2, 0.3) // Max penalty of 0.3

	confidence := math.Max(0.0, obsConfidence-variancePenalty)

	return &avg, confidence
}

// queueForVerification adds a pair to the uncertain queue for later LLM verification
func (a *ComputationAgent) queueForVerification(anchor1, anchor2 *types.SemanticAnchor) {
	a.learnedMutex.Lock()
	defer a.learnedMutex.Unlock()

	// Only queue if not already in queue and queue isn't too large
	if len(a.uncertainQueue) < 50 && !a.isInUncertainQueueUnsafe(anchor1, anchor2) {
		a.uncertainQueue = append(a.uncertainQueue, [2]*types.SemanticAnchor{anchor1, anchor2})
		a.logger.Debug("Queued for verification",
			"anchor1", anchor1.ID,
			"anchor2", anchor2.ID,
			"queue_size", len(a.uncertainQueue))
	}
}

// isInUncertainQueue checks if a pair is in the uncertain queue (thread-safe)
func (a *ComputationAgent) isInUncertainQueue(anchor1, anchor2 *types.SemanticAnchor) bool {
	a.learnedMutex.RLock()
	defer a.learnedMutex.RUnlock()
	return a.isInUncertainQueueUnsafe(anchor1, anchor2)
}

// isInUncertainQueueUnsafe checks queue without locking (caller must hold lock)
func (a *ComputationAgent) isInUncertainQueueUnsafe(anchor1, anchor2 *types.SemanticAnchor) bool {
	for _, pair := range a.uncertainQueue {
		if (pair[0].ID == anchor1.ID && pair[1].ID == anchor2.ID) ||
			(pair[0].ID == anchor2.ID && pair[1].ID == anchor1.ID) {
			return true
		}
	}
	return false
}

// removeFromUncertainQueue removes a pair from the queue
func (a *ComputationAgent) removeFromUncertainQueue(anchor1, anchor2 *types.SemanticAnchor) {
	a.learnedMutex.Lock()
	defer a.learnedMutex.Unlock()

	for i, pair := range a.uncertainQueue {
		if (pair[0].ID == anchor1.ID && pair[1].ID == anchor2.ID) ||
			(pair[0].ID == anchor2.ID && pair[1].ID == anchor1.ID) {
			// Remove from queue
			a.uncertainQueue = append(a.uncertainQueue[:i], a.uncertainQueue[i+1:]...)
			a.logger.Debug("Removed from verification queue",
				"anchor1", anchor1.ID,
				"anchor2", anchor2.ID,
				"queue_size", len(a.uncertainQueue))
			return
		}
	}
}

// computeLLMDistance asks LLM to rate semantic relatedness
func (a *ComputationAgent) computeLLMDistance(
	ctx context.Context,
	anchor1, anchor2 *types.SemanticAnchor,
) (float64, string, error) {

	prompt := fmt.Sprintf(`Rate the semantic relatedness of these two behavioral anchors.

Anchor 1:
- Location: %s
- Time: %s
- Context: %s (day: %s, season: %s)
- Signals: %d observed

Anchor 2:
- Location: %s
- Time: %s
- Context: %s (day: %s, season: %s)
- Signals: %d observed

Consider:
- Temporal proximity (but context matters more than clock time)
- Location transitions (kitchen→dining natural, bedroom→garage unusual)
- Time of day context (morning prep vs late night)
- Seasonal patterns (winter mornings darker, routines different)
- Day type (weekday routine vs weekend leisure)
- Concurrent activities: Different locations at the SAME time usually indicate SEPARATE activities (distance >= 0.5)
- Sequential activities: Different locations with time progression often indicate RELATED flow (distance < 0.3)

Rate semantic distance on scale 0.0 (same activity/pattern) to 1.0 (completely unrelated).

Examples:
- Kitchen @ 7am Monday winter + Dining @ 7:30am Monday winter = 0.15 (breakfast sequence across locations)
- Kitchen @ 7am Monday + Kitchen @ 2am Saturday = 0.8 (same space, very different context)
- Bedroom @ 10pm + Bedroom @ 7am = 0.7 (same space, sleep boundary between)
- Living_room @ 20:00 + Study @ 20:00 = 0.6 (concurrent activities in different spaces)
- Bedroom @ 7:00 + Bathroom @ 7:15 = 0.1 (morning routine flow across locations)

Respond with ONLY valid JSON (no markdown, no explanation):
{
  "distance": 0.0-1.0,
  "reasoning": "brief explanation"
}`,
		anchor1.Location,
		anchor1.Timestamp.Format("15:04"),
		getContextValue(anchor1.Context, "time_of_day"),
		getContextValue(anchor1.Context, "day_type"),
		getContextValue(anchor1.Context, "season"),
		len(anchor1.Signals),
		anchor2.Location,
		anchor2.Timestamp.Format("15:04"),
		getContextValue(anchor2.Context, "time_of_day"),
		getContextValue(anchor2.Context, "day_type"),
		getContextValue(anchor2.Context, "season"),
		len(anchor2.Signals))

	req := llm.GenerateRequest{
		Model:  a.config.Model,
		Prompt: prompt,
		Format: "json", // Request JSON response
	}

	response, err := a.llm.Generate(ctx, req)
	if err != nil {
		return 0, "", fmt.Errorf("LLM request failed: %w", err)
	}

	var result struct {
		Distance  float64 `json:"distance"`
		Reasoning string  `json:"reasoning"`
	}

	if err := json.Unmarshal([]byte(response.Response), &result); err != nil {
		return 0, "", fmt.Errorf("failed to parse LLM response: %w", err)
	}

	// Validate distance range
	if result.Distance < 0 || result.Distance > 1 {
		return 0, "", fmt.Errorf("invalid distance value: %f", result.Distance)
	}

	a.logger.Debug("LLM computed distance",
		"anchor1", anchor1.ID,
		"anchor2", anchor2.ID,
		"distance", result.Distance,
		"reasoning", result.Reasoning)

	return result.Distance, "llm", nil
}

// getLearnedDistance checks if we have a learned pattern for this pair
func (a *ComputationAgent) getLearnedDistance(
	anchor1, anchor2 *types.SemanticAnchor,
) *float64 {

	// Generate pattern key from anchor characteristics
	key := generatePatternKey(anchor1, anchor2)

	a.learnedMutex.RLock()
	defer a.learnedMutex.RUnlock()

	if distance, exists := a.learnedDistances[key]; exists {
		a.logger.Debug("Found learned distance", "key", key, "distance", distance)
		return &distance
	}

	return nil
}

// learnDistance stores a distance pattern for future use
func (a *ComputationAgent) learnDistance(
	anchor1, anchor2 *types.SemanticAnchor,
	distance float64,
) {
	key := generatePatternKey(anchor1, anchor2)

	a.learnedMutex.Lock()
	defer a.learnedMutex.Unlock()

	// Store or update learned distance (average multiple observations)
	if existing, exists := a.learnedDistances[key]; exists {
		// Average with existing
		a.learnedDistances[key] = (existing + distance) / 2.0
		a.logger.Debug("Updated learned distance",
			"key", key,
			"old", existing,
			"new", a.learnedDistances[key])
	} else {
		a.learnedDistances[key] = distance
		a.logger.Debug("Learned new distance", "key", key, "distance", distance)
	}
}

// generatePatternKey creates a canonical key from anchor characteristics
func generatePatternKey(anchor1, anchor2 *types.SemanticAnchor) string {
	// Generate key from semantic characteristics
	// Format: "location1_timeofday1_daytype1->location2_timeofday2_daytype2"

	loc1 := anchor1.Location
	loc2 := anchor2.Location

	time1 := getContextValue(anchor1.Context, "time_of_day")
	time2 := getContextValue(anchor2.Context, "time_of_day")

	day1 := getContextValue(anchor1.Context, "day_type")
	day2 := getContextValue(anchor2.Context, "day_type")

	// Canonical ordering (alphabetical)
	if loc1 > loc2 {
		loc1, loc2 = loc2, loc1
		time1, time2 = time2, time1
		day1, day2 = day2, day1
	}

	return fmt.Sprintf("%s_%s_%s->%s_%s_%s",
		loc1, time1, day1,
		loc2, time2, day2)
}

// getContextValue safely extracts string value from context map
func getContextValue(context map[string]interface{}, key string) string {
	if val, ok := context[key].(string); ok {
		return val
	}
	return "unknown"
}

func (a *ComputationAgent) loadLearnedDistances(ctx context.Context) error {
	// TODO: Implement persistent storage of learned distances
	// For now, starts empty each run
	a.logger.Debug("Loaded learned distances", "count", len(a.learnedDistances))
	return nil
}

func (a *ComputationAgent) publishCompletion(distancesComputed int) {
	payload := map[string]interface{}{
		"distances_computed": distancesComputed,
		"timestamp":          time.Now().Format(time.RFC3339),
	}

	payloadBytes, _ := json.Marshal(payload)
	if err := a.mqtt.Publish("automation/behavior/distances/completed", 0, false, payloadBytes); err != nil {
		a.logger.Error("Failed to publish completion", "error", err)
	} else {
		a.logger.Info("Published distance computation completion",
			"distances_computed", distancesComputed)
	}
}
