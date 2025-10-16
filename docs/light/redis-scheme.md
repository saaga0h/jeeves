# Redis Storage Guide - Light Agent

This guide explains how the Light Agent uses Redis for intelligent lighting decisions and what data requirements are needed for optimal operation.

## How the Light Agent Uses Redis

The Light Agent is primarily a **read-only** consumer of sensor data for lighting analysis. It reads historical illuminance data to make intelligent decisions about brightness and color temperature.

**The Agent's Role with Redis**:
- 📖 **Reads** historical illuminance data for trend analysis
- 🔍 **Discovers** rooms by scanning sensor keys
- 📊 **Analyzes** lighting patterns over different time periods
- 💾 **Stores** state in-memory (manual overrides, rate limiting)
- ❌ **Never writes** sensor data (handled by Collector Agent)

---

## Storage Structure the Agent Needs

### Illuminance Data: `sensor:environmental:{location}`

The agent reads historical illuminance readings to understand lighting patterns and make context-aware decisions.

**Key format**: `sensor:environmental:living_room`, `sensor:environmental:bedroom`, etc.
**Type**: Sorted set (scores are Unix millisecond timestamps)

```go
type IlluminanceReading struct {
    Timestamp       string  `json:"timestamp"`       // ISO 8601 format
    CollectedAt     int64   `json:"collected_at"`    // Unix milliseconds
    Illuminance     float64 `json:"illuminance"`     // Lux value
    IlluminanceUnit string  `json:"illuminance_unit"` // "lux"
}
```

**Example Redis data**:
```redis
ZADD sensor:environmental:living_room 1704110400123 '{"timestamp":"2025-01-01T12:00:00.123Z","illuminance":450.0,"illuminance_unit":"lux"}'
ZADD sensor:environmental:living_room 1704110460456 '{"timestamp":"2025-01-01T12:01:00.456Z","illuminance":455.2,"illuminance_unit":"lux"}'
```

### Location Discovery: `sensor:motion:{location}`

The agent scans motion sensor keys to discover which rooms exist in the system.

**Used for**: Building list of all locations to monitor
**Pattern**: `sensor:motion:living_room`, `sensor:motion:bedroom`, etc.

---

## How Illuminance Analysis Works

The Light Agent uses a **three-tier strategy** to assess current lighting conditions:

### Strategy 1: Recent Data (Preferred)

**Time Window**: Last 10 minutes
**Priority**: Highest - most accurate representation of current conditions

```go
func getRecentIlluminance(redisClient *redis.Client, location string, minutesBack int) ([]IlluminanceReading, error) {
    ctx := context.Background()
    
    // Calculate time window
    endTime := time.Now().UnixMilli()
    startTime := endTime - int64(minutesBack*60*1000)
    
    // Query sorted set by score (timestamp)
    results, err := redisClient.ZRangeByScoreWithScores(ctx, 
        fmt.Sprintf("sensor:environmental:%s", location),
        &redis.ZRangeBy{
            Min: fmt.Sprintf("%d", startTime),
            Max: fmt.Sprintf("%d", endTime),
        }).Result()
    
    if err != nil {
        return nil, fmt.Errorf("failed to query illuminance: %w", err)
    }
    
    var readings []IlluminanceReading
    for _, result := range results {
        var reading IlluminanceReading
        if err := json.Unmarshal([]byte(result.Member.(string)), &reading); err != nil {
            continue // Skip invalid readings
        }
        
        // Filter for entries with illuminance data
        if reading.Illuminance > 0 {
            readings = append(readings, reading)
        }
    }
    
    return readings, nil
}
```

**Confidence Levels**:
- Data < 1 minute old: **95% confidence**
- Data < 2 minutes old: **70% confidence**  
- Data > 2 minutes old: Falls back to Strategy 2

### Strategy 2: Historical Pattern (Fallback)

**Time Window**: Same hour for the last 7 days
**Priority**: Medium - pattern-based prediction when recent data unavailable

```go
func getHistoricalPattern(redisClient *redis.Client, location string, hour int, daysBack int) ([]float64, error) {
    var samples []float64
    
    for day := 0; day < daysBack; day++ {
        // Calculate time window for this hour on this day
        targetTime := time.Now().AddDate(0, 0, -day)
        startOfHour := time.Date(targetTime.Year(), targetTime.Month(), targetTime.Day(), 
            hour, 0, 0, 0, targetTime.Location())
        endOfHour := startOfHour.Add(time.Hour)
        
        // Query illuminance for this hour
        readings, err := getIlluminanceInWindow(redisClient, location, 
            startOfHour.UnixMilli(), endOfHour.UnixMilli())
        if err != nil {
            continue
        }
        
        // Calculate average for this hour
        if len(readings) > 0 {
            var sum float64
            for _, reading := range readings {
                sum += reading.Illuminance
            }
            samples = append(samples, sum/float64(len(readings)))
        }
    }
    
    return samples, nil
}
```

