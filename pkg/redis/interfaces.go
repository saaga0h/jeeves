package redis

import (
	"context"
	"time"
)

// ZMember represents a sorted set member with its score
type ZMember struct {
	Score  float64
	Member string
}

// Client represents a Redis client interface for testing and abstraction
type Client interface {
	// Set sets a key to a value with an optional TTL
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error

	// Get gets the value of a key
	Get(ctx context.Context, key string) (string, error)

	// HSet sets a field in a hash
	HSet(ctx context.Context, key string, field string, value interface{}) error

	// HGet gets a field from a hash
	HGet(ctx context.Context, key string, field string) (string, error)

	// HGetAll gets all fields from a hash
	HGetAll(ctx context.Context, key string) (map[string]string, error)

	// ZAdd adds a member with a score to a sorted set
	ZAdd(ctx context.Context, key string, score float64, member interface{}) error

	// ZRemRangeByScore removes members with scores between min and max
	ZRemRangeByScore(ctx context.Context, key string, min, max string) error

	// ZCard returns the number of members in a sorted set
	ZCard(ctx context.Context, key string) (int64, error)

	// ZRangeByScoreWithScores returns members in a sorted set within a score range with their scores
	ZRangeByScoreWithScores(ctx context.Context, key string, min, max float64) ([]ZMember, error)

	// Keys returns all keys matching a pattern
	Keys(ctx context.Context, pattern string) ([]string, error)

	// LPush pushes values to the head of a list
	LPush(ctx context.Context, key string, values ...interface{}) error

	// LTrim trims a list to the specified range
	LTrim(ctx context.Context, key string, start, stop int64) error

	// LLen returns the length of a list
	LLen(ctx context.Context, key string) (int64, error)

	// LRange returns a range of elements from a list
	LRange(ctx context.Context, key string, start, stop int64) ([]string, error)

	// Expire sets a TTL on a key
	Expire(ctx context.Context, key string, ttl time.Duration) error

	// ZRevRangeByScoreWithScores returns members in a sorted set within a score range with their scores (reverse order - highest first)
	ZRevRangeByScoreWithScores(ctx context.Context, key string, max, min float64, offset, count int64) ([]ZMember, error)

	// Ping checks the connection to Redis
	Ping(ctx context.Context) error

	// Close closes the Redis connection
	Close() error
}
