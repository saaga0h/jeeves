# Illuminance Agent - Message Examples

This document shows real examples of the messages that flow through the Illuminance Agent, helping you understand how raw illuminance data becomes intelligent lighting context.

## Message Flow Overview

```
Illuminance Sensors → Collector Agent → Redis Storage
                                           ↓
MQTT Trigger → Illuminance Agent → Analysis → Rich Context Messages
```

The Illuminance Agent receives simple triggers but produces rich, contextual information about lighting conditions.

---

## Input: What Triggers Analysis

The Illuminance Agent doesn't process raw sensor data directly. Instead, it receives triggers from the Collector Agent and reads historical data from Redis for analysis.

### Trigger Messages: `automation/sensor/illuminance/{location}`

**Example Triggers**:
```
Topic: automation/sensor/illuminance/study
Payload: (ignored - content doesn't matter)

Topic: automation/sensor/illuminance/living_room
Payload: (ignored - analysis reads from Redis)
```

**What This Means**:
- "New illuminance data has been stored for this location"
- "Please analyze lighting conditions now"
- The actual sensor data is read from Redis, not from the trigger payload

---

## Output: Intelligent Lighting Context

The Illuminance Agent publishes rich context messages to `automation/context/illuminance/{location}` with detailed lighting analysis.

### Bright Daylight Example

### Bright Daylight Example

When the living room has good natural lighting during midday:

**Topic**: `automation/context/illuminance/living_room`

```json
{
  "source": "illuminance-agent",
  "type": "illuminance", 
  "location": "living_room",
  "state": "bright",
  "message": "Illuminance is bright (650 lux)",
  "data": {
    "current_lux": 650,
    "current_label": "bright",
    "trend": "stable",
    "stability": "stable",
    "avg_2min": 645,
    "avg_10min": 638,
    "min_10min": 620,
    "max_10min": 680,
    "relative_to_typical": "above_typical",
    "likely_sources": ["natural", "mixed"],
    "is_daytime": true,
    "theoretical_outdoor_lux": 85000,
    "time_of_day": "midday"
  },
  "timestamp": "2025-01-01T12:00:00.123Z"
}
```

**What This Tells Us**:
- Room is **bright** (650 lux) with stable lighting
- Above typical lighting for midday 
- Natural light is primary source, possibly with some artificial light
- Outdoor theoretical lux is very high (85,000), so indoor is well-lit naturally

### Dim Evening Lighting Example

When the study has artificial lighting in the evening:

**Topic**: `automation/context/illuminance/study`

```json
{
  "source": "illuminance-agent",
  "type": "illuminance",
  "location": "study", 
  "state": "dim",
  "message": "Illuminance is dim (45 lux)",
  "data": {
    "current_lux": 45,
    "current_label": "dim",
    "trend": "dimming",
    "stability": "stable",
    "avg_2min": 48,
    "avg_10min": 52,
    "min_10min": 40,
    "max_10min": 65,
    "relative_to_typical": "below_typical",
    "likely_sources": ["artificial"],
    "is_daytime": false,
    "theoretical_outdoor_lux": 0,
    "time_of_day": "evening"
  },
  "timestamp": "2025-01-01T19:30:00.456Z"
}
```

**What This Tells Us**:
- Room is **dim** (45 lux) and getting dimmer
- Below typical lighting for evening time
- Artificial lighting only (no natural light available)
- May benefit from additional lighting

### Dark Nighttime Example

When the bedroom is dark at night:

**Topic**: `automation/context/illuminance/bedroom`

```json
{
  "source": "illuminance-agent",
  "type": "illuminance",
  "location": "bedroom",
  "state": "dark",
  "message": "Illuminance is dark (2 lux)",
  "data": {
    "current_lux": 2,
    "current_label": "dark",
    "trend": "stable",
    "stability": "stable", 
    "avg_2min": 2,
    "avg_10min": 3,
    "min_10min": 1,
    "max_10min": 5,
    "relative_to_typical": "near_typical",
    "likely_sources": ["none"],
    "is_daytime": false,
    "theoretical_outdoor_lux": 0,
    "time_of_day": "night"
  },
  "timestamp": "2025-01-01T23:15:00.789Z"
}
```

