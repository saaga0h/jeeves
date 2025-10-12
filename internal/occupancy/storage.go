package occupancy

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/saaga0h/jeeves-platform/pkg/config"
	"github.com/saaga0h/jeeves-platform/pkg/redis"
)

// Storage wraps Redis operations for occupancy agent
type Storage struct {
	redis  redis.Client
	cfg    *config.Config
	logger *slog.Logger
}

// NewStorage creates a new storage wrapper
func NewStorage(redisClient redis.Client, cfg *config.Config, logger *slog.Logger) *Storage {
	return &Storage{
		redis:  redisClient,
		cfg:    cfg,
		logger: logger,
	}
}

// GetMotionCountInWindow returns the count of motion events in a time window
func (s *Storage) GetMotionCountInWindow(ctx context.Context, location string, start, end time.Time) (int, error) {
	key := fmt.Sprintf("sensor:motion:%s", location)

	// Query sorted set by score range (timestamps in milliseconds)
	members, err := s.redis.ZRangeByScoreWithScores(ctx, key, float64(start.UnixMilli()), float64(end.UnixMilli()))
	if err != nil {
		s.logger.Warn("Failed to query motion window", "location", location, "error", err)
		return 0, err
	}

	return len(members), nil
}

// GetMotionEventsInWindow returns timestamps of motion events in a time window
func (s *Storage) GetMotionEventsInWindow(ctx context.Context, location string, start, end time.Time) ([]time.Time, error) {
	key := fmt.Sprintf("sensor:motion:%s", location)

	// Query sorted set by score range
	members, err := s.redis.ZRangeByScoreWithScores(ctx, key, float64(start.UnixMilli()), float64(end.UnixMilli()))
	if err != nil {
		s.logger.Warn("Failed to query motion events", "location", location, "error", err)
		return nil, err
	}

	// Extract timestamps from scores
	timestamps := make([]time.Time, len(members))
	for i, member := range members {
		timestamps[i] = time.UnixMilli(int64(member.Score))
	}

	return timestamps, nil
}

// GetMinutesSinceLastMotion returns minutes since the last motion event
func (s *Storage) GetMinutesSinceLastMotion(ctx context.Context, location string, referenceTime time.Time) (float64, error) {
	key := fmt.Sprintf("meta:motion:%s", location)

	// Get last motion time from metadata hash
	lastMotionStr, err := s.redis.HGet(ctx, key, "lastMotionTime")
	if err != nil {
		// No motion history
		return 999.0, nil
	}

	// Parse timestamp
	lastMotionMs, err := strconv.ParseInt(lastMotionStr, 10, 64)
	if err != nil {
		s.logger.Warn("Failed to parse last motion time", "location", location, "error", err)
		return 999.0, nil
	}

	lastMotionTime := time.UnixMilli(lastMotionMs)
	minutesSince := referenceTime.Sub(lastMotionTime).Minutes()

	return minutesSince, nil
}

// GetAllLocations returns all locations with sensor data
func (s *Storage) GetAllLocations(ctx context.Context) ([]string, error) {
	// Get all motion sensor keys
	motionKeys, err := s.redis.Keys(ctx, "sensor:motion:*")
	if err != nil {
		s.logger.Warn("Failed to get motion keys", "error", err)
		return nil, err
	}

	// Extract locations (strip "sensor:motion:" prefix)
	locationMap := make(map[string]bool)
	for _, key := range motionKeys {
		if len(key) > 14 { // len("sensor:motion:") = 14
			location := key[14:]
			locationMap[location] = true
		}
	}

	// Convert to slice
	locations := make([]string, 0, len(locationMap))
	for location := range locationMap {
		locations = append(locations, location)
	}

	return locations, nil
}

// HasMotionHistory checks if a location has any motion data
func (s *Storage) HasMotionHistory(ctx context.Context, location string) bool {
	key := fmt.Sprintf("sensor:motion:%s", location)

	count, err := s.redis.ZCard(ctx, key)
	if err != nil {
		return false
	}

	return count > 0
}

// TemporalState represents the current state of a location
type TemporalState struct {
	CurrentOccupancy *bool
	LastStateChange  *time.Time
	LastAnalysis     *time.Time
	PredictionHistory []PredictionRecord
}

