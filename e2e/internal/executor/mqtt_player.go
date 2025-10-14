package executor

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/saaga0h/jeeves-platform/e2e/internal/scenario"
)

// MQTTPlayer publishes sensor events to MQTT broker
type MQTTPlayer struct {
	client mqtt.Client
	logger *log.Logger
}

// NewMQTTPlayer creates a new MQTT player
func NewMQTTPlayer(broker string, logger *log.Logger) (*MQTTPlayer, error) {
	if logger == nil {
		logger = log.Default()
	}

	opts := mqtt.NewClientOptions()
	opts.AddBroker(broker)
	opts.SetClientID("jeeves-test-player")
	opts.SetCleanSession(true)
	opts.SetAutoReconnect(true)

	client := mqtt.NewClient(opts)
	token := client.Connect()
	token.Wait()
	if token.Error() != nil {
		return nil, fmt.Errorf("failed to connect to MQTT broker: %w", token.Error())
	}

	logger.Printf("Connected to MQTT broker at %s", broker)

	return &MQTTPlayer{
		client: client,
		logger: logger,
	}, nil
}

// PublishEvent publishes a sensor event to MQTT
func (p *MQTTPlayer) PublishEvent(event scenario.SensorEvent) error {
	// Parse sensor string: "type:location"
	parts := strings.SplitN(event.Sensor, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid sensor format %q, expected type:location", event.Sensor)
	}

	sensorType := parts[0]
	location := parts[1]

	// Build topic following J.E.E.V.E.S. spec: automation/raw/{type}/{location}
	topic := fmt.Sprintf("automation/raw/%s/%s", sensorType, location)

	// Build payload following collector expectations (message-examples.md)
	var payload map[string]interface{}

	if sensorType == "motion" {
		// Motion sensors send "state" field ("on" or "off"), not "value"
		state := "off"
		if boolValue, ok := event.Value.(bool); ok && boolValue {
			state = "on"
		}

		payload = map[string]interface{}{
			"data": map[string]interface{}{
				"state":     state,
				"entity_id": fmt.Sprintf("motion.%s", location),
				"sequence":  1,
			},
		}
	} else if sensorType == "temperature" || sensorType == "illuminance" {
		// Environmental sensors send numeric "value" and "unit"
		unit := "°C"
		if sensorType == "illuminance" {
			unit = "lux"
		}

		payload = map[string]interface{}{
			"data": map[string]interface{}{
				"value": event.Value,
				"unit":  unit,
			},
		}
	} else {
		// Generic sensors - use simple structure
		payload = map[string]interface{}{
			"data": map[string]interface{}{
				"value": event.Value,
			},
		}
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Publish with QoS 1 to ensure delivery
	token := p.client.Publish(topic, 1, false, payloadBytes)
	token.Wait()
	if token.Error() != nil {
		return fmt.Errorf("failed to publish to %s: %w", topic, token.Error())
	}

	p.logger.Printf("Published event to %s: %s", topic, string(payloadBytes))

	return nil
}

// PublishContextEvent publishes context messages (occupancy, lighting, etc)
func (p *MQTTPlayer) PublishContextEvent(eventType, location string, data map[string]interface{}) error {
	// Context events use: automation/context/{type}/{location}
	topic := fmt.Sprintf("automation/context/%s/%s", eventType, location)

	payloadBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	token := p.client.Publish(topic, 1, false, payloadBytes)
	token.Wait()
	if token.Error() != nil {
		return fmt.Errorf("failed to publish to %s: %w", topic, token.Error())
	}

	p.logger.Printf("Published context event to %s: %s", topic, string(payloadBytes))
	return nil
}

// PublishMediaEvent publishes media events (play, pause, stop)
func (p *MQTTPlayer) PublishMediaEvent(location string, data map[string]interface{}) error {
	// Media events use: automation/media/{action}/{location}
	action := data["state"].(string) // playing, paused, stopped
	topic := fmt.Sprintf("automation/media/%s/%s", action, location)

	payloadBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	token := p.client.Publish(topic, 1, false, payloadBytes)
	token.Wait()
	if token.Error() != nil {
		return fmt.Errorf("failed to publish to %s: %w", topic, token.Error())
	}

	p.logger.Printf("Published media event to %s: %s", topic, string(payloadBytes))
	return nil
}

// Close disconnects from MQTT broker
func (p *MQTTPlayer) Close() {
	if p.client != nil && p.client.IsConnected() {
		p.client.Disconnect(250)
		p.logger.Printf("Disconnected from MQTT broker")
	}
}
