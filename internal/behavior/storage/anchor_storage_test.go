package storage

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/saaga0h/jeeves-platform/internal/behavior/types"
)

// setupTestDB creates a test database connection with semantic_anchors schema.
// This requires a PostgreSQL instance with pgvector extension.
func setupTestDB(t *testing.T) *sql.DB {
	// This is a placeholder - in real tests, you would:
	// 1. Use a test PostgreSQL instance (e.g., via testcontainers)
	// 2. Run the migration scripts to create tables
	// 3. Return the database connection
	t.Skip("Integration test - requires PostgreSQL with pgvector")
	return nil
}

func TestCreateAndGetAnchor(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	storage := NewAnchorStorage(db)
	ctx := context.Background()

	// Create a semantic anchor
	anchor := &types.SemanticAnchor{
		Timestamp:         time.Now(),
		Location:          "kitchen",
		SemanticEmbedding: makeTestVector(128),
		Context: map[string]interface{}{
			"time_of_day":    "morning",
			"day_type":       "weekday",
			"household_mode": "waking",
		},
		Signals: []types.ActivitySignal{
			{
				Type: "motion",
				Value: map[string]interface{}{
					"state": "detected",
				},
				Confidence: 0.9,
				Timestamp:  time.Now(),
			},
		},
	}

	// Set optional duration
	duration := 12
	source := "measured"
	confidence := 0.95
	anchor.DurationMinutes = &duration
	anchor.DurationSource = &source
	anchor.DurationConfidence = &confidence

	// Store anchor
	err := storage.CreateAnchor(ctx, anchor)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, anchor.ID)

	// Retrieve anchor
	retrieved, err := storage.GetAnchor(ctx, anchor.ID)
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	// Verify fields
	assert.Equal(t, anchor.ID, retrieved.ID)
	assert.Equal(t, anchor.Location, retrieved.Location)
	assert.Equal(t, "morning", retrieved.Context["time_of_day"])
	assert.Equal(t, 12, *retrieved.DurationMinutes)
	assert.Equal(t, "measured", *retrieved.DurationSource)
	assert.Equal(t, 0.95, *retrieved.DurationConfidence)
	assert.Len(t, retrieved.Signals, 1)
	assert.Equal(t, "motion", retrieved.Signals[0].Type)
}

func TestFindSimilarAnchors(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	storage := NewAnchorStorage(db)
	ctx := context.Background()

	// Create multiple anchors with different embeddings
	baseVector := makeTestVector(128)

	anchors := []*types.SemanticAnchor{
		{
			Location:          "kitchen",
			Timestamp:         time.Now(),
			SemanticEmbedding: baseVector,
			Context:           map[string]interface{}{"activity": "cooking"},
			Signals:           []types.ActivitySignal{},
		},
		{
			Location:          "kitchen",
			Timestamp:         time.Now().Add(1 * time.Hour),
			SemanticEmbedding: makeSlightlyDifferentVector(baseVector, 0.1),
			Context:           map[string]interface{}{"activity": "breakfast"},
			Signals:           []types.ActivitySignal{},
		},
		{
			Location:          "bedroom",
			Timestamp:         time.Now().Add(2 * time.Hour),
			SemanticEmbedding: makeSlightlyDifferentVector(baseVector, 0.8),
			Context:           map[string]interface{}{"activity": "sleeping"},
			Signals:           []types.ActivitySignal{},
		},
	}

	for _, anchor := range anchors {
		err := storage.CreateAnchor(ctx, anchor)
		require.NoError(t, err)
	}

	// Find similar anchors to the base vector
	similar, err := storage.FindSimilarAnchors(ctx, baseVector, 2)
	require.NoError(t, err)
	require.Len(t, similar, 2)

	// First result should be exact match (kitchen cooking)
	assert.Equal(t, "kitchen", similar[0].Location)
	assert.Equal(t, "cooking", similar[0].Context["activity"])
}

