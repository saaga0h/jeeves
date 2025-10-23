package context

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// ContextGatherer collects semantic context dimensions for anchor creation.
type ContextGatherer struct {
	redis  *redis.Client
	logger *slog.Logger
}

// NewContextGatherer creates a new context gatherer instance.
func NewContextGatherer(
	redisClient *redis.Client,
	logger *slog.Logger,
) *ContextGatherer {
	return &ContextGatherer{
		redis:  redisClient,
		logger: logger,
	}
}

// GatherContext collects all semantic context dimensions for an anchor.
// Returns a context map with time, weather, lighting, and household mode.
func (g *ContextGatherer) GatherContext(
	ctx context.Context,
	location string,
	timestamp time.Time,
) (map[string]interface{}, error) {
	contextMap := make(map[string]interface{})

	// Time-based context (always available)
	contextMap["time_of_day"] = categorizeTimeOfDay(timestamp)
	contextMap["day_type"] = categorizeDayType(timestamp)
	contextMap["season"] = categorizeSeason(timestamp)
	contextMap["household_mode"] = categorizeHouseholdMode(timestamp)

	// Weather context (best effort - non-blocking)
	if weather, err := g.getWeatherContext(ctx); err == nil {
		contextMap["weather"] = weather
	} else {
		g.logger.Debug("Weather context unavailable", "error", err)
	}

	// Lighting state (from recent events)
	if lighting, err := g.getLightingState(ctx, location); err == nil {
		contextMap["lighting_state"] = lighting
	} else {
		g.logger.Debug("Lighting state unavailable", "location", location, "error", err)
	}

	// Add raw timestamp for reference
	contextMap["timestamp"] = timestamp.Format(time.RFC3339)

	return contextMap, nil
}

// categorizeTimeOfDay returns the time period category.
func categorizeTimeOfDay(t time.Time) string {
	hour := t.Hour()
	switch {
	case hour >= 5 && hour < 12:
		return "morning"
	case hour >= 12 && hour < 17:
		return "afternoon"
	case hour >= 17 && hour < 21:
		return "evening"
	default:
		return "night"
	}
}

// categorizeDayType returns weekday/weekend/holiday category.
func categorizeDayType(t time.Time) string {
	// TODO: Implement holiday detection
	if t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
		return "weekend"
	}
	return "weekday"
}

// categorizeSeason returns the season based on month.
func categorizeSeason(t time.Time) string {
	month := t.Month()
	switch {
	case month >= 3 && month <= 5:
		return "spring"
	case month >= 6 && month <= 8:
		return "summer"
	case month >= 9 && month <= 11:
		return "fall"
	default:
		return "winter"
	}
}

// categorizeHouseholdMode returns the typical household activity mode.
func categorizeHouseholdMode(t time.Time) string {
	hour := t.Hour()
	switch {
	case hour >= 5 && hour < 9:
		return "waking"
	case hour >= 9 && hour < 18:
		return "active"
	case hour >= 18 && hour < 22:
		return "winding_down"
	default:
		return "sleeping"
	}
}

// getWeatherContext retrieves current weather information.
// Returns nil if weather data is unavailable (non-critical).
func (g *ContextGatherer) getWeatherContext(ctx context.Context) (map[string]interface{}, error) {
	// Check for weather data in Redis
	// Expected key: "weather:current"
	val, err := g.redis.Get(ctx, "weather:current").Result()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("no weather data available")
		}
		return nil, fmt.Errorf("failed to get weather data: %w", err)
	}

	var weather map[string]interface{}
	if err := json.Unmarshal([]byte(val), &weather); err != nil {
		return nil, fmt.Errorf("failed to parse weather data: %w", err)
	}

	return weather, nil
}

// getLightingState retrieves the current lighting state for a location.
func (g *ContextGatherer) getLightingState(ctx context.Context, location string) (map[string]interface{}, error) {
	// Get most recent lighting event for this location
	key := fmt.Sprintf("sensor:lighting:%s", location)

	// Get the most recent entry (highest score)
	members, err := g.redis.ZRevRangeWithScores(ctx, key, 0, 0).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get lighting state: %w", err)
	}

	if len(members) == 0 {
		return nil, fmt.Errorf("no lighting data for location: %s", location)
	}

	var lightingData map[string]interface{}
	if err := json.Unmarshal([]byte(members[0].Member.(string)), &lightingData); err != nil {
		return nil, fmt.Errorf("failed to parse lighting data: %w", err)
	}

	return lightingData, nil
}
