package distance

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
)

// LearnedPattern represents a pattern with temporal decay support
type LearnedPattern struct {
	PatternKey        string
	WeightedDistance  float64
	ConfidenceScore   float64
	ObservationCount  int
	FirstSeen         time.Time
	LastUpdated       time.Time
	LastComputed      time.Time
	DecayHalfLifeHours int

	// Pattern characteristics (for querying)
	Location1   string
	Location2   string
	TimeOfDay1  string
	TimeOfDay2  string
	DayType1    string
	DayType2    string

	// Statistics
	MinDistance   float64
	MaxDistance   float64
	StdDeviation  float64

	// Sample references
	SampleAnchor1ID *uuid.UUID
	SampleAnchor2ID *uuid.UUID
}

// Observation represents a single distance observation with metadata
type Observation struct {
	ID             uuid.UUID
	PatternKey     string
	Distance       float64
	Source         string // 'llm', 'llm_verify', 'llm_seed', 'similarity_cached', 'vector'
	Timestamp      time.Time
	Weight         float64
	Season         string
	DayType        string
	TimeOfDay      string
	Anchor1ID      *uuid.UUID
	Anchor2ID      *uuid.UUID
	VectorDistance *float64
}

// LearnedPatternConfig configures temporal decay behavior
type LearnedPatternConfig struct {
	// Temporal decay settings
	DecayHalfLifeDays        int     // Default: 30 days
	MaxObservationAgeDays    int     // Discard observations older than this (90 days)
	MaxObservationsPerPattern int    // Keep only N most recent (20)

	// Confidence thresholds
	HighConfidenceThreshold    float64 // 0.80
	MediumConfidenceThreshold  float64 // 0.50
	RelearnConfidenceThreshold float64 // 0.40 - trigger re-learning

	// Contextual decay modifiers
	SeasonChangeDecayMultiplier  float64 // 0.5 (2x faster decay)
	DayTypeChangeDecayMultiplier float64 // 0.7 (1.4x faster)
	DSTTransitionDecayMultiplier float64 // 0.7

	// Observation weighting
	WeightLLM              float64 // 1.0
	WeightLLMVerify        float64 // 1.0
	WeightLLMSeed          float64 // 1.2
	WeightLearnedReuse     float64 // 0.8 - reusing a learned pattern validates it
	WeightSimilarityCached float64 // 0.5
	WeightVector           float64 // 0.3

	// Outlier rejection
	OutlierRejectionEnabled     bool
	OutlierStdDevThreshold      float64 // 2.0 - reject observations > 2 std deviations
	MinObservationsForOutlierDetection int // 5
}

// DefaultLearnedPatternConfig returns default configuration
func DefaultLearnedPatternConfig() LearnedPatternConfig {
	return LearnedPatternConfig{
		DecayHalfLifeDays:         30,
		MaxObservationAgeDays:     90,
		MaxObservationsPerPattern: 20,

		HighConfidenceThreshold:    0.80,
		MediumConfidenceThreshold:  0.50,
		RelearnConfidenceThreshold: 0.40,

		SeasonChangeDecayMultiplier:  0.5,
		DayTypeChangeDecayMultiplier: 0.7,
		DSTTransitionDecayMultiplier: 0.7,

		WeightLLM:              1.0,
		WeightLLMVerify:        1.0,
		WeightLLMSeed:          1.2,
		WeightLearnedReuse:     0.8,
		WeightSimilarityCached: 0.5,
		WeightVector:           0.3,

		OutlierRejectionEnabled:         true,
		OutlierStdDevThreshold:          2.0,
		MinObservationsForOutlierDetection: 5,
	}
}

// ComputeWeightedDistance calculates the weighted average distance with temporal decay
func (p *LearnedPattern) ComputeWeightedDistance(observations []Observation, now time.Time, config LearnedPatternConfig) (float64, float64) {
	if len(observations) == 0 {
		return 0.0, 0.0
	}

	// STEP 1: Filter by age (discard observations older than max age)
	maxAge := time.Duration(config.MaxObservationAgeDays) * 24 * time.Hour
	validObs := make([]Observation, 0)
	for _, obs := range observations {
		if now.Sub(obs.Timestamp) <= maxAge {
			validObs = append(validObs, obs)
		}
	}

	if len(validObs) == 0 {
		return 0.0, 0.0
	}

	// STEP 2: Reject outliers (if enabled and enough observations)
	if config.OutlierRejectionEnabled && len(validObs) >= config.MinObservationsForOutlierDetection {
		mean, stdDev := computeStats(validObs)
		filtered := make([]Observation, 0)

		for _, obs := range validObs {
			if math.Abs(obs.Distance-mean) <= config.OutlierStdDevThreshold*stdDev {
				filtered = append(filtered, obs)
			}
		}

		if len(filtered) > 0 {
			validObs = filtered
		}
	}

	// STEP 3: Apply exponential decay with contextual modifiers
	totalWeight := 0.0
	weightedSum := 0.0
	decayHalfLife := float64(config.DecayHalfLifeDays * 24) // Convert to hours

	for _, obs := range validObs {
		// Base exponential decay: weight = e^(-λt)
		age := now.Sub(obs.Timestamp).Hours()
		decayFactor := math.Exp(-age / decayHalfLife)

		// Contextual decay adjustments
		contextPenalty := computeContextualDecay(obs, now, config)

		// Source-based weight
		sourceWeight := obs.Weight

		// Final weight
		weight := sourceWeight * decayFactor * contextPenalty

		weightedSum += obs.Distance * weight
		totalWeight += weight
	}

	if totalWeight == 0 {
		return 0.0, 0.0
	}

	weightedDistance := weightedSum / totalWeight

	// STEP 4: Compute confidence
	confidence := computeConfidence(validObs, totalWeight, now, config)

	return weightedDistance, confidence
}

// computeContextualDecay applies context-based decay modifiers
func computeContextualDecay(obs Observation, now time.Time, config LearnedPatternConfig) float64 {
	penalty := 1.0

	// Season change? → faster decay
	if getCurrentSeason(now) != obs.Season {
		penalty *= config.SeasonChangeDecayMultiplier
	}

	// Day type change? → faster decay
	currentDayType := getDayType(now)
	if currentDayType != obs.DayType {
		penalty *= config.DayTypeChangeDecayMultiplier
	}

	// DST transition? → faster decay
	if crossesDSTBoundary(obs.Timestamp, now) {
		penalty *= config.DSTTransitionDecayMultiplier
	}

	return penalty
}

// computeConfidence calculates confidence score based on multiple factors
func computeConfidence(observations []Observation, totalWeight float64, now time.Time, config LearnedPatternConfig) float64 {
	if len(observations) == 0 {
		return 0.0
	}

	// Factor 1: Number of observations (more = higher)
	// Confidence increases with observations, max at 10
	obsConfidence := math.Min(float64(len(observations))/10.0, 1.0)

	// Factor 2: Total weight (accounts for recency)
	// Higher total weight means more recent/reliable observations
	weightConfidence := math.Min(totalWeight/5.0, 1.0)

	// Factor 3: Recency (most recent observation)
	mostRecentIdx := 0
	mostRecentTime := observations[0].Timestamp
	for i, obs := range observations {
		if obs.Timestamp.After(mostRecentTime) {
			mostRecentTime = obs.Timestamp
			mostRecentIdx = i
		}
	}
	daysSince := now.Sub(observations[mostRecentIdx].Timestamp).Hours() / 24.0
	recencyConfidence := math.Exp(-daysSince / 30.0) // Decay over 30 days

	// Factor 4: Consistency (low variance = higher confidence)
	_, stdDev := computeStats(observations)
	consistencyConfidence := math.Max(0.0, 1.0-stdDev*5.0) // Penalty for variance

	// Weighted combination
	confidence := (obsConfidence * 0.3) +
		(weightConfidence * 0.2) +
		(recencyConfidence * 0.3) +
		(consistencyConfidence * 0.2)

	return math.Max(0.0, math.Min(1.0, confidence))
}

// computeStats calculates mean and standard deviation
func computeStats(observations []Observation) (float64, float64) {
	if len(observations) == 0 {
		return 0.0, 0.0
	}

	// Calculate mean
	sum := 0.0
	for _, obs := range observations {
		sum += obs.Distance
	}
	mean := sum / float64(len(observations))

	// Calculate standard deviation
	variance := 0.0
	for _, obs := range observations {
		diff := obs.Distance - mean
		variance += diff * diff
	}
	variance = variance / float64(len(observations))
	stdDev := math.Sqrt(variance)

	return mean, stdDev
}

// getCurrentSeason returns the season for a given timestamp
func getCurrentSeason(t time.Time) string {
	month := t.Month()
	switch {
	case month == 12 || month <= 2:
		return "winter"
	case month >= 3 && month <= 5:
		return "spring"
	case month >= 6 && month <= 8:
		return "summer"
	default:
		return "fall"
	}
}

// getDayType returns the day type (weekday/weekend)
func getDayType(t time.Time) string {
	weekday := t.Weekday()
	if weekday == time.Saturday || weekday == time.Sunday {
		return "weekend"
	}
	return "weekday"
}

// crossesDSTBoundary checks if a time range crosses DST transition
func crossesDSTBoundary(start, end time.Time) bool {
	// Simple heuristic: if UTC offset changed, crossed DST
	_, startOffset := start.Zone()
	_, endOffset := end.Zone()
	return startOffset != endOffset
}

// PruneObservations removes old observations and keeps only most recent N
func PruneObservations(observations []Observation, now time.Time, config LearnedPatternConfig) []Observation {
	// STEP 1: Remove observations older than max age
	maxAge := time.Duration(config.MaxObservationAgeDays) * 24 * time.Hour
	valid := make([]Observation, 0)

	for _, obs := range observations {
		if now.Sub(obs.Timestamp) <= maxAge {
			valid = append(valid, obs)
		}
	}

	// STEP 2: Keep only most recent N observations
	if len(valid) > config.MaxObservationsPerPattern {
		// Sort by timestamp descending
		sort.Slice(valid, func(i, j int) bool {
			return valid[i].Timestamp.After(valid[j].Timestamp)
		})
		valid = valid[:config.MaxObservationsPerPattern]
	}

	return valid
}

// GetObservationWeight returns the appropriate weight for a given source
func GetObservationWeight(source string, config LearnedPatternConfig) float64 {
	switch source {
	case "llm":
		return config.WeightLLM
	case "llm_verify":
		return config.WeightLLMVerify
	case "llm_seed":
		return config.WeightLLMSeed
	case "learned_reuse":
		return config.WeightLearnedReuse
	case "similarity_cached":
		return config.WeightSimilarityCached
	case "vector":
		return config.WeightVector
	default:
		return 1.0
	}
}

// LearnedPatternStorage handles database operations for learned patterns
type LearnedPatternStorage struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewLearnedPatternStorage creates a new storage instance
func NewLearnedPatternStorage(db *sql.DB, logger *slog.Logger) *LearnedPatternStorage {
	return &LearnedPatternStorage{
		db:     db,
		logger: logger,
	}
}

// LoadPattern loads a learned pattern with its observations
func (s *LearnedPatternStorage) LoadPattern(ctx context.Context, patternKey string) (*LearnedPattern, []Observation, error) {
	// Load pattern
	query := `
		SELECT pattern_key, weighted_distance, confidence_score, observation_count,
		       first_seen, last_updated, last_computed, decay_half_life_hours,
		       location1, location2, time_of_day1, time_of_day2, day_type1, day_type2,
		       min_distance, max_distance, std_deviation,
		       sample_anchor1_id, sample_anchor2_id
		FROM learned_patterns
		WHERE pattern_key = $1
	`

	var pattern LearnedPattern
	err := s.db.QueryRowContext(ctx, query, patternKey).Scan(
		&pattern.PatternKey,
		&pattern.WeightedDistance,
		&pattern.ConfidenceScore,
		&pattern.ObservationCount,
		&pattern.FirstSeen,
		&pattern.LastUpdated,
		&pattern.LastComputed,
		&pattern.DecayHalfLifeHours,
		&pattern.Location1,
		&pattern.Location2,
		&pattern.TimeOfDay1,
		&pattern.TimeOfDay2,
		&pattern.DayType1,
		&pattern.DayType2,
		&pattern.MinDistance,
		&pattern.MaxDistance,
		&pattern.StdDeviation,
		&pattern.SampleAnchor1ID,
		&pattern.SampleAnchor2ID,
	)

	if err == sql.ErrNoRows {
		return nil, nil, nil // Pattern not found
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load pattern: %w", err)
	}

	// Load observations
	observations, err := s.LoadObservations(ctx, patternKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load observations: %w", err)
	}

	return &pattern, observations, nil
}

// LoadObservations loads all observations for a pattern
func (s *LearnedPatternStorage) LoadObservations(ctx context.Context, patternKey string) ([]Observation, error) {
	query := `
		SELECT id, pattern_key, distance, source, timestamp, weight,
		       season, day_type, time_of_day, anchor1_id, anchor2_id, vector_distance
		FROM pattern_observations
		WHERE pattern_key = $1
		ORDER BY timestamp DESC
	`

	rows, err := s.db.QueryContext(ctx, query, patternKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var observations []Observation
	for rows.Next() {
		var obs Observation
		err := rows.Scan(
			&obs.ID,
			&obs.PatternKey,
			&obs.Distance,
			&obs.Source,
			&obs.Timestamp,
			&obs.Weight,
			&obs.Season,
			&obs.DayType,
			&obs.TimeOfDay,
			&obs.Anchor1ID,
			&obs.Anchor2ID,
			&obs.VectorDistance,
		)
		if err != nil {
			return nil, err
		}
		observations = append(observations, obs)
	}

	return observations, rows.Err()
}

// SavePattern saves or updates a learned pattern
func (s *LearnedPatternStorage) SavePattern(ctx context.Context, pattern *LearnedPattern) error {
	query := `
		INSERT INTO learned_patterns (
			pattern_key, weighted_distance, confidence_score, observation_count,
			first_seen, last_updated, last_computed, decay_half_life_hours,
			location1, location2, time_of_day1, time_of_day2, day_type1, day_type2,
			min_distance, max_distance, std_deviation,
			sample_anchor1_id, sample_anchor2_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
		ON CONFLICT (pattern_key) DO UPDATE SET
			weighted_distance = EXCLUDED.weighted_distance,
			confidence_score = EXCLUDED.confidence_score,
			observation_count = EXCLUDED.observation_count,
			last_updated = EXCLUDED.last_updated,
			last_computed = EXCLUDED.last_computed,
			decay_half_life_hours = EXCLUDED.decay_half_life_hours,
			min_distance = EXCLUDED.min_distance,
			max_distance = EXCLUDED.max_distance,
			std_deviation = EXCLUDED.std_deviation,
			sample_anchor1_id = EXCLUDED.sample_anchor1_id,
			sample_anchor2_id = EXCLUDED.sample_anchor2_id
	`

	_, err := s.db.ExecContext(ctx, query,
		pattern.PatternKey,
		pattern.WeightedDistance,
		pattern.ConfidenceScore,
		pattern.ObservationCount,
		pattern.FirstSeen,
		pattern.LastUpdated,
		pattern.LastComputed,
		pattern.DecayHalfLifeHours,
		pattern.Location1,
		pattern.Location2,
		pattern.TimeOfDay1,
		pattern.TimeOfDay2,
		pattern.DayType1,
		pattern.DayType2,
		pattern.MinDistance,
		pattern.MaxDistance,
		pattern.StdDeviation,
		pattern.SampleAnchor1ID,
		pattern.SampleAnchor2ID,
	)

	return err
}

// SaveObservation saves a new observation
func (s *LearnedPatternStorage) SaveObservation(ctx context.Context, obs *Observation) error {
	query := `
		INSERT INTO pattern_observations (
			id, pattern_key, distance, source, timestamp, weight,
			season, day_type, time_of_day, anchor1_id, anchor2_id, vector_distance
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`

	if obs.ID == uuid.Nil {
		obs.ID = uuid.New()
	}

	_, err := s.db.ExecContext(ctx, query,
		obs.ID,
		obs.PatternKey,
		obs.Distance,
		obs.Source,
		obs.Timestamp,
		obs.Weight,
		obs.Season,
		obs.DayType,
		obs.TimeOfDay,
		obs.Anchor1ID,
		obs.Anchor2ID,
		obs.VectorDistance,
	)

	return err
}

// DeleteOldObservations removes observations older than max age
func (s *LearnedPatternStorage) DeleteOldObservations(ctx context.Context, patternKey string, maxAgeDays int) error {
	query := `
		DELETE FROM pattern_observations
		WHERE pattern_key = $1
		  AND timestamp < NOW() - INTERVAL '1 day' * $2
	`

	_, err := s.db.ExecContext(ctx, query, patternKey, maxAgeDays)
	return err
}

// QueueForRelearning adds a pattern to the re-learning queue
func (s *LearnedPatternStorage) QueueForRelearning(ctx context.Context, patternKey, reason string, priority int, originalConfidence, originalDistance float64) error {
	query := `
		INSERT INTO pattern_relearning_queue (
			pattern_key, reason, priority, queued_at, original_confidence, original_distance
		) VALUES ($1, $2, $3, NOW(), $4, $5)
		ON CONFLICT (pattern_key) DO UPDATE SET
			reason = EXCLUDED.reason,
			priority = GREATEST(pattern_relearning_queue.priority, EXCLUDED.priority),
			queued_at = EXCLUDED.queued_at,
			original_confidence = EXCLUDED.original_confidence,
			original_distance = EXCLUDED.original_distance
	`

	_, err := s.db.ExecContext(ctx, query, patternKey, reason, priority, originalConfidence, originalDistance)
	return err
}
