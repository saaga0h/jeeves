package patterns

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
	"github.com/saaga0h/jeeves-platform/internal/behavior/types"
)

func TestSemanticValidator_SingleAnchor(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	validator := NewSemanticValidator(logger)

	// Single anchor is always valid
	sequence := &ActivitySequence{
		ID:              uuid.New().String(),
		Anchors:         []*types.SemanticAnchor{createTestAnchorWithEmbedding("living_room", time.Now(), makeSimilarEmbedding(0))},
		Locations:       []string{"living_room"},
		IsCrossLocation: false,
	}

	valid, avgDist := validator.ValidateSequence(sequence)

	if !valid {
		t.Error("Single anchor sequence should always be valid")
	}
	if avgDist != 0.0 {
		t.Errorf("Expected distance 0.0 for single anchor, got %f", avgDist)
	}
}

func TestSemanticValidator_SingleLocationCoherent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	validator := NewSemanticValidator(logger)

	// Create sequence with very similar embeddings (same location, similar time)
	baseTime := time.Now()
	sequence := &ActivitySequence{
		ID: uuid.New().String(),
		Anchors: []*types.SemanticAnchor{
			createTestAnchorWithEmbedding("living_room", baseTime, makeSimilarEmbedding(0)),
			createTestAnchorWithEmbedding("living_room", baseTime.Add(5*time.Minute), makeSimilarEmbedding(0.02)),
			createTestAnchorWithEmbedding("living_room", baseTime.Add(10*time.Minute), makeSimilarEmbedding(0.03)),
		},
		Locations:       []string{"living_room"},
		IsCrossLocation: false,
	}

	valid, avgDist := validator.ValidateSequence(sequence)

	if !valid {
		t.Errorf("Coherent single-location sequence should be valid, got avgDist=%f", avgDist)
	}
	// Single-location threshold is 0.25
	if avgDist >= 0.25 {
		t.Errorf("Expected avgDist < 0.25 for coherent sequence, got %f", avgDist)
	}
}

func TestSemanticValidator_SingleLocationIncoherent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	validator := NewSemanticValidator(logger)

	// Create sequence with very different embeddings (should fail validation)
	baseTime := time.Now()
	sequence := &ActivitySequence{
		ID: uuid.New().String(),
		Anchors: []*types.SemanticAnchor{
			createTestAnchorWithEmbedding("living_room", baseTime, makeSimilarEmbedding(0)),
			createTestAnchorWithEmbedding("living_room", baseTime.Add(5*time.Minute), makeSimilarEmbedding(0.5)), // Very different
			createTestAnchorWithEmbedding("living_room", baseTime.Add(10*time.Minute), makeSimilarEmbedding(0.8)), // Very different
		},
		Locations:       []string{"living_room"},
		IsCrossLocation: false,
	}

	valid, avgDist := validator.ValidateSequence(sequence)

	if valid {
		t.Errorf("Incoherent single-location sequence should be invalid, got avgDist=%f", avgDist)
	}
	// Single-location threshold is 0.25
	if avgDist < 0.25 {
		t.Errorf("Expected avgDist >= 0.25 for incoherent sequence, got %f", avgDist)
	}
}

func TestSemanticValidator_CrossLocationCoherent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	validator := NewSemanticValidator(logger)

	// Create cross-location sequence with moderate coherence (morning routine)
	baseTime := time.Now()
	sequence := &ActivitySequence{
		ID: uuid.New().String(),
		Anchors: []*types.SemanticAnchor{
			createTestAnchorWithEmbedding("bedroom", baseTime, makeSimilarEmbedding(0)),
			createTestAnchorWithEmbedding("bathroom", baseTime.Add(5*time.Minute), makeSimilarEmbedding(0.15)),
			createTestAnchorWithEmbedding("kitchen", baseTime.Add(10*time.Minute), makeSimilarEmbedding(0.20)),
		},
		Locations:       []string{"bedroom", "bathroom", "kitchen"},
		IsCrossLocation: true,
	}

	valid, avgDist := validator.ValidateSequence(sequence)

	if !valid {
		t.Errorf("Coherent cross-location routine should be valid, got avgDist=%f", avgDist)
	}
	// Cross-location threshold is 0.35
	if avgDist >= 0.35 {
		t.Errorf("Expected avgDist < 0.35 for coherent routine, got %f", avgDist)
	}
}

