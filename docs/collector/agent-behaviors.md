# Collector Agent - How It Works

The **Collector Agent** is a core component of the Jeeves platform that acts as a data gateway between raw sensor inputs and the rest of the system. It receives sensor data via MQTT, stores it in Redis for fast access, and notifies other agents when new data is available.

## What the Collector Agent Does

## What the Collector Agent Does

The Collector Agent serves as the **central data ingestion hub** for all sensor data in the Jeeves smart home system. It:

1. **Collects** raw sensor data from MQTT topics (motion, temperature, illuminance, etc.)
2. **Processes** and validates incoming sensor messages
3. **Stores** data efficiently in Redis with automatic cleanup
4. **Forwards** processed data to VictoriaMetrics for long-term analytics (optional)
5. **Notifies** other agents when new sensor data is available via MQTT triggers

### Key Responsibilities

- **Data Gateway**: First point of contact for all sensor data
- **Data Storage**: Maintains 24-hour rolling buffer of sensor readings in Redis
- **Event Distribution**: Publishes trigger messages for downstream processing
- **Data Validation**: Handles malformed messages gracefully
- **Performance**: Optimized for high-throughput sensor data (1-2 messages/second typical)

---

## How the Agent Starts Up

### Initialization Process
### Initialization Process

1. **Configuration Loading**
   - MQTT broker connection details
   - Redis connection settings
   - VictoriaMetrics integration (optional)
   - Sensor topic subscriptions (default: `automation/raw/+/+`)
   - Health check service port (default: 5012)

2. **Service Connections**
   - Establishes Redis connection with automatic reconnection
   - Connects to MQTT broker with credential support
   - Verifies both connections are healthy before proceeding

3. **Topic Subscriptions**
   - Subscribes to configured sensor topics (supports wildcards)
   - Default subscription: `automation/raw/+/+` (all sensor types and locations)
   - Continues with other subscriptions if one fails

4. **Health Check Server**
   - Starts HTTP server for health monitoring
   - Simple endpoint: `GET /health` returns status and timestamp
   - Used by container orchestration for health checks

5. **Ready State**
   - Logs startup completion with configuration summary
   - Ready to receive and process sensor messages

---

## How Sensor Data Flows Through the System

### Data Processing Pipeline

### Data Processing Pipeline

When a sensor publishes data (e.g., motion detected in study), here's what happens:

```
1. Sensor → MQTT Broker
   Topic: automation/raw/motion/study
   Data: {"data": {"state": "on", "entity_id": "motion.study"}}

2. Collector Agent Receives Message
   ↓ Validates topic format and JSON payload
   ↓ Extracts sensor type (motion) and location (study)

3. Stores in Redis
   ↓ Motion: Sorted set with timestamp-based ordering
   ↓ Environmental: Consolidated sorted set per location  
   ↓ Other sensors: Simple list with newest-first ordering
   ↓ Applies 24-hour TTL and cleanup old data

4. Optional: Forwards to VictoriaMetrics
   ↓ Extracts numeric fields for long-term metrics
   ↓ Non-blocking operation (continues if VictoriaMetrics unavailable)

5. Publishes Trigger Message
   Topic: automation/sensor/motion/study
   Data: {original data + "stored_at" timestamp}
   ↓ Notifies other agents (occupancy, light, etc.) that new data is available
```

### Message Format Details

### Message Format Details

**Input Topics**: `automation/raw/{sensor_type}/{location}`
- `sensor_type`: motion, temperature, illuminance, etc.
- `location`: study, living_room, bedroom, etc.

**Output Topics**: `automation/sensor/{sensor_type}/{location}`
- Same structure, but "raw" becomes "sensor"
- Indicates data has been processed and stored

---

## Storage Strategy by Sensor Type

The Collector Agent uses different Redis data structures optimized for each sensor type's access patterns:

### Motion Sensors
**Why Special Handling**: Motion detection is critical for occupancy detection and requires fast "time since last motion" queries.

**Storage Method**: 
- **Main Data**: Sorted set `sensor:motion:{location}` with timestamp scores
- **Quick Access**: Hash `meta:motion:{location}` tracks last motion time
- **Cleanup**: Automatically removes entries older than 24 hours
- **TTL**: 24 hours on all keys

**Access Pattern**: Fast queries for "motion in last X minutes" and "time since last motion"

### Environmental Sensors (Temperature + Illuminance)
**Why Consolidated**: Often queried together for environmental context.

**Storage Method**:
- **Main Data**: Sorted set `sensor:environmental:{location}` 
- **Data Format**: Each entry contains available environmental readings
- **Cleanup**: Automatically removes entries older than 24 hours
- **TTL**: 24 hours

**Access Pattern**: Time-series queries for environmental trends

### Other Sensor Types
**Why Generic**: Unknown sensor types get flexible storage.

