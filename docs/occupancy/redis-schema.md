# Storage Guide - Occupancy Agent

The occupancy agent uses Redis to store motion sensor data, occupancy state, and prediction history. This guide explains how data is organized and accessed for maintenance and debugging.

## Data Sources (Read from Collector)

### Motion Sensor Events

**Key Pattern**: `sensor:motion:{location}`  
**Type**: Sorted Set (timestamped events)  
**Written By**: Collector agent  
**Read By**: Occupancy agent  

**Purpose**: Historical motion events for pattern analysis

**Example Data**:
```
Key: sensor:motion:study
Score: 1704110400000 (timestamp)
Value: {"timestamp": "2024-01-01T12:00:00.000Z", "state": "on", "entity_id": "binary_sensor.motion_study"}
```

**Usage**: The agent queries multiple time windows (2min, 8min, 20min, 60min) to understand motion patterns.

### Motion Metadata

**Key Pattern**: `meta:motion:{location}`  
**Type**: Hash  
**Written By**: Collector agent  
**Read By**: Occupancy agent  

**Purpose**: Quick access to last motion time without scanning full event history

**Fields**:
- `lastMotionTime`: Unix timestamp in milliseconds

**Example**:
```
Key: meta:motion:study
Field: lastMotionTime
Value: "1704110400000"
```

## Data Written by Occupancy Agent

### Room State Information

**Key Pattern**: `temporal:{location}`  
**Type**: Hash  
**Purpose**: Current occupancy status and timing metadata  
**TTL**: No expiration (persistent state)  

**Fields**:
- `currentOccupancy`: "true" or "false" (string boolean)
- `lastStateChange`: Unix timestamp when occupancy last changed
- `lastAnalysis`: ISO 8601 timestamp of last analysis

**Example**:
```
Key: temporal:study
Fields:
  currentOccupancy: "true"
  lastStateChange: "1704110400000"
  lastAnalysis: "2024-01-01T12:05:00.000Z"
```

**Usage**: Tracks when rooms were last analyzed (rate limiting) and when state changes occurred (time gates).

### Prediction History

**Key Pattern**: `predictions:{location}`  
**Type**: List (newest entries first)  
**Purpose**: Track prediction history for stability analysis  
**Max Length**: 10 entries (automatically trimmed)  

**Entry Format** (JSON string):
```json
{
  "timestamp": "2024-01-01T12:00:00.000Z",
  "occupied": true,
  "confidence": 0.85,
  "reasoning": "Multiple recent motions indicate person actively present",
  "stabilizationApplied": false,
  "actualOutcome": undefined
}
```

**Usage**: The Vonich-Hakim stabilization algorithm analyzes this history to detect oscillation patterns and adjust confidence requirements.

## Common Data Operations

### Go Code Examples

**Query Motion Data**:
```go
package main

import (
    "context"
    "fmt"
    "time"
    
    "github.com/go-redis/redis/v8"
)

func getMotionCountInWindow(rdb *redis.Client, location string, startTime, endTime time.Time) (int64, error) {
    ctx := context.Background()
    key := fmt.Sprintf("sensor:motion:%s", location)
    
    // Query motion events in time window
    count, err := rdb.ZCount(ctx, key, 
        fmt.Sprintf("%d", startTime.UnixMilli()),
        fmt.Sprintf("%d", endTime.UnixMilli())).Result()
    
    if err != nil {
        return 0, fmt.Errorf("failed to count motion events: %w", err)
    }
    
    return count, nil
}

func getLastMotionTime(rdb *redis.Client, location string) (time.Time, error) {
    ctx := context.Background()
    key := fmt.Sprintf("meta:motion:%s", location)
    
    timestampStr, err := rdb.HGet(ctx, key, "lastMotionTime").Result()
    if err == redis.Nil {
        return time.Time{}, nil // No motion data
    }
    if err != nil {
        return time.Time{}, fmt.Errorf("failed to get last motion time: %w", err)
    }
    
    timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
    if err != nil {
        return time.Time{}, fmt.Errorf("invalid timestamp format: %w", err)
    }
    
    return time.UnixMilli(timestamp), nil
}
```

