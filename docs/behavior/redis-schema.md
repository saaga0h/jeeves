# Behavior Agent - Redis Access Patterns

This document explains how the Behavior Agent reads sensor data from Redis during consolidation to detect behavioral episodes and patterns.

## Data Access Overview

The Behavior Agent **reads** sensor data from Redis (written by Collector Agent) but **does not write** to Redis. All behavioral episode data is stored in PostgreSQL.

```
Collector Agent → Writes to Redis (sensor data)
                ↓
Behavior Agent  → Reads from Redis (during consolidation)
                → Writes to PostgreSQL (episodes, vectors, macros)
```

---

## Sensor Data Read During Consolidation

### Motion Sensor Data

**Redis Key**: `sensor:motion:{location}`
- **Type**: Sorted Set (ZSET)
- **Score**: Unix timestamp in milliseconds (virtual time in test mode)
- **Written by**: Collector Agent
- **Read by**: Behavior Agent during consolidation

**Query Pattern**:
```redis
ZRANGEBYSCORE sensor:motion:study <min_timestamp> <max_timestamp> WITHSCORES
```

**Example**:
```redis
# Get all motion events in last 2 hours (7200000 ms)
ZRANGEBYSCORE sensor:motion:bedroom 1729152024000 1729159224000 WITHSCORES
```

**Data Structure**:
```json
{
  "timestamp": "2025-10-17T07:00:36.065Z",
  "state": "on",
  "entity_id": "motion.bedroom",
  "sequence": 1,
  "collected_at": 1729152036065
}
```

**Episode Detection Use**:
- Motion "on" events start or continue episodes
- Sequence of motion events builds location timeline
- Gaps > 5 minutes split episodes in same location
- Motion in new location ends previous episode

### Lighting Sensor Data

**Redis Key**: `sensor:lighting:{location}`
- **Type**: Sorted Set (ZSET)
- **Score**: Unix timestamp in milliseconds
- **Written by**: Collector Agent
- **Read by**: Behavior Agent during consolidation

**Query Pattern**:
```redis
ZRANGEBYSCORE sensor:lighting:dining_room <min_timestamp> <max_timestamp> WITHSCORES
```

**Data Structure**:
```json
{
  "timestamp": "2025-10-17T07:22:36.086Z",
  "state": "on",
  "brightness": 60,
  "color_temp": 2700,
  "source": "manual",
  "collected_at": 1729157556086
}
```

**Episode Detection Use**:
- **Manual** lighting ON can start an episode (especially in rooms without motion sensors)
- **Manual** lighting OFF ends an episode
- **Automated** lighting events are ignored (status updates, not occupancy signals)
- Critical for detecting dining room, reading room episodes where people sit still

### Presence Sensor Data (Future)

**Redis Key**: `sensor:presence:{location}`
- **Type**: Sorted Set (ZSET)
- **Status**: Planned for future implementation

**Use Case**:
- More accurate than motion sensors
- Better episode end detection
- Complement to motion/lighting data

---

## Time Range Query Strategy

### Consolidation Time Windows

**Lookback Period**:
```
Default: 2 hours from consolidation trigger time
Configurable via MQTT trigger payload
```

**Query Calculation**:
```go
// Virtual time aware
virtualNow := timeManager.Now()
sinceTime := virtualNow.Add(-lookbackHours * time.Hour)

// Convert to Redis score range
minScore := float64(sinceTime.UnixMilli())
maxScore := float64(virtualNow.UnixMilli())

// Query all motion sensors
for _, location := range locations {
    key := fmt.Sprintf("sensor:motion:%s", location)
    members := redis.ZRangeByScoreWithScores(key, minScore, maxScore)
}
```

### Virtual Time Support

**Why It Matters**:
- Test scenarios use compressed virtual time
- Redis scores must match virtual timestamps
- Collector stores virtual time when time_config is active

**Example**:
```
Virtual time:  2025-10-17T07:00:00Z (scenario start)
Wall-clock:    2025-10-20T10:00:00Z (actual time)

Redis score:   1729152000000 (virtual time)
Not:           1729422000000 (wall-clock time)

Query uses virtual time range to find test data
```

