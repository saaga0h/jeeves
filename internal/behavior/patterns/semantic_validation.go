package patterns

import (
	"log/slog"
	"math"

	"github.com/pgvector/pgvector-go"
	"github.com/saaga0h/jeeves-platform/internal/behavior/types"
)

// SemanticValidator validates activity sequences using semantic distances
type SemanticValidator struct {
	logger *slog.Logger
}

// NewSemanticValidator creates a new semantic validator
func NewSemanticValidator(logger *slog.Logger) *SemanticValidator {
	return &SemanticValidator{
		logger: logger,
	}
}

// ValidateSequence checks if a sequence has semantic coherence
// Returns true if the sequence appears to be a coherent activity
func (v *SemanticValidator) ValidateSequence(sequence *ActivitySequence) (bool, float64) {
	if len(sequence.Anchors) < 2 {
		return true, 0.0 // Single anchor is always valid
	}

	// Calculate average pairwise distance within sequence
	var totalDistance float64
	var pairCount int

	for i := 0; i < len(sequence.Anchors); i++ {
		for j := i + 1; j < len(sequence.Anchors); j++ {
			dist := v.computeDistance(sequence.Anchors[i], sequence.Anchors[j])
			totalDistance += dist
			pairCount++
		}
	}

	avgDistance := totalDistance / float64(pairCount)

	// Validation thresholds
	if sequence.IsCrossLocation {
		// Cross-location sequences (routines) should have moderate coherence
		// Example: bedroom→bathroom→kitchen morning routine
		// These can have slightly higher distances due to location changes
		// but should still be semantically related (same time period, day type)
		isValid := avgDistance < 0.35
		v.logger.Debug("Cross-location sequence validation",
			"sequence_id", sequence.ID,
			"locations", sequence.Locations,
			"anchor_count", len(sequence.Anchors),
			"avg_distance", avgDistance,
			"valid", isValid)
		return isValid, avgDistance
	}

	// Single-location sequences (extended activities) should be very coherent
	// Example: working in study for 3 hours
	// These should have low distances as they're the same activity
	isValid := avgDistance < 0.25
	v.logger.Debug("Single-location sequence validation",
		"sequence_id", sequence.ID,
		"location", sequence.Locations[0],
		"anchor_count", len(sequence.Anchors),
		"avg_distance", avgDistance,
		"valid", isValid)
	return isValid, avgDistance
}

// SplitIncoherentSequence attempts to split a sequence with high internal distances
// Returns sub-sequences that are more coherent
func (v *SemanticValidator) SplitIncoherentSequence(sequence *ActivitySequence) []*ActivitySequence {
	if len(sequence.Anchors) < 3 {
		// Can't split sequences with fewer than 3 anchors
		return []*ActivitySequence{sequence}
	}

	// Find the largest distance gap between consecutive anchors
	var maxGap float64
	var splitIndex int = -1

	for i := 0; i < len(sequence.Anchors)-1; i++ {
		dist := v.computeDistance(sequence.Anchors[i], sequence.Anchors[i+1])
		if dist > maxGap {
			maxGap = dist
			splitIndex = i
		}
	}

	// Only split if gap is significant (> 0.4)
	if maxGap < 0.4 {
		return []*ActivitySequence{sequence}
	}

	v.logger.Info("Splitting incoherent sequence",
		"sequence_id", sequence.ID,
		"split_distance", maxGap,
		"split_index", splitIndex,
		"original_length", len(sequence.Anchors))

	// Split at the gap
	seq1Anchors := sequence.Anchors[:splitIndex+1]
	seq2Anchors := sequence.Anchors[splitIndex+1:]

	sequences := make([]*ActivitySequence, 0, 2)

	if len(seq1Anchors) >= 2 {
		seq1 := &ActivitySequence{
			ID:              sequence.ID + "_a",
			Anchors:         seq1Anchors,
			Locations:       getUniqueLocations(seq1Anchors),
			StartTime:       seq1Anchors[0].Timestamp,
			EndTime:         seq1Anchors[len(seq1Anchors)-1].Timestamp,
			IsCrossLocation: hasMultipleLocations(seq1Anchors),
		}
		sequences = append(sequences, seq1)
	}

	if len(seq2Anchors) >= 2 {
		seq2 := &ActivitySequence{
			ID:              sequence.ID + "_b",
			Anchors:         seq2Anchors,
			Locations:       getUniqueLocations(seq2Anchors),
			StartTime:       seq2Anchors[0].Timestamp,
			EndTime:         seq2Anchors[len(seq2Anchors)-1].Timestamp,
			IsCrossLocation: hasMultipleLocations(seq2Anchors),
		}
		sequences = append(sequences, seq2)
	}

	return sequences
}

// computeDistance calculates semantic distance between two anchors
func (v *SemanticValidator) computeDistance(a1, a2 *types.SemanticAnchor) float64 {
	return structuredDist(a1.SemanticEmbedding, a2.SemanticEmbedding)
}

// structuredDist computes distance using block-wise metrics (same as in clustering/dbscan.go)
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

func cyclicDistance(v1, v2 []float32) float64 {
	var totalDist float64
	pairs := len(v1) / 2

	for i := 0; i < pairs; i++ {
		sin1 := float64(v1[i*2])
		cos1 := float64(v1[i*2+1])
		sin2 := float64(v2[i*2])
		cos2 := float64(v2[i*2+1])

		dotProd := sin1*sin2 + cos1*cos2
		dotProd = math.Max(-1.0, math.Min(1.0, dotProd))

		angle := math.Acos(dotProd)
		totalDist += angle / math.Pi
	}

	return totalDist / float64(pairs)
}

func euclideanDistance(v1, v2 []float32) float64 {
	var sum float64
	for i := 0; i < len(v1); i++ {
		diff := float64(v1[i]) - float64(v2[i])
		sum += diff * diff
	}
	distance := math.Sqrt(sum) / math.Sqrt(2.0)
	return math.Min(1.0, distance)
}

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

// Helper functions

func getUniqueLocations(anchors []*types.SemanticAnchor) []string {
	locationSet := make(map[string]bool)
	var locations []string
	for _, anchor := range anchors {
		if !locationSet[anchor.Location] {
			locationSet[anchor.Location] = true
			locations = append(locations, anchor.Location)
		}
	}
	return locations
}

func hasMultipleLocations(anchors []*types.SemanticAnchor) bool {
	if len(anchors) == 0 {
		return false
	}
	firstLoc := anchors[0].Location
	for _, anchor := range anchors {
		if anchor.Location != firstLoc {
			return true
		}
	}
	return false
}
