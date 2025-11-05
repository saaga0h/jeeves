package distance

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
	"github.com/saaga0h/jeeves-platform/internal/behavior/types"
)

// TestTimeManager for testing
type TestTimeManager struct {
	currentTime time.Time
}

func (t *TestTimeManager) Now() time.Time {
	if t.currentTime.IsZero() {
		return time.Date(2025, 10, 30, 19, 0, 0, 0, time.UTC)
	}
	return t.currentTime
}

func (t *TestTimeManager) IsTestMode() bool {
	return true
}

// Test Helper Functions

func TestGeneratePatternKey(t *testing.T) {
	tests := []struct {
		name     string
		anchor1  *types.SemanticAnchor
		anchor2  *types.SemanticAnchor
		expected string
	}{
		{
			name: "same location, same time of day",
			anchor1: createTestAnchorWithContext("living_room", time.Now(), map[string]interface{}{
				"time_of_day": "evening",
				"day_type":    "weekday",
			}),
			anchor2: createTestAnchorWithContext("living_room", time.Now().Add(1*time.Hour), map[string]interface{}{
				"time_of_day": "evening",
				"day_type":    "weekday",
			}),
			expected: "living_room_evening_weekday->living_room_evening_weekday",
		},
		{
			name: "different locations, cross-location routine",
			anchor1: createTestAnchorWithContext("bedroom", time.Now(), map[string]interface{}{
				"time_of_day": "morning",
				"day_type":    "weekday",
			}),
			anchor2: createTestAnchorWithContext("bathroom", time.Now().Add(15*time.Minute), map[string]interface{}{
				"time_of_day": "morning",
				"day_type":    "weekday",
			}),
			expected: "bathroom_morning_weekday->bedroom_morning_weekday", // Alphabetical ordering
		},
		{
			name: "canonical ordering (reverse input)",
			anchor1: createTestAnchorWithContext("study", time.Now(), map[string]interface{}{
				"time_of_day": "evening",
				"day_type":    "weekday",
			}),
			anchor2: createTestAnchorWithContext("living_room", time.Now().Add(10*time.Minute), map[string]interface{}{
				"time_of_day": "evening",
				"day_type":    "weekday",
			}),
			expected: "living_room_evening_weekday->study_evening_weekday", // Alphabetical ordering
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := generatePatternKey(tt.anchor1, tt.anchor2)
			if key != tt.expected {
				t.Errorf("Expected key %q, got %q", tt.expected, key)
			}
		})
	}
}

func TestIsAdjacentLocations(t *testing.T) {
	tests := []struct {
		name     string
		loc1     string
		loc2     string
		expected bool
	}{
		{
			name:     "bedroom -> bathroom (adjacent)",
			loc1:     "bedroom",
			loc2:     "bathroom",
			expected: true,
		},
		{
			name:     "kitchen -> dining_room (adjacent)",
			loc1:     "kitchen",
			loc2:     "dining_room",
			expected: true,
		},
		{
			name:     "bedroom -> garage (not adjacent)",
			loc1:     "bedroom",
			loc2:     "garage",
			expected: false,
		},
		{
			name:     "reverse order (bathroom -> bedroom)",
			loc1:     "bathroom",
			loc2:     "bedroom",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isAdjacentLocations(tt.loc1, tt.loc2)
			if result != tt.expected {
				t.Errorf("Expected %v for %s/%s, got %v", tt.expected, tt.loc1, tt.loc2, result)
			}
		})
	}
}

// Test Vector Distance Computation

func TestStructuredDist_IdenticalVectors(t *testing.T) {
	vec := createTestEmbedding(0.0)
	dist := structuredDist(vec, vec)

	// Allow small floating point error
	if dist > 0.01 {
		t.Errorf("Expected distance ~0.0 for identical vectors, got %f", dist)
	}
}

func TestStructuredDist_OrthogonalSpatial(t *testing.T) {
	vec1 := createTestEmbedding(0.0)
	vec2 := createTestEmbedding(1.0)

	dist := structuredDist(vec1, vec2)

	// Spatial component is 30% weight, orthogonal vectors = 1.0 distance
	// Activity component is 25% weight, also differs
	// Total should be in range 0.30-0.45 depending on other components
	if dist < 0.30 || dist > 0.50 {
		t.Errorf("Expected distance 0.30-0.50 for orthogonal vectors, got %f", dist)
	}
}