func TestStoreAndGetDistance(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	storage := NewAnchorStorage(db)
	ctx := context.Background()

	// Create two anchors
	anchor1 := &types.SemanticAnchor{
		Location:          "kitchen",
		Timestamp:         time.Now(),
		SemanticEmbedding: makeTestVector(128),
		Context:           map[string]interface{}{},
		Signals:           []types.ActivitySignal{},
	}
	anchor2 := &types.SemanticAnchor{
		Location:          "dining_room",
		Timestamp:         time.Now(),
		SemanticEmbedding: makeTestVector(128),
		Context:           map[string]interface{}{},
		Signals:           []types.ActivitySignal{},
	}

	err := storage.CreateAnchor(ctx, anchor1)
	require.NoError(t, err)
	err = storage.CreateAnchor(ctx, anchor2)
	require.NoError(t, err)

	// Store distance between them
	distance := &types.AnchorDistance{
		Anchor1ID: anchor1.ID,
		Anchor2ID: anchor2.ID,
		Distance:  0.15,
		Source:    "vector",
	}

	err = storage.StoreDistance(ctx, distance)
	require.NoError(t, err)

	// Retrieve distance
	retrieved, err := storage.GetDistance(ctx, anchor1.ID, anchor2.ID)
	require.NoError(t, err)
	require.NotNil(t, retrieved)
	assert.Equal(t, 0.15, retrieved.Distance)
	assert.Equal(t, "vector", retrieved.Source)

	// Test retrieval with reversed IDs (should work due to ordering constraint)
	retrieved2, err := storage.GetDistance(ctx, anchor2.ID, anchor1.ID)
	require.NoError(t, err)
	require.NotNil(t, retrieved2)
	assert.Equal(t, 0.15, retrieved2.Distance)
}

func TestGetAnchorsNeedingDistances(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	storage := NewAnchorStorage(db)
	ctx := context.Background()

	// Create three anchors
	anchors := []*types.SemanticAnchor{
		{
			Location:          "kitchen",
			Timestamp:         time.Now(),
			SemanticEmbedding: makeTestVector(128),
			Context:           map[string]interface{}{},
			Signals:           []types.ActivitySignal{},
		},
		{
			Location:          "bedroom",
			Timestamp:         time.Now(),
			SemanticEmbedding: makeTestVector(128),
			Context:           map[string]interface{}{},
			Signals:           []types.ActivitySignal{},
		},
		{
			Location:          "bathroom",
			Timestamp:         time.Now(),
			SemanticEmbedding: makeTestVector(128),
			Context:           map[string]interface{}{},
			Signals:           []types.ActivitySignal{},
		},
	}

	for _, anchor := range anchors {
		err := storage.CreateAnchor(ctx, anchor)
		require.NoError(t, err)
	}

	// Get pairs needing distances (should be 3: [0,1], [0,2], [1,2])
	pairs, err := storage.GetAnchorsNeedingDistances(ctx, 10)
	require.NoError(t, err)
	assert.Len(t, pairs, 3)

	// Store one distance
	distance := &types.AnchorDistance{
		Anchor1ID: anchors[0].ID,
		Anchor2ID: anchors[1].ID,
		Distance:  0.2,
		Source:    "vector",
	}
	err = storage.StoreDistance(ctx, distance)
	require.NoError(t, err)

	// Get pairs again (should be 2 now)
	pairs, err = storage.GetAnchorsNeedingDistances(ctx, 10)
	require.NoError(t, err)
	assert.Len(t, pairs, 2)
}

func TestCreateAndGetInterpretation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	storage := NewAnchorStorage(db)
	ctx := context.Background()

	// Create anchor first
	anchor := &types.SemanticAnchor{
		Location:          "kitchen",
		Timestamp:         time.Now(),
		SemanticEmbedding: makeTestVector(128),
		Context:           map[string]interface{}{},
		Signals:           []types.ActivitySignal{},
	}
	err := storage.CreateAnchor(ctx, anchor)
	require.NoError(t, err)

	// Create interpretation
	interpretation := &types.ActivityInterpretation{
		AnchorID:     anchor.ID,
		ActivityType: "cooking",
		Confidence:   0.85,
		Evidence:     []string{"motion_detected", "stove_on", "high_lighting"},
	}

	err = storage.CreateInterpretation(ctx, interpretation)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, interpretation.ID)

	// Retrieve interpretations
	interpretations, err := storage.GetInterpretations(ctx, anchor.ID)
	require.NoError(t, err)
	require.Len(t, interpretations, 1)

	assert.Equal(t, "cooking", interpretations[0].ActivityType)
	assert.Equal(t, 0.85, interpretations[0].Confidence)
	assert.Len(t, interpretations[0].Evidence, 3)
	assert.Contains(t, interpretations[0].Evidence, "motion_detected")
}

func TestCreateAndUpdatePattern(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	storage := NewAnchorStorage(db)
	ctx := context.Background()

	// Create pattern
	pattern := &types.BehavioralPattern{
		Name:        "morning_routine",
		PatternType: "routine",
		Weight:      0.1, // Default starting weight
		Context: map[string]interface{}{
			"typical_time": "07:00-08:00",
		},
	}

	err := storage.CreatePattern(ctx, pattern)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, pattern.ID)

	// Retrieve pattern
	retrieved, err := storage.GetPattern(ctx, pattern.ID)
	require.NoError(t, err)
	assert.Equal(t, "morning_routine", retrieved.Name)
	assert.Equal(t, 0.1, retrieved.Weight)

	// Update pattern after successful prediction
	retrieved.Observations = 5
	retrieved.Predictions = 3
	retrieved.Acceptances = 2
	retrieved.Weight = 0.15 // Weight increases with success

	err = storage.UpdatePattern(ctx, retrieved)
	require.NoError(t, err)

	// Verify update
	updated, err := storage.GetPattern(ctx, pattern.ID)
	require.NoError(t, err)
	assert.Equal(t, 5, updated.Observations)
	assert.Equal(t, 3, updated.Predictions)
	assert.Equal(t, 2, updated.Acceptances)
	assert.Equal(t, 0.15, updated.Weight)
}

func TestGetTopPatterns(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	storage := NewAnchorStorage(db)
	ctx := context.Background()

	// Create patterns with different weights
	patterns := []*types.BehavioralPattern{
		{
			Name:   "morning_routine",
			Weight: 0.5,
		},
		{
			Name:   "evening_routine",
			Weight: 0.8,
		},
		{
			Name:   "lunch_break",
			Weight: 0.3,
		},
	}

	for _, pattern := range patterns {
		err := storage.CreatePattern(ctx, pattern)
		require.NoError(t, err)
	}

	// Get top 2 patterns
	top, err := storage.GetTopPatterns(ctx, 2)
	require.NoError(t, err)
	require.Len(t, top, 2)

	// Should be ordered by weight (descending)
	assert.Equal(t, "evening_routine", top[0].Name)
	assert.Equal(t, 0.8, top[0].Weight)
	assert.Equal(t, "morning_routine", top[1].Name)
	assert.Equal(t, 0.5, top[1].Weight)
}

// Helper functions for tests

func makeTestVector(dimensions int) pgvector.Vector {
	vec := make([]float32, dimensions)
	for i := range vec {
		vec[i] = float32(i) / float32(dimensions)
	}
	return pgvector.NewVector(vec)
}

func makeSlightlyDifferentVector(base pgvector.Vector, difference float32) pgvector.Vector {
	vec := make([]float32, len(base.Slice()))
	copy(vec, base.Slice())

	// Add some difference to make vectors distinct
	for i := range vec {
		vec[i] += difference * float32(i%10) / 100.0
	}

	return pgvector.NewVector(vec)
}
