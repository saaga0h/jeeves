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

func TestLocationTemporalClusterer_SingleLocation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	clusterer := NewLocationTemporalClusterer(logger)

	// Create anchors all in same location, continuous time
	baseTime := time.Date(2025, 10, 30, 19, 0, 0, 0, time.UTC)
	anchors := []*types.SemanticAnchor{
		createTestAnchor("living_room", baseTime),
		createTestAnchor("living_room", baseTime.Add(10*time.Minute)),
		createTestAnchor("living_room", baseTime.Add(20*time.Minute)),
		createTestAnchor("living_room", baseTime.Add(25*time.Minute)),
	}

	sequences := clusterer.ClusterByLocationTemporal(anchors)

	// Should create single sequence for continuous single-location activity
	if len(sequences) != 1 {
		t.Errorf("Expected 1 sequence, got %d", len(sequences))
	}

	if len(sequences) > 0 {
		seq := sequences[0]
		if len(seq.Anchors) != 4 {
			t.Errorf("Expected 4 anchors in sequence, got %d", len(seq.Anchors))
		}
		if seq.IsCrossLocation {
			t.Error("Expected single-location sequence, got cross-location")
		}
		if len(seq.Locations) != 1 || seq.Locations[0] != "living_room" {
			t.Errorf("Expected location [living_room], got %v", seq.Locations)
		}
	}
}

func TestLocationTemporalClusterer_TemporalGap(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	clusterer := NewLocationTemporalClusterer(logger)

	// Create anchors with large temporal gap (> 30 min threshold)
	baseTime := time.Date(2025, 10, 30, 19, 0, 0, 0, time.UTC)
	anchors := []*types.SemanticAnchor{
		createTestAnchor("living_room", baseTime),
		createTestAnchor("living_room", baseTime.Add(10*time.Minute)),
		// 40-minute gap here (exceeds 30-minute threshold)
		createTestAnchor("living_room", baseTime.Add(50*time.Minute)),
		createTestAnchor("living_room", baseTime.Add(55*time.Minute)),
	}

	sequences := clusterer.ClusterByLocationTemporal(anchors)

	// Should create two separate sessions due to temporal gap
	if len(sequences) != 2 {
		t.Errorf("Expected 2 sequences (split by temporal gap), got %d", len(sequences))
	}

	for i, seq := range sequences {
		if len(seq.Anchors) != 2 {
			t.Errorf("Sequence %d: Expected 2 anchors, got %d", i, len(seq.Anchors))
		}
		if seq.IsCrossLocation {
			t.Errorf("Sequence %d: Expected single-location, got cross-location", i)
		}
	}
}

func TestLocationTemporalClusterer_CrossLocationRoutine(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	clusterer := NewLocationTemporalClusterer(logger)

	// Create morning routine: bedroom -> bathroom -> kitchen
	baseTime := time.Date(2025, 10, 30, 6, 0, 0, 0, time.UTC)
	anchors := []*types.SemanticAnchor{
		createTestAnchor("bedroom", baseTime),
		createTestAnchor("bedroom", baseTime.Add(2*time.Minute)),
		// Transition to bathroom (within 20-min sequence gap)
		createTestAnchor("bathroom", baseTime.Add(5*time.Minute)),
		createTestAnchor("bathroom", baseTime.Add(10*time.Minute)),
		// Transition to kitchen
		createTestAnchor("kitchen", baseTime.Add(15*time.Minute)),
		createTestAnchor("kitchen", baseTime.Add(20*time.Minute)),
	}

	sequences := clusterer.ClusterByLocationTemporal(anchors)

	// Should create one cross-location sequence
	if len(sequences) != 1 {
		t.Errorf("Expected 1 cross-location sequence, got %d", len(sequences))
	}

	if len(sequences) > 0 {
		seq := sequences[0]
		if !seq.IsCrossLocation {
			t.Error("Expected cross-location sequence, got single-location")
		}
		if len(seq.Locations) != 3 {
			t.Errorf("Expected 3 locations, got %d: %v", len(seq.Locations), seq.Locations)
		}
		if len(seq.Anchors) != 6 {
			t.Errorf("Expected 6 anchors, got %d", len(seq.Anchors))
		}
	}
}

