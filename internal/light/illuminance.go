package light

import (
	"context"
	"log/slog"
	"time"

	"github.com/saaga0h/jeeves-platform/internal/illuminance"
	"github.com/saaga0h/jeeves-platform/pkg/config"
	"github.com/saaga0h/jeeves-platform/pkg/redis"
)

// IlluminanceAssessment represents the illuminance state for decision making
type IlluminanceAssessment struct {
	State      string  // dark/dim/moderate/bright
	Lux        float64
	Confidence float64
	Source     string // recent_reading/historical_pattern/time_based_default
}

// IlluminanceAnalyzer handles illuminance assessment using a 3-tier fallback strategy
type IlluminanceAnalyzer struct {
	storage *illuminance.Storage
	cfg     *config.Config
	logger  *slog.Logger
}

// NewIlluminanceAnalyzer creates a new illuminance analyzer
func NewIlluminanceAnalyzer(redisClient redis.Client, cfg *config.Config, logger *slog.Logger) *IlluminanceAnalyzer {
	storage := illuminance.NewStorage(redisClient, cfg, logger)
	return &IlluminanceAnalyzer{
		storage: storage,
		cfg:     cfg,
		logger:  logger,
	}
}

// GetIlluminanceAssessment performs 3-tier fallback strategy
func (ia *IlluminanceAnalyzer) GetIlluminanceAssessment(ctx context.Context, location, timeOfDay string) *IlluminanceAssessment {
	// Strategy 1: Recent Data (highest confidence)
	if assessment := ia.tryRecentData(ctx, location); assessment != nil {
		return assessment
	}

	// Strategy 2: Historical Pattern (medium confidence)
	if assessment := ia.tryHistoricalPattern(ctx, location, time.Now().Hour()); assessment != nil {
		return assessment
	}

	// Strategy 3: Time-Based Default (lowest confidence)
	return ia.getTimeBasedDefault(timeOfDay)
}

// tryRecentData attempts to get recent illuminance data
func (ia *IlluminanceAnalyzer) tryRecentData(ctx context.Context, location string) *IlluminanceAssessment {
	summary, err := ia.storage.GetIlluminanceSummary(ctx, location)
	if err != nil || summary.LatestReading == nil {
		return nil
	}

	age := time.Since(summary.LatestReading.Timestamp)

	// Only use if < 2 minutes old
	if age < 2*time.Minute {
		state := categorizeIlluminance(summary.LatestReading.Lux)
		return &IlluminanceAssessment{
			State:      state,
			Lux:        summary.LatestReading.Lux,
			Confidence: 0.95,
			Source:     "recent_reading",
		}
	}

	return nil
}

// tryHistoricalPattern attempts to get historical pattern data
func (ia *IlluminanceAnalyzer) tryHistoricalPattern(ctx context.Context, location string, hour int) *IlluminanceAssessment {
	// Get data from the same hour over the last 7 days
	// This is a simplified implementation - in production, you'd query specific historical data
	summary, err := ia.storage.GetIlluminanceSummary(ctx, location)
	if err != nil || len(summary.LastHour) < ia.cfg.MinReadingsRequired {
		return nil
	}

	// Calculate average from available data
	var sum float64
	for _, reading := range summary.LastHour {
		sum += reading.Lux
	}
	avgLux := sum / float64(len(summary.LastHour))

	// Confidence scales with sample count
	confidence := 0.5 + (float64(len(summary.LastHour)) / 14.0) // Max 0.5 + 7/14 = 0.75
	if confidence > 0.9 {
		confidence = 0.9
	}

	state := categorizeIlluminance(avgLux)
	return &IlluminanceAssessment{
		State:      state,
		Lux:        avgLux,
		Confidence: confidence,
		Source:     "historical_pattern",
	}
}

// getTimeBasedDefault returns default values based on time of day
func (ia *IlluminanceAnalyzer) getTimeBasedDefault(timeOfDay string) *IlluminanceAssessment {
	// Night hours - assume dark
	if timeOfDay == "night" || timeOfDay == "late_evening" {
		return &IlluminanceAssessment{
			State:      "dark",
			Lux:        10,
			Confidence: 0.5,
			Source:     "time_based_default",
		}
	}

	// Day hours - assume dim (some ambient light)
	return &IlluminanceAssessment{
		State:      "dim",
		Lux:        30,
		Confidence: 0.5,
		Source:     "time_based_default",
	}
}

// categorizeIlluminance converts lux value to semantic state
func categorizeIlluminance(lux float64) string {
	if lux < 5 {
		return "dark"
	} else if lux < 50 {
		return "dim"
	} else if lux < 200 {
		return "moderate"
	}
	return "bright"
}

// isLikelyNaturalLight infers if the light is natural based on lux and time of day
func isLikelyNaturalLight(lux float64, timeOfDay string) bool {
	// During daytime hours with moderate to high lux, likely natural
	isDaytime := timeOfDay == "morning" || timeOfDay == "midday" || timeOfDay == "afternoon"
	return isDaytime && lux > 100
}

// getTimeOfDay returns the semantic time period based on current hour
func getTimeOfDay() string {
	hour := time.Now().Hour()

	switch {
	case hour >= 5 && hour < 7:
		return "early_morning"
	case hour >= 7 && hour < 10:
		return "morning"
	case hour >= 10 && hour < 14:
		return "midday"
	case hour >= 14 && hour < 17:
		return "afternoon"
	case hour >= 17 && hour < 20:
		return "evening"
	case hour >= 20 && hour < 22:
		return "late_evening"
	default:
		return "night"
	}
}
