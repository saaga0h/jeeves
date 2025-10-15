# Collector Agent - MQTT Topics

This document explains the MQTT topics that the Collector Agent listens to and publishes to, helping you understand the message routing in the Jeeves system.

## Topic Overview

The Collector Agent acts as a bridge between raw sensor data and processed sensor data:

```
Raw Sensors → automation/raw/+/+ → Collector Agent → automation/sensor/+/+ → Other Agents
```

## Topics the Collector Agent Subscribes To

### Primary Subscription: `automation/raw/+/+`

**Pattern**: `automation/raw/{sensor_type}/{location}`
**Purpose**: Receive raw sensor data from all sensor types and locations
**QoS**: 0 (fire-and-forget)
**Configurable**: Yes, via `SENSOR_TOPICS` environment variable

**Wildcard Explanation**:
- First `+`: Matches any sensor type (motion, temperature, illuminance, etc.)
- Second `+`: Matches any location (study, living_room, bedroom, etc.)

**Real Examples**:
- `automation/raw/motion/study` - Motion sensor in study
- `automation/raw/temperature/living_room` - Temperature sensor in living room  
- `automation/raw/illuminance/bedroom` - Light sensor in bedroom
- `automation/raw/humidity/bathroom` - Humidity sensor in bathroom

### Custom Subscriptions

You can configure specific subscriptions instead of the wildcard:

```bash
SENSOR_TOPICS=automation/raw/motion/+,automation/raw/temperature/+
```

**Use Cases**:
- Subscribe only to specific sensor types
- Ignore certain locations
- Multiple Collector Agents with different responsibilities

## Topics the Collector Agent Publishes To

### Trigger Messages: `automation/sensor/{sensor_type}/{location}`

**Pattern**: `automation/sensor/{sensor_type}/{location}`
**Purpose**: Notify other agents that new sensor data has been stored
**QoS**: 0 (fire-and-forget)
**Retained**: No (not persistent)
**Timing**: Published immediately after successful Redis storage

**Topic Transformation**:
```
Input:  automation/raw/motion/study
Output: automation/sensor/motion/study
        ^^^                    ^^^
        raw → sensor           (replace "raw" with "sensor")
```

**Real Examples**:
- `automation/sensor/motion/study` - Motion data stored for study
- `automation/sensor/temperature/living_room` - Temperature data stored for living room
- `automation/sensor/illuminance/bedroom` - Illuminance data stored for bedroom

## Who Listens to What

### Input Topic Subscribers (Who Publishes Raw Data)

**`automation/raw/motion/+`**:
- PIR motion sensors
- Camera motion detection systems
- Smart switches with motion detection

**`automation/raw/temperature/+`**:
- Thermostats
- Environmental sensors
- Weather stations

**`automation/raw/illuminance/+`**:
- Light sensors
- Environmental sensors
- Smart light bulbs with sensors

### Output Topic Subscribers (Who Uses Processed Data)

**`automation/sensor/motion/+`**:
- **Occupancy Agent**: Determines room occupancy from motion patterns
- **Light Agent**: Adjusts lighting when motion detected
- **Behavior Agent**: Tracks movement patterns for behavioral analysis

**`automation/sensor/temperature/+`**:
- **Light Agent**: Uses temperature for adaptive lighting (warmer light when cooler)
- **Behavior Agent**: Environmental context for behavioral patterns

**`automation/sensor/illuminance/+`**:
- **Illuminance Agent**: Determines ambient light levels
- **Light Agent**: Adjusts brightness based on ambient light
- **Behavior Agent**: Light conditions for behavioral context

**`automation/sensor/+/+`** (All sensor types):
- **Behavior Agent**: Subscribes to all sensor data for comprehensive pattern analysis

## Topic Structure Rules

### Input Topic Parsing

The Collector Agent expects exactly this format:
```
automation/raw/{sensor_type}/{location}
│         │   │            │
│         │   │            └─ Location identifier (e.g., "study", "living_room")
│         │   └─ Sensor type (e.g., "motion", "temperature") 
│         └─ Always "raw" for input topics
└─ Always "automation" for Jeeves topics
```