**Read Occupancy State**:
```go
type OccupancyState struct {
    Occupied        *bool
    LastStateChange *time.Time
    LastAnalysis    *time.Time
}

func getCurrentOccupancyState(rdb *redis.Client, location string) (*OccupancyState, error) {
    ctx := context.Background()
    key := fmt.Sprintf("temporal:%s", location)
    
    // Get all fields at once
    fields, err := rdb.HGetAll(ctx, key).Result()
    if err != nil {
        return nil, fmt.Errorf("failed to get occupancy state: %w", err)
    }
    
    state := &OccupancyState{}
    
    // Parse currentOccupancy
    if occupiedStr, exists := fields["currentOccupancy"]; exists {
        occupied := occupiedStr == "true"
        state.Occupied = &occupied
    }
    
    // Parse lastStateChange
    if changeStr, exists := fields["lastStateChange"]; exists {
        if timestamp, err := strconv.ParseInt(changeStr, 10, 64); err == nil {
            changeTime := time.UnixMilli(timestamp)
            state.LastStateChange = &changeTime
        }
    }
    
    // Parse lastAnalysis
    if analysisStr, exists := fields["lastAnalysis"]; exists {
        if analysisTime, err := time.Parse(time.RFC3339, analysisStr); err == nil {
            state.LastAnalysis = &analysisTime
        }
    }
    
    return state, nil
}
```

**Update Occupancy State**:
```go
func updateOccupancy(rdb *redis.Client, location string, occupied bool) error {
    ctx := context.Background()
    key := fmt.Sprintf("temporal:%s", location)
    
    // Update both occupancy and state change time atomically
    pipe := rdb.Pipeline()
    pipe.HSet(ctx, key, "currentOccupancy", fmt.Sprintf("%t", occupied))
    pipe.HSet(ctx, key, "lastStateChange", fmt.Sprintf("%d", time.Now().UnixMilli()))
    
    _, err := pipe.Exec(ctx)
    if err != nil {
        return fmt.Errorf("failed to update occupancy: %w", err)
    }
    
    return nil
}
```

**Work with Prediction History**:
```go
type PredictionRecord struct {
    Timestamp            time.Time `json:"timestamp"`
    Occupied             bool      `json:"occupied"`
    Confidence           float64   `json:"confidence"`
    Reasoning            string    `json:"reasoning"`
    StabilizationApplied bool      `json:"stabilizationApplied"`
    ActualOutcome        *bool     `json:"actualOutcome,omitempty"`
}

func addPredictionHistory(rdb *redis.Client, location string, prediction PredictionRecord) error {
    ctx := context.Background()
    key := fmt.Sprintf("predictions:%s", location)
    
    // Serialize prediction to JSON
    predictionJSON, err := json.Marshal(prediction)
    if err != nil {
        return fmt.Errorf("failed to marshal prediction: %w", err)
    }
    
    // Add to list and trim to 10 entries
    pipe := rdb.Pipeline()
    pipe.LPush(ctx, key, predictionJSON)
    pipe.LTrim(ctx, key, 0, 9) // Keep only last 10
    
    _, err = pipe.Exec(ctx)
    if err != nil {
        return fmt.Errorf("failed to add prediction history: %w", err)
    }
    
    return nil
}

func getPredictionHistory(rdb *redis.Client, location string) ([]PredictionRecord, error) {
    ctx := context.Background()
    key := fmt.Sprintf("predictions:%s", location)
    
    // Get all predictions (newest first)
    predictions, err := rdb.LRange(ctx, key, 0, -1).Result()
    if err != nil {
        return nil, fmt.Errorf("failed to get prediction history: %w", err)
    }
    
    var records []PredictionRecord
    for i := len(predictions) - 1; i >= 0; i-- { // Reverse to oldest first
        var record PredictionRecord
        if err := json.Unmarshal([]byte(predictions[i]), &record); err != nil {
            // Skip malformed entries
            continue
        }
        records = append(records, record)
    }
    
    return records, nil
}
```

## Time Window Queries

The occupancy agent analyzes motion in exclusive time windows for pattern recognition:

**Efficient Window Analysis**:
```go
func getExclusiveWindowCounts(rdb *redis.Client, location string, referenceTime time.Time) (map[string]int64, error) {
    // Define time boundaries
    now := referenceTime
    time2min := now.Add(-2 * time.Minute)
    time8min := now.Add(-8 * time.Minute)
    time20min := now.Add(-20 * time.Minute)
    time60min := now.Add(-60 * time.Minute)
    
    // Query cumulative counts (parallel operations)
    ctx := context.Background()
    key := fmt.Sprintf("sensor:motion:%s", location)
    
    pipe := rdb.Pipeline()
    count2min := pipe.ZCount(ctx, key, fmt.Sprintf("%d", time2min.UnixMilli()), fmt.Sprintf("%d", now.UnixMilli()))
    count8min := pipe.ZCount(ctx, key, fmt.Sprintf("%d", time8min.UnixMilli()), fmt.Sprintf("%d", now.UnixMilli()))
    count20min := pipe.ZCount(ctx, key, fmt.Sprintf("%d", time20min.UnixMilli()), fmt.Sprintf("%d", now.UnixMilli()))
    count60min := pipe.ZCount(ctx, key, fmt.Sprintf("%d", time60min.UnixMilli()), fmt.Sprintf("%d", now.UnixMilli()))
    
    results, err := pipe.Exec(ctx)
    if err != nil {
        return nil, fmt.Errorf("failed to query motion windows: %w", err)
    }
    
    // Extract counts
    c2 := count2min.Val()
    c8 := count8min.Val()
    c20 := count20min.Val()
    c60 := count60min.Val()
    
    // Calculate exclusive windows
    return map[string]int64{
        "last_2min":  c2,           // 0-2 minutes
        "last_8min":  c8 - c2,      // 2-8 minutes (exclusive)
        "last_20min": c20 - c8,     // 8-20 minutes (exclusive)
        "last_60min": c60 - c20,    // 20-60 minutes (exclusive)
    }, nil
}
```

**Pattern Recognition**:
```go
func analyzeMotionPattern(windowCounts map[string]int64) string {
    last2min := windowCounts["last_2min"]
    last8min := windowCounts["last_8min"]
    
    if last2min >= 2 {
        return "active_motion" // Someone moving right now
    }
    if last2min == 1 {
        return "recent_motion" // Just moved
    }
    if last8min >= 4 {
        return "settling_in" // Was active recently, now quiet
    }
    if last8min == 1 {
        return "pass_through" // Single motion, now gone
    }
    
    return "no_activity"
}
```

## Maintenance and Debugging

### Data Inspection Commands

**Check All Locations**:
```bash
# Find all locations with motion data
redis-cli KEYS 'sensor:motion:*' | sed 's/sensor:motion://'

# Check temporal state for location
redis-cli HGETALL temporal:study

# Check prediction history
redis-cli LRANGE predictions:study 0 -1
```

**Monitor Real-Time Data**:
```bash
# Watch motion events as they arrive
redis-cli MONITOR | grep 'sensor:motion'

# Check latest motion event
redis-cli ZREVRANGE sensor:motion:study 0 0 WITHSCORES
```

**Debugging Go Tool**:
```go
func debugOccupancyAgent(rdb *redis.Client, location string) {
    fmt.Printf("=== Debugging Occupancy Agent for %s ===\n", location)
    
    // Check motion data
    ctx := context.Background()
    motionKey := fmt.Sprintf("sensor:motion:%s", location)
    count, _ := rdb.ZCard(ctx, motionKey).Result()
    fmt.Printf("Motion events stored: %d\n", count)
    
    // Get latest motion
    latest, _ := rdb.ZRevRange(ctx, motionKey, 0, 0).Result()
    if len(latest) > 0 {
        fmt.Printf("Latest motion: %s\n", latest[0])
    }
    
    // Check occupancy state
    state, _ := getCurrentOccupancyState(rdb, location)
    if state.Occupied != nil {
        fmt.Printf("Currently occupied: %t\n", *state.Occupied)
    } else {
        fmt.Printf("Occupancy state: unknown\n")
    }
    
    if state.LastStateChange != nil {
        fmt.Printf("Last state change: %s (%s ago)\n", 
            state.LastStateChange.Format(time.RFC3339),
            time.Since(*state.LastStateChange))
    }
    
    // Check prediction history
    predictions, _ := getPredictionHistory(rdb, location)
    fmt.Printf("Prediction history: %d entries\n", len(predictions))
    
    if len(predictions) > 0 {
        latest := predictions[len(predictions)-1]
        fmt.Printf("Latest prediction: %t (confidence: %.2f)\n", 
            latest.Occupied, latest.Confidence)
        fmt.Printf("Reasoning: %s\n", latest.Reasoning)
    }
}
```

### Data Cleanup

**Remove Old Data**:
```go
func cleanupOldData(rdb *redis.Client) error {
    ctx := context.Background()
    
    // Motion data is auto-expired by collector (24 hours)
    // But we can clean up orphaned temporal state
    
    // Get all temporal keys
    temporalKeys, err := rdb.Keys(ctx, "temporal:*").Result()
    if err != nil {
        return err
    }
    
    for _, temporalKey := range temporalKeys {
        location := strings.TrimPrefix(temporalKey, "temporal:")
        motionKey := fmt.Sprintf("sensor:motion:%s", location)
        
        // Check if motion data still exists
        exists, err := rdb.Exists(ctx, motionKey).Result()
        if err != nil {
            continue
        }
        
        if exists == 0 {
            // No motion data, clean up temporal state
            fmt.Printf("Cleaning up orphaned temporal state for %s\n", location)
            rdb.Del(ctx, temporalKey)
            rdb.Del(ctx, fmt.Sprintf("predictions:%s", location))
        }
    }
    
    return nil
}
```

### Performance Monitoring

**Track Query Performance**:
```go
func benchmarkQueries(rdb *redis.Client, location string) {
    start := time.Now()
    
    // Simulate full analysis query pattern
    getExclusiveWindowCounts(rdb, location, time.Now())
    getCurrentOccupancyState(rdb, location)
    getPredictionHistory(rdb, location)
    
    duration := time.Since(start)
    fmt.Printf("Full analysis queries took: %v\n", duration)
    
    // Should be < 10ms for good performance
    if duration > 50*time.Millisecond {
        fmt.Printf("WARNING: Slow Redis performance detected\n")
    }
}
```

## Error Handling and Fallbacks

### Connection Issues

The occupancy agent is designed to handle Redis connection problems gracefully:

**Read Failures**: Return safe defaults
- Empty motion counts (0)
- Unknown occupancy state (nil)
- Empty prediction history

**Write Failures**: Log errors but continue operation
- State updates may be lost but don't crash agent
- Prediction history gaps are handled
- System recovers when connection restored

**Example Error Handling**:
```go
func safeGetMotionCount(rdb *redis.Client, location string, start, end time.Time) int64 {
    count, err := getMotionCountInWindow(rdb, location, start, end)
    if err != nil {
        log.Printf("Redis error getting motion count for %s: %v", location, err)
        return 0 // Safe default
    }
    return count
}
```

### Data Corruption Recovery

**Malformed JSON in Prediction History**:
- Skip corrupted entries, continue with valid ones
- System self-heals as new predictions are added

**Invalid Timestamps**:
- Use current time as fallback
- Log warnings for investigation

**Missing Hash Fields**:
- Treat as initial state (nil values)
- System initializes missing fields on next update

This storage design provides reliable data persistence while handling real-world issues like connection failures and data corruption gracefully.