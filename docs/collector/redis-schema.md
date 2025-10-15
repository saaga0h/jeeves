# Collector Agent - Redis Storage Schema

This document explains how the Collector Agent organizes sensor data in Redis, helping you understand the storage patterns and how to query the data.

## Storage Strategy Overview

The Collector Agent uses different Redis data structures based on sensor type to optimize for common access patterns:

```
Motion Sensors    → Sorted Sets + Metadata (time-based queries)
Environmental     → Sorted Sets (time-series data)  
Other Sensors     → Lists + Metadata (recent data access)
```

All data has a **24-hour TTL** to prevent unbounded growth.

## Motion Sensor Storage

**Why Special Treatment**: Motion detection is critical for occupancy analysis and requires fast "time since last motion" queries.

### Data Storage: `sensor:motion:{location}`
- **Type**: Sorted Set (ZSET)
- **Score**: Unix timestamp in milliseconds  
- **Purpose**: Time-ordered motion events for range queries
- **TTL**: 24 hours
- **Cleanup**: Automatically removes entries older than 24 hours

**Value Structure**:
```json
{
  "timestamp": "2025-01-01T12:00:00.000Z",
  "state": "on",
  "entity_id": "binary_sensor.motion_study",
  "sequence": 42,
  "collected_at": 1704110400000
}
```

**Example Key**: `sensor:motion:study`

### Quick Access Metadata: `meta:motion:{location}`
- **Type**: Hash
- **Purpose**: Fast access to latest motion time
- **TTL**: 24 hours

**Hash Fields**:
- `lastMotionTime`: Unix timestamp in milliseconds (string)
- **Updated Only When**: Motion state = "on" (motion detected)

**Example Key**: `meta:motion:study`
**Example Value**: `lastMotionTime: "1704110400123"`

### Common Motion Queries

**Get recent motion events**:
```redis
ZREVRANGEBYSCORE sensor:motion:study +inf -inf LIMIT 0 10
```

**Get motion in last 5 minutes**:
```redis
ZRANGEBYSCORE sensor:motion:study (now-300000) +inf
```

**Get time since last motion**:
```redis
HGET meta:motion:study lastMotionTime
```

## Environmental Sensor Storage

**Why Consolidated**: Temperature and illuminance are often queried together for environmental context.

### Data Storage: `sensor:environmental:{location}`
- **Type**: Sorted Set (ZSET)
- **Score**: Unix timestamp in milliseconds
- **Purpose**: Time-series environmental data
- **TTL**: 24 hours
- **Cleanup**: Automatically removes entries older than 24 hours

**Value Structure (Temperature)**:
```json
{
  "timestamp": "2025-01-01T12:00:00.000Z",
  "collected_at": 1704110400000,
  "temperature": 22.5,
  "temperature_unit": "°C"
}
```

**Value Structure (Illuminance)**:
```json
{
  "timestamp": "2025-01-01T12:00:00.456Z",
  "collected_at": 1704110400456,
  "illuminance": 450.0,
  "illuminance_unit": "lux"
}
```

**Example Key**: `sensor:environmental:living_room`

**Note**: Each sensor reading creates a separate entry. The "environmental" key consolidates multiple sensor types for the same location.

### Common Environmental Queries

**Get recent temperature readings**:
```redis
ZREVRANGEBYSCORE sensor:environmental:living_room +inf -inf LIMIT 0 5
```

**Get environmental data from last hour**:
```redis
ZRANGEBYSCORE sensor:environmental:living_room (now-3600000) +inf
```

## Generic Sensor Storage

**Why Generic**: Unknown sensor types get flexible storage that can handle any data structure.

### Data Storage: `sensor:{sensor_type}:{location}`
- **Type**: List (newest entries at head)
- **Purpose**: Recent data buffer for unknown sensor types
- **Max Length**: 1000 entries (configurable via `MAX_SENSOR_HISTORY`)
- **TTL**: 24 hours

**Value Structure**:
```json
{
  "data": {
    "value": 1013.25,
    "unit": "hPa", 
    "trend": "stable"
  },
  "original_topic": "automation/raw/pressure/kitchen",
  "timestamp": "2025-01-01T12:00:00.000Z",
  "collected_at": 1704110400000
}
```

**Example Key**: `sensor:pressure:kitchen`

### Metadata Storage: `meta:{sensor_type}:{location}`
- **Type**: Hash
- **Purpose**: Sensor discovery and metadata
- **TTL**: 24 hours

**Hash Fields**:
- `last_update`: Unix timestamp in milliseconds
- `sensor_type`: String (sensor type identifier)
- `location`: String (location identifier)

**Example Key**: `meta:pressure:kitchen`
**Example Values**:
- `last_update`: "1704110400000"
- `sensor_type`: "pressure"
- `location`: "kitchen"

### Common Generic Sensor Queries

**Get most recent reading**:
```redis
LINDEX sensor:pressure:kitchen 0
```

**Get last 10 readings**:
```redis
LRANGE sensor:pressure:kitchen 0 9
```

**Get all sensor types in location**:
```redis
KEYS meta:*:kitchen
```

## Key Naming Patterns

### Motion Sensors
```
sensor:motion:{location}    # Motion event data
meta:motion:{location}      # Quick access metadata
```

