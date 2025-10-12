package light

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/saaga0h/jeeves-platform/pkg/config"
	"github.com/saaga0h/jeeves-platform/pkg/mqtt"
)

// Agent represents the light agent that bridges MQTT commands with Matter/physical lights
// TODO: Implement Matter/MQTT bridge for light control
// See docs/light/ for specifications
type Agent struct {
	mqtt   mqtt.Client
	cfg    *config.Config
	logger *slog.Logger
}

// NewAgent creates a new light agent with the given dependencies
func NewAgent(mqttClient mqtt.Client, cfg *config.Config, logger *slog.Logger) *Agent {
	return &Agent{
		mqtt:   mqttClient,
		cfg:    cfg,
		logger: logger,
	}
}

// Start starts the light agent and begins processing light commands
func (a *Agent) Start(ctx context.Context) error {
	a.logger.Info("Starting light agent",
		"service_name", a.cfg.ServiceName,
		"mqtt_broker", a.cfg.MQTTAddress())

	// Connect to MQTT broker
	if err := a.mqtt.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to MQTT: %w", err)
	}

	// TODO: Subscribe to light command topics
	// TODO: Implement Matter protocol integration
	// TODO: Handle light state commands (on, off, brightness, color)
	// TODO: Publish light state updates back to MQTT
	// See docs/light/agent-behaviors.md for complete specification

	a.logger.Info("Light agent started (stub implementation - business logic TODO)")

	// Block until context is cancelled
	<-ctx.Done()
	a.logger.Info("Light agent stopping")

	return nil
}

// Stop gracefully stops the light agent
func (a *Agent) Stop() error {
	a.logger.Info("Stopping light agent")

	// Disconnect from MQTT
	a.mqtt.Disconnect()

	a.logger.Info("Light agent stopped")
	return nil
}
