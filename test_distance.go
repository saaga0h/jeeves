package main

import (
	"fmt"
	"math"
)

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

func main() {
	// Spatial blocks from the actual embeddings (indices 12-27, 16 dimensions)
	study_spatial := []float32{0.17926429, 0.059754763, 0.26889643, 0, 0.029877381, 0.23901905, 0.059754763, 0, 0.20914166, 0.089632146, 0, 0.17926429, 0.089632146, 0.059754763, 0.20914166, 0}
	living_spatial := []float32{0.056071706, 0.056071706, 0, 0.25232267, 0.028035853, 0.22428682, 0.056071706, 0, 0.028035853, 0.19625096, 0.19625096, 0.16821513, 0.14017926, 0.22428682, 0.11214341, 0}

	// Compute cosine similarity
	cosSim := cosineSimilaritySlice(study_spatial, living_spatial)
	spatialDist := 1.0 - cosSim

	fmt.Printf("Cosine similarity: %.6f\n", cosSim)
	fmt.Printf("Spatial distance (1 - cosine): %.6f\n", spatialDist)
	fmt.Printf("Weighted contribution (0.30 * %.6f): %.6f\n", spatialDist, 0.30*spatialDist)

	// Test euclidean distance normalization issue
	// Max case: one vector all 1s, other all 0s for 16-dim vector
	maxCase1 := make([]float32, 16)
	maxCase2 := make([]float32, 16)
	for i := 0; i < 16; i++ {
		maxCase1[i] = 1.0
		maxCase2[i] = 0.0
	}

	var sum float64
	for i := 0; i < 16; i++ {
		diff := float64(maxCase1[i]) - float64(maxCase2[i])
		sum += diff * diff
	}
	rawDist := math.Sqrt(sum)
	normalizedDist := rawDist / math.Sqrt(2.0)

	fmt.Printf("\nEuclidean distance normalization issue:\n")
	fmt.Printf("For 16-dim vector, max raw distance: %.6f\n", rawDist)
	fmt.Printf("Normalized by √2: %.6f (WRONG - exceeds 1.0!)\n", normalizedDist)
	fmt.Printf("Should normalize by: %.6f (√16 = 4)\n", math.Sqrt(16.0))
	fmt.Printf("Correct normalized: %.6f\n", rawDist/math.Sqrt(16.0))
}