**Storage Method**:
- **Main Data**: List `sensor:{sensor_type}:{location}` (newest first)
- **Metadata**: Hash `meta:{sensor_type}:{location}` for discovery
- **Cleanup**: Keeps only newest 1000 entries per sensor
- **TTL**: 24 hours

**Access Pattern**: Recent data access and sensor type discovery

---

## What Other Agents Receive

When the Collector Agent processes sensor data, it notifies other agents via trigger messages:

### Illuminance Agent
- **Listens for**: `automation/sensor/illuminance/+`
- **Uses data for**: Determining ambient light levels for automated lighting
- **Reads from Redis**: Time-series illuminance data for trend analysis

### Occupancy Agent  
- **Listens for**: `automation/sensor/motion/+`
- **Uses data for**: Detecting room occupancy patterns
- **Reads from Redis**: Motion event history and "last motion time" metadata

### Light Agent
- **Listens for**: `automation/sensor/+/+` (all sensor types)
- **Uses data for**: Environmental context for lighting decisions
- **Reads from Redis**: Recent environmental readings for adaptive lighting

### Behavior Agent
- **Listens for**: `automation/sensor/+/+` (all sensor types) 
- **Uses data for**: Tracking behavioral patterns and episodes
- **Reads from Redis**: Historical sensor data for pattern analysis

---

## Monitoring and Health

### Health Check Endpoint
```
GET /health
→ {"status": "ok", "timestamp": "2025-01-01T12:00:00Z"}
```

**Purpose**: Container orchestration health monitoring
**Design**: Lightweight and fast (no external service checks)

### Key Metrics to Monitor

**Performance Indicators**:
- Message processing rate (should handle 1-2 msg/sec easily)
- Redis memory usage (bounded by 24-hour TTL)
- MQTT connection stability

**Error Indicators**:
- Invalid message format warnings
- Redis connection failures
- MQTT subscription failures
- VictoriaMetrics forwarding errors (non-critical)

**Success Indicators**:
- All sensor data stored in Redis with TTL
- Trigger messages published successfully
- No memory leaks over 24+ hours
- Automatic reconnection after network issues

---

## Configuration Guide

### Essential Settings
```bash
# Required: MQTT and Redis connections
MQTT_BROKER=mosquitto.local
REDIS_HOST=redis.local

# Optional: Custom sensor topic patterns  
SENSOR_TOPICS=automation/raw/+/+,automation/raw/motion/+

# Optional: VictoriaMetrics integration
ENABLE_VICTORIA_METRICS=true
VICTORIA_METRICS_URL=http://victoria-metrics:8428
```

### Production Considerations

**Performance Tuning**:
- Single Redis connection handles expected load
- MQTT wildcards reduce subscription overhead
- 24-hour TTL prevents unbounded memory growth

**Reliability Features**:
- Automatic MQTT/Redis reconnection
- Graceful handling of malformed messages
- Non-blocking VictoriaMetrics forwarding
- Container health checks

**Monitoring**:
- Structured logging for debugging
- Buffer size logging for capacity planning
- Error logging for alerting

---

## Troubleshooting Common Issues

### "No sensor data appearing in Redis"
1. Check MQTT subscriptions in logs
2. Verify sensor topic format: `automation/raw/{type}/{location}`
3. Confirm JSON payload format
4. Check Redis connection in logs

### "High memory usage in Redis"
1. Verify TTL is being set (should see 86400 seconds)
2. Check for sensors producing excessive data
3. Monitor buffer sizes in logs
4. Consider reducing MAX_SENSOR_HISTORY for generic sensors

### "Missing trigger messages"
1. Check MQTT publish errors in logs
2. Verify Redis storage succeeded first
3. Confirm downstream agents are subscribed to `automation/sensor/+/+`

### "VictoriaMetrics errors"
1. Check if ENABLE_VICTORIA_METRICS is needed
2. Verify VICTORIA_METRICS_URL is accessible
3. Note: VictoriaMetrics errors don't stop sensor processing

---

## Data Flow Summary

The Collector Agent sits at the center of sensor data flow:

```
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│   Sensors   │───▶│  Collector  │───▶│ Other Agents│
│             │    │   Agent     │    │             │
└─────────────┘    └─────────────┘    └─────────────┘
                          │
                          ▼
                   ┌─────────────┐
                   │    Redis    │
                   │ (24h buffer)│
                   └─────────────┘
                          │
                          ▼
                   ┌─────────────┐
                   │VictoriaMetri│
                   │cs (optional)│
                   └─────────────┘
```

**Input**: Raw sensor MQTT messages  
**Output**: Stored data + trigger notifications  
**Storage**: Redis with automatic cleanup  
**Metrics**: VictoriaMetrics for long-term analysis  
**Notifications**: MQTT triggers for real-time processing