func TestCyclicDistance_SamePhase(t *testing.T) {
	// Two vectors with same sin/cos values
	v1 := []float32{0.5, 0.866, 0.707, 0.707} // 60°, 45°
	v2 := []float32{0.5, 0.866, 0.707, 0.707} // 60°, 45°

	dist := cyclicDistance(v1, v2)

	// Allow small floating point error
	if dist > 0.01 {
		t.Errorf("Expected cyclic distance ~0.0 for same phase, got %f", dist)
	}
}

func TestCyclicDistance_OppositePhase(t *testing.T) {
	// Two vectors 180° apart
	v1 := []float32{1.0, 0.0}  // 90°
	v2 := []float32{-1.0, 0.0} // 270°

	dist := cyclicDistance(v1, v2)

	// 180° apart = π radians, normalized by π = 1.0
	if dist < 0.95 || dist > 1.0 {
		t.Errorf("Expected cyclic distance ~1.0 for opposite phase, got %f", dist)
	}
}

func TestEuclideanDistance_IdenticalVectors(t *testing.T) {
	v1 := []float32{0.5, 0.5, 0.5}
	v2 := []float32{0.5, 0.5, 0.5}

	dist := euclideanDistance(v1, v2)

	if dist != 0.0 {
		t.Errorf("Expected euclidean distance 0.0 for identical vectors, got %f", dist)
	}
}

func TestEuclideanDistance_MaximallyDifferent(t *testing.T) {
	// Vectors at maximum distance (corners of hypercube)
	v1 := []float32{0.0, 0.0, 0.0}
	v2 := []float32{1.0, 1.0, 1.0}

	dist := euclideanDistance(v1, v2)

	// sqrt(3) / sqrt(2) ≈ 1.22, but capped at 1.0
	if dist != 1.0 {
		t.Errorf("Expected euclidean distance 1.0 (capped) for maximally different, got %f", dist)
	}
}

func TestCosineSimilaritySlice_ParallelVectors(t *testing.T) {
	v1 := []float32{1.0, 0.0, 0.0}
	v2 := []float32{2.0, 0.0, 0.0} // Same direction, different magnitude

	similarity := cosineSimilaritySlice(v1, v2)

	if similarity < 0.99 || similarity > 1.01 {
		t.Errorf("Expected cosine similarity ~1.0 for parallel vectors, got %f", similarity)
	}
}

func TestCosineSimilaritySlice_OrthogonalVectors(t *testing.T) {
	v1 := []float32{1.0, 0.0, 0.0}
	v2 := []float32{0.0, 1.0, 0.0} // Orthogonal

	similarity := cosineSimilaritySlice(v1, v2)

	if similarity < -0.01 || similarity > 0.01 {
		t.Errorf("Expected cosine similarity ~0.0 for orthogonal vectors, got %f", similarity)
	}
}

func TestCosineSimilaritySlice_ZeroMagnitude(t *testing.T) {
	v1 := []float32{0.0, 0.0, 0.0}
	v2 := []float32{1.0, 0.0, 0.0}

	similarity := cosineSimilaritySlice(v1, v2)

	// Zero magnitude should return 0
	if similarity != 0.0 {
		t.Errorf("Expected cosine similarity 0.0 for zero magnitude, got %f", similarity)
	}
}

// Test Context Extraction

func TestGetContextValue_ValidKey(t *testing.T) {
	context := map[string]interface{}{
		"time_of_day": "evening",
		"day_type":    "weekday",
	}

	value := getContextValue(context, "time_of_day")

	if value != "evening" {
		t.Errorf("Expected 'evening', got %q", value)
	}
}

func TestGetContextValue_MissingKey(t *testing.T) {
	context := map[string]interface{}{
		"time_of_day": "evening",
	}

	value := getContextValue(context, "nonexistent")

	if value != "unknown" {
		t.Errorf("Expected 'unknown' for missing key, got %q", value)
	}
}