**Critical**: Both Collector and Behavior agents must use same timeManager for virtual time to work correctly.

---

## Multi-Location Aggregation

### Gathering Events from All Locations

**Location List**:
```go
locations := []string{
    "bedroom", "bathroom", "kitchen", "dining_room",
    "living_room", "hallway", "study"
}
```

**Parallel Queries**:
```go
// Gather motion events from all locations
for _, location := range locations {
    // Query motion
    motionKey := fmt.Sprintf("sensor:motion:%s", location)
    motionEvents := queryRedis(motionKey, minScore, maxScore)

    // Query lighting
    lightingKey := fmt.Sprintf("sensor:lighting:%s", location)
    lightingEvents := queryRedis(lightingKey, minScore, maxScore)
}
```

### Event Merging

**Unified Timeline**:
```go
type Event struct {
    Location  string
    Timestamp time.Time
    Type      string // "motion" or "lighting"
    State     string // "on" or "off"
    Source    string // "manual" or "automated" (lighting only)
}

// Merge all events from all locations
allEvents := []Event{}
allEvents = append(allEvents, motionEvents...)
allEvents = append(allEvents, lightingEvents...)

// Sort chronologically
sort.Slice(allEvents, func(i, j int) bool {
    return allEvents[i].Timestamp.Before(allEvents[j].Timestamp)
})
```

**Why Merge**:
- Understand actual sequence of activities
- Detect location transitions accurately
- Handle mixed motion/lighting events
- Create comprehensive behavioral timeline

---

## Query Performance Considerations

### Efficient Range Queries

**Sorted Set Benefits**:
- O(log(N)+M) complexity for range queries
- N = total entries, M = entries in range
- Fast even with thousands of sensor events

**Index Usage**:
- Timestamp scores are already indexed
- No secondary index needed
- Range queries are optimized operation

### Data Volume Estimates

**Typical Consolidation**:
```
Locations: 7 rooms
Lookback: 2 hours
Motion sensors: ~5 events/hour/room = 70 events
Lighting events: ~10 events/hour/room = 140 events
Total queries: 14 Redis operations (7 motion + 7 lighting)
Total events processed: ~210 events
Processing time: < 100ms
```

**Large Consolidation**:
```
Locations: 7 rooms
Lookback: 24 hours (full day)
Motion sensors: ~120 events/day/room = 840 events
Lighting events: ~240 events/day/room = 1680 events
Total events: ~2520 events
Processing time: < 500ms
```

### Memory Impact

**Redis Data**:
- Collector maintains 24-hour rolling buffer
- Behavior agent only reads, doesn't write
- No additional Redis memory overhead
- Query results are transient (not cached)

---

## Error Handling

### Missing Data Scenarios

**No Motion Data for Location**:
```go
members, err := redis.ZRangeByScoreWithScores(key, minScore, maxScore)
if err != nil {
    // Log but continue - not all rooms may have sensors
    logger.Debug("No motion data for location", "location", loc)
    continue
}
```

**No Events in Time Window**:
```go
if len(members) == 0 {
    // Normal - room may be unoccupied
    logger.Debug("No events in time window", "location", loc)
    continue
}
```

### Data Quality Issues

**Malformed JSON**:
```go
if err := json.Unmarshal([]byte(member.Member), &eventData); err != nil {
    // Skip this event, continue with others
    logger.Warn("Failed to parse event", "error", err)
    continue
}
```

**Missing Timestamps**:
```go
ts, err := time.Parse(time.RFC3339, eventData.Timestamp)
if err != nil {
    // Use score as fallback
    ts = time.UnixMilli(int64(member.Score))
}
```

---

## Redis Connection Management

### Connection Strategy

**Single Connection**:
```go
type Agent struct {
    redis       redis.Client
    // ... other fields
}
```

**Why Single Connection**:
- Consolidation is batch processing (not high-throughput)
- Queries are grouped and short-lived
- Reduces connection overhead
- Simplifies resource management

### Connection Health

**Automatic Reconnection**:
```go
// Redis client handles reconnection automatically
// No special handling needed in behavior agent
```

