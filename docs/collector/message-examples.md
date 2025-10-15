# Collector Agent - Message Examples

This document shows real examples of the messages that flow through the Collector Agent, helping you understand the data formats and transformations.

## Message Flow Overview

```
Raw Sensor Data → Collector Agent → Stored Data + Trigger Messages
```

The Collector Agent receives raw sensor data, processes it, stores it in Redis, and publishes trigger messages for other agents.

---

## Input Messages: What Sensors Send

All sensors publish to topics following the pattern: `automation/raw/{sensor_type}/{location}`

### Motion Sensor Examples

### Motion Sensor Examples

Motion sensors detect movement and send simple on/off states.

**Topic**: `automation/raw/motion/study`

**When motion is detected:**
```json
{
  "data": {
    "state": "on",
    "entity_id": "binary_sensor.motion_study",
    "sequence": 42
  }
}
```

**When motion clears:**
```json
{
  "data": {
    "state": "off",
    "entity_id": "binary_sensor.motion_study", 
    "sequence": 43
  }
}
```

**Minimal motion event (missing optional fields):**
```json
{
  "data": {
    "state": "on"
  }
}
```

**Key Fields:**
- `state`: "on" (motion detected) or "off" (motion cleared) 
- `entity_id`: sensor identifier (optional)
- `sequence`: event counter (optional)

### Temperature Sensor Examples

Temperature sensors report current temperature readings.

### Temperature Sensor Examples

Temperature sensors report current temperature readings.

**Topic**: `automation/raw/temperature/living_room`

**Celsius reading:**
```json
{
  "data": {
    "value": 22.5,
    "unit": "°C"
  }
}
```

**Fahrenheit reading:**
```json
{
  "data": {
    "value": 72.5,
    "unit": "°F"
  }
}
```

**Reading without unit (defaults to Celsius):**
```json
{
  "data": {
    "value": 21.8
  }
}
```

**Key Fields:**
- `value`: temperature reading (required)
- `unit`: "°C" or "°F" (defaults to "°C")

### Illuminance Sensor Examples

Illuminance sensors measure light levels in lux.

**Topic**: `automation/raw/illuminance/bedroom`

**Standard light reading:**
```json
{
  "data": {
    "value": 450.0,
    "unit": "lux"
  }
}
```

**Low light reading:**
```json
{
  "data": {
    "value": 12.5,
    "unit": "lux"
  }
}
```

**Bright daylight:**
```json
{
  "data": {
    "value": 8500.0,
    "unit": "lux"
  }
}
```

**Key Fields:**
- `value`: light level in lux (required)
- `unit`: measurement unit (defaults to "lux")

### Other Sensor Types

The Collector Agent handles any sensor type, storing unknown types using generic storage.

**Pressure sensor example:**
**Topic**: `automation/raw/pressure/kitchen`
```json
{
  "data": {
    "value": 1013.25,
    "unit": "hPa",
    "trend": "stable"
  }
}
```

**Humidity sensor example:**
**Topic**: `automation/raw/humidity/bathroom`
```json
{
  "data": {
    "value": 65.5,
    "unit": "%"
  }
}
```

---

## Output Messages: What the Collector Agent Publishes

After storing sensor data, the Collector Agent publishes trigger messages to notify other agents.

### Trigger Message Pattern

All trigger messages follow: `automation/sensor/{sensor_type}/{location}`

The payload includes the original data plus a `stored_at` timestamp.

### Motion Trigger Examples

### Motion Trigger Examples

**Topic**: `automation/sensor/motion/study`

**After storing motion detection:**
```json
{
  "data": {
    "state": "on",
    "entity_id": "binary_sensor.motion_study",
    "sequence": 42
  },
  "original_topic": "automation/raw/motion/study",
  "stored_at": "2025-01-01T12:00:00.123Z"
}
```

**After storing motion clear:**
```json
{
  "data": {
    "state": "off", 
    "entity_id": "binary_sensor.motion_study",
    "sequence": 43
  },
  "original_topic": "automation/raw/motion/study",
  "stored_at": "2025-01-01T12:00:15.456Z"
}
```

### Environmental Trigger Examples

**Temperature trigger:**
**Topic**: `automation/sensor/temperature/living_room`
```json
{
  "data": {
    "value": 22.5,
    "unit": "°C"
  },
  "original_topic": "automation/raw/temperature/living_room",
  "stored_at": "2025-01-01T12:00:00.789Z"
}
```

**Illuminance trigger:**
**Topic**: `automation/sensor/illuminance/bedroom`
```json
{
  "data": {
    "value": 450.0,
    "unit": "lux"
  },
  "original_topic": "automation/raw/illuminance/bedroom", 
  "stored_at": "2025-01-01T12:00:00.321Z"
}
```

**Added Fields in Triggers:**
- `original_topic`: The raw topic where data came from
- `stored_at`: ISO timestamp when data was stored in Redis

---

## Stored Data: What's in Redis