func TestLocationTemporalClusterer_BackAndForthDetection(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	clusterer := NewLocationTemporalClusterer(logger)

	// Create back-and-forth pattern: living_room -> study -> living_room -> study
	// Each session is distinct (gap > 30min for session break),
	// but close enough (< 20min between sessions) to attempt sequence building
	baseTime := time.Date(2025, 10, 30, 19, 0, 0, 0, time.UTC)
	anchors := []*types.SemanticAnchor{
		// Session 1: living_room
		createTestAnchor("living_room", baseTime),
		createTestAnchor("living_room", baseTime.Add(10*time.Minute)),
		// Gap of 15 min to study (< 20min sequence gap)
		// Session 2: study
		createTestAnchor("study", baseTime.Add(25*time.Minute)),
		createTestAnchor("study", baseTime.Add(30*time.Minute)),
		// Gap of 15 min back to living_room
		// Session 3: living_room (BACK to first location)
		createTestAnchor("living_room", baseTime.Add(45*time.Minute)),
		createTestAnchor("living_room", baseTime.Add(50*time.Minute)),
		// Gap of 15 min to study again
		// Session 4: study (FORTH to second location again)
		createTestAnchor("study", baseTime.Add(65*time.Minute)),
		createTestAnchor("study", baseTime.Add(70*time.Minute)),
	}

	sequences := clusterer.ClusterByLocationTemporal(anchors)

	// NOTE: Current behavior is that the algorithm detects back-and-forth pattern
	// ONLY for the first sequence attempt (living_room->study). After rejecting that,
	// it marks those sessions as unused, and remaining sessions become standalone.
	// Result: First 2 sessions form partial cross-location + last 2 standalone
	//
	// KNOWN LIMITATION: The algorithm processes sessions sequentially, so it doesn't
	// catch the full back-and-forth pattern across all 4 sessions.
	// This test documents the current behavior.

	if len(sequences) == 0 {
		t.Fatal("Expected at least some sequences")
	}

	t.Logf("Got %d sequences", len(sequences))
	for i, seq := range sequences {
		t.Logf("  Sequence %d: %v, cross=%v, anchors=%d", i, seq.Locations, seq.IsCrossLocation, len(seq.Anchors))
	}

	// Verify all anchors are captured in sequences
	totalAnchors := 0
	for _, seq := range sequences {
		totalAnchors += len(seq.Anchors)
	}
	if totalAnchors != len(anchors) {
		t.Errorf("Expected all %d anchors in sequences, got %d", len(anchors), totalAnchors)
	}

	// Should have detected at least some cross-location attempt
	// (even if partially - this documents current behavior)
	crossLocationFound := false
	for _, seq := range sequences {
		if seq.IsCrossLocation {
			crossLocationFound = true
			// The cross-location sequence should only include first part before detection
			if len(seq.Locations) > 2 {
				t.Errorf("Cross-location sequence has too many locations: %v", seq.Locations)
			}
		}
	}
	if !crossLocationFound {
		t.Error("Expected at least one cross-location sequence attempt")
	}
}

func TestLocationTemporalClusterer_ParallelActivities(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	clusterer := NewLocationTemporalClusterer(logger)

	// Simulate parallel activities with interleaved timestamps
	// This mimics the real test scenario: TV in living_room + work in study
	baseTime := time.Date(2025, 10, 30, 19, 0, 0, 0, time.UTC)
	anchors := []*types.SemanticAnchor{
		// Start TV in living room
		createTestAnchor("living_room", baseTime),
		createTestAnchor("living_room", baseTime.Add(2*time.Minute)),
		// Start work in study
		createTestAnchor("study", baseTime.Add(3*time.Minute)),
		createTestAnchor("study", baseTime.Add(5*time.Minute)),
		// Continue TV
		createTestAnchor("living_room", baseTime.Add(7*time.Minute)),
		createTestAnchor("living_room", baseTime.Add(10*time.Minute)),
		// Continue work
		createTestAnchor("study", baseTime.Add(12*time.Minute)),
		createTestAnchor("study", baseTime.Add(15*time.Minute)),
		// Continue TV
		createTestAnchor("living_room", baseTime.Add(18*time.Minute)),
		// Continue work
		createTestAnchor("study", baseTime.Add(20*time.Minute)),
	}

	sequences := clusterer.ClusterByLocationTemporal(anchors)

	// With the current algorithm, parallel activities create a back-and-forth pattern
	// that gets detected and rejected. The result depends on exact timing and gaps.
	// Key assertion: we should have sequences, and they should be mostly single-location
	if len(sequences) == 0 {
		t.Fatal("Expected at least some sequences from parallel activities")
	}

	t.Logf("Got %d sequences from parallel activities", len(sequences))
	for i, seq := range sequences {
		t.Logf("  Sequence %d: %v, cross=%v, anchors=%d", i, seq.Locations, seq.IsCrossLocation, len(seq.Anchors))
	}

	// Count location distribution
	livingRoomAnchors := 0
	studyAnchors := 0

	for _, seq := range sequences {
		for _, anchor := range seq.Anchors {
			switch anchor.Location {
			case "living_room":
				livingRoomAnchors++
			case "study":
				studyAnchors++
			}
		}
	}

	// We should have captured anchors from both locations
	if livingRoomAnchors == 0 {
		t.Error("Expected some living_room anchors in sequences")
	}
	if studyAnchors == 0 {
		t.Error("Expected some study anchors in sequences")
	}

	// Total anchors should match input
	totalAnchors := livingRoomAnchors + studyAnchors
	if totalAnchors != len(anchors) {
		t.Errorf("Expected %d total anchors in sequences, got %d", len(anchors), totalAnchors)
	}
}

