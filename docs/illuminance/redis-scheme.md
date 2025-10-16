# Redis Storage Guide - Illuminance Agent

This guide explains how the Illuminance Agent uses Redis to analyze lighting patterns and what data structure you need to maintain for proper operation.

## How the Agent Uses Redis

The Illuminance Agent is a **read-only** consumer of sensor data. It doesn't store data itself - instead, it reads historical sensor readings that were previously stored by the Collector Agent.

**The Agent's Role**:
- 📖 **Reads** historical illuminance data from Redis
- 🔍 **Analyzes** lighting trends and patterns
- 📊 **Publishes** context and insights via MQTT
- ❌ **Never writes** to Redis (maintains clean separation)

---

## Storage Structure You Need

### Sensor Data Storage: `sensor:environmental:{location}`

Each room's sensor data is stored as a **sorted set** where:
- **Key format**: `sensor:environmental:living_room`, `sensor:environmental:bedroom`, etc.
- **Score**: Timestamp in Unix milliseconds (for time-based queries)
- **Value**: JSON with sensor readings

**Example data structure**:
```redis
ZADD sensor:environmental:living_room 1704110400123 '{"timestamp":"2025-01-01T12:00:00.123Z","illuminance":450.0,"illuminance_unit":"lux"}'
ZADD sensor:environmental:living_room 1704110460456 '{"timestamp":"2025-01-01T12:01:00.456Z","illuminance":455.2,"illuminance_unit":"lux"}'
```

### Required JSON Fields

For the Illuminance Agent to work properly, each sensor reading must include:

```json
{
  "timestamp": "2025-01-01T12:00:00.123Z",  // ISO 8601 format
  "illuminance": 450.0,                     // Required: lux value
  "illuminance_unit": "lux"                 // Optional: assumed "lux"
}
```

**Optional but helpful fields**:
- `source`: Identifies which sensor/system provided the data
- `collected_at`: Unix timestamp (can use as fallback for sorting)

---

## How Analysis Works

### Time Windows the Agent Queries

The agent analyzes multiple time periods to understand lighting patterns:

**Recent Activity (Last 5 Minutes)**:
- Purpose: Detect immediate changes
- Query: `ZRANGEBYSCORE sensor:environmental:living_room {5min_ago} {now}`
- Used for: Trend detection (brightening/dimming)

**Short-term Patterns (Last 30 Minutes)**:  
- Purpose: Understand current lighting session
- Query: `ZRANGEBYSCORE sensor:environmental:living_room {30min_ago} {now}`
- Used for: Stability analysis, averages

**Historical Context (Last Hour)**:
- Purpose: Determine if enough data exists for analysis
- Query: `ZRANGEBYSCORE sensor:environmental:living_room {1hour_ago} {now}`
- Used for: Data sufficiency check, long-term trends

### Data Requirements

**Minimum for analysis**: 3 readings in the last hour
- **✅ Sufficient data**: Agent performs full analysis
- **❌ Insufficient data**: Falls back to daylight calculations based on time/location

**Configurable parameters**:
- `MAX_DATA_AGE_HOURS`: How far back to look (default: 1 hour)
- `MIN_READINGS_REQUIRED`: Minimum readings needed (default: 3)

---

## What the Agent Calculates

From your stored sensor data, the agent computes:

### Current State Analysis
```go
type CurrentState struct {
    CurrentLux   int    `json:"current_lux"`   // Latest reading
    CurrentLabel string `json:"current_label"` // Semantic category
    Trend        string `json:"trend"`         // Direction of change
    Stability    string `json:"stability"`     // Variability measure
}

// Example values:
// CurrentState{
//     CurrentLux:   450,
//     CurrentLabel: "moderate",
//     Trend:        "stable",
//     Stability:    "stable",
// }
```

### Statistical Analysis
```go
type Statistics struct {
    Avg2Min   int `json:"avg_2min"`   // 2-minute average
    Avg10Min  int `json:"avg_10min"`  // 10-minute average
    Min10Min  int `json:"min_10min"`  // 10-minute minimum
    Max10Min  int `json:"max_10min"`  // 10-minute maximum
}

// Example values:
// Statistics{
//     Avg2Min:  448,
//     Avg10Min: 445,
//     Min10Min: 420,
//     Max10Min: 470,
// }
```

### Contextual Analysis
```go
type ContextualAnalysis struct {
    RelativeToTypical string   `json:"relative_to_typical"` // Compared to expected
    LikelySources     []string `json:"likely_sources"`      // Inferred light sources
    TimeOfDay         string   `json:"time_of_day"`         // Time-based context
    IsDaytime         bool     `json:"is_daytime"`          // Daylight availability
}

// Example values:
// ContextualAnalysis{
//     RelativeToTypical: "above_typical",
//     LikelySources:     []string{"natural", "mixed"},
//     TimeOfDay:         "afternoon",
//     IsDaytime:         true,
// }
```

---

## Setting Up Data Storage

### If Using the Collector Agent

The Collector Agent automatically handles storage. Just ensure:

1. **Sensor data flows** to the collector via MQTT
2. **Collector stores** readings in the correct Redis format
3. **Collector publishes** triggers to wake up the Illuminance Agent

### If Implementing Custom Storage

Here's how to store data compatible with the Illuminance Agent:

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "time"
    
    "github.com/go-redis/redis/v8"
)

var ctx = context.Background()

type SensorReading struct {
    Timestamp       string  `json:"timestamp"`
    Illuminance     float64 `json:"illuminance"`
    IlluminanceUnit string  `json:"illuminance_unit"`
    Source          string  `json:"source"`
}

func storeIlluminanceReading(redisClient *redis.Client, location string, luxValue float64) error {
    timestampMs := time.Now().UnixMilli()
    
    readingData := SensorReading{
        Timestamp:       time.Now().UTC().Format(time.RFC3339),
        Illuminance:     luxValue,
        IlluminanceUnit: "lux",
        Source:          "your_sensor_system",
    }
    
    jsonData, err := json.Marshal(readingData)
    if err != nil {
        return fmt.Errorf("failed to marshal reading data: %w", err)
    }
    
    key := fmt.Sprintf("sensor:environmental:%s", location)
    
    // Store in Redis sorted set
    err = redisClient.ZAdd(ctx, key, &redis.Z{
        Score:  float64(timestampMs),
        Member: string(jsonData),
    }).Err()
    if err != nil {
        return fmt.Errorf("failed to store reading: %w", err)
    }
    
    // Optional: Set expiration to clean up old data
    err = redisClient.Expire(ctx, key, 24*time.Hour).Err()
    if err != nil {
        return fmt.Errorf("failed to set expiration: %w", err)
    }
    
    return nil
}

// Example usage
func main() {
    rdb := redis.NewClient(&redis.Options{
        Addr: "localhost:6379",
    })
    
    err := storeIlluminanceReading(rdb, "living_room", 425.5)
    if err != nil {
        fmt.Printf("Error storing reading: %v\n", err)
        return
    }
    
    err = storeIlluminanceReading(rdb, "bedroom", 15.2)
    if err != nil {
        fmt.Printf("Error storing reading: %v\n", err)
        return
    }
    
    fmt.Println("Readings stored successfully")
}
```

### Database Queries the Agent Performs

```bash
# Get recent data for analysis
ZRANGEBYSCORE sensor:environmental:living_room 1704106800000 1704110400000 WITHSCORES

# Get latest reading
ZRANGE sensor:environmental:living_room -1 -1 WITHSCORES  

# Find all rooms with data
KEYS sensor:environmental:*
```

---

## Data Lifecycle Management

### Automatic Cleanup

**TTL (Time To Live)**: Redis keys should have expiration
```bash
# Set 24-hour expiration on sensor data
EXPIRE sensor:environmental:living_room 86400
```

**Why TTL matters**:
- Prevents Redis from growing indefinitely
- Maintains performance with historical data
- Usually handled automatically by Collector Agent

### Storage Efficiency

**Optimal frequency**: Store readings every 30-60 seconds
- **Too frequent**: Wastes storage, minimal analysis benefit  
- **Too sparse**: Misses important lighting transitions

**Key naming**: Use consistent location identifiers
- ✅ `living_room`, `bedroom`, `study` 
- ❌ `living room` (spaces), `LivingRoom` (inconsistent case)

---

## Troubleshooting Storage Issues

### Agent Can't Find Data

**Symptoms**: 
- Health check shows 0 locations
- No context messages published
- Agent logs show "insufficient data"

**Check Redis directly**:
```bash
# List all environmental keys
redis-cli KEYS "sensor:environmental:*"

# Check specific location
redis-cli ZRANGE sensor:environmental:living_room 0 -1 WITHSCORES

# Verify recent data exists
redis-cli ZRANGEBYSCORE sensor:environmental:living_room -inf +inf WITHSCORES
```

**Common fixes**:
- Verify key naming matches pattern: `sensor:environmental:{location}`
- Ensure timestamps are in Unix milliseconds
- Check JSON format includes `illuminance` field

### Data Too Old

**Symptoms**: 
- Agent falls back to daylight calculations
- Context shows theoretical outdoor lux instead of real readings

**Check data age**:
```bash
# Get latest reading timestamp  
redis-cli ZRANGE sensor:environmental:living_room -1 -1 WITHSCORES

# Compare to current time
date +%s000  # Current time in milliseconds
```

**Fix**: Ensure sensor data is being written regularly (every 30-60 seconds)

### Mixed Data in Keys

**Symptoms**: 
- Inconsistent analysis results
- Some readings ignored during analysis

**Issue**: Environmental keys may contain both illuminance and temperature data

**Example of mixed data**:
```json
// Good - has illuminance
{"timestamp": "...", "illuminance": 450, "temperature": 22}

// Skipped - no illuminance  
{"timestamp": "...", "temperature": 23}
```

**Solution**: This is normal behavior. Agent filters for readings with `illuminance` field.

---

## Redis Performance Tips

### Optimal Queries

**Efficient time windows**:
```bash
# Get last hour efficiently
ZRANGEBYSCORE sensor:environmental:living_room 1704106800000 +inf WITHSCORES
```

**Avoid expensive operations**:
- Don't use `ZRANGE` with large ranges
- Use `WITHSCORES` to get timestamps
- Limit time windows to what you need

### Memory Management

**Monitor key sizes**:
```bash
# Check memory usage of a key
redis-cli MEMORY USAGE sensor:environmental:living_room

# Count entries in a key
redis-cli ZCARD sensor:environmental:living_room
```

**Expected sizes**:
- **Per reading**: ~150-200 bytes JSON
- **Per day**: ~10,000-20,000 readings (30-60 second intervals)  
- **Per location**: ~2-4 MB per day with TTL cleanup

This storage structure enables the Illuminance Agent to provide rich temporal analysis while maintaining clean separation between data collection and analysis responsibilities.