The Collector Agent stores data differently based on sensor type for optimal performance.

### Motion Data in Redis

**Key**: `sensor:motion:study` (sorted set)
**Score**: Unix timestamp in milliseconds
**Value**:
```json
{
  "timestamp": "2025-01-01T12:00:00.123Z",
  "state": "on",
  "entity_id": "binary_sensor.motion_study",
  "sequence": 42,
  "collected_at": 1704110400123
}
```

**Metadata Key**: `meta:motion:study` (hash)
**Fields**:
- `lastMotionTime`: "1704110400123" (only updated when state = "on")

### Environmental Data in Redis

**Key**: `sensor:environmental:living_room` (sorted set)
**Score**: Unix timestamp in milliseconds

**Temperature entry:**
```json
{
  "timestamp": "2025-01-01T12:00:00.123Z",
  "collected_at": 1704110400123,
  "temperature": 22.5,
  "temperature_unit": "°C"
}
```

**Illuminance entry:**
```json
{
  "timestamp": "2025-01-01T12:00:00.456Z", 
  "collected_at": 1704110400456,
  "illuminance": 450.0,
  "illuminance_unit": "lux"
}
```

### Generic Sensor Data in Redis

**Key**: `sensor:pressure:kitchen` (list, newest first)
**Value**:
```json
{
  "data": {
    "value": 1013.25,
    "unit": "hPa",
    "trend": "stable"
  },
  "original_topic": "automation/raw/pressure/kitchen",
  "timestamp": "2025-01-01T12:00:00.123Z",
  "collected_at": 1704110400123
}
```

**Metadata Key**: `meta:pressure:kitchen` (hash)
**Fields**:
- `last_update`: "1704110400123"
- `sensor_type`: "pressure"
- `location`: "kitchen"

---

## VictoriaMetrics: Long-term Storage (Optional)

If enabled, the Collector Agent forwards numeric sensor data to VictoriaMetrics for long-term analytics.

### Temperature Metric Example
```json
{
  "metric": {
    "__name__": "sensor_temperature_value",
    "location": "living_room",
    "sensor_type": "temperature"
  },
  "value": 22.5,
  "timestamp": 1704110400123
}
```

### Multiple Metrics from Complex Sensor
```json
[
  {
    "metric": {
      "__name__": "sensor_pressure_value",
      "location": "kitchen", 
      "sensor_type": "pressure"
    },
    "value": 1013.25,
    "timestamp": 1704110400123
  }
]
```

**Naming Convention**: `sensor_{sensor_type}_{field_name}`
**Labels**: Always include `location` and `sensor_type`
**Values**: Only numeric fields are extracted and forwarded

---

## Health Check Response

**Endpoint**: `GET /health`

**Simple response (current implementation):**
```json
{
  "status": "ok",
  "timestamp": "2025-01-01T12:00:00.123Z"
}
```

This lightweight response ensures fast health checks for container orchestration.

---

## Message Validation and Error Handling

### What Happens to Invalid Messages

**Invalid topic format:**
```
Topic: automation/raw/motion
Error: Not enough topic parts (needs at least 4)
Action: Log warning, skip message
```

**Invalid JSON:**
```
Topic: automation/raw/motion/study
Payload: {invalid json
Error: JSON parse error
Action: Log error, skip message  
```

**Missing required fields:**
```json
{
  "data": {}
}
```
**Action**: Use default values where possible (e.g., state = "unknown")

### Default Value Behavior

**Motion sensors:**
- Missing `state`: defaults to "unknown"
- Missing `entity_id`: defaults to null
- Missing `sequence`: defaults to 0

**Temperature sensors:**
- Missing `unit`: defaults to "°C"
- Missing `value`: logs error, skips message

**Illuminance sensors:**
- Missing `unit`: defaults to "lux"
- Missing `value`: logs error, skips message

---

## Real-World Data Flow Example

Here's how a complete motion detection event flows through the system:

```
1. PIR sensor detects motion in study
   └─ Publishes: automation/raw/motion/study
   └─ Payload: {"data": {"state": "on", "entity_id": "motion.study"}}

2. Collector Agent receives message
   └─ Validates topic and JSON
   └─ Extracts: sensor_type="motion", location="study"

3. Stores in Redis
   └─ Key: sensor:motion:study (sorted set)
   └─ Value: {..., "state": "on", "timestamp": "...", ...}
   └─ Key: meta:motion:study (hash)
   └─ Field: lastMotionTime = current_timestamp

4. Forwards to VictoriaMetrics (if enabled)
   └─ Skipped (motion state is not numeric)

5. Publishes trigger
   └─ Topic: automation/sensor/motion/study
   └─ Payload: {original data + "stored_at": "..."}

6. Other agents receive trigger
   └─ Occupancy Agent: Updates room occupancy status
   └─ Light Agent: May adjust lighting based on motion
   └─ Behavior Agent: Records motion event for pattern analysis
```

This shows how a single sensor event ripples through the entire system via the Collector Agent.