func TestGetContextValue_WrongType(t *testing.T) {
	context := map[string]interface{}{
		"time_of_day": 123, // Not a string
	}

	value := getContextValue(context, "time_of_day")

	if value != "unknown" {
		t.Errorf("Expected 'unknown' for wrong type, got %q", value)
	}
}

// Test Helper Functions for Test Setup

func createTestAnchorWithContext(location string, timestamp time.Time, context map[string]interface{}) *types.SemanticAnchor {
	return &types.SemanticAnchor{
		ID:                uuid.New(),
		Location:          location,
		Timestamp:         timestamp,
		Context:           context,
		SemanticEmbedding: createTestEmbedding(0.0),
	}
}

// createTestEmbedding creates a 128-dimensional test embedding
// offset controls spatial component variation (0.0 = identical, 1.0 = orthogonal)
func createTestEmbedding(offset float32) pgvector.Vector {
	vec := make([]float32, 128)

	// Initialize all with small base value
	for i := 0; i < 128; i++ {
		vec[i] = 0.1
	}

	// Temporal (0-3): sin/cos pairs for hour and day
	vec[0] = 0.5  // sin(hour)
	vec[1] = 0.86 // cos(hour)
	vec[2] = 0.7  // sin(day)
	vec[3] = 0.7  // cos(day)

	// Seasonal (4-7): sin/cos pairs
	vec[4] = 0.0  // sin(season)
	vec[5] = 1.0  // cos(season)
	vec[6] = 0.5  // sin(month)
	vec[7] = 0.86 // cos(month)

	// Day type (8-11)
	vec[8] = 1.0 // weekday
	vec[9] = 0.0 // weekend
	vec[10] = 0.0 // holiday
	vec[11] = 0.8 // morning

	// Spatial (12-27) - 30% weight in distance
	if offset < 0.01 {
		// Very similar: same direction
		for i := 12; i < 28; i++ {
			vec[i] = 1.0
		}
	} else {
		// Create rotation: offset controls angle
		vec[12] = 1.0 - offset
		vec[13] = offset
		// Rest stay at base value
	}

	// Weather (28-43)
	for i := 28; i < 44; i++ {
		vec[i] = 0.5
	}

	// Lighting (44-59)
	for i := 44; i < 60; i++ {
		vec[i] = 0.7
	}

	// Activity (60-79) - 25% weight
	for i := 60; i < 80; i++ {
		vec[i] = 0.5 + offset*0.3
	}

	// Rhythm (80-95)
	for i := 80; i < 96; i++ {
		vec[i] = 0.6
	}

	return pgvector.NewVector(vec)
}

// Test Similarity Consistency Check

func TestCheckSimilarityConsistency_Consistent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	timeManager := &TestTimeManager{}
	agent := &ComputationAgent{
		timeManager: timeManager,
		logger:      logger,
	}

	// Create candidates with low variance
	candidates := []SimilarPairCandidate{
		{Distance: 0.30},
		{Distance: 0.32},
		{Distance: 0.28},
		{Distance: 0.31},
	}

	consistent, mean := agent.checkSimilarityConsistency(candidates, 0.05)

	if !consistent {
		t.Error("Expected consistent for low variance candidates")
	}

	if mean < 0.295 || mean > 0.315 {
		t.Errorf("Expected mean around 0.30, got %f", mean)
	}
}

func TestCheckSimilarityConsistency_Inconsistent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	timeManager := &TestTimeManager{}
	agent := &ComputationAgent{
		timeManager: timeManager,
		logger:      logger,
	}

	// Create candidates with high variance
	candidates := []SimilarPairCandidate{
		{Distance: 0.10},
		{Distance: 0.50},
		{Distance: 0.90},
		{Distance: 0.30},
	}

	consistent, _ := agent.checkSimilarityConsistency(candidates, 0.05)

	if consistent {
		t.Error("Expected inconsistent for high variance candidates")
	}
}

func TestCheckSimilarityConsistency_TooFewCandidates(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	timeManager := &TestTimeManager{}
	agent := &ComputationAgent{
		timeManager: timeManager,
		logger:      logger,
	}

	// Only 1 candidate (need at least 2)
	candidates := []SimilarPairCandidate{
		{Distance: 0.30},
	}

	consistent, _ := agent.checkSimilarityConsistency(candidates, 0.05)

	if consistent {
		t.Error("Expected false for too few candidates")
	}
}

