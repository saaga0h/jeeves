package collector

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

const (
	// TTL for all sensor data (24 hours as per redis-schema.md)
	sensorDataTTL = 24 * time.Hour

	// Max age for sorted set entries (24 hours in milliseconds)
	maxAge = 24 * 60 * 60 * 1000
)

// Storage handles Redis storage operations for sensor data
type Storage struct {
	redis            redis.Client
	maxSensorHistory int
	logger           *slog.Logger
}

// NewStorage creates a new storage handler
func NewStorage(redisClient redis.Client, cfg *config.Config, logger *slog.Logger) *Storage {
	return &Storage{
		redis:            redisClient,
		maxSensorHistory: cfg.MaxSensorHistory,
		logger:           logger,
	}
}

// StoreSensorData routes sensor data to the appropriate storage strategy
// Based on sensor type as per redis-schema.md
func (s *Storage) StoreSensorData(ctx context.Context, msg *SensorMessage, processor *Processor) error {
	switch msg.SensorType {
	case "motion":
		return s.storeMotionData(ctx, msg, processor)
	case "temperature", "illuminance":
		return s.storeEnvironmentalData(ctx, msg, processor)
	default:
		return s.storeGenericData(ctx, msg, processor)
	}
}

// storeMotionData stores motion sensor data using sorted set + metadata hash
// Pattern from redis-schema.md:
// - sensor:motion:{location} (sorted set)
// - meta:motion:{location} (hash with lastMotionTime)
func (s *Storage) storeMotionData(ctx context.Context, msg *SensorMessage, processor *Processor) error {
	key := redis.MotionSensorKey(msg.Location)
	metaKey := redis.MotionMetaKey(msg.Location)

	// Build motion data
	motionData := processor.BuildMotionData(msg)

	// Serialize to JSON
	jsonData, err := json.Marshal(motionData)
	if err != nil {
		return fmt.Errorf("failed to marshal motion data: %w", err)
	}

	// Add to sorted set with timestamp as score
	score := float64(msg.CollectedAt)
	if err := s.redis.ZAdd(ctx, key, score, jsonData); err != nil {
		return fmt.Errorf("failed to add motion data to sorted set: %w", err)
	}

	// Update metadata if motion detected (state == "on")
	if motionData.State == "on" {
		if err := s.redis.HSet(ctx, metaKey, "lastMotionTime", strconv.FormatInt(msg.CollectedAt, 10)); err != nil {
			s.logger.Warn("Failed to update motion metadata", "location", msg.Location, "error", err)
			// Don't fail the entire operation if metadata update fails
		}
		if err := s.redis.Expire(ctx, metaKey, sensorDataTTL); err != nil {
			s.logger.Warn("Failed to set TTL on motion metadata", "location", msg.Location, "error", err)
		}
	}

	// Clean old entries (older than 24 hours)
	maxAgeTimestamp := msg.CollectedAt - maxAge
	if err := s.redis.ZRemRangeByScore(ctx, key, "-inf", strconv.FormatInt(maxAgeTimestamp, 10)); err != nil {
		s.logger.Warn("Failed to clean old motion data", "location", msg.Location, "error", err)
	}

	// Set TTL
	if err := s.redis.Expire(ctx, key, sensorDataTTL); err != nil {
		return fmt.Errorf("failed to set TTL on motion data: %w", err)
	}

	// Log buffer size
	count, err := s.redis.ZCard(ctx, key)
	if err != nil {
		s.logger.Warn("Failed to get motion buffer size", "location", msg.Location, "error", err)
	} else {
		s.logger.Debug("Stored motion data",
			"location", msg.Location,
			"state", motionData.State,
			"buffer_size", count)
	}

	return nil
}