**What This Tells Us**:
- Room is appropriately **dark** (2 lux) for nighttime
- Lighting is near typical for night (expected to be dark)
- No active light sources
- Good conditions for sleep

### Very Bright Sunlight Example

When the kitchen receives direct sunlight:

**Topic**: `automation/context/illuminance/kitchen`

```json
{
  "source": "illuminance-agent",
  "type": "illuminance",
  "location": "kitchen",
  "state": "very_bright",
  "message": "Illuminance is very_bright (8500 lux)",
  "data": {
    "current_lux": 8500,
    "current_label": "very_bright",
    "trend": "brightening",
    "stability": "variable",
    "avg_2min": 8200,
    "avg_10min": 7800,
    "min_10min": 7200,
    "max_10min": 8600,
    "relative_to_typical": "well_above_typical",
    "likely_sources": ["natural", "mixed"],
    "is_daytime": true,
    "theoretical_outdoor_lux": 95000,
    "time_of_day": "afternoon"
  },
  "timestamp": "2025-01-01T14:30:00.321Z"
}
```

**What This Tells Us**:
- Room is **very bright** (8,500 lux) and getting brighter
- Well above typical for afternoon
- Strong natural light, possibly enhanced by artificial lights
- May be too bright - could benefit from automated dimming or blinds

### Sensor Failure Fallback Example

When sensors fail but the agent continues working using daylight calculations:

**Topic**: `automation/context/illuminance/garage`

```json
{
  "source": "illuminance-agent",
  "type": "illuminance",
  "location": "garage",
  "state": "dark",
  "message": "Illuminance is dark (5 lux)",
  "data": {
    "current_lux": 5,
    "current_label": "dark",
    "trend": "unknown",
    "stability": "unknown",
    "avg_2min": 5,
    "avg_10min": 5,
    "min_10min": 5,
    "max_10min": 5,
    "relative_to_typical": "well_below_typical",
    "likely_sources": ["none"],
    "is_daytime": false,
    "theoretical_outdoor_lux": 0,
    "time_of_day": "night"
  },
  "timestamp": "2025-01-01T22:00:00.654Z"
}
```