// Test Progressive Learning Strategy Components

func TestComputeVectorDistance(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	timeManager := &TestTimeManager{}
	agent := &ComputationAgent{
		timeManager: timeManager,
		logger:      logger,
	}

	anchor1 := createTestAnchorWithContext("living_room", time.Now(), map[string]interface{}{
		"time_of_day": "evening",
		"day_type":    "weekday",
	})
	anchor2 := createTestAnchorWithContext("living_room", time.Now().Add(10*time.Minute), map[string]interface{}{
		"time_of_day": "evening",
		"day_type":    "weekday",
	})

	distance, source, err := agent.computeVectorDistance(anchor1, anchor2)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if source != "vector" {
		t.Errorf("Expected source 'vector', got %q", source)
	}

	// Same embeddings should have distance ~0 (allow small floating point error)
	if distance > 0.01 {
		t.Errorf("Expected distance ~0.0 for identical embeddings, got %f", distance)
	}
}

func TestShouldSampleForLearning_UniquePattern(t *testing.T) {
	timeManager := &TestTimeManager{}
	agent := &ComputationAgent{
		timeManager:  timeManager,
		patternCache: make(map[string]*LearnedPattern),
	}

	anchor1 := createTestAnchorWithContext("bedroom", time.Now(), map[string]interface{}{
		"time_of_day": "morning",
		"day_type":    "weekday",
	})
	anchor2 := createTestAnchorWithContext("bathroom", time.Now().Add(15*time.Minute), map[string]interface{}{
		"time_of_day": "morning",
		"day_type":    "weekday",
	})

	// First time seeing this pattern
	shouldSample := agent.shouldSampleForLearning(anchor1, anchor2)

	if !shouldSample {
		t.Error("Expected true for unique pattern during seeding")
	}
}

func TestShouldSampleForLearning_AlreadySampled(t *testing.T) {
	timeManager := &TestTimeManager{}
	agent := &ComputationAgent{
		timeManager:  timeManager,
		patternCache: make(map[string]*LearnedPattern),
	}

	anchor1 := createTestAnchorWithContext("bedroom", time.Now(), map[string]interface{}{
		"time_of_day": "morning",
		"day_type":    "weekday",
	})
	anchor2 := createTestAnchorWithContext("bathroom", time.Now().Add(15*time.Minute), map[string]interface{}{
		"time_of_day": "morning",
		"day_type":    "weekday",
	})

	// Mark pattern as already sampled by adding to cache
	key := generatePatternKey(anchor1, anchor2)
	agent.patternCache[key] = &LearnedPattern{PatternKey: key}

	// Second time seeing this pattern
	shouldSample := agent.shouldSampleForLearning(anchor1, anchor2)

	if shouldSample {
		t.Error("Expected false for already sampled pattern during seeding")
	}
}

// Legacy tests removed - getLearnedDistanceWithConfidence() method has been replaced
// with the new temporal decay system. See learned_patterns_test.go for new pattern tests.

func TestPatternCacheLookup_NoPattern(t *testing.T) {
	timeManager := &TestTimeManager{}
	agent := &ComputationAgent{
		timeManager:  timeManager,
		patternCache: make(map[string]*LearnedPattern),
	}

	anchor1 := createTestAnchorWithContext("living_room", time.Now(), map[string]interface{}{
		"time_of_day": "evening",
		"day_type":    "weekday",
	})
	anchor2 := createTestAnchorWithContext("living_room", time.Now().Add(30*time.Minute), map[string]interface{}{
		"time_of_day": "evening",
		"day_type":    "weekday",
	})

	key := generatePatternKey(anchor1, anchor2)

	// Verify no pattern in cache
	if _, exists := agent.patternCache[key]; exists {
		t.Error("Expected no pattern in empty cache")
	}
}

