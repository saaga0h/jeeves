package redis

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/saaga0h/jeeves-platform/pkg/config"
)

// redisClient implements the Client interface using go-redis
type redisClient struct {
	client *redis.Client
	cfg    *config.Config
	logger *slog.Logger
}

// NewClient creates a new Redis client with the given configuration
func NewClient(cfg *config.Config, logger *slog.Logger) Client {
	opts := &redis.Options{
		Addr:     cfg.RedisAddress(),
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	}

	client := redis.NewClient(opts)

	return &redisClient{
		client: client,
		cfg:    cfg,
		logger: logger,
	}
}

// Set sets a key to a value with an optional TTL
func (r *redisClient) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	err := r.client.Set(ctx, key, value, ttl).Err()
	if err != nil {
		return fmt.Errorf("failed to set key %s: %w", key, err)
	}
	return nil
}

// Get gets the value of a key
func (r *redisClient) Get(ctx context.Context, key string) (string, error) {
	val, err := r.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", fmt.Errorf("key %s does not exist", key)
	}
	if err != nil {
		return "", fmt.Errorf("failed to get key %s: %w", key, err)
	}
	return val, nil
}

// HSet sets a field in a hash
func (r *redisClient) HSet(ctx context.Context, key string, field string, value interface{}) error {
	err := r.client.HSet(ctx, key, field, value).Err()
	if err != nil {
		return fmt.Errorf("failed to set hash field %s:%s: %w", key, field, err)
	}
	return nil
}

// HGet gets a field from a hash
func (r *redisClient) HGet(ctx context.Context, key string, field string) (string, error) {
	val, err := r.client.HGet(ctx, key, field).Result()
	if err == redis.Nil {
		return "", fmt.Errorf("hash field %s:%s does not exist", key, field)
	}
	if err != nil {
		return "", fmt.Errorf("failed to get hash field %s:%s: %w", key, field, err)
	}
	return val, nil
}

// HGetAll gets all fields from a hash
func (r *redisClient) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	val, err := r.client.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get hash %s: %w", key, err)
	}
	return val, nil
}

// ZAdd adds a member with a score to a sorted set
func (r *redisClient) ZAdd(ctx context.Context, key string, score float64, member interface{}) error {
	err := r.client.ZAdd(ctx, key, redis.Z{
		Score:  score,
		Member: member,
	}).Err()
	if err != nil {
		return fmt.Errorf("failed to add to sorted set %s: %w", key, err)
	}
	return nil
}

// ZRemRangeByScore removes members with scores between min and max
func (r *redisClient) ZRemRangeByScore(ctx context.Context, key string, min, max string) error {
	err := r.client.ZRemRangeByScore(ctx, key, min, max).Err()
	if err != nil {
		return fmt.Errorf("failed to remove from sorted set %s: %w", key, err)
	}
	return nil
}

// ZCard returns the number of members in a sorted set
func (r *redisClient) ZCard(ctx context.Context, key string) (int64, error) {
	count, err := r.client.ZCard(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to get cardinality of sorted set %s: %w", key, err)
	}
	return count, nil
}

// LPush pushes values to the head of a list
func (r *redisClient) LPush(ctx context.Context, key string, values ...interface{}) error {
	err := r.client.LPush(ctx, key, values...).Err()
	if err != nil {
		return fmt.Errorf("failed to push to list %s: %w", key, err)
	}
	return nil
}

// LTrim trims a list to the specified range
func (r *redisClient) LTrim(ctx context.Context, key string, start, stop int64) error {
	err := r.client.LTrim(ctx, key, start, stop).Err()
	if err != nil {
		return fmt.Errorf("failed to trim list %s: %w", key, err)
	}
	return nil
}

// LLen returns the length of a list
func (r *redisClient) LLen(ctx context.Context, key string) (int64, error) {
	length, err := r.client.LLen(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to get length of list %s: %w", key, err)
	}
	return length, nil
}

// Expire sets a TTL on a key
func (r *redisClient) Expire(ctx context.Context, key string, ttl time.Duration) error {
	err := r.client.Expire(ctx, key, ttl).Err()
	if err != nil {
		return fmt.Errorf("failed to set expiration on key %s: %w", key, err)
	}
	return nil
}

// Ping checks the connection to Redis
func (r *redisClient) Ping(ctx context.Context) error {
	err := r.client.Ping(ctx).Err()
	if err != nil {
		return fmt.Errorf("redis ping failed: %w", err)
	}
	r.logger.Info("Connected to Redis", "address", r.cfg.RedisAddress())
	return nil
}

// Close closes the Redis connection
func (r *redisClient) Close() error {
	r.logger.Info("Closing Redis connection")
	return r.client.Close()
}