func TestLocationTemporalClusterer_MinSequenceLength(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	clusterer := NewLocationTemporalClusterer(logger)

	// Create one anchor (below minimum of 2)
	baseTime := time.Date(2025, 10, 30, 19, 0, 0, 0, time.UTC)
	anchors := []*types.SemanticAnchor{
		createTestAnchor("living_room", baseTime),
	}

	sequences := clusterer.ClusterByLocationTemporal(anchors)

	// Should not create sequence with only 1 anchor (minimum is 2)
	if len(sequences) != 0 {
		t.Errorf("Expected 0 sequences (below min length), got %d", len(sequences))
	}
}

func TestLocationTemporalClusterer_EmptyInput(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	clusterer := NewLocationTemporalClusterer(logger)

	sequences := clusterer.ClusterByLocationTemporal(nil)

	if sequences != nil {
		t.Errorf("Expected nil for empty input, got %d sequences", len(sequences))
	}

	sequences = clusterer.ClusterByLocationTemporal([]*types.SemanticAnchor{})

	if sequences != nil {
		t.Errorf("Expected nil for empty slice, got %d sequences", len(sequences))
	}
}

func TestIsBackAndForthPattern_Simple(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	clusterer := NewLocationTemporalClusterer(logger)

	tests := []struct {
		name      string
		locations []string
		expected  bool
	}{
		{
			name:      "A->B->A (back-and-forth)",
			locations: []string{"living_room", "study", "living_room"},
			expected:  true,
		},
		{
			name:      "A->B->A->B (back-and-forth)",
			locations: []string{"living_room", "study", "living_room", "study"},
			expected:  true,
		},
		{
			name:      "A->B->C (linear routine)",
			locations: []string{"bedroom", "bathroom", "kitchen"},
			expected:  false,
		},
		{
			name:      "A->B (single transition)",
			locations: []string{"living_room", "study"},
			expected:  false,
		},
		{
			name:      "A only (no transitions)",
			locations: []string{"living_room"},
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create sequence with specified location pattern
			baseTime := time.Now()
			var anchors []*types.SemanticAnchor
			for i, loc := range tt.locations {
				anchors = append(anchors, createTestAnchor(loc, baseTime.Add(time.Duration(i)*5*time.Minute)))
			}

			sequence := &ActivitySequence{
				ID:              uuid.New().String(),
				Anchors:         anchors,
				Locations:       uniqueLocations(tt.locations),
				IsCrossLocation: len(uniqueLocations(tt.locations)) > 1,
			}

			result := clusterer.isBackAndForthPattern(sequence)
			if result != tt.expected {
				t.Errorf("Expected %v for pattern %v, got %v", tt.expected, tt.locations, result)
			}
		})
	}
}

// Helper functions

func createTestAnchor(location string, timestamp time.Time) *types.SemanticAnchor {
	return &types.SemanticAnchor{
		ID:        uuid.New(),
		Location:  location,
		Timestamp: timestamp,
		SemanticEmbedding: pgvector.NewVector(make([]float32, 128)), // Empty embedding for testing
	}
}

func uniqueLocations(locations []string) []string {
	seen := make(map[string]bool)
	var unique []string
	for _, loc := range locations {
		if !seen[loc] {
			seen[loc] = true
			unique = append(unique, loc)
		}
	}
	return unique
}