**Error Handling**:
```go
// If Redis unavailable during consolidation:
// 1. Log error
// 2. Return error to caller
// 3. Can retry consolidation later (idempotent)
```

---

## Debugging Redis Access

### Manual Query Examples

**Check what motion events exist**:
```bash
# Count events in time range
redis-cli ZCOUNT sensor:motion:bedroom 1729152000000 1729159200000

# Get all events with scores
redis-cli ZRANGEBYSCORE sensor:motion:bedroom 1729152000000 1729159200000 WITHSCORES

# Get latest 10 events
redis-cli ZREVRANGE sensor:motion:bedroom 0 9 WITHSCORES
```

**Check lighting events**:
```bash
# Get all lighting events for dining room
redis-cli ZRANGE sensor:lighting:dining_room 0 -1 WITHSCORES

# Get events in specific time window
redis-cli ZRANGEBYSCORE sensor:lighting:dining_room 1729157000000 1729158000000 WITHSCORES
```

**Inspect event data**:
```bash
# Get specific event and format JSON
redis-cli ZRANGE sensor:motion:study 0 0 | jq .
```

### Common Debug Scenarios

**"No episodes created"**:
```bash
# Check if sensor data exists
redis-cli KEYS "sensor:motion:*"
redis-cli KEYS "sensor:lighting:*"

# Check if data in correct time range
redis-cli ZRANGE sensor:motion:bedroom 0 -1 WITHSCORES | grep timestamp

# Verify virtual time configuration matches
redis-cli GET test:time_config
```

**"Wrong episode durations"**:
```bash
# Check timestamp format in data
redis-cli ZRANGE sensor:motion:bedroom 0 0

# Verify score matches timestamp
redis-cli ZRANGE sensor:motion:bedroom 0 0 WITHSCORES

# Compare virtual vs wall-clock time
date +%s%3N  # Current wall-clock time in ms
```

---

## Data Retention

### Collector Agent TTL

**Automatic Cleanup**:
- All sensor data has 24-hour TTL
- Behavior agent must read within 24 hours
- Older data automatically expires

**Implications**:
- Consolidation should run at least daily
- Historical analysis requires PostgreSQL query
- Redis is cache, not permanent storage

### Behavior Agent Storage

**PostgreSQL Persistence**:
- Episodes stored permanently in PostgreSQL
- Vectors and macros also in PostgreSQL
- Can recreate historical analysis from database
- Redis only needed for new episode detection

---

## Schema Evolution

### Current Redis Keys Used

**Read Operations**:
- `sensor:motion:{location}` - Motion sensor sorted sets
- `sensor:lighting:{location}` - Lighting sensor sorted sets

**Not Used**:
- `meta:motion:{location}` - Quick access metadata (not needed for batch processing)
- `sensor:environmental:{location}` - Environmental data (future use)

### Future Additions

**Planned**:
- `sensor:presence:{location}` - Presence sensor data
- `sensor:media:{location}` - Media activity tracking
- `sensor:door:{location}` - Door open/close events

**Backward Compatibility**:
- New sensor types will be additive
- Existing motion/lighting logic unchanged
- Graceful handling of missing sensor types

---

## Comparison: Redis vs PostgreSQL

### Redis (Collector Storage)
**Purpose**: Short-term sensor data cache
**Retention**: 24 hours (TTL)
**Access Pattern**: Time-range queries during consolidation
**Data Type**: Raw sensor events
**Performance**: Very fast (in-memory)
**Use Case**: Recent data for episode detection

### PostgreSQL (Behavior Storage)
**Purpose**: Long-term behavioral pattern storage
**Retention**: Permanent (application-managed)
**Access Pattern**: Complex semantic queries, pattern matching
**Data Type**: Processed behavioral insights (episodes, vectors, macros)
**Performance**: Fast enough for analytics
**Use Case**: Historical analysis, pattern learning, trend detection

### Why Both?

- **Redis**: Optimized for recent sensor data access
- **PostgreSQL**: Optimized for behavioral insights and complex queries
- **Separation**: Clean architecture - sensing vs. understanding
- **Efficiency**: Right tool for each job
