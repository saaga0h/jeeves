package collector

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Processor handles parsing and processing of sensor messages
type Processor struct {
	logger *slog.Logger
}

// NewProcessor creates a new message processor
func NewProcessor(logger *slog.Logger) *Processor {
	return &Processor{
		logger: logger,
	}
}

// SensorMessage represents a parsed sensor message with metadata
type SensorMessage struct {
	SensorType    string
	Location      string
	OriginalTopic string
	Data          map[string]interface{}
	Timestamp     time.Time
	CollectedAt   int64 // Unix milliseconds
}

// MotionData represents motion sensor data
type MotionData struct {
	Timestamp   string      `json:"timestamp"`
	State       string      `json:"state"`
	EntityID    interface{} `json:"entity_id"`
	Sequence    int         `json:"sequence"`
	CollectedAt int64       `json:"collected_at"`
}

// EnvironmentalData represents environmental sensor data (temperature/illuminance)
type EnvironmentalData struct {
	Timestamp   string  `json:"timestamp"`
	CollectedAt int64   `json:"collected_at"`
	Temperature *float64 `json:"temperature,omitempty"`
	TempUnit    *string  `json:"temperature_unit,omitempty"`
	Illuminance *float64 `json:"illuminance,omitempty"`
	IllumUnit   *string  `json:"illuminance_unit,omitempty"`
}

// GenericData represents generic sensor data
type GenericData struct {
	Data          map[string]interface{} `json:"data"`
	OriginalTopic string                 `json:"original_topic"`
	Timestamp     string                 `json:"timestamp"`
	CollectedAt   int64                  `json:"collected_at"`
}

// ParseMessage parses an MQTT message into a structured sensor message
// Based on message-examples.md and agent-behaviors.md specifications
func (p *Processor) ParseMessage(topic string, payload []byte) (*SensorMessage, error) {
	// Parse topic to extract sensor type and location
	// Topic pattern: automation/raw/{sensor_type}/{location}
	parts := strings.Split(topic, "/")
	if len(parts) < 4 {
		p.logger.Warn("Invalid topic format", "topic", topic)
		return nil, fmt.Errorf("invalid topic format: %s (expected at least 4 parts)", topic)
	}

	sensorType := parts[2]
	location := parts[3]

	// Parse JSON payload
	var rawData map[string]interface{}
	if err := json.Unmarshal(payload, &rawData); err != nil {
		p.logger.Error("Failed to parse JSON payload", "topic", topic, "error", err)
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Extract data field (messages are wrapped in {"data": {...}})
	data, ok := rawData["data"].(map[string]interface{})
	if !ok {
		p.logger.Warn("Missing or invalid 'data' field in payload", "topic", topic)
		// Fall back to using raw data if no data field
		data = rawData
	}

	msg := &SensorMessage{
		SensorType:    sensorType,
		Location:      location,
		OriginalTopic: topic,
		Data:          data,
		Timestamp:     time.Now().UTC(),
		CollectedAt:   time.Now().UnixMilli(),
	}

	p.logger.Debug("Parsed sensor message",
		"sensor_type", sensorType,
		"location", location,
		"topic", topic)

	return msg, nil
}

// BuildMotionData converts a sensor message to motion data for Redis storage
func (p *Processor) BuildMotionData(msg *SensorMessage) *MotionData {
	// Extract fields with defaults as per message-examples.md
	state := "unknown"
	if s, ok := msg.Data["state"].(string); ok {
		state = s
	}

	var entityID interface{} = nil
	if eid, ok := msg.Data["entity_id"]; ok {
		entityID = eid
	}

	sequence := 0
	if seq, ok := msg.Data["sequence"].(float64); ok {
		sequence = int(seq)
	}

	return &MotionData{
		Timestamp:   msg.Timestamp.Format(time.RFC3339Nano),
		State:       state,
		EntityID:    entityID,
		Sequence:    sequence,
		CollectedAt: msg.CollectedAt,
	}
}

// BuildEnvironmentalData converts a sensor message to environmental data for Redis storage
func (p *Processor) BuildEnvironmentalData(msg *SensorMessage) *EnvironmentalData {
	data := &EnvironmentalData{
		Timestamp:   msg.Timestamp.Format(time.RFC3339Nano),
		CollectedAt: msg.CollectedAt,
	}

	// Handle temperature sensor
	if msg.SensorType == "temperature" {
		if value, ok := msg.Data["value"].(float64); ok {
			data.Temperature = &value
		}

		unit := "°C" // Default unit
		if u, ok := msg.Data["unit"].(string); ok {
			unit = u
		}
		data.TempUnit = &unit
	}

	// Handle illuminance sensor
	if msg.SensorType == "illuminance" {
		if value, ok := msg.Data["value"].(float64); ok {
			data.Illuminance = &value
		}

		unit := "lux" // Default unit
		if u, ok := msg.Data["unit"].(string); ok {
			unit = u
		}
		data.IllumUnit = &unit
	}

	return data
}

// BuildGenericData converts a sensor message to generic data for Redis storage
func (p *Processor) BuildGenericData(msg *SensorMessage) *GenericData {
	return &GenericData{
		Data:          msg.Data,
		OriginalTopic: msg.OriginalTopic,
		Timestamp:     msg.Timestamp.Format(time.RFC3339Nano),
		CollectedAt:   msg.CollectedAt,
	}
}

// BuildTriggerPayload creates the payload for the trigger message published to MQTT
// Includes the original data plus metadata
func (p *Processor) BuildTriggerPayload(msg *SensorMessage) ([]byte, error) {
	payload := map[string]interface{}{
		"data":           msg.Data,
		"original_topic": msg.OriginalTopic,
		"stored_at":      msg.Timestamp.Format(time.RFC3339Nano),
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal trigger payload: %w", err)
	}

	return data, nil
}
