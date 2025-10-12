package mqtt

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/saaga0h/jeeves-platform/pkg/config"
)

// mqttClient implements the Client interface using the Paho MQTT client
type mqttClient struct {
	client pahomqtt.Client
	cfg    *config.Config
	logger *slog.Logger
}

// NewClient creates a new MQTT client with the given configuration
func NewClient(cfg *config.Config, logger *slog.Logger) Client {
	opts := pahomqtt.NewClientOptions()
	opts.AddBroker(cfg.MQTTAddress())

	// Set client ID (auto-generate if not provided)
	if cfg.MQTTClientID != "" {
		opts.SetClientID(cfg.MQTTClientID)
	} else {
		opts.SetClientID(fmt.Sprintf("%s-%d", cfg.ServiceName, time.Now().Unix()))
	}

	// Set credentials if provided
	if cfg.MQTTUser != "" {
		opts.SetUsername(cfg.MQTTUser)
	}
	if cfg.MQTTPassword != "" {
		opts.SetPassword(cfg.MQTTPassword)
	}

	// Connection settings
	opts.SetCleanSession(true)
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(5 * time.Second)
	opts.SetMaxReconnectInterval(30 * time.Second)

	// Connection handlers
	opts.OnConnect = func(c pahomqtt.Client) {
		logger.Info("Connected to MQTT broker", "broker", cfg.MQTTAddress())
	}

	opts.OnConnectionLost = func(c pahomqtt.Client, err error) {
		logger.Warn("MQTT connection lost", "error", err)
	}

	opts.OnReconnecting = func(c pahomqtt.Client, opts *pahomqtt.ClientOptions) {
		logger.Info("MQTT reconnecting...")
	}

	return &mqttClient{
		client: pahomqtt.NewClient(opts),
		cfg:    cfg,
		logger: logger,
	}
}

// Connect establishes a connection to the MQTT broker
func (m *mqttClient) Connect(ctx context.Context) error {
	m.logger.Info("Connecting to MQTT broker", "broker", m.cfg.MQTTAddress())

	token := m.client.Connect()

	// Wait for connection with context timeout
	select {
	case <-token.Done():
		if token.Error() != nil {
			return fmt.Errorf("failed to connect to MQTT broker: %w", token.Error())
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("connection timeout: %w", ctx.Err())
	}
}

// Disconnect closes the connection to the MQTT broker
func (m *mqttClient) Disconnect() {
	m.logger.Info("Disconnecting from MQTT broker")
	m.client.Disconnect(250) // 250ms grace period
}

// Subscribe subscribes to a topic with the given QoS and handler
func (m *mqttClient) Subscribe(topic string, qos byte, handler MessageHandler) error {
	m.logger.Info("Subscribing to MQTT topic", "topic", topic, "qos", qos)

	// Wrap the handler to convert paho message to our interface
	pahoHandler := func(client pahomqtt.Client, msg pahomqtt.Message) {
		handler(&mqttMessage{msg: msg})
	}

	token := m.client.Subscribe(topic, qos, pahoHandler)
	token.Wait()

	if token.Error() != nil {
		return fmt.Errorf("failed to subscribe to topic %s: %w", topic, token.Error())
	}

	m.logger.Info("Successfully subscribed to topic", "topic", topic)
	return nil
}

// Publish publishes a message to a topic
func (m *mqttClient) Publish(topic string, qos byte, retained bool, payload []byte) error {
	token := m.client.Publish(topic, qos, retained, payload)
	token.Wait()

	if token.Error() != nil {
		return fmt.Errorf("failed to publish to topic %s: %w", topic, token.Error())
	}

	m.logger.Debug("Published message", "topic", topic, "size", len(payload))
	return nil
}

// IsConnected returns whether the client is currently connected
func (m *mqttClient) IsConnected() bool {
	return m.client.IsConnected()
}

// mqttMessage wraps a Paho MQTT message to implement our Message interface
type mqttMessage struct {
	msg pahomqtt.Message
}

func (m *mqttMessage) Topic() string {
	return m.msg.Topic()
}

func (m *mqttMessage) Payload() []byte {
	return m.msg.Payload()
}

func (m *mqttMessage) Ack() {
	m.msg.Ack()
}
