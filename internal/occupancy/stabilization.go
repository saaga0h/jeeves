package occupancy

import (
	"math"
	"time"
)

// PredictionRecord represents a single occupancy prediction with metadata
type PredictionRecord struct {
	Timestamp            time.Time `json:"timestamp"`
	Occupied             bool      `json:"occupied"`
	Confidence           float64   `json:"confidence"`
	Reasoning            string    `json:"reasoning"`
	StabilizationApplied bool      `json:"stabilizationApplied"`
	ActualOutcome        *bool     `json:"actualOutcome,omitempty"` // For future ground truth learning
}

// StabilizationResult contains Vonich-Hakim stabilization metrics and recommendations
type StabilizationResult struct {
	StabilizationFactor float64
	VarianceFactor      float64
	OscillationFactor   float64
	ErrorFactor         float64
	OscillationCount    int
	ShouldDampen        bool
	Recommendation      string
}

// ComputeVonichHakimStabilization calculates stabilization metrics from prediction history
func ComputeVonichHakimStabilization(predictionHistory []PredictionRecord) StabilizationResult {
	// Insufficient history - need at least 2 predictions
	if len(predictionHistory) < 2 {
		return StabilizationResult{
			StabilizationFactor: 0.0,
			VarianceFactor:      0.0,
			OscillationFactor:   0.0,
			ErrorFactor:         0.0,
			OscillationCount:    0,
			ShouldDampen:        false,
			Recommendation:      "insufficient_history",
		}
	}

	// Take last 6 predictions (or fewer if not enough history)
	windowSize := 6
	if len(predictionHistory) < windowSize {
		windowSize = len(predictionHistory)
	}
	recentPredictions := predictionHistory[len(predictionHistory)-windowSize:]

	// Calculate confidence variance
	varianceFactor := calculateConfidenceVariance(recentPredictions)

	// Calculate oscillation count
	oscillationCount := calculateOscillationCount(recentPredictions)
	oscillationFactor := math.Min(0.3, float64(oscillationCount)*0.1)

	// Calculate error trend (requires actualOutcome - currently unused)
	errorFactor := calculateErrorTrend(recentPredictions)

	// Combined stabilization factor
	stabilizationFactor := varianceFactor + oscillationFactor + errorFactor

	// Determine recommendation
	shouldDampen := stabilizationFactor >= 0.15 || oscillationCount > 2
	recommendation := getRecommendation(stabilizationFactor, oscillationCount)

	return StabilizationResult{
		StabilizationFactor: stabilizationFactor,
		VarianceFactor:      varianceFactor,
		OscillationFactor:   oscillationFactor,
		ErrorFactor:         errorFactor,
		OscillationCount:    oscillationCount,
		ShouldDampen:        shouldDampen,
		Recommendation:      recommendation,
	}
}

// calculateConfidenceVariance measures consistency of model confidence over time
func calculateConfidenceVariance(predictions []PredictionRecord) float64 {
	if len(predictions) < 2 {
		return 0.0
	}

	// Extract confidence values
	confidences := make([]float64, len(predictions))
	for i, p := range predictions {
		confidences[i] = p.Confidence
	}

	// Calculate mean
	sum := 0.0
	for _, c := range confidences {
		sum += c
	}
	mean := sum / float64(len(confidences))

	// Calculate variance
	variance := 0.0
	for _, c := range confidences {
		diff := c - mean
		variance += diff * diff
	}
	variance /= float64(len(confidences))

	// Convert to factor (capped at 0.4)
	varianceFactor := math.Min(0.4, variance*2.0)

	return varianceFactor
}

// calculateOscillationCount counts state flips in prediction history
func calculateOscillationCount(predictions []PredictionRecord) int {
	if len(predictions) < 2 {
		return 0
	}

	flips := 0
	for i := 1; i < len(predictions); i++ {
		if predictions[i].Occupied != predictions[i-1].Occupied {
			flips++
		}
	}

	return flips
}

// calculateErrorTrend measures whether predictions are improving or degrading
// Currently returns 0 as actualOutcome is not populated yet
func calculateErrorTrend(predictions []PredictionRecord) float64 {
	// Filter predictions with known outcomes
	var predictionsWithOutcomes []PredictionRecord
	for _, p := range predictions {
		if p.ActualOutcome != nil {
			predictionsWithOutcomes = append(predictionsWithOutcomes, p)
		}
	}

	// Need at least 2 predictions with outcomes to calculate trend
	if len(predictionsWithOutcomes) < 2 {
		return 0.0
	}

	// Calculate errors (0 = correct, 1 = wrong)
	errors := make([]float64, len(predictionsWithOutcomes))
	for i, p := range predictionsWithOutcomes {
		if p.Occupied == *p.ActualOutcome {
			errors[i] = 0.0
		} else {
			errors[i] = 1.0
		}
	}

	// Calculate trend (slope)
	// Positive = errors increasing, negative = errors decreasing
	firstError := errors[0]
	lastError := errors[len(errors)-1]
	errorTrend := (lastError - firstError) / float64(len(errors)-1)

	// Convert to factor (capped at 0.3)
	errorFactor := math.Min(0.3, math.Abs(errorTrend)*0.5)

	return errorFactor
}

// getRecommendation determines the stabilization recommendation based on metrics
func getRecommendation(stabilizationFactor float64, oscillationCount int) string {
	// High oscillation - prefer current state
	if oscillationCount > 2 {
		return "bias_current_state"
	}

	// High instability factor
	if stabilizationFactor > 0.3 {
		return "high_dampening"
	}

	// Moderate instability
	if stabilizationFactor >= 0.15 {
		return "moderate_dampening"
	}

	// Stable system
	return "maintain_course"
}
