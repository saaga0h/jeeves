package mqtt

import "fmt"

// Topic constants based on mqtt-topics.md specification
const (
	// Raw sensor data topics (input)
	TopicRawSensors = "automation/raw/+/+"
	TopicRawMotion  = "automation/raw/motion/+"
	TopicRawTemp    = "automation/raw/temperature/+"
	TopicRawIllum   = "automation/raw/illuminance/+"

	// Processed sensor topics (output)
	TopicSensorBase  = "automation/sensor"
	TopicSensorMotion = "automation/sensor/motion/+"
	TopicSensorTemp   = "automation/sensor/temperature/+"
	TopicSensorIllum  = "automation/sensor/illuminance/+"
)

// RawSensorTopic constructs a raw sensor topic for a specific sensor type and location
// Pattern: automation/raw/{sensor_type}/{location}
func RawSensorTopic(sensorType, location string) string {
	return fmt.Sprintf("automation/raw/%s/%s", sensorType, location)
}

// ProcessedSensorTopic constructs a processed sensor topic for a specific sensor type and location
// Pattern: automation/sensor/{sensor_type}/{location}
// This is the output topic after the collector stores data in Redis
func ProcessedSensorTopic(sensorType, location string) string {
	return fmt.Sprintf("automation/sensor/%s/%s", sensorType, location)
}

// ConvertRawToProcessed converts a raw sensor topic to its processed equivalent
// automation/raw/{type}/{location} -> automation/sensor/{type}/{location}
func ConvertRawToProcessed(rawTopic string) string {
	// Simple string replacement as per specification
	return rawTopic[0:14] + "sensor" + rawTopic[17:]
}