**Requirements**:
- Minimum 3 days of data out of 7
- Confidence = 0.5 + (sample_count / 14), max 0.9

### Strategy 3: Time-Based Default (Ultimate Fallback)

**No Redis queries** - Pure calculation based on time of day
**Priority**: Lowest - safe assumptions when no data available

```go
func getTimeBasedDefault(hour int) IlluminanceAssessment {
    // Simple time-based assumptions
    switch {
    case hour >= 22 || hour < 6: // Night
        return IlluminanceAssessment{
            State:      "dark",
            Lux:        10,
            Confidence: 0.5,
            Source:     "time_based_default",
        }
    case hour >= 6 && hour < 8: // Early morning
        return IlluminanceAssessment{
            State:      "dim", 
            Lux:        30,
            Confidence: 0.5,
            Source:     "time_based_default",
        }
    case hour >= 8 && hour < 18: // Daytime
        return IlluminanceAssessment{
            State:      "moderate",
            Lux:        150,
            Confidence: 0.5,
            Source:     "time_based_default",
        }
    default: // Evening
        return IlluminanceAssessment{
            State:      "dim",
            Lux:        50,
            Confidence: 0.5,
            Source:     "time_based_default",
        }
    }
}
```

---

## Location Discovery

### Finding All Rooms

```go
func getAllLocations(redisClient *redis.Client) ([]string, error) {
    ctx := context.Background()
    
    // Get all sensor keys
    motionKeys, err := redisClient.Keys(ctx, "sensor:motion:*").Result()
    if err != nil {
        return nil, fmt.Errorf("failed to get motion keys: %w", err)
    }
    
    envKeys, err := redisClient.Keys(ctx, "sensor:environmental:*").Result()
    if err != nil {
        return nil, fmt.Errorf("failed to get environmental keys: %w", err)
    }
    
    // Extract unique locations
    locationSet := make(map[string]bool)
    
    for _, key := range motionKeys {
        if location := extractLocation(key, "sensor:motion:"); location != "" {
            locationSet[location] = true
        }
    }
    
    for _, key := range envKeys {
        if location := extractLocation(key, "sensor:environmental:"); location != "" {
            locationSet[location] = true
        }
    }
    
    // Convert to slice
    var locations []string
    for location := range locationSet {
        locations = append(locations, location)
    }
    
    return locations, nil
}

func extractLocation(key, prefix string) string {
    if strings.HasPrefix(key, prefix) {
        return strings.TrimPrefix(key, prefix)
    }
    return ""
}
```

---

## In-Memory State Management

The Light Agent maintains several in-memory data structures (currently not persisted to Redis):

### Manual Override Tracking

```go
type ManualOverride struct {
    Location  string    `json:"location"`
    ExpiresAt time.Time `json:"expires_at"`
    Duration  int       `json:"duration_minutes"`
}

type OverrideManager struct {
    overrides map[string]time.Time
    mutex     sync.RWMutex
}

func (om *OverrideManager) SetOverride(location string, durationMinutes int) {
    om.mutex.Lock()
    defer om.mutex.Unlock()
    
    expiresAt := time.Now().Add(time.Duration(durationMinutes) * time.Minute)
    om.overrides[location] = expiresAt
}

func (om *OverrideManager) IsOverrideActive(location string) bool {
    om.mutex.RLock()
    defer om.mutex.RUnlock()
    
    expiresAt, exists := om.overrides[location]
    if !exists {
        return false
    }
    
    if time.Now().After(expiresAt) {
        // Clean up expired override
        delete(om.overrides, location)
        return false
    }
    
    return true
}
```

### Rate Limiting

```go
type RateLimiter struct {
    lastDecision map[string]time.Time
    mutex        sync.RWMutex
    minInterval  time.Duration
}

func (rl *RateLimiter) CanMakeDecision(location string) bool {
    rl.mutex.RLock()
    defer rl.mutex.RUnlock()
    
    lastTime, exists := rl.lastDecision[location]
    if !exists {
        return true
    }
    
    return time.Since(lastTime) >= rl.minInterval
}

func (rl *RateLimiter) RecordDecision(location string) {
    rl.mutex.Lock()
    defer rl.mutex.Unlock()
    
    rl.lastDecision[location] = time.Now()
}
```

### Occupancy Context Cache

```go
type OccupancyContext struct {
    State      string    `json:"state"`
    Confidence float64   `json:"confidence"`
    Timestamp  time.Time `json:"timestamp"`
    LastUpdate time.Time `json:"last_update"`
}

type ContextManager struct {
    contexts map[string]OccupancyContext
    mutex    sync.RWMutex
}

func (cm *ContextManager) UpdateContext(location string, state string, confidence float64) {
    cm.mutex.Lock()
    defer cm.mutex.Unlock()
    
    cm.contexts[location] = OccupancyContext{
        State:      state,
        Confidence: confidence,
        Timestamp:  time.Now(),
        LastUpdate: time.Now(),
    }
}
```

---

## Data Requirements for Optimal Operation

### Minimum Data Needs

**For basic operation**:
- At least 1 motion sensor key per room (for location discovery)
- Recent illuminance readings (last 10 minutes) for accurate decisions

**For intelligent operation**:
- 7 days of historical illuminance data
- Regular sensor updates (every 30-60 seconds)
- Consistent key naming across rooms

### Storage Efficiency

**Recommended data retention**: 7-14 days
- **Recent decisions**: Last 10 minutes (most accurate)
- **Pattern analysis**: 7 days of history
- **Beyond 14 days**: Diminishing returns for lighting decisions

**Key naming consistency**:
```bash
# Correct naming pattern
sensor:motion:living_room
sensor:environmental:living_room

sensor:motion:bedroom  
sensor:environmental:bedroom

# Avoid inconsistent naming
sensor:motion:living_room
sensor:environmental:livingroom  # Missing underscore
```

---

## Troubleshooting Data Issues

### Agent Can't Find Rooms

**Symptoms**:
- Health check shows 0 locations
- No periodic lighting decisions

**Check Redis**:
```bash
# Verify motion sensor keys exist
redis-cli KEYS "sensor:motion:*"

# Verify environmental keys exist  
redis-cli KEYS "sensor:environmental:*"

# Check specific location
redis-cli ZRANGE sensor:environmental:living_room 0 -1 WITHSCORES
```

**Common fixes**:
- Ensure Collector Agent is running and storing data
- Verify key naming follows pattern: `sensor:{type}:{location}`
- Check that motion sensors are detected

### Poor Lighting Decisions

**Symptoms**:
- Lights too bright/dim at wrong times
- Agent always uses time-based defaults

**Check recent data availability**:
```bash
# Check for recent illuminance data (last 10 minutes)
current_time=$(date +%s)000
ten_min_ago=$((current_time - 600000))
redis-cli ZRANGEBYSCORE sensor:environmental:living_room $ten_min_ago +inf WITHSCORES
```

**Debug illuminance analysis**:
```bash
# Force decision and check logs for data source
curl -X POST http://light-agent:8080/decide/living_room

# Check agent logs for:
# - "using recent_reading" (best case)
# - "using historical_pattern" (fallback)  
# - "using time_based_default" (no data)
```

### Rate Limiting Issues

**Symptoms**:
- Occupancy changes but no immediate lighting response
- Commands only sent every 30 seconds

**Check decision timing**:
```bash
# Force immediate decision (bypasses rate limiting)
curl -X POST http://light-agent:8080/decide/living_room

# Check if manual override is active
curl http://light-agent:8080/contexts | jq '.manual_overrides'
```

**Adjust rate limiting**:
```bash
# Start agent with lower rate limit (5 seconds instead of 10)
./bin/light-agent -log-level debug -rate-limit 5
```

---

## Health Check and Monitoring

### Health Endpoint

```bash
curl http://localhost:8080/health
```

```json
{
  "status": "healthy",
  "timestamp": "2025-01-01T12:00:00Z",
  "uptime": 3600,
  "services": {
    "redis": "connected",
    "mqtt": "connected"
  },
  "metrics": {
    "locations_tracked": 5,
    "locations_in_redis": 5,
    "sensor_stats": {
      "total_keys": 10,
      "by_type": {
        "motion": 5,
        "environmental": 5
      },
      "by_location": {
        "living_room": 2,
        "bedroom": 2,
        "study": 2
      }
    },
    "manual_overrides": ["living_room"]
  }
}
```

### Performance Monitoring

```go
// Example monitoring queries
func monitorDataFreshness(redisClient *redis.Client, location string) {
    recent, _ := getRecentIlluminance(redisClient, location, 10)
    
    if len(recent) == 0 {
        log.Printf("WARNING: No recent illuminance data for %s", location)
        return
    }
    
    latest := recent[len(recent)-1]
    age := time.Since(parseTimestamp(latest.Timestamp))
    
    if age > 2*time.Minute {
        log.Printf("WARNING: Illuminance data for %s is %v old", location, age)
    }
}
```

The Light Agent's intelligent use of Redis enables sophisticated lighting decisions while maintaining performance and reliability even when sensor data is incomplete or unavailable.