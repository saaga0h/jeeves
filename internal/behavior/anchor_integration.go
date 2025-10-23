package behavior

import (
	"context"
	"database/sql"
	"fmt"

	goredis "github.com/redis/go-redis/v9"

	"github.com/saaga0h/jeeves-platform/internal/behavior/anchor"
	behaviorcontext "github.com/saaga0h/jeeves-platform/internal/behavior/context"
	"github.com/saaga0h/jeeves-platform/internal/behavior/storage"
	"github.com/saaga0h/jeeves-platform/internal/behavior/types"
	"github.com/saaga0h/jeeves-platform/pkg/config"
)

// initializeAnchorCreator sets up the semantic anchor creation system.
// This should be called during agent initialization.
func (a *Agent) initializeAnchorCreator(cfg *config.Config) error {
	// Get database connection from pgClient
	// Note: This assumes pgClient has a way to get the underlying *sql.DB
	// You may need to adapt this based on your postgres.Client interface
	db, err := a.getDBConnection()
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}

	// Create storage layer
	anchorStorage := storage.NewAnchorStorage(db)

	// Create context gatherer
	// Note: Need to convert redis.Client interface to *redis.Client
	// We need the underlying go-redis client for ZRevRangeWithScores
	redisClient := a.getRedisClient()
	contextGatherer := behaviorcontext.NewContextGatherer(redisClient, a.logger)

	// Create anchor creator
	a.anchorCreator = anchor.NewAnchorCreator(anchorStorage, contextGatherer, a.logger)

	a.logger.Info("Semantic anchor system initialized")
	return nil
}

// createAnchorFromEvent creates a semantic anchor from an event during episode detection.
// This is called for significant events (motion ON, lighting ON) to create anchor points.
func (a *Agent) createAnchorFromEvent(ctx context.Context, event Event) error {
	// Skip if anchor creator not initialized
	if a.anchorCreator == nil {
		return nil
	}

	// Gather signals from the event
	signals := []types.ActivitySignal{
		{
			Type:       event.Type,
			Confidence: 0.8, // Default confidence
			Timestamp:  event.Timestamp,
			Value:      a.buildSignalValue(event),
		},
	}

	// Create semantic anchor
	anchor, err := a.anchorCreator.CreateAnchor(ctx, event.Location, event.Timestamp, signals)
	if err != nil {
		a.logger.Warn("Failed to create semantic anchor",
			"location", event.Location,
			"event_type", event.Type,
			"error", err)
		// Don't fail episode creation if anchor creation fails
		return nil
	}

	a.logger.Debug("Created semantic anchor",
		"anchor_id", anchor.ID,
		"location", event.Location,
		"event_type", event.Type,
		"embedding_dims", len(anchor.SemanticEmbedding.Slice()))

	return nil
}

// buildSignalValue constructs the signal value map from an event.
func (a *Agent) buildSignalValue(event Event) map[string]interface{} {
	value := map[string]interface{}{
		"type": event.Type,
	}

	switch event.Type {
	case "motion":
		value["state"] = event.State
	case "lighting":
		value["state"] = event.State
		value["source"] = event.Source
	case "presence":
		value["state"] = event.State
	}

	return value
}

// getDBConnection extracts the underlying *sql.DB from the postgres client.
// The postgres.Client interface doesn't expose DB(), but the concrete
// *PostgresClient implementation does, so we need a type assertion.
func (a *Agent) getDBConnection() (*sql.DB, error) {
	// Type assert to access the DB() method
	// This is safe because we know the concrete type is *PostgresClient
	type dbGetter interface {
		DB() *sql.DB
	}

	if getter, ok := a.pgClient.(dbGetter); ok {
		db := getter.DB()
		if db == nil {
			return nil, fmt.Errorf("postgres client not connected")
		}
		return db, nil
	}

	return nil, fmt.Errorf("postgres client does not expose DB() method")
}

// getRedisClient extracts the underlying *redis.Client from the redis client interface.
// Since our redis.Client is an interface wrapping go-redis, we need to create a new
// direct connection for the context gatherer.
func (a *Agent) getRedisClient() *goredis.Client {
	// Create a new go-redis client with the same configuration
	// This is necessary because our redis.Client interface doesn't expose
	// ZRevRangeWithScores method needed by the context gatherer
	opts := &goredis.Options{
		Addr:     a.cfg.RedisAddress(),
		Password: a.cfg.RedisPassword,
		DB:       a.cfg.RedisDB,
	}

	return goredis.NewClient(opts)
}
