# Illuminance Agent - How It Works

The **Illuminance Agent** analyzes light levels across your home and provides intelligent lighting context for other agents. It transforms raw illuminance sensor data into meaningful insights about lighting conditions, trends, and patterns.

## What the Illuminance Agent Does

## What the Illuminance Agent Does

The Illuminance Agent is the **smart lighting brain** that understands your home's lighting conditions and provides context for automated lighting decisions.

**Core Functions:**
1. **Analyzes** illuminance sensor data to understand lighting patterns
2. **Contextualizes** raw lux readings with time-of-day and daylight information
3. **Tracks** lighting trends (brightening, dimming, stable) over time
4. **Identifies** light sources (natural daylight, artificial lights, or mixed)
5. **Provides** fallback analysis even when sensors fail using solar calculations

### Key Responsibilities

- **Smart Analysis**: Converts raw lux numbers into meaningful lighting context
- **Temporal Understanding**: Tracks how lighting changes over minutes and hours
- **Daylight Integration**: Uses geographic location to understand natural light cycles
- **Trend Detection**: Identifies when rooms are getting brighter or dimmer
- **Reliability**: Continues working even with sensor failures or sparse data

### Why This Matters

Rather than just knowing "it's 450 lux in the living room," the system understands:
- "Living room is **bright** for midday, trending **stable**, likely **natural light**"
- "Study is **dim** for evening, **dimming** trend, **artificial light** sources"
- "Bedroom is **dark** as expected for night, **no light sources** detected"

This rich context enables other agents to make intelligent lighting decisions.

---

## How the Agent Works

### Dual Analysis Approach

The Illuminance Agent uses two complementary analysis strategies:

**1. Sensor-Based Analysis** (Preferred)
- Uses actual illuminance sensor readings from Redis
- Analyzes trends over 2, 10, and 30-minute windows
- Requires at least 3 readings in the last hour for reliability
- Provides the most accurate lighting context

**2. Daylight Fallback** (When sensors fail)
- Uses geographic location (latitude/longitude) and time
- Calculates theoretical outdoor illuminance based on sun position
- Combines with any available sensor readings (even old ones)
- Ensures continuous operation during sensor outages

### Analysis Triggers

**Real-Time Triggers**:
- New sensor data arrives (via MQTT from Collector Agent)
- Immediate analysis of that specific location
- Publishes updated context if conditions changed

**Periodic Analysis**:
- Runs every 30 seconds (configurable)
- Analyzes all locations with sufficient data
- Ensures regular updates even without new sensor data

### Smart Publishing Logic

The agent only publishes when it has something meaningful to say:

**Publishes When**:
- Lighting state changes (dark → dim, bright → moderate, etc.)
- At least 5 minutes since last update (periodic minimum)
- New sensor data arrives (immediate analysis)

**Skips Publishing When**:
- No state change and recent update (< 5 minutes)
- Analysis fails or no useful data

This reduces MQTT traffic while ensuring timely updates.

---

## Understanding Illuminance Context

### Lighting States

The agent categorizes lighting into five semantic levels:

| State | Lux Range | Example Conditions |
|-------|-----------|-------------------|
| **Dark** | ≤ 10 lux | Night with no lights, closed blinds |
| **Dim** | 11-50 lux | Evening with minimal artificial lighting |
| **Moderate** | 51-200 lux | Comfortable indoor lighting |
| **Bright** | 201-500 lux | Good daylight or bright artificial lighting |
| **Very Bright** | > 500 lux | Direct sunlight, very bright artificial lighting |

### Trend Analysis

**Temporal Trends** (over 2, 10, 30 minutes):
- **Brightening**: Light increasing > 20%
- **Dimming**: Light decreasing > 20%  
- **Stable**: Light levels consistent (±20%)

**Stability Assessment**:
- **Stable**: Consistent light levels
- **Variable**: Some fluctuation
- **Volatile**: Rapidly changing conditions

### Light Source Detection

**Automatic Source Identification**:
- **Natural**: Daytime with sufficient light levels
- **Artificial**: Nighttime lighting or evening illumination
- **Mixed**: Combination of daylight and artificial lights
- **None**: Very low light levels

### Contextual Intelligence

**Time-of-Day Awareness**:
- Understands typical lighting patterns for different times
- Compares current levels to expected levels
- Identifies unusual lighting conditions

**Daylight Integration**:
- Calculates theoretical outdoor illuminance based on sun position
- Determines whether observed indoor lighting is typical
- Provides context about natural vs artificial lighting

---

## Data Flow and Integration

### Input Sources

**Primary Data Source**: Redis sensor data
- Reads illuminance data stored by Collector Agent
- Accesses historical readings for trend analysis
- Key pattern: `sensor:environmental:{location}`

**Trigger Source**: MQTT notifications  
- Receives triggers from Collector Agent
- Topic pattern: `automation/sensor/illuminance/{location}`
- Triggers immediate analysis of specific locations

**Fallback Data**: Solar calculations
- Uses geographic coordinates (configurable, defaults to Helsinki)
- Calculates sun position and theoretical illuminance
- Provides context when sensor data is insufficient

### Output Destinations

**Primary Output**: MQTT context messages
- Publishes to: `automation/context/illuminance/{location}`
- Rich context data for other agents to consume
- Includes current state, trends, and contextual information

**Consumers of Illuminance Context**:
- **Light Agent**: Uses context for adaptive lighting decisions
- **Behavior Agent**: Environmental context for behavioral pattern analysis
- **Other Agents**: Any agent needing lighting intelligence

---

## Configuration and Setup

### Essential Configuration

**Geographic Location** (for daylight calculations):
```bash
JEEVES_ILLUMINANCE_LATITUDE=60.1695    # Helsinki latitude
JEEVES_ILLUMINANCE_LONGITUDE=24.9354   # Helsinki longitude
```

**Analysis Tuning**:
```bash
JEEVES_ILLUMINANCE_ANALYSIS_INTERVAL=30    # Analysis frequency (seconds)
JEEVES_ILLUMINANCE_MAX_DATA_AGE=1          # How far back to look (hours)
JEEVES_ILLUMINANCE_MIN_READINGS=3          # Minimum readings for sensor analysis
```

**Service Configuration**:
```bash
JEEVES_HEALTH_PORT=5000                    # Health check endpoint
JEEVES_LOG_LEVEL=info                      # Logging detail level
```

### Operational Parameters

**Analysis Strategy**:
- Prefers sensor data when available (≥3 readings in last hour)
- Falls back to daylight calculation when data insufficient
- Combines both for most accurate analysis

**Update Frequency**:
- Immediate analysis on new sensor data
- Periodic analysis every 30 seconds
- Minimum 5-minute intervals between publications

**Data Requirements**:
- At least 3 illuminance readings in last hour for "sufficient data"
- Single reading can still provide useful context with daylight data
- No readings triggers pure daylight calculation mode

---

## Monitoring and Health

### Health Check Endpoint

```bash
GET /health
→ {
  "status": "OK",
  "locations": 5,
  "mqtt_connected": true,
  "redis": {"status": "ok", "connected": true},
  "config": {"max_data_age_hours": 1, "min_readings_required": 3}
}
```

### Key Metrics to Monitor

**Performance Indicators**:
- Number of locations being analyzed
- Analysis frequency and timing
- MQTT and Redis connection health

**Data Quality Indicators**:
- Locations with sufficient sensor data vs fallback mode
- Analysis success rate
- Context publication frequency

**Operational Health**:
- Agent uptime and stability
- Memory usage (should be minimal)
- Response time to sensor triggers

---

## Troubleshooting Common Issues

### "No illuminance context being published"

1. **Check sensor data availability**:
   - Verify illuminance sensors are sending data to Collector Agent
   - Check Redis for `sensor:environmental:{location}` keys
   - Ensure readings contain `illuminance` field

2. **Check trigger reception**:
   - Verify Collector Agent is publishing `automation/sensor/illuminance/+` triggers
   - Check Illuminance Agent subscription in logs
   - Confirm MQTT connectivity

3. **Check analysis conditions**:
   - Verify sufficient data (≥3 readings) or fallback mode working
   - Check geographic coordinates for daylight calculations
   - Review analysis logs for errors

### "Context always shows 'unknown' trends"

1. **Insufficient historical data**:
   - Need multiple readings over time for trend calculation
   - Check data retention in Redis (24-hour TTL)
   - Verify sensors are reporting regularly

2. **Clock synchronization issues**:
   - Ensure system clocks are synchronized
   - Check timestamp accuracy in sensor data
   - Verify timezone consistency

### "Daylight calculations seem wrong"

1. **Geographic configuration**:
   - Verify latitude/longitude settings match your location
   - Check coordinate format (decimal degrees)
   - Ensure timezone configuration is correct

2. **Time accuracy**:
   - Verify system time is accurate
   - Check daylight saving time handling
   - Confirm solar calculation library is working

### "High memory usage or performance issues"

1. **Data accumulation**:
   - Illuminance Agent doesn't store data, only reads it
   - Check if too many locations are being analyzed
   - Review analysis frequency settings

2. **Connection issues**:
   - Check for Redis connection retries
   - Monitor MQTT connection stability
   - Review error rates in logs

---

## Integration Examples

### How Other Agents Use Illuminance Context

**Light Agent Decision Making**:
```javascript
// Received illuminance context
{
  "state": "dim",
  "data": {
    "current_lux": 45,
    "trend": "dimming", 
    "likely_sources": ["artificial"],
    "is_daytime": false
  }
}

// Light Agent logic
if (context.state === "dim" && context.data.trend === "dimming") {
  // Room is getting darker, may need more artificial lighting
  adjustLighting("increase_brightness");
}
```

**Behavior Agent Pattern Recognition**:
```javascript
// Track lighting patterns during activities
episode.environmental_context = {
  lighting_state: "bright",
  lighting_trend: "stable",
  light_sources: ["natural", "mixed"],
  time_of_day: "afternoon"
};
```

The Illuminance Agent provides the intelligence that makes these smart decisions possible, turning simple lux numbers into actionable lighting insights.