**Validation Rules**:
- Must have at least 4 parts when split by `/`
- Parts[0] must be "automation"
- Parts[1] must be "raw"
- Parts[2] becomes the sensor_type
- Parts[3] becomes the location
- Parts[4+] are ignored

**Valid Topics**:
- ✅ `automation/raw/motion/study`
- ✅ `automation/raw/temperature/living_room`
- ✅ `automation/raw/illuminance/bedroom/sensor1` (extra parts ignored)

**Invalid Topics**:
- ❌ `automation/motion/study` (missing "raw")
- ❌ `raw/motion/study` (missing "automation")
- ❌ `automation/raw/motion` (missing location)

### Output Topic Construction

The Collector Agent constructs output topics by replacing "raw" with "sensor":

```
Input:  automation/raw/{sensor_type}/{location}
Output: automation/sensor/{sensor_type}/{location}
```

**Examples**:
- `automation/raw/motion/study` → `automation/sensor/motion/study`
- `automation/raw/temperature/bedroom` → `automation/sensor/temperature/bedroom`
- `automation/raw/pressure/kitchen` → `automation/sensor/pressure/kitchen`

## Message Flow Example

Here's how topics route a motion detection event:

```
1. PIR Sensor publishes to:
   Topic: automation/raw/motion/study
   Data: {"data": {"state": "on"}}

2. Collector Agent receives message:
   - Subscribed to: automation/raw/+/+
   - Matches: automation/raw/motion/study
   - Parses: sensor_type="motion", location="study"

3. Collector Agent stores data in Redis:
   - Key: sensor:motion:study
   - Value: {motion data with timestamps}

4. Collector Agent publishes trigger:
   Topic: automation/sensor/motion/study
   Data: {original data + "stored_at": timestamp}

5. Other agents receive trigger:
   - Occupancy Agent (subscribed to automation/sensor/motion/+)
   - Light Agent (subscribed to automation/sensor/+/+)
   - Behavior Agent (subscribed to automation/sensor/+/+)

6. Other agents read full data from Redis:
   - Query: sensor:motion:study
   - Get: Complete motion history for analysis
```

## Configuration Examples

### Default Configuration (Recommended)
```bash
# Subscribe to all sensor types and locations
SENSOR_TOPICS=automation/raw/+/+
```

### Selective Configuration
```bash
# Only motion and temperature sensors
SENSOR_TOPICS=automation/raw/motion/+,automation/raw/temperature/+
```

### Location-Specific Configuration
```bash
# Only sensors from specific rooms
SENSOR_TOPICS=automation/raw/+/living_room,automation/raw/+/bedroom
```

### Multiple Topic Patterns
```bash
# Combination of patterns
SENSOR_TOPICS=automation/raw/motion/+,automation/raw/environmental/+,automation/raw/security/+
```

## Troubleshooting Topic Issues

### "Collector not receiving sensor data"

1. **Check topic format**:
   ```bash
   # Correct format
   automation/raw/motion/study
   
   # Wrong formats
   sensor/motion/study         # Missing "automation"
   automation/motion/study     # Missing "raw"
   automation/raw/motion       # Missing location
   ```

2. **Check subscription configuration**:
   ```bash
   # Verify environment variable
   echo $SENSOR_TOPICS
   # Should show: automation/raw/+/+
   ```

3. **Check MQTT connection**:
   - Verify Collector Agent connected to MQTT broker
   - Check subscription success messages in logs

### "Other agents not receiving triggers"

1. **Check trigger topic generation**:
   ```bash
   # Input: automation/raw/motion/study
   # Should become: automation/sensor/motion/study
   ```

2. **Check agent subscriptions**:
   - Occupancy Agent: `automation/sensor/motion/+`
   - Light Agent: `automation/sensor/+/+`
   - Verify agents are subscribed to correct patterns

3. **Check Redis storage**:
   - Trigger only published after successful Redis storage
   - Check for Redis connection errors in Collector logs

### "Inconsistent message delivery"

1. **QoS Settings**: All Collector topics use QoS 0 (at-most-once delivery)
2. **Network Issues**: Check MQTT broker connectivity
3. **Message Size**: Large payloads may be dropped by broker
4. **Broker Limits**: Check MQTT broker client limits and message rate limits

The topic structure is designed to be simple, predictable, and scalable for the Jeeves smart home system.