### Environmental Sensors  
```
sensor:environmental:{location}    # Temperature + illuminance data
```

### Generic Sensors
```
sensor:{sensor_type}:{location}    # Generic sensor data
meta:{sensor_type}:{location}      # Generic sensor metadata
```

### Real Examples
```
sensor:motion:study
meta:motion:study
sensor:environmental:living_room
sensor:pressure:kitchen
meta:pressure:kitchen
sensor:humidity:bathroom
meta:humidity:bathroom
```

## TTL and Cleanup Strategy

### Automatic Expiration
- **All keys**: 86400 seconds (24 hours)
- **Applied**: After every write operation
- **Purpose**: Prevent unbounded memory growth

### Active Cleanup During Storage

**Motion & Environmental (Sorted Sets)**:
```redis
ZREMRANGEBYSCORE sensor:motion:study -inf (now-86400000)
```
- Removes entries older than 24 hours
- Executed on every new data point

**Generic Sensors (Lists)**:
```redis
LTRIM sensor:pressure:kitchen 0 999
```
- Keeps only newest 1000 entries (configurable)
- Executed after every new data point

### Why Both TTL and Active Cleanup?

1. **Active cleanup**: Prevents memory growth during operation
2. **TTL**: Safety net for keys that don't receive updates
3. **Redundant safety**: Ensures data doesn't accumulate indefinitely

## Storage Decision Logic

The Collector Agent chooses storage strategy based on sensor type:

```go
if sensorType == "motion" {
    // Use sorted set + metadata for time-based queries
    store_in_sorted_set("sensor:motion:" + location)
    update_metadata("meta:motion:" + location)
} else if sensorType == "temperature" || sensorType == "illuminance" {
    // Use consolidated environmental sorted set
    store_in_sorted_set("sensor:environmental:" + location)
} else {
    // Use generic list + metadata storage
    store_in_list("sensor:" + sensorType + ":" + location)
    update_metadata("meta:" + sensorType + ":" + location)
}
```

## Querying Examples by Use Case

### Occupancy Agent Needs

**"Has there been motion in study in last 10 minutes?"**
```redis
ZRANGEBYSCORE sensor:motion:study (now-600000) +inf
```

**"When was the last motion in study?"**
```redis
HGET meta:motion:study lastMotionTime
```

### Light Agent Needs

**"What's the current temperature in living room?"**
```redis
ZREVRANGEBYSCORE sensor:environmental:living_room +inf -inf LIMIT 0 1
```

**"What was the illuminance trend in last hour?"**
```redis
ZRANGEBYSCORE sensor:environmental:living_room (now-3600000) +inf
```

### Behavior Agent Needs

**"All motion events in study today"**
```redis
ZRANGEBYSCORE sensor:motion:study (start-of-day) +inf
```

**"Environmental conditions during episode"**
```redis
ZRANGEBYSCORE sensor:environmental:living_room (episode-start) (episode-end)
```

## Memory Usage Estimation

### Per Location Motion Storage
- **Events per day**: ~100 (1 every 15 minutes average)
- **Entry size**: ~150 bytes JSON
- **Daily total**: ~15 KB per location
- **With metadata**: ~15.1 KB per location

### Per Location Environmental Storage  
- **Readings per day**: ~144 (1 every 10 minutes average)
- **Entry size**: ~120 bytes JSON
- **Daily total**: ~17 KB per location

### Per Sensor Generic Storage
- **Max entries**: 1000 (configurable)
- **Entry size**: ~200 bytes JSON average
- **Max total**: ~200 KB per sensor

### Total Estimate (10 locations)
- **Motion**: 10 × 15 KB = 150 KB
- **Environmental**: 10 × 17 KB = 170 KB  
- **Generic**: 5 sensors × 200 KB = 1 MB
- **Grand Total**: ~1.3 MB for typical home

## Troubleshooting Storage Issues

### "Redis memory growing without bound"

1. **Check TTL settings**:
   ```redis
   TTL sensor:motion:study
   # Should return ~86400 or less
   ```

2. **Check active cleanup**:
   ```redis
   ZCARD sensor:motion:study
   # Should be reasonable size, not thousands
   ```

3. **Check for sensors with excessive data**:
   ```redis
   KEYS sensor:*
   # Look for unexpected sensor types
   ```

### "Missing sensor data"

1. **Check key existence**:
   ```redis
   EXISTS sensor:motion:study
   # Should return 1 if data exists
   ```

2. **Check TTL expiration**:
   ```redis
   TTL sensor:motion:study
   # Should not return -2 (expired)
   ```

3. **Check Collector Agent logs**:
   - Look for Redis connection errors
   - Verify sensor data processing

### "Slow queries"

1. **Use appropriate data structure**:
   - Sorted sets for time-range queries
   - Lists for recent data access
   - Hashes for metadata lookup

2. **Limit query results**:
   ```redis
   ZREVRANGEBYSCORE sensor:motion:study +inf -inf LIMIT 0 10
   # Add LIMIT to prevent large result sets
   ```

3. **Consider Redis memory and CPU**:
   - Monitor Redis performance metrics
   - Ensure sufficient memory for working set

The Redis schema is designed for efficient access patterns while maintaining bounded memory usage through automatic cleanup.