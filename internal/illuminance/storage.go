package illuminance

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/saaga0h/jeeves-platform/pkg/config"
	"github.com/saaga0h/jeeves-platform/pkg/redis"
)

// IlluminanceReading represents a single illuminance measurement
type IlluminanceReading struct {
	Timestamp time.Time
	Lux       float64
	Source    string
}

// DataSummary contains illuminance data for multiple time windows
type DataSummary struct {
	Location         string
	LatestReading    *IlluminanceReading
	Last5Min         []IlluminanceReading
	Last30Min        []IlluminanceReading
	LastHour         []IlluminanceReading
	HasSufficientData bool
}

// Storage handles read-only Redis operations for illuminance data
type Storage struct {
	redis  redis.Client
	cfg    *config.Config
	logger *slog.Logger
}

// NewStorage creates a new Storage instance
func NewStorage(redisClient redis.Client, cfg *config.Config, logger *slog.Logger) *Storage {
	return &Storage{
		redis:  redisClient,
		cfg:    cfg,
		logger: logger,
	}
}

// GetIlluminanceSummary retrieves a comprehensive summary of illuminance data for a location
func (s *Storage) GetIlluminanceSummary(ctx context.Context, location string) (*DataSummary, error) {
	key := fmt.Sprintf("sensor:environmental:%s", location)
	now := time.Now()

	// Calculate time boundaries
	fiveMinAgo := now.Add(-5 * time.Minute)
	thirtyMinAgo := now.Add(-30 * time.Minute)
	oneHourAgo := now.Add(-time.Duration(s.cfg.MaxDataAgeHours * float64(time.Hour)))

	// Query different time windows
	last5Min, err := s.getIlluminanceInWindow(ctx, key, fiveMinAgo, now)
	if err != nil {
		return nil, fmt.Errorf("failed to get 5-minute data: %w", err)
	}

	last30Min, err := s.getIlluminanceInWindow(ctx, key, thirtyMinAgo, now)
	if err != nil {
		return nil, fmt.Errorf("failed to get 30-minute data: %w", err)
	}

	lastHour, err := s.getIlluminanceInWindow(ctx, key, oneHourAgo, now)
	if err != nil {
		return nil, fmt.Errorf("failed to get hourly data: %w", err)
	}

	// Determine latest reading
	var latestReading *IlluminanceReading
	if len(last5Min) > 0 {
		latestReading = &last5Min[len(last5Min)-1]
	} else if len(last30Min) > 0 {
		latestReading = &last30Min[len(last30Min)-1]
	} else if len(lastHour) > 0 {
		latestReading = &lastHour[len(lastHour)-1]
	}

	// Check if we have sufficient data (configurable threshold)
	hasSufficientData := len(lastHour) >= s.cfg.MinReadingsRequired

	summary := &DataSummary{
		Location:          location,
		LatestReading:     latestReading,
		Last5Min:          last5Min,
		Last30Min:         last30Min,
		LastHour:          lastHour,
		HasSufficientData: hasSufficientData,
	}

	s.logger.Debug("Retrieved illuminance summary",
		"location", location,
		"5min_count", len(last5Min),
		"30min_count", len(last30Min),
		"hour_count", len(lastHour),
		"sufficient_data", hasSufficientData)

	return summary, nil
}

// getIlluminanceInWindow retrieves illuminance readings within a time window
func (s *Storage) getIlluminanceInWindow(ctx context.Context, key string, start, end time.Time) ([]IlluminanceReading, error) {
	// Query Redis sorted set by score (timestamp in milliseconds)
	minScore := float64(start.UnixMilli())
	maxScore := float64(end.UnixMilli())

	values, err := s.redis.ZRangeByScoreWithScores(ctx, key, minScore, maxScore)
	if err != nil {
		return nil, fmt.Errorf("Redis query failed: %w", err)
	}

	readings := make([]IlluminanceReading, 0, len(values))

	for _, item := range values {
		// Parse JSON value
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(item.Member), &data); err != nil {
			s.logger.Warn("Failed to parse JSON", "error", err, "key", key)
			continue
		}

		// Check if this entry contains illuminance data
		luxValue, hasLux := data["illuminance"]
		if !hasLux {
			// This environmental reading doesn't have illuminance data (might be temperature-only)
			continue
		}

		// Extract lux value (handle both float64 and int)
		var lux float64
		switch v := luxValue.(type) {
		case float64:
			lux = v
		case int:
			lux = float64(v)
		default:
			s.logger.Warn("Invalid lux value type", "type", fmt.Sprintf("%T", v), "key", key)
			continue
		}

		// Extract timestamp (use score as fallback)
		timestamp := time.UnixMilli(int64(item.Score))
		if tsStr, ok := data["timestamp"].(string); ok {
			if parsed, err := time.Parse(time.RFC3339, tsStr); err == nil {
				timestamp = parsed
			}
		}

		// Extract source (optional)
		source := ""
		if src, ok := data["source"].(string); ok {
			source = src
		}

		readings = append(readings, IlluminanceReading{
			Timestamp: timestamp,
			Lux:       lux,
			Source:    source,
		})
	}

	return readings, nil
}

// GetAllLocations retrieves all locations that have illuminance data
func (s *Storage) GetAllLocations(ctx context.Context) ([]string, error) {
	pattern := "sensor:environmental:*"
	keys, err := s.redis.Keys(ctx, pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to get keys: %w", err)
	}

	locations := make([]string, 0, len(keys))
	for _, key := range keys {
		// Extract location from key: sensor:environmental:{location}
		location := key[len("sensor:environmental:"):]
		locations = append(locations, location)
	}

	return locations, nil
}
