package illuminance

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/saaga0h/jeeves-platform/pkg/config"
	"github.com/saaga0h/jeeves-platform/pkg/mqtt"
	"github.com/saaga0h/jeeves-platform/pkg/redis"
)

// Agent represents the illuminance agent that monitors light levels and adjusts lighting
// TODO: Implement illuminance monitoring and light adjustment logic
// See docs/illuminance/ for specifications
type Agent struct {
	mqtt   mqtt.Client
	redis  redis.Client
	cfg    *config.Config
	logger *slog.Logger
}

// NewAgent creates a new illuminance agent with the given dependencies
func NewAgent(mqttClient mqtt.Client, redisClient redis.Client, cfg *config.Config, logger *slog.Logger) *Agent {
	return &Agent{
		mqtt:   mqttClient,
		redis:  redisClient,
		cfg:    cfg,
		logger: logger,
	}
}

// Start starts the illuminance agent and begins monitoring light levels
func (a *Agent) Start(ctx context.Context) error {
	a.logger.Info("Starting illuminance agent",
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

	// TODO: Subscribe to illuminance sensor topics
	// TODO: Implement light level monitoring logic
	// TODO: Implement automated light adjustment based on illuminance thresholds
	// See docs/illuminance/agent_behaviors.md for complete specification

	a.logger.Info("Illuminance agent started (stub implementation - business logic TODO)")

	// Block until context is cancelled
	<-ctx.Done()
	a.logger.Info("Illuminance agent stopping")

	return nil
}

// Stop gracefully stops the illuminance agent
func (a *Agent) Stop() error {
	a.logger.Info("Stopping illuminance agent")

	// Disconnect from MQTT
	a.mqtt.Disconnect()

	// Close Redis connection
	if err := a.redis.Close(); err != nil {
		a.logger.Error("Error closing Redis connection", "error", err)
		return err
	}

	a.logger.Info("Illuminance agent stopped")
	return nil
}