func TestSemanticValidator_CrossLocationIncoherent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	validator := NewSemanticValidator(logger)

	// Create cross-location sequence with high distances (unrelated activities)
	baseTime := time.Now()
	sequence := &ActivitySequence{
		ID: uuid.New().String(),
		Anchors: []*types.SemanticAnchor{
			createTestAnchorWithEmbedding("bedroom", baseTime, makeSimilarEmbedding(0)),
			createTestAnchorWithEmbedding("study", baseTime.Add(5*time.Minute), makeSimilarEmbedding(0.6)), // Very different
			createTestAnchorWithEmbedding("garage", baseTime.Add(10*time.Minute), makeSimilarEmbedding(0.9)), // Very different
		},
		Locations:       []string{"bedroom", "study", "garage"},
		IsCrossLocation: true,
	}

	valid, avgDist := validator.ValidateSequence(sequence)

	if valid {
		t.Errorf("Incoherent cross-location sequence should be invalid, got avgDist=%f", avgDist)
	}
	// Cross-location threshold is 0.35
	if avgDist < 0.35 {
		t.Errorf("Expected avgDist >= 0.35 for incoherent sequence, got %f", avgDist)
	}
}

func TestSemanticValidator_SplitIncoherentSequence(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	validator := NewSemanticValidator(logger)

	// Create sequence with clear split point (large distance gap in middle)
	baseTime := time.Now()
	sequence := &ActivitySequence{
		ID: uuid.New().String(),
		Anchors: []*types.SemanticAnchor{
			// Group 1: Similar
			createTestAnchorWithEmbedding("living_room", baseTime, makeSimilarEmbedding(0)),
			createTestAnchorWithEmbedding("living_room", baseTime.Add(5*time.Minute), makeSimilarEmbedding(0.05)),
			// Large gap here
			createTestAnchorWithEmbedding("living_room", baseTime.Add(10*time.Minute), makeSimilarEmbedding(0.7)),
			// Group 2: Similar
			createTestAnchorWithEmbedding("living_room", baseTime.Add(15*time.Minute), makeSimilarEmbedding(0.75)),
		},
		Locations:       []string{"living_room"},
		IsCrossLocation: false,
	}

	split := validator.SplitIncoherentSequence(sequence)

	// Should split into 2 sub-sequences
	if len(split) != 2 {
		t.Errorf("Expected 2 sub-sequences after split, got %d", len(split))
	}

	for i, subSeq := range split {
		t.Logf("Sub-sequence %d: %d anchors", i, len(subSeq.Anchors))
		if len(subSeq.Anchors) < 2 {
			t.Errorf("Sub-sequence %d has < 2 anchors: %d", i, len(subSeq.Anchors))
		}
	}
}

func TestSemanticValidator_SplitTooSmall(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	validator := NewSemanticValidator(logger)

	// Sequence with only 2 anchors (can't split)
	baseTime := time.Now()
	sequence := &ActivitySequence{
		ID: uuid.New().String(),
		Anchors: []*types.SemanticAnchor{
			createTestAnchorWithEmbedding("living_room", baseTime, makeSimilarEmbedding(0)),
			createTestAnchorWithEmbedding("living_room", baseTime.Add(5*time.Minute), makeSimilarEmbedding(0.9)),
		},
		Locations:       []string{"living_room"},
		IsCrossLocation: false,
	}

	split := validator.SplitIncoherentSequence(sequence)

	// Should return original sequence (can't split < 3 anchors)
	if len(split) != 1 {
		t.Errorf("Expected 1 sequence (no split), got %d", len(split))
	}
	if len(split[0].Anchors) != 2 {
		t.Errorf("Expected 2 anchors in returned sequence, got %d", len(split[0].Anchors))
	}
}

func TestSemanticValidator_SplitNoGap(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	validator := NewSemanticValidator(logger)

	// Sequence with uniform distances (no clear split point)
	baseTime := time.Now()
	sequence := &ActivitySequence{
		ID: uuid.New().String(),
		Anchors: []*types.SemanticAnchor{
			createTestAnchorWithEmbedding("living_room", baseTime, makeSimilarEmbedding(0)),
			createTestAnchorWithEmbedding("living_room", baseTime.Add(5*time.Minute), makeSimilarEmbedding(0.2)),
			createTestAnchorWithEmbedding("living_room", baseTime.Add(10*time.Minute), makeSimilarEmbedding(0.25)),
			createTestAnchorWithEmbedding("living_room", baseTime.Add(15*time.Minute), makeSimilarEmbedding(0.3)),
		},
		Locations:       []string{"living_room"},
		IsCrossLocation: false,
	}

	split := validator.SplitIncoherentSequence(sequence)

	// Should not split (max gap < 0.4 threshold)
	if len(split) != 1 {
		t.Errorf("Expected 1 sequence (no significant gap), got %d", len(split))
	}
	if len(split[0].Anchors) != 4 {
		t.Errorf("Expected 4 anchors in returned sequence, got %d", len(split[0].Anchors))
	}
}

// Helper functions

func createTestAnchorWithEmbedding(location string, timestamp time.Time, embedding pgvector.Vector) *types.SemanticAnchor {
	return &types.SemanticAnchor{
		ID:                uuid.New(),
		Location:          location,
		Timestamp:         timestamp,
		SemanticEmbedding: embedding,
	}
}

// makeSimilarEmbedding creates a 128-dimensional embedding with controlled similarity
// baseOffset controls how different the embedding is (0.0 = identical, 1.0 = very different)
// The distance formula uses weighted components:
// - 0.30 * spatial distance (dims 12-27) using cosine similarity
// - 0.25 * activity distance (dims 60-79) using euclidean
// - Other components with smaller weights
//
// Strategy: Create a base vector with magnitude, then add offset to create controlled distances
func makeSimilarEmbedding(baseOffset float32) pgvector.Vector {
	vec := make([]float32, 128)

	// Initialize all dimensions with small base value (needed for cosine similarity magnitude)
	for i := 0; i < 128; i++ {
		vec[i] = 0.1
	}

	// Spatial dimensions (12-27) - 30% weight, uses cosine distance
	// For cosine distance: orthogonal vectors = distance 1.0
	// Create base vector [1, 0, 0, ...] and add offset to create angle
	if baseOffset < 0.01 {
		// Very similar: all pointing same direction
		for i := 12; i < 28; i++ {
			vec[i] = 1.0
		}
	} else {
		// Add rotation based on offset: mix of original direction and orthogonal
		// baseOffset = 0.0 -> [1,0,0,...] (identical)
		// baseOffset = 1.0 -> [0,1,0,...] (orthogonal, distance = 1.0)
		vec[12] = 1.0 - baseOffset
		vec[13] = baseOffset
		// Rest stay at 0.1 base value
	}

	// Activity dimensions (60-79) - 25% weight, uses euclidean distance
	// For euclidean: sqrt(sum((v1-v2)^2)) / sqrt(2)
	// To get target distance D: need values that differ by sqrt(2*D^2) = D*sqrt(2)
	for i := 60; i < 80; i++ {
		vec[i] = 0.5 + baseOffset*0.5 // Range from 0.5 to 1.0
	}

	return pgvector.NewVector(vec)
}
