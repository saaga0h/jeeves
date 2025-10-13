package checker

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/saaga0h/jeeves-platform/e2e/internal/scenario"
)

// CheckRedisExpectation validates a Redis state expectation
func CheckRedisExpectation(ctx context.Context, client *redis.Client, exp scenario.Expectation) (bool, string, interface{}) {
	if exp.RedisKey == "" {
		return false, "redis_key is empty", nil
	}

	// Get the value from Redis
	value, err := client.HGet(ctx, exp.RedisKey, exp.RedisField).Result()
	if err == redis.Nil {
		return false, fmt.Sprintf("key %q field %q not found in Redis", exp.RedisKey, exp.RedisField), nil
	}
	if err != nil {
		return false, fmt.Sprintf("Redis error: %v", err), nil
	}

	// Match against expected value
	matches, reason := MatchesExpectation(value, exp.Expected)
	if !matches {
		return false, reason, value
	}

	return true, "", value
}
