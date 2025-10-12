package occupancy

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/saaga0h/jeeves-platform/pkg/config"
	"github.com/saaga0h/jeeves-platform/pkg/mqtt"
	"github.com/saaga0h/jeeves-platform/pkg/redis"
)

// Agent represents the occupancy agent that detects and tracks occupancy in rooms
// TODO: Implement occupancy detection and behavioral tracking
// See docs/occupancy/ for specifications
type Agent struct {
	mqtt   mqtt.Client
	redis  redis.Client
	cfg    *config.Config
	logger *slog.Logger
}

// NewAgent creates a new occupancy agent with the given dependencies
func NewAgent(mqttClient mqtt.Client, redisClient redis.Client, cfg *config.Config, logger *slog.Logger) *Agent {
	return &Agent{
		mqtt:   mqttClient,
		redis:  redisClient,
		cfg:    cfg,
		logger: logger,
	}
}

// Start starts the occupancy agent and begins tracking occupancy
func (a *Agent) Start(ctx context.Context) error {
	a.logger.Info("Starting occupancy agent",
		"service_name", a.cfg.ServiceName,
		"mqtt_broker", a.cfg.MQTTAddress())

	// Connect to MQTT broker
	if err := a.mqtt.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to MQTT: %w", err)
	}

	// Verify Redis connection
	if err := a.redis.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping Redis: %w", err)
	}

	// TODO: Subscribe to motion/occupancy sensor topics
	// TODO: Implement occupancy detection logic
	// TODO: Track behavioral patterns (time-since-motion, typical occupancy times)
	// TODO: Publish occupancy status updates
	// See docs/occupancy/agent-behaviors.md for complete specification

	a.logger.Info("Occupancy agent started (stub implementation - business logic TODO)")

	// Block until context is cancelled
	<-ctx.Done()
	a.logger.Info("Occupancy agent stopping")

	return nil
}

// Stop gracefully stops the occupancy agent
func (a *Agent) Stop() error {
	a.logger.Info("Stopping occupancy agent")

	// Disconnect from MQTT
	a.mqtt.Disconnect()

	// Close Redis connection
	if err := a.redis.Close(); err != nil {
		a.logger.Error("Error closing Redis connection", "error", err)
		return err
	}

	a.logger.Info("Occupancy agent stopped")
	return nil
}
