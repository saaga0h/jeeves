package mqtt

import "context"

// Client represents an MQTT client interface for testing and abstraction
type Client interface {
	// Connect establishes a connection to the MQTT broker
	Connect(ctx context.Context) error

	// Disconnect closes the connection to the MQTT broker
	Disconnect()

	// Subscribe subscribes to a topic with the given QoS and handler
	Subscribe(topic string, qos byte, handler MessageHandler) error

	// Publish publishes a message to a topic
	Publish(topic string, qos byte, retained bool, payload []byte) error

	// IsConnected returns whether the client is currently connected
	IsConnected() bool
}

// MessageHandler is a callback function for handling incoming MQTT messages
type MessageHandler func(Message)

// Message represents an MQTT message
type Message interface {
	// Topic returns the topic the message was published to
	Topic() string

	// Payload returns the message payload
	Payload() []byte

	// Ack acknowledges the message (for QoS > 0)
	Ack()
}