func TestPatternCacheLookup_WithPattern(t *testing.T) {
	timeManager := &TestTimeManager{}
	agent := &ComputationAgent{
		timeManager:  timeManager,
		patternCache: make(map[string]*LearnedPattern),
	}

	anchor1 := createTestAnchorWithContext("living_room", time.Now(), map[string]interface{}{
		"time_of_day": "evening",
		"day_type":    "weekday",
	})
	anchor2 := createTestAnchorWithContext("living_room", time.Now().Add(30*time.Minute), map[string]interface{}{
		"time_of_day": "evening",
		"day_type":    "weekday",
	})

	key := generatePatternKey(anchor1, anchor2)
	// Add pattern to cache
	agent.patternCache[key] = &LearnedPattern{
		PatternKey:       key,
		WeightedDistance: 0.25,
		ConfidenceScore:  0.85,
	}

	// Verify pattern can be retrieved
	pattern, exists := agent.patternCache[key]
	if !exists {
		t.Fatal("Expected pattern in cache")
	}

	if pattern.WeightedDistance != 0.25 {
		t.Errorf("Expected distance 0.25, got %f", pattern.WeightedDistance)
	}

	if pattern.ConfidenceScore != 0.85 {
		t.Errorf("Expected confidence 0.85, got %f", pattern.ConfidenceScore)
	}
}

func TestPatternCacheLookup_TemporalDecay(t *testing.T) {
	timeManager := &TestTimeManager{}
	agent := &ComputationAgent{
		timeManager:          timeManager,
		patternCache:         make(map[string]*LearnedPattern),
		learnedPatternConfig: DefaultLearnedPatternConfig(),
	}

	anchor1 := createTestAnchorWithContext("living_room", time.Now(), map[string]interface{}{
		"time_of_day": "evening",
		"day_type":    "weekday",
	})
	anchor2 := createTestAnchorWithContext("living_room", time.Now().Add(30*time.Minute), map[string]interface{}{
		"time_of_day": "evening",
		"day_type":    "weekday",
	})

	key := generatePatternKey(anchor1, anchor2)

	// This test verifies that pattern cache exists and can store patterns
	// Temporal decay calculations are tested in learned_patterns_test.go
	agent.patternCache[key] = &LearnedPattern{
		PatternKey:       key,
		WeightedDistance: 0.25,
		ConfidenceScore:  0.90,
	}

	pattern, exists := agent.patternCache[key]
	if !exists {
		t.Fatal("Expected pattern to exist in cache")
	}

	if pattern.WeightedDistance != 0.25 {
		t.Errorf("Expected distance 0.25, got %f", pattern.WeightedDistance)
	}

	if pattern.ConfidenceScore != 0.90 {
		t.Errorf("Expected confidence 0.90, got %f", pattern.ConfidenceScore)
	}
}

// Legacy test removed - replaced with new temporal decay system
func TestPatternCache_MultiplePatterns(t *testing.T) {
	timeManager := &TestTimeManager{}
	agent := &ComputationAgent{
		timeManager:  timeManager,
		patternCache: make(map[string]*LearnedPattern),
	}

	key1 := "pattern1"
	key2 := "pattern2"

	agent.patternCache[key1] = &LearnedPattern{
		PatternKey:       key1,
		WeightedDistance: 0.2,
		ConfidenceScore:  0.9,
	}

	agent.patternCache[key2] = &LearnedPattern{
		PatternKey:       key2,
		WeightedDistance: 0.4,
		ConfidenceScore:  0.8,
	}

	// Verify both patterns are stored
	if len(agent.patternCache) != 2 {
		t.Errorf("Expected 2 patterns in cache, got %d", len(agent.patternCache))
	}

	pattern1, exists1 := agent.patternCache[key1]
	pattern2, exists2 := agent.patternCache[key2]

	if !exists1 || !exists2 {
		t.Fatal("Expected both patterns to exist in cache")
	}

	if pattern1.WeightedDistance != 0.2 {
		t.Errorf("Pattern 1: Expected distance 0.2, got %f", pattern1.WeightedDistance)
	}

	if pattern2.WeightedDistance != 0.4 {
		t.Errorf("Pattern 2: Expected distance 0.4, got %f", pattern2.WeightedDistance)
	}
}

// Legacy uncertain queue tests removed - queue management was removed along with
// learned_first, vector_first, and hybrid strategies.