// storeEnvironmentalData stores temperature/illuminance data in consolidated sorted set
// Pattern from redis-schema.md:
// - sensor:environmental:{location} (sorted set with all environmental readings)
func (s *Storage) storeEnvironmentalData(ctx context.Context, msg *SensorMessage, processor *Processor) error {
	key := redis.EnvironmentalSensorKey(msg.Location)

	// Build environmental data
	envData := processor.BuildEnvironmentalData(msg)

	// Serialize to JSON
	jsonData, err := json.Marshal(envData)
	if err != nil {
		return fmt.Errorf("failed to marshal environmental data: %w", err)
	}

	// Add to sorted set with timestamp as score
	score := float64(msg.CollectedAt)
	if err := s.redis.ZAdd(ctx, key, score, jsonData); err != nil {
		return fmt.Errorf("failed to add environmental data to sorted set: %w", err)
	}

	// Clean old entries (older than 24 hours)
	maxAgeTimestamp := msg.CollectedAt - maxAge
	if err := s.redis.ZRemRangeByScore(ctx, key, "-inf", strconv.FormatInt(maxAgeTimestamp, 10)); err != nil {
		s.logger.Warn("Failed to clean old environmental data", "location", msg.Location, "error", err)
	}

	// Set TTL
	if err := s.redis.Expire(ctx, key, sensorDataTTL); err != nil {
		return fmt.Errorf("failed to set TTL on environmental data: %w", err)
	}

	// Log buffer size
	count, err := s.redis.ZCard(ctx, key)
	if err != nil {
		s.logger.Warn("Failed to get environmental buffer size", "location", msg.Location, "error", err)
	} else {
		s.logger.Debug("Stored environmental data",
			"sensor_type", msg.SensorType,
			"location", msg.Location,
			"buffer_size", count)
	}

	return nil
}

// storeGenericData stores unknown sensor types using list + metadata hash
// Pattern from redis-schema.md:
// - sensor:{sensor_type}:{location} (list)
// - meta:{sensor_type}:{location} (hash with metadata)
func (s *Storage) storeGenericData(ctx context.Context, msg *SensorMessage, processor *Processor) error {
	key := redis.GenericSensorKey(msg.SensorType, msg.Location)
	metaKey := redis.GenericMetaKey(msg.SensorType, msg.Location)

	// Build generic data
	genericData := processor.BuildGenericData(msg)

	// Serialize to JSON
	jsonData, err := json.Marshal(genericData)
	if err != nil {
		return fmt.Errorf("failed to marshal generic data: %w", err)
	}

	// Push to head of list (LPUSH)
	if err := s.redis.LPush(ctx, key, jsonData); err != nil {
		return fmt.Errorf("failed to push generic data to list: %w", err)
	}

	// Trim to max history size
	if err := s.redis.LTrim(ctx, key, 0, int64(s.maxSensorHistory-1)); err != nil {
		s.logger.Warn("Failed to trim generic sensor list", "sensor_type", msg.SensorType, "location", msg.Location, "error", err)
	}

	// Update metadata
	if err := s.redis.HSet(ctx, metaKey, "last_update", strconv.FormatInt(msg.CollectedAt, 10)); err != nil {
		s.logger.Warn("Failed to update generic sensor metadata", "sensor_type", msg.SensorType, "location", msg.Location, "error", err)
	}
	if err := s.redis.HSet(ctx, metaKey, "sensor_type", msg.SensorType); err != nil {
		s.logger.Warn("Failed to update generic sensor metadata", "sensor_type", msg.SensorType, "location", msg.Location, "error", err)
	}
	if err := s.redis.HSet(ctx, metaKey, "location", msg.Location); err != nil {
		s.logger.Warn("Failed to update generic sensor metadata", "sensor_type", msg.SensorType, "location", msg.Location, "error", err)
	}

	// Set TTL on both keys
	if err := s.redis.Expire(ctx, key, sensorDataTTL); err != nil {
		return fmt.Errorf("failed to set TTL on generic data: %w", err)
	}
	if err := s.redis.Expire(ctx, metaKey, sensorDataTTL); err != nil {
		s.logger.Warn("Failed to set TTL on generic metadata", "sensor_type", msg.SensorType, "location", msg.Location, "error", err)
	}

	// Log buffer size
	count, err := s.redis.LLen(ctx, key)
	if err != nil {
		s.logger.Warn("Failed to get generic buffer size", "sensor_type", msg.SensorType, "location", msg.Location, "error", err)
	} else {
		s.logger.Debug("Stored generic sensor data",
			"sensor_type", msg.SensorType,
			"location", msg.Location,
			"buffer_size", count)
	}

	return nil
}
