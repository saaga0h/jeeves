package collector

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/saaga0h/jeeves-platform/pkg/config"
	"github.com/saaga0h/jeeves-platform/pkg/mqtt"
	"github.com/saaga0h/jeeves-platform/pkg/redis"
)

// Agent represents the collector agent that receives sensor data and stores it in Redis
type Agent struct {
	mqtt        mqtt.Client
	redis       redis.Client
	processor   *Processor
	storage     *Storage
	cfg         *config.Config
	logger      *slog.Logger
	timeManager *TimeManager
}

// NewAgent creates a new collector agent with the given dependencies
func NewAgent(mqttClient mqtt.Client, redisClient redis.Client, cfg *config.Config, logger *slog.Logger) *Agent {
	timeManager := NewTimeManager(logger)

	processor := NewProcessor(logger, timeManager)
	storage := NewStorage(redisClient, mqttClient, cfg, logger, timeManager)

	return &Agent{
		mqtt:        mqttClient,
		redis:       redisClient,
		processor:   processor,
		storage:     storage,
		cfg:         cfg,
		logger:      logger,
		timeManager: timeManager,
	}
}

// Start starts the collector agent and begins processing sensor messages
func (a *Agent) Start(ctx context.Context) error {
	a.logger.Info("Starting collector agent",
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

	if err := a.timeManager.ConfigureFromMQTT(a.mqtt); err != nil {
		a.logger.Warn("Failed to subscribe to test mode config", "error", err)
		// Not fatal - continue without test mode support
	}

	// Subscribe to sensor topics
	for _, topic := range a.cfg.SensorTopics {
		if err := a.mqtt.Subscribe(topic, 0, a.handleMessage); err != nil {
			a.logger.Error("Failed to subscribe to topic", "topic", topic, "error", err)
			// Continue subscribing to other topics even if one fails
			continue
		}
	}

	a.logger.Info("Collector agent started and ready to receive messages",
		"subscribed_topics", strings.Join(a.cfg.SensorTopics, ", "))

	// Block until context is cancelled
	<-ctx.Done()
	a.logger.Info("Collector agent stopping")

	return nil
}

// Stop gracefully stops the collector agent
func (a *Agent) Stop() error {
	a.logger.Info("Stopping collector agent")

	// Disconnect from MQTT
	a.mqtt.Disconnect()

	// Close Redis connection
	if err := a.redis.Close(); err != nil {
		a.logger.Error("Error closing Redis connection", "error", err)
		return err
	}

	a.logger.Info("Collector agent stopped")
	return nil
}

// handleMessage processes incoming MQTT messages
func (a *Agent) handleMessage(msg mqtt.Message) {
	topic := msg.Topic()
	payload := msg.Payload()

	a.logger.Debug("Received MQTT message", "topic", topic, "size", len(payload))

	// Parse the message
	sensorMsg, err := a.processor.ParseMessage(topic, payload)
	if err != nil {
		a.logger.Error("Failed to parse message", "topic", topic, "error", err)
		return
	}

	// Create context for storage operations
	ctx := context.Background()

	// Store sensor data in Redis
	if err := a.storage.StoreSensorData(ctx, sensorMsg, a.processor); err != nil {
		a.logger.Error("Failed to store sensor data",
			"sensor_type", sensorMsg.SensorType,
			"location", sensorMsg.Location,
			"error", err)
		// Continue to publish trigger even if storage fails
		// Downstream consumers can retry
	}

	// Publish trigger message to processed topic
	if err := a.publishTrigger(sensorMsg); err != nil {
		a.logger.Error("Failed to publish trigger message",
			"sensor_type", sensorMsg.SensorType,
			"location", sensorMsg.Location,
			"error", err)
	}

	a.logger.Info("Sensor data processed",
		"sensor_type", sensorMsg.SensorType,
		"location", sensorMsg.Location)
}

// publishTrigger publishes a trigger message to the processed sensor topic
// Converts automation/raw/{type}/{location} -> automation/sensor/{type}/{location}
func (a *Agent) publishTrigger(msg *SensorMessage) error {
	// Build trigger topic
	triggerTopic := mqtt.ProcessedSensorTopic(msg.SensorType, msg.Location)

	// Build trigger payload
	payload, err := a.processor.BuildTriggerPayload(msg)
	if err != nil {
		return fmt.Errorf("failed to build trigger payload: %w", err)
	}

	// Publish trigger message (QoS 0, not retained)
	if err := a.mqtt.Publish(triggerTopic, 0, false, payload); err != nil {
		return fmt.Errorf("failed to publish trigger: %w", err)
	}

	a.logger.Info("Published trigger", "topic", triggerTopic)

	return nil
}
