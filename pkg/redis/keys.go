package redis

import "fmt"

// Key construction helpers based on redis-schema.md

// MotionSensorKey returns the key for motion sensor data (sorted set)
// Pattern: sensor:motion:{location}
func MotionSensorKey(location string) string {
	return fmt.Sprintf("sensor:motion:%s", location)
}

// MotionMetaKey returns the key for motion sensor metadata (hash)
// Pattern: meta:motion:{location}
func MotionMetaKey(location string) string {
	return fmt.Sprintf("meta:motion:%s", location)
}

// EnvironmentalSensorKey returns the key for environmental sensor data (sorted set)
// Pattern: sensor:environmental:{location}
func EnvironmentalSensorKey(location string) string {
	return fmt.Sprintf("sensor:environmental:%s", location)
}

// GenericSensorKey returns the key for generic sensor data (list)
// Pattern: sensor:{sensor_type}:{location}
func GenericSensorKey(sensorType, location string) string {
	return fmt.Sprintf("sensor:%s:%s", sensorType, location)
}

// GenericMetaKey returns the key for generic sensor metadata (hash)
// Pattern: meta:{sensor_type}:{location}
func GenericMetaKey(sensorType, location string) string {
	return fmt.Sprintf("meta:%s:%s", sensorType, location)
}