// GetTemporalState retrieves all temporal state for a location
func (s *Storage) GetTemporalState(ctx context.Context, location string) (*TemporalState, error) {
	key := fmt.Sprintf("temporal:%s", location)

	// Get hash fields
	fields, err := s.redis.HGetAll(ctx, key)
	if err != nil {
		s.logger.Warn("Failed to get temporal state", "location", location, "error", err)
		// Return empty state
		return &TemporalState{}, nil
	}

	state := &TemporalState{}

	// Parse currentOccupancy
	if occupancyStr, ok := fields["currentOccupancy"]; ok {
		if occupancyStr == "true" {
			occupied := true
			state.CurrentOccupancy = &occupied
		} else if occupancyStr == "false" {
			occupied := false
			state.CurrentOccupancy = &occupied
		}
	}

	// Parse lastStateChange
	if changeStr, ok := fields["lastStateChange"]; ok {
		if changeMs, err := strconv.ParseInt(changeStr, 10, 64); err == nil {
			changeTime := time.UnixMilli(changeMs)
			state.LastStateChange = &changeTime
		}
	}

	// Parse lastAnalysis
	if analysisStr, ok := fields["lastAnalysis"]; ok {
		if analysisTime, err := time.Parse(time.RFC3339, analysisStr); err == nil {
			state.LastAnalysis = &analysisTime
		}
	}

	// Get prediction history
	history, err := s.GetPredictionHistory(ctx, location)
	if err == nil {
		state.PredictionHistory = history
	}

	return state, nil
}

// UpdateOccupancy updates the occupancy state and last state change time
func (s *Storage) UpdateOccupancy(ctx context.Context, location string, occupied bool) error {
	key := fmt.Sprintf("temporal:%s", location)

	// Update currentOccupancy
	if err := s.redis.HSet(ctx, key, "currentOccupancy", fmt.Sprintf("%t", occupied)); err != nil {
		return err
	}

	// Update lastStateChange
	if err := s.redis.HSet(ctx, key, "lastStateChange", fmt.Sprintf("%d", time.Now().UnixMilli())); err != nil {
		return err
	}

	return nil
}

// UpdateLastAnalysis updates the last analysis timestamp
func (s *Storage) UpdateLastAnalysis(ctx context.Context, location string) error {
	key := fmt.Sprintf("temporal:%s", location)
	return s.redis.HSet(ctx, key, "lastAnalysis", time.Now().Format(time.RFC3339))
}

// AddPredictionHistory adds a prediction to the history (FIFO, max 10)
func (s *Storage) AddPredictionHistory(ctx context.Context, location string, prediction PredictionRecord) error {
	key := fmt.Sprintf("predictions:%s", location)

	// Serialize to JSON
	data, err := json.Marshal(prediction)
	if err != nil {
		return fmt.Errorf("failed to marshal prediction: %w", err)
	}

	// Add to front of list
	if err := s.redis.LPush(ctx, key, string(data)); err != nil {
		return err
	}

	// Trim to 10 entries
	if err := s.redis.LTrim(ctx, key, 0, 9); err != nil {
		return err
	}

	return nil
}

// GetPredictionHistory retrieves prediction history for a location
func (s *Storage) GetPredictionHistory(ctx context.Context, location string) ([]PredictionRecord, error) {
	key := fmt.Sprintf("predictions:%s", location)

	// Get list length
	length, err := s.redis.LLen(ctx, key)
	if err != nil || length == 0 {
		return []PredictionRecord{}, nil
	}

	// Get all predictions (stored newest first)
	values, err := s.redis.LRange(ctx, key, 0, -1)
	if err != nil {
		s.logger.Warn("Failed to get prediction history", "location", location, "error", err)
		return []PredictionRecord{}, nil
	}

	// Parse JSON and reverse order (oldest first for analysis)
	predictions := make([]PredictionRecord, 0, len(values))
	for i := len(values) - 1; i >= 0; i-- {
		var pred PredictionRecord
		if err := json.Unmarshal([]byte(values[i]), &pred); err != nil {
			s.logger.Warn("Failed to parse prediction", "location", location, "error", err)
			continue
		}
		predictions = append(predictions, pred)
	}

	return predictions, nil
}