**What This Tells Us**:
- Using fallback mode due to insufficient sensor data
- Trend and stability are "unknown" (can't calculate without historical data)
- All averages equal current reading (no historical data available)
- System continues operating even with sensor failures

---

## Understanding the Context Data

### Lighting State Categories

| State | Lux Range | Typical Use Cases |
|-------|-----------|-------------------|
| **dark** | ≤ 10 lux | Sleep, relaxation, nighttime |
| **dim** | 11-50 lux | Evening ambiance, minimal task lighting |
| **moderate** | 51-200 lux | General indoor activities, reading |
| **bright** | 201-500 lux | Detailed work, cooking, active areas |
| **very_bright** | > 500 lux | Direct sunlight, high-precision tasks |

### Trend Analysis

**What Trends Mean**:
- **brightening**: Light levels increasing > 20% over time
- **dimming**: Light levels decreasing > 20% over time  
- **stable**: Light levels consistent within ±20%
- **unknown**: Insufficient historical data for calculation

**Why This Matters**:
- Helps predict lighting needs
- Enables proactive adjustments
- Identifies patterns (sunset dimming, morning brightening)

### Light Source Detection

**Source Identification**:
- **natural**: Daylight is the primary source
- **artificial**: Electric lighting (lamps, overhead lights)
- **mixed**: Combination of natural and artificial
- **none**: Very low light, no active sources
- **unknown**: Unable to determine source type

**How It Works**:
- Combines current lux level with time-of-day context
- Uses daylight calculations to understand natural light availability
- Infers artificial lighting based on patterns

### Temporal Context

**Time of Day Categories**:
- **early_morning**: 5:00-6:59 (sunrise transition)
- **morning**: 7:00-9:59 (active morning hours)
- **midday**: 10:00-13:59 (peak daylight)
- **afternoon**: 14:00-16:59 (good natural light)
- **evening**: 17:00-19:59 (transitioning to artificial)
- **late_evening**: 20:00-21:59 (primarily artificial)
- **night**: 22:00-4:59 (minimal or no lighting)

---

## How Other Agents Use This Context

### Light Agent Integration

```javascript
// Light Agent receives illuminance context
const context = {
  "state": "dim",
  "data": {
    "current_lux": 45,
    "trend": "dimming",
    "likely_sources": ["artificial"],
    "relative_to_typical": "below_typical"
  }
};

// Smart lighting decision
if (context.state === "dim" && context.data.relative_to_typical === "below_typical") {
  // Room is dimmer than expected - increase artificial lighting
  lightAgent.adjustBrightness("living_room", "increase");
}

if (context.data.trend === "brightening" && context.data.likely_sources.includes("natural")) {
  // Natural light increasing - reduce artificial lighting
  lightAgent.adjustBrightness("living_room", "decrease");
}
```

### Behavior Agent Integration

```javascript
// Behavior Agent uses lighting context for episodes
const episode = {
  activity: "movie_watching",
  location: "living_room",
  lighting_context: {
    start_state: "bright",
    end_state: "dim", 
    trend: "dimming",
    sources: ["artificial"],
    // This suggests lights were dimmed for movie watching
  }
};
```

### Example: Complete Lighting State Change

Here's how lighting context changes when someone turns on lights in the evening:

**Before (Lights Off)**:
```json
{
  "state": "dark",
  "data": {
    "current_lux": 8,
    "trend": "stable",
    "likely_sources": ["none"],
    "time_of_day": "evening"
  }
}
```

**After (Lights Turned On)**:
```json
{
  "state": "moderate", 
  "data": {
    "current_lux": 180,
    "trend": "brightening",
    "likely_sources": ["artificial"],
    "time_of_day": "evening"
  }
}
```

**What Agents Learn**:
- **Light Agent**: Successful lighting adjustment, room now adequately lit
- **Behavior Agent**: Manual lighting action detected, user preference for moderate lighting in evening
- **Occupancy Agent**: Room activity indicated by lighting change

---

## Health Check Information

**Endpoint**: `GET /health`

```json
{
  "status": "OK",
  "timestamp": "2025-01-01T12:00:00.123Z",
  "uptime": 12345.67,
  "locations": 5,
  "mqtt_connected": true,
  "redis": {
    "status": "ok",
    "connected": true,
    "max_data_age_hours": 1
  },
  "config": {
    "max_data_age_hours": 1,
    "min_readings_required": 3
  }
}
```

**Key Health Indicators**:
- **locations**: Number of rooms being analyzed
- **mqtt_connected**: Can receive triggers and publish context
- **redis.connected**: Can read historical sensor data
- **config values**: Analysis parameters currently in use

---

## Real-World Usage Patterns

### Daily Lighting Cycle

**Morning Progression**:
```
6:00 AM: "dark" → "dim" (sunrise beginning)
8:00 AM: "dim" → "moderate" (natural light increasing)
10:00 AM: "moderate" → "bright" (full daylight)
```

**Evening Progression**:
```
6:00 PM: "bright" → "moderate" (sunset approaching)  
8:00 PM: "moderate" → "dim" (artificial lighting only)
10:00 PM: "dim" → "dark" (lights turned off)
```

### Activity-Based Patterns

**Movie Night Sequence**:
```
1. "bright" → "dim" (lights dimmed manually)
2. "dim" → "dark" (lights turned off for movie)
3. "dark" → "dim" (lights turned on for break)
4. "dim" → "dark" (back to movie)
5. "dark" → "moderate" (movie finished, normal lighting)
```

The Illuminance Agent captures these patterns and provides the context that enables intelligent automation throughout your home.