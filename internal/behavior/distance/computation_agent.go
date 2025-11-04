package distance

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"
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

	// Learned patterns with temporal decay (NEW!)
	learnedPatternStorage *LearnedPatternStorage
	learnedPatternConfig  LearnedPatternConfig
	patternCache          map[string]*LearnedPattern // In-memory cache
	observationCache      map[string][]Observation   // In-memory cache
	cacheMutex            sync.RWMutex

	// Legacy learned distance patterns (deprecated, keeping for compatibility)
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
		patternCache:        make(map[string]*LearnedPattern),
		observationCache:    make(map[string][]Observation),
		learnedPatternConfig: DefaultLearnedPatternConfig(),
		// Note: learnedPatternStorage will be set via SetLearnedPatternStorage() after construction
	}
}

// SetLearnedPatternStorage sets the learned pattern storage (called after agent creation)
func (a *ComputationAgent) SetLearnedPatternStorage(db *sql.DB) {
	a.learnedPatternStorage = NewLearnedPatternStorage(db, a.logger)
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

// computeVectorDistance calculates structured distance between embeddings
func (a *ComputationAgent) computeVectorDistance(
	anchor1, anchor2 *types.SemanticAnchor,
) (float64, string, error) {

	// Use structured distance that respects semantic blocks
	distance := structuredDist(anchor1.SemanticEmbedding, anchor2.SemanticEmbedding)

	a.logger.Debug("Computed structured vector distance",
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

// structuredDist computes distance using block-wise metrics for 128D structured tensor
// Embedding structure:
// [0-3]:   Temporal cyclical (hour, day of week)
// [4-7]:   Seasonal cyclical (day of year, month)
// [8-11]:  Day type (weekday/weekend/holiday, time period)
// [12-27]: Spatial (location embedding)
// [28-43]: Weather context
// [44-59]: Lighting context
// [60-79]: Activity signals
// [80-95]: Household rhythm
// [96-127]: Reserved for learned features
func structuredDist(v1, v2 pgvector.Vector) float64 {
	s1 := v1.Slice()
	s2 := v2.Slice()

	// 1. Temporal distance (cyclic, dimensions 0-3)
	temporalDist := cyclicDistance(s1[0:4], s2[0:4])

	// 2. Seasonal distance (cyclic, dimensions 4-7)
	seasonalDist := cyclicDistance(s1[4:8], s2[4:8])

	// 3. Day type distance (categorical, dimensions 8-11)
	dayTypeDist := euclideanDistance(s1[8:12], s2[8:12])

	// 4. Spatial/Location distance (semantic, dimensions 12-27)
	// Use cosine for LLM-derived embeddings
	spatialDist := 1.0 - cosineSimilaritySlice(s1[12:28], s2[12:28])

	// 5. Weather distance (continuous, dimensions 28-43)
	weatherDist := euclideanDistance(s1[28:44], s2[28:44])

	// 6. Lighting distance (dimensions 44-59)
	lightingDist := euclideanDistance(s1[44:60], s2[44:60])

	// 7. Activity signals (dimensions 60-79)
	activityDist := euclideanDistance(s1[60:80], s2[60:80])

	// 8. Household rhythm (dimensions 80-95)
	rhythmDist := euclideanDistance(s1[80:96], s2[80:96])

	// Weighted combination
	// Location and activity are most important for semantic distance
	distance := 0.10*temporalDist +
		0.05*seasonalDist +
		0.10*dayTypeDist +
		0.30*spatialDist +
		0.05*weatherDist +
		0.10*lightingDist +
		0.25*activityDist +
		0.05*rhythmDist

	return math.Max(0, math.Min(1, distance))
}

// cyclicDistance computes distance for cyclic dimensions (sin/cos encoded)
func cyclicDistance(v1, v2 []float32) float64 {
	// For sin/cos pairs, compute angular distance
	// Assuming pairs: [sin1, cos1, sin2, cos2, ...]
	var totalDist float64
	pairs := len(v1) / 2

	for i := 0; i < pairs; i++ {
		sin1 := float64(v1[i*2])
		cos1 := float64(v1[i*2+1])
		sin2 := float64(v2[i*2])
		cos2 := float64(v2[i*2+1])

		// Dot product of unit vectors gives cos(angle)
		dotProd := sin1*sin2 + cos1*cos2
		// Clamp to [-1, 1] to handle floating point errors
		dotProd = math.Max(-1.0, math.Min(1.0, dotProd))

		// Angular distance: acos(dot) / π to normalize to [0, 1]
		angle := math.Acos(dotProd)
		totalDist += angle / math.Pi
	}

	return totalDist / float64(pairs)
}

// euclideanDistance computes normalized Euclidean distance
func euclideanDistance(v1, v2 []float32) float64 {
	var sum float64
	for i := 0; i < len(v1); i++ {
		diff := float64(v1[i]) - float64(v2[i])
		sum += diff * diff
	}
	// Normalize by sqrt(dimensions) to get range roughly [0, 1]
	// Since embeddings are normalized, max distance is sqrt(2)
	distance := math.Sqrt(sum) / math.Sqrt(2.0)
	return math.Min(1.0, distance)
}

// cosineSimilaritySlice computes cosine similarity for a slice
func cosineSimilaritySlice(v1, v2 []float32) float64 {
	var dot, mag1, mag2 float64
	for i := 0; i < len(v1); i++ {
		dot += float64(v1[i]) * float64(v2[i])
		mag1 += float64(v1[i]) * float64(v1[i])
		mag2 += float64(v2[i]) * float64(v2[i])
	}

	if mag1 == 0 || mag2 == 0 {
		return 0
	}

	return dot / (math.Sqrt(mag1) * math.Sqrt(mag2))
}

// computeHybridDistance uses vector for clear cases, LLM for ambiguous ones
func (a *ComputationAgent) computeHybridDistance(
	ctx context.Context,
	anchor1, anchor2 *types.SemanticAnchor,
) (float64, string, error) {

	// Always compute structured vector distance first (it's fast)
	vectorDist := structuredDist(anchor1.SemanticEmbedding, anchor2.SemanticEmbedding)

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

// SimilarPairCandidate represents a similar pair found in the database
type SimilarPairCandidate struct {
	Anchor1ID      uuid.UUID
	Anchor2ID      uuid.UUID
	Distance       float64
	Source         string
	ComputedAt     time.Time
	Location1      string
	Location2      string
	Timestamp1     time.Time
	Timestamp2     time.Time
	VectorDistance float64
}

// findSimilarComputedPairs searches DB for similar pairs with LLM distances
func (a *ComputationAgent) findSimilarComputedPairs(
	ctx context.Context,
	anchor1, anchor2 *types.SemanticAnchor,
	vectorDist float64,
) ([]SimilarPairCandidate, error) {
	if a.learnedPatternStorage == nil {
		return nil, fmt.Errorf("learned pattern storage not initialized")
	}

	// Determine location adjacency type for filtering
	sameLocation := anchor1.Location == anchor2.Location
	adjacent := isAdjacentLocations(anchor1.Location, anchor2.Location)

	// Calculate time gap (in minutes) between anchors
	timeGap := math.Abs(anchor2.Timestamp.Sub(anchor1.Timestamp).Minutes())

	// Query for similar pairs from the view
	query := `
		SELECT
			anchor1_id, anchor2_id, distance, source, computed_at,
			location1, location2, timestamp1, timestamp2,
			1 - vector_similarity as vector_distance
		FROM recent_llm_distances
		WHERE
			-- Similar vector distance (±0.15 tolerance)
			ABS(1 - vector_similarity - $1) < 0.15
			-- Same location pattern
			AND (
				-- Both same location
				($2 = true AND location1 = location2)
				-- Both adjacent locations
				OR ($3 = true AND location1 != location2 AND is_adjacent(location1, location2))
				-- Both distant locations
				OR ($2 = false AND $3 = false AND location1 != location2 AND NOT is_adjacent(location1, location2))
			)
			-- Similar time gap (within 30 minutes)
			AND ABS(EXTRACT(EPOCH FROM (timestamp2 - timestamp1))/60 - $4) < 30
			-- Only high-quality LLM computations
			AND source IN ('llm', 'llm_verify', 'llm_seed')
		ORDER BY ABS(1 - vector_similarity - $1) ASC
		LIMIT 10
	`

	rows, err := a.learnedPatternStorage.db.QueryContext(ctx, query,
		vectorDist, sameLocation, adjacent, timeGap)
	if err != nil {
		return nil, fmt.Errorf("failed to query similar pairs: %w", err)
	}
	defer rows.Close()

	var candidates []SimilarPairCandidate
	for rows.Next() {
		var c SimilarPairCandidate
		err := rows.Scan(
			&c.Anchor1ID,
			&c.Anchor2ID,
			&c.Distance,
			&c.Source,
			&c.ComputedAt,
			&c.Location1,
			&c.Location2,
			&c.Timestamp1,
			&c.Timestamp2,
			&c.VectorDistance,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan similar pair: %w", err)
		}
		candidates = append(candidates, c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating similar pairs: %w", err)
	}

	a.logger.Debug("Found similar pair candidates",
		"anchor1", anchor1.ID,
		"anchor2", anchor2.ID,
		"vector_dist", vectorDist,
		"candidates", len(candidates))

	return candidates, nil
}

// checkSimilarityConsistency checks if similar pairs have consistent distances
func (a *ComputationAgent) checkSimilarityConsistency(
	candidates []SimilarPairCandidate,
	maxStdDev float64,
) (bool, float64) {
	if len(candidates) < 2 {
		return false, 0.0
	}

	// Calculate mean and standard deviation
	sum := 0.0
	for _, c := range candidates {
		sum += c.Distance
	}
	mean := sum / float64(len(candidates))

	variance := 0.0
	for _, c := range candidates {
		diff := c.Distance - mean
		variance += diff * diff
	}
	variance = variance / float64(len(candidates))
	stdDev := math.Sqrt(variance)

	// Check consistency
	consistent := stdDev <= maxStdDev

	a.logger.Debug("Similarity consistency check",
		"candidates", len(candidates),
		"mean", mean,
		"stddev", stdDev,
		"max_stddev", maxStdDev,
		"consistent", consistent)

	return consistent, mean
}

// computeProgressiveLearnedDistance implements progressive learning strategy with temporal decay
func (a *ComputationAgent) computeProgressiveLearnedDistance(
	ctx context.Context,
	anchor1, anchor2 *types.SemanticAnchor,
) (float64, string, error) {
	a.learnedMutex.Lock()
	a.totalComputations++
	currentTotal := a.totalComputations
	a.learnedMutex.Unlock()

	now := a.timeManager.Now()

	// ===========================================
	// PHASE 1: Vector Screening (ALWAYS)
	// ===========================================
	// Fast structured distance screening to filter obvious cases
	vectorDist := structuredDist(anchor1.SemanticEmbedding, anchor2.SemanticEmbedding)

	// Very similar - high confidence, skip LLM (after initial seeding)
	if vectorDist < 0.10 && currentTotal > 50 {
		a.logger.Debug("Progressive: Vector screening - very similar",
			"anchor1", anchor1.ID,
			"anchor2", anchor2.ID,
			"vector_dist", vectorDist)
		return vectorDist, "vector_similar", nil
	}

	// Very different - high confidence, skip LLM
	if vectorDist > 0.70 {
		a.logger.Debug("Progressive: Vector screening - very different",
			"anchor1", anchor1.ID,
			"anchor2", anchor2.ID,
			"vector_dist", vectorDist)
		return vectorDist, "vector_different", nil
	}

	// Ambiguous range (0.10-0.70) - continue to learned pattern lookup

	// ===========================================
	// PHASE 2: Exact Pattern Lookup with Temporal Decay
	// ===========================================
	patternKey := generatePatternKey(anchor1, anchor2)

	// Try cache first
	a.cacheMutex.RLock()
	cachedPattern, hasCached := a.patternCache[patternKey]
	cachedObservations, hasObservations := a.observationCache[patternKey]
	a.cacheMutex.RUnlock()

	// Load from DB if not in cache
	if !hasCached && a.learnedPatternStorage != nil {
		pattern, observations, err := a.learnedPatternStorage.LoadPattern(ctx, patternKey)
		if err == nil && pattern != nil {
			// Prune old observations
			observations = PruneObservations(observations, now, a.learnedPatternConfig)

			// Recompute with temporal decay
			weightedDistance, confidence := pattern.ComputeWeightedDistance(observations, now, a.learnedPatternConfig)
			pattern.WeightedDistance = weightedDistance
			pattern.ConfidenceScore = confidence
			pattern.LastComputed = now

			// Update cache
			a.cacheMutex.Lock()
			a.patternCache[patternKey] = pattern
			a.observationCache[patternKey] = observations
			a.cacheMutex.Unlock()

			cachedPattern = pattern
			cachedObservations = observations
			hasCached = true
			hasObservations = true
		}
	}

	if hasCached && hasObservations && len(cachedObservations) > 0 {
		// Recompute with current time (apply decay)
		weightedDistance, confidence := cachedPattern.ComputeWeightedDistance(
			cachedObservations, now, a.learnedPatternConfig)

		if confidence >= a.learnedPatternConfig.HighConfidenceThreshold {
			a.logger.Debug("Progressive: Exact pattern - high confidence",
				"pattern_key", patternKey,
				"confidence", confidence,
				"distance", weightedDistance,
				"observations", len(cachedObservations))

			// IMPORTANT: Record the actual vector distance as an observation
			// This allows the pattern to learn from real data and build variance
			go a.recordObservationWithMetadata(ctx, anchor1, anchor2, vectorDist, "learned_reuse", vectorDist)

			return weightedDistance, "learned_high_conf", nil
		}

		if confidence >= a.learnedPatternConfig.MediumConfidenceThreshold {
			// Medium confidence - use but maybe queue for verification
			a.logger.Debug("Progressive: Exact pattern - medium confidence",
				"pattern_key", patternKey,
				"confidence", confidence,
				"distance", weightedDistance)

			// IMPORTANT: Record the actual vector distance to update the pattern
			go a.recordObservationWithMetadata(ctx, anchor1, anchor2, vectorDist, "learned_reuse", vectorDist)

			// Check if confidence is dropping
			if confidence < a.learnedPatternConfig.RelearnConfidenceThreshold*1.5 {
				// Queue for re-learning if available
				if a.learnedPatternStorage != nil {
					_ = a.learnedPatternStorage.QueueForRelearning(ctx, patternKey,
						"confidence_declining", 5, confidence, weightedDistance)
				}
			}

			return weightedDistance, "learned_medium_conf", nil
		}

		// Low confidence - continue to similarity lookup
	}

	// ===========================================
	// PHASE 3: Similarity-Based Cache Lookup (NEW!)
	// ===========================================
	// Only after building base library
	if currentTotal > 100 && a.learnedPatternStorage != nil {
		similarPairs, err := a.findSimilarComputedPairs(ctx, anchor1, anchor2, vectorDist)
		if err == nil && len(similarPairs) >= 2 {
			// Check consistency of similar pairs
			consistent, avgDistance := a.checkSimilarityConsistency(similarPairs, 0.10)

			if consistent {
				a.logger.Debug("Progressive: Similarity-based cache hit",
					"anchor1", anchor1.ID,
					"anchor2", anchor2.ID,
					"similar_pairs", len(similarPairs),
					"avg_distance", avgDistance)

				// Record this as an observation (with lower weight)
				a.recordObservationWithMetadata(ctx, anchor1, anchor2, avgDistance,
					"similarity_cached", vectorDist)

				return avgDistance, "similarity_cached", nil
			}
		}
	}

	// ===========================================
	// PHASE 4: Strategic LLM Computation
	// ===========================================
	// Use LLM for:
	// 1. Initial seeding (first ~150 pairs - diverse sampling)
	// 2. Novel patterns not in cache
	// 3. Verification queue processing

	shouldUseLLM := false
	source := "llm"

	// Initial seeding phase
	if currentTotal <= 150 {
		if a.shouldSampleForLearning(anchor1, anchor2) {
			shouldUseLLM = true
			source = "llm_seed"
			a.logger.Debug("Progressive: LLM seeding",
				"computation", currentTotal,
				"anchor1", anchor1.ID,
				"anchor2", anchor2.ID)
		}
	} else {
		// After seeding, use LLM for novel patterns
		shouldUseLLM = true
		a.logger.Debug("Progressive: Novel pattern - using LLM",
			"pattern_key", patternKey,
			"computation", currentTotal)
	}

	if shouldUseLLM {
		dist, _, err := a.computeLLMDistance(ctx, anchor1, anchor2)
		if err == nil {
			// Record observation with full weight
			a.recordObservationWithMetadata(ctx, anchor1, anchor2, dist, source, vectorDist)
			return dist, source, nil
		}

		// LLM failed - log warning
		a.logger.Warn("LLM computation failed, using vector fallback",
			"error", err,
			"anchor1", anchor1.ID,
			"anchor2", anchor2.ID)
	}

	// ===========================================
	// FALLBACK: Vector Distance
	// ===========================================
	a.logger.Debug("Progressive: Using vector fallback",
		"anchor1", anchor1.ID,
		"anchor2", anchor2.ID,
		"vector_dist", vectorDist)

	return vectorDist, "vector_fallback", nil
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

// recordObservation adds an observation to the learned model (legacy method)
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

// recordObservationWithMetadata records an observation with full metadata and temporal decay support
func (a *ComputationAgent) recordObservationWithMetadata(
	ctx context.Context,
	anchor1, anchor2 *types.SemanticAnchor,
	distance float64,
	source string,
	vectorDistance float64,
) {
	if a.learnedPatternStorage == nil {
		// Fallback to legacy method if storage not available
		a.recordObservation(anchor1, anchor2, distance)
		return
	}

	patternKey := generatePatternKey(anchor1, anchor2)
	now := a.timeManager.Now()

	// Extract context for the observation
	season := getCurrentSeason(now)
	dayType := getDayType(now)
	timeOfDay := getContextValue(anchor1.Context, "time_of_day")

	// Get observation weight based on source
	weight := GetObservationWeight(source, a.learnedPatternConfig)

	// Create observation
	obs := Observation{
		ID:             uuid.New(),
		PatternKey:     patternKey,
		Distance:       distance,
		Source:         source,
		Timestamp:      now,
		Weight:         weight,
		Season:         season,
		DayType:        dayType,
		TimeOfDay:      timeOfDay,
		Anchor1ID:      &anchor1.ID,
		Anchor2ID:      &anchor2.ID,
		VectorDistance: &vectorDistance,
	}

	// Save observation to database
	if err := a.learnedPatternStorage.SaveObservation(ctx, &obs); err != nil {
		a.logger.Error("Failed to save observation",
			"pattern_key", patternKey,
			"error", err)
		// Continue with cache update even if DB save fails
	}

	// Update cache
	a.cacheMutex.Lock()
	defer a.cacheMutex.Unlock()

	// Add to observation cache
	if _, exists := a.observationCache[patternKey]; !exists {
		a.observationCache[patternKey] = make([]Observation, 0)
	}
	a.observationCache[patternKey] = append(a.observationCache[patternKey], obs)

	// Prune old observations from cache
	a.observationCache[patternKey] = PruneObservations(
		a.observationCache[patternKey], now, a.learnedPatternConfig)

	// Load or create pattern
	pattern, exists := a.patternCache[patternKey]
	if !exists {
		// Extract pattern characteristics from key
		loc1 := anchor1.Location
		loc2 := anchor2.Location
		timeOfDay1 := getContextValue(anchor1.Context, "time_of_day")
		timeOfDay2 := getContextValue(anchor2.Context, "time_of_day")
		dayType1 := getContextValue(anchor1.Context, "day_type")
		dayType2 := getContextValue(anchor2.Context, "day_type")

		pattern = &LearnedPattern{
			PatternKey:         patternKey,
			FirstSeen:          now,
			LastUpdated:        now,
			LastComputed:       now,
			DecayHalfLifeHours: a.learnedPatternConfig.DecayHalfLifeDays * 24,
			Location1:          loc1,
			Location2:          loc2,
			TimeOfDay1:         timeOfDay1,
			TimeOfDay2:         timeOfDay2,
			DayType1:           dayType1,
			DayType2:           dayType2,
			SampleAnchor1ID:    &anchor1.ID,
			SampleAnchor2ID:    &anchor2.ID,
		}
	}

	// Recompute pattern with all observations
	observations := a.observationCache[patternKey]
	weightedDistance, confidence := pattern.ComputeWeightedDistance(observations, now, a.learnedPatternConfig)

	pattern.WeightedDistance = weightedDistance
	pattern.ConfidenceScore = confidence
	pattern.ObservationCount = len(observations)
	pattern.LastUpdated = now
	pattern.LastComputed = now

	// Calculate statistics
	if len(observations) > 0 {
		minDist := observations[0].Distance
		maxDist := observations[0].Distance
		for _, o := range observations {
			if o.Distance < minDist {
				minDist = o.Distance
			}
			if o.Distance > maxDist {
				maxDist = o.Distance
			}
		}
		_, stdDev := computeStats(observations)
		pattern.MinDistance = minDist
		pattern.MaxDistance = maxDist
		pattern.StdDeviation = stdDev
	}

	a.patternCache[patternKey] = pattern

	// Async save pattern to database
	go func() {
		if err := a.learnedPatternStorage.SavePattern(context.Background(), pattern); err != nil {
			a.logger.Error("Failed to save learned pattern",
				"pattern_key", patternKey,
				"error", err)
		}
	}()

	a.logger.Debug("Recorded observation with metadata",
		"pattern_key", patternKey,
		"distance", distance,
		"source", source,
		"weight", weight,
		"confidence", confidence,
		"observations", len(observations))
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
