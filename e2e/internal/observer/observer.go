package observer

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// CapturedMessage represents a single MQTT message captured during observation
type CapturedMessage struct {
	Timestamp time.Time   `json:"timestamp"`
	Topic     string      `json:"topic"`
	Payload   interface{} `json:"payload"`
	QoS       byte        `json:"qos"`
}

// Observer captures all MQTT traffic for later analysis
type Observer struct {
	client    mqtt.Client
	messages  []CapturedMessage
	startTime time.Time
	mutex     sync.RWMutex
	broker    string
	logger    *log.Logger
}

// NewObserver creates a new MQTT observer
func NewObserver(broker string, logger *log.Logger) *Observer {
	if logger == nil {
		logger = log.Default()
	}

	return &Observer{
		broker:   broker,
		messages: make([]CapturedMessage, 0),
		logger:   logger,
	}
}

// Start begins capturing MQTT traffic
func (o *Observer) Start() error {
	o.startTime = time.Now()

	opts := mqtt.NewClientOptions()
	opts.AddBroker(o.broker)
	opts.SetClientID("jeeves-observer")
	opts.SetCleanSession(true)
	opts.SetAutoReconnect(true)
	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		o.logger.Printf("Connection lost: %v", err)
	})
	opts.SetOnConnectHandler(func(client mqtt.Client) {
		o.logger.Printf("Connected to MQTT broker at %s", o.broker)
		// Subscribe to all topics
		token := client.Subscribe("#", 0, o.messageHandler)
		token.Wait()
		if token.Error() != nil {
			o.logger.Printf("Failed to subscribe to all topics: %v", token.Error())
		} else {
			o.logger.Printf("Subscribed to all topics (#)")
		}
	})

	o.client = mqtt.NewClient(opts)
	token := o.client.Connect()
	token.Wait()
	if token.Error() != nil {
		return fmt.Errorf("failed to connect to MQTT broker: %w", token.Error())
	}

	return nil
}

// messageHandler processes incoming MQTT messages
func (o *Observer) messageHandler(client mqtt.Client, msg mqtt.Message) {
	elapsed := time.Since(o.startTime).Seconds()

	// Try to parse payload as JSON
	var payload interface{}
	if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
		// If not JSON, store as string
		payload = string(msg.Payload())
	}

	captured := CapturedMessage{
		Timestamp: time.Now(),
		Topic:     msg.Topic(),
		Payload:   payload,
		QoS:       msg.Qos(),
	}

	o.mutex.Lock()
	o.messages = append(o.messages, captured)
	o.mutex.Unlock()

	// Log with elapsed time
	payloadStr, _ := json.Marshal(payload)
	o.logger.Printf("[%7.2fs] %s: %s", elapsed, msg.Topic(), string(payloadStr))
}

// GetMessagesByTopic returns all messages for a specific topic
func (o *Observer) GetMessagesByTopic(topic string) []CapturedMessage {
	o.mutex.RLock()
	defer o.mutex.RUnlock()

	var matches []CapturedMessage
	for _, msg := range o.messages {
		if msg.Topic == topic {
			matches = append(matches, msg)
		}
	}

	return matches
}

// GetMessagesInTimeRange returns messages within a time range
func (o *Observer) GetMessagesInTimeRange(start, end time.Time) []CapturedMessage {
	o.mutex.RLock()
	defer o.mutex.RUnlock()

	var matches []CapturedMessage
	for _, msg := range o.messages {
		if msg.Timestamp.After(start) && msg.Timestamp.Before(end) {
			matches = append(matches, msg)
		}
	}

	return matches
}

// GetMessagesSince returns messages since a specific time
func (o *Observer) GetMessagesSince(since time.Time) []CapturedMessage {
	o.mutex.RLock()
	defer o.mutex.RUnlock()

	var matches []CapturedMessage
	for _, msg := range o.messages {
		if msg.Timestamp.After(since) || msg.Timestamp.Equal(since) {
			matches = append(matches, msg)
		}
	}

	return matches
}

// GetAllMessages returns all captured messages
func (o *Observer) GetAllMessages() []CapturedMessage {
	o.mutex.RLock()
	defer o.mutex.RUnlock()

	// Return a copy
	messages := make([]CapturedMessage, len(o.messages))
	copy(messages, o.messages)
	return messages
}

// SaveCapture saves all captured messages to a JSON file
func (o *Observer) SaveCapture(filename string) error {
	o.mutex.RLock()
	defer o.mutex.RUnlock()

	data, err := json.MarshalIndent(o.messages, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal messages: %w", err)
	}

	if err := saveToFile(filename, data); err != nil {
		return fmt.Errorf("failed to save capture: %w", err)
	}

	o.logger.Printf("Saved %d messages to %s", len(o.messages), filename)
	return nil
}

// Stop disconnects from the MQTT broker
func (o *Observer) Stop() {
	if o.client != nil && o.client.IsConnected() {
		o.client.Disconnect(250)
		o.logger.Printf("Disconnected from MQTT broker")
	}
}

// GetStartTime returns when observation started
func (o *Observer) GetStartTime() time.Time {
	return o.startTime
}

// GetMessageCount returns the number of captured messages
func (o *Observer) GetMessageCount() int {
	o.mutex.RLock()
	defer o.mutex.RUnlock()
	return len(o.messages)
}
