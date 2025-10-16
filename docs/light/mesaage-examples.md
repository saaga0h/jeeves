# Light Agent Message Guide

This guide shows the message flows for the Light Agent and how to integrate with its communication patterns.

## Context Messages the Agent Receives

The Light Agent listens for room context from other agents to make intelligent lighting decisions.

### Occupancy Context (Primary Input)

**Topic**: `automation/context/occupancy/{location}`

The agent primarily responds to occupancy changes, triggering immediate lighting adjustments.

```go
type OccupancyContext struct {
    Source     string  `json:"source"`
    Type       string  `json:"type"`
    Location   string  `json:"location"`
    State      string  `json:"state"`      // "empty", "occupied", "likely", "unknown"
    Confidence float64 `json:"confidence"` // 0.0-1.0
    Data       struct {
        MotionEvents      int `json:"motion_events"`
        LastMotionSeconds int `json:"last_motion_seconds"`
    } `json:"data"`
    Timestamp string `json:"timestamp"`
}
```

**Room Becomes Occupied (High Confidence)**:
```json
{
  "source": "occupancy-agent",
  "type": "occupancy",
  "location": "study",
  "state": "occupied",
  "confidence": 0.95,
  "data": {
    "motion_events": 5,
    "last_motion_seconds": 15
  },
  "timestamp": "2025-01-01T19:30:00Z"
}
```

**Room Becomes Empty**:
```json
{
  "source": "occupancy-agent",
  "type": "occupancy", 
  "location": "study",
  "state": "empty",
  "confidence": 0.90,
  "data": {
    "motion_events": 0,
    "last_motion_seconds": 900
  },
  "timestamp": "2025-01-01T20:45:00Z"
}
```

**Agent Response**:
- **Immediate action**: Occupancy changes trigger instant lighting decisions
- **State tracking**: Agent remembers previous state to detect changes
- **Confidence threshold**: Requires ≥50% confidence for lighting changes

### Illuminance Context (Background Input)

**Topic**: `automation/context/illuminance/{location}`

Currently logged but not actively used for decisions. The agent reads illuminance data directly from Redis for better historical analysis.

```go
type IlluminanceContext struct {
    Source   string `json:"source"`
    Type     string `json:"type"`
    Location string `json:"location"`
    State    string `json:"state"` // "dark", "dim", "moderate", "bright"
    Data     struct {
        CurrentLux int    `json:"current_lux"`
        Trend      string `json:"trend"`
    } `json:"data"`
    Timestamp string `json:"timestamp"`
}
```

---

## Commands the Agent Publishes

The Light Agent publishes lighting commands that physical light controllers can execute.

### Light Command Messages

**Topic**: `automation/command/light/{location}`

```go
type LightCommand struct {
    Action     string  `json:"action"`      // "on", "off"
    Brightness int     `json:"brightness"`  // 0-100 percentage
    ColorTemp  *int    `json:"color_temp"`  // Kelvin (2400-5500), null for off
    Reason     string  `json:"reason"`      // Decision explanation
    Confidence float64 `json:"confidence"`  // 0.0-1.0
    Timestamp  string  `json:"timestamp"`   // ISO 8601
}
```

**Turn On Example (Evening Dimming)**:
```json
{
  "action": "on",
  "brightness": 60,
  "color_temp": 2700,
  "reason": "occupied_dim_evening_recent_reading",
  "confidence": 0.98,
  "timestamp": "2025-01-01T19:30:00Z"
}
```

**Turn Off Example (Room Empty)**:
```json
{
  "action": "off",
  "brightness": 0,
  "color_temp": null,
  "reason": "room_empty",
  "confidence": 0.90,
  "timestamp": "2025-01-01T20:45:00Z"
}
```

**Turn Off Example (Bright Natural Light)**:
```json
{
  "action": "off",
  "brightness": 0,
  "color_temp": null,
  "reason": "occupied_bright_midday_recent_reading",
  "confidence": 0.95,
  "timestamp": "2025-01-01T12:00:00Z"
}
```

### Lighting Context Messages

**Topic**: `automation/context/lighting/{location}`

Published alongside every command for behavior tracking and integration with other agents.

```go
type LightingContext struct {
    Type         string  `json:"type"`         // Always "lighting"
    Location     string  `json:"location"`
    State        string  `json:"state"`        // "on", "off"
    Illuminating bool    `json:"illuminating"` // true when lights are on
    Brightness   int     `json:"brightness"`   // 0-100
    ColorTemp    *int    `json:"color_temp"`   // Kelvin or null
    Automated    bool    `json:"automated"`    // Always true for this agent
    Reason       string  `json:"reason"`
    Confidence   float64 `json:"confidence"`
    Timestamp    string  `json:"timestamp"`
    Source       string  `json:"source"`       // Always "light-agent"
}
```

**Lights On Context**:
```json
{
  "type": "lighting",
  "location": "study",
  "state": "on",
  "illuminating": true,
  "brightness": 60,
  "color_temp": 2700,
  "automated": true,
  "reason": "occupied_dim_evening_recent_reading",
  "confidence": 0.98,
  "timestamp": "2025-01-01T19:30:00Z",
  "source": "light-agent"
}
```

---

## Understanding Decision Reasons

The `reason` field explains why the Light Agent made each decision:

### Occupancy-Based Reasons

**`"room_empty"`** → Lights off
- Room detected as empty - energy saving priority

**`"awaiting_occupancy_confirmation_{state}"`** → No action
- Occupancy is uncertain (likely/transitioning states)

**`"occupancy_confidence_too_low"`** → No action  
- Occupancy confidence below 50% threshold

**`"manual_override_active"`** → No action
- Manual control is active for this room

### Intelligent Lighting Reasons

Format: `"occupied_{lighting_condition}_{time_period}_{data_source}"`

**Examples**:
- `"occupied_dark_night_recent_reading"` → Bright lights (80%) at warm temperature
- `"occupied_dim_evening_historical_pattern"` → Medium lights (60%) at warm temperature  
- `"occupied_bright_midday_time_based_default"` → Lights off (natural light sufficient)
- `"occupied_moderate_afternoon_recent_reading"` → Low lights (20%) at neutral temperature

**Components Explained**:
- **Lighting condition**: dark, dim, moderate, bright (from sensor analysis)
- **Time period**: night, evening, afternoon, etc. (affects color temperature)
- **Data source**: recent_reading (sensors), historical_pattern (past data), time_based_default (fallback)

---

## Brightness and Color Patterns

### Brightness by Lighting Conditions

**Dark Conditions** (< 5 lux):
```go
// Night/Late Evening: Gentler lighting
brightness := 50
reason := "dark conditions, late hours - dim"

// Active Hours: Full lighting needed  
brightness := 80
reason := "dark conditions, active hours - bright"
```

**Dim Conditions** (5-50 lux):
```go
// Night/Late Evening
brightness := 40
reason := "dim conditions, late hours - lower"

// Active Hours
brightness := 60  
reason := "dim conditions, active hours - moderate"
```

**Moderate Conditions** (50-200 lux):
```go
// Natural light present
brightness := 20
reason := "moderate natural light - minimal supplement"

// Artificial light only
brightness := 40
reason := "moderate artificial light - more supplement needed"
```

**Bright Conditions** (> 200 lux):
```go
// Natural light - no artificial needed
brightness := 0
action := "off"
reason := "bright natural light - no artificial light needed"

// Artificial light - minimal addition
brightness := 10
reason := "bright artificial light - minimal addition"
```

### Color Temperature by Time

```go
colorTemperatureMap := map[string]int{
    "early_morning": 3000, // 5-7 AM: Warm start
    "morning":       4500, // 7-10 AM: Neutral white  
    "midday":        5500, // 10 AM-2 PM: Cool daylight
    "afternoon":     4500, // 2-5 PM: Neutral white
    "evening":       2700, // 5-8 PM: Warm white
    "late_evening":  2500, // 8-10 PM: Very warm
    "night":         2400, // 10 PM-5 AM: Ultra warm for sleep
}
```

---

## Integration Examples

### For Light Controllers (Subscribing to Commands)

```go
package main

import (
    "encoding/json"
    "fmt"
    "log"
    "strings"
    
    mqtt "github.com/eclipse/paho.mqtt.golang"
)

type LightCommand struct {
    Action     string  `json:"action"`
    Brightness int     `json:"brightness"`
    ColorTemp  *int    `json:"color_temp"`
    Reason     string  `json:"reason"`
    Confidence float64 `json:"confidence"`
    Timestamp  string  `json:"timestamp"`
}

func setupLightCommandSubscription(client mqtt.Client) {
    // Subscribe to all light commands
    client.Subscribe("automation/command/light/+", 0, handleLightCommand)
}

func handleLightCommand(client mqtt.Client, msg mqtt.Message) {
    topic := msg.Topic()
    parts := strings.Split(topic, "/")
    location := parts[len(parts)-1] // Extract location
    
    var command LightCommand
    if err := json.Unmarshal(msg.Payload(), &command); err != nil {
        log.Printf("Error parsing light command: %v", err)
        return
    }
    
    // Execute lighting command
    switch command.Action {
    case "on":
        fmt.Printf("Turning on %s: %d%% brightness, %dK\n", 
            location, command.Brightness, *command.ColorTemp)
        // Call your light control API here
        
    case "off":
        fmt.Printf("Turning off %s\n", location)
        // Call your light control API here
    }
    
    log.Printf("Light command for %s: %s (reason: %s)", 
        location, command.Action, command.Reason)
}
```

### For Publishing Occupancy Context

```go
func publishOccupancyContext(client mqtt.Client, location string, state string, confidence float64) error {
    context := OccupancyContext{
        Source:     "your-occupancy-system",
        Type:       "occupancy",
        Location:   location,
        State:      state,
        Confidence: confidence,
        Timestamp:  time.Now().Format(time.RFC3339),
    }
    
    payload, err := json.Marshal(context)
    if err != nil {
        return fmt.Errorf("failed to marshal context: %w", err)
    }
    
    topic := fmt.Sprintf("automation/context/occupancy/%s", location)
    token := client.Publish(topic, 0, false, payload)
    token.Wait()
    
    return token.Error()
}

// Usage examples
publishOccupancyContext(client, "living_room", "occupied", 0.95)
publishOccupancyContext(client, "bedroom", "empty", 0.88)
```

---

## Manual Override API

### Setting an Override

```bash
# Override living room for default duration (30 minutes)
curl -X POST http://localhost:8080/override/living_room

# Override bedroom for 2 hours  
curl -X POST http://localhost:8080/override/bedroom \
  -H "Content-Type: application/json" \
  -d '{"duration": 120}'
```

**Response**:
```json
{
  "message": "Manual override set",
  "location": "bedroom", 
  "duration_minutes": 120,
  "expires_at": "2025-01-01T22:00:00Z"
}
```

### Checking Override Status

```bash
curl http://localhost:8080/contexts
```

**Response**:
```json
{
  "locations": ["living_room", "bedroom", "study"],
  "contexts": {
    "living_room": {
      "occupancy": {
        "state": "occupied",
        "confidence": 0.95,
        "timestamp": 1704138600123
      },
      "lastUpdate": 1704138600123
    }
  },
  "manual_overrides": ["bedroom"]
}
```

### Clearing an Override

```bash
curl -X DELETE http://localhost:8080/override/bedroom
```

**Response**:
```json
{
  "message": "Manual override cleared",
  "location": "bedroom"
}
```

---

## State Transition Examples

### Room Becomes Occupied

**Sequence**:
```
1. 📨 Occupancy Agent → `automation/context/occupancy/study`
   {"state": "occupied", "confidence": 0.95}

2. 🧠 Light Agent detects state change (null → occupied)

3. 🔍 Light Agent analyzes current conditions:
   - Illuminance: 45 lux (dim)
   - Time: 19:30 (evening)
   - Natural light: No

4. 💡 Light Agent calculates:
   - Brightness: 60% (dim conditions, evening)
   - Color temp: 2700K (warm evening lighting)

5. 📤 Light Agent publishes command → `automation/command/light/study`
   {"action": "on", "brightness": 60, "color_temp": 2700}

6. 📊 Light Agent publishes context → `automation/context/lighting/study`
   {"state": "on", "illuminating": true, "brightness": 60}
```

### Room Becomes Empty

**Sequence**:
```
1. 📨 Occupancy Agent → `automation/context/occupancy/study`
   {"state": "empty", "confidence": 0.90}

2. 🧠 Light Agent detects state change (occupied → empty)

3. ⚡ Light Agent applies immediate rule: Empty room = lights off

4. 📤 Light Agent publishes command → `automation/command/light/study`
   {"action": "off", "brightness": 0, "reason": "room_empty"}

5. 📊 Light Agent publishes context → `automation/context/lighting/study`
   {"state": "off", "illuminating": false, "brightness": 0}
```

### Natural Light Increases (Periodic Check)

**Sequence**:
```
1. ⏰ Periodic timer triggers (every 30 seconds)

2. 🔍 Light Agent checks study conditions:
   - Occupancy: Still occupied (0.95 confidence) 
   - Illuminance: 650 lux (bright, recent reading)
   - Time: 12:00 (midday)
   - Natural light: Yes (high lux + daytime)

3. 💡 Light Agent calculates:
   - Brightness: 0% (bright natural light sufficient)
   - Action: Turn off lights

4. 📤 Light Agent publishes command → `automation/command/light/study`
   {"action": "off", "reason": "occupied_bright_midday_recent_reading"}
```

---

## Message Frequency and Rate Limiting

### When Commands Are Published

✅ **Immediate triggers** (< 1 second):
- Room occupancy changes (empty ↔ occupied)
- Manual API decision trigger

✅ **Periodic triggers** (every 30 seconds):
- Conditions change requiring lighting adjustment
- Rate limited to prevent flickering

❌ **Not published**:
- Decision is "maintain" (no change needed)
- Rate limited (< 10 seconds since last decision for that room)
- Manual override is active

### Typical Message Volume

**Low activity periods**:
- 1-2 commands per room per hour
- Context messages match command frequency

**High activity periods** (people moving around):
- Up to 6 commands per room per hour
- Immediate responses to occupancy changes

**Rate limiting protection**:
- Maximum 6 decisions per room per minute
- Prevents light flickering from noisy sensors

The Light Agent's intelligent messaging ensures responsive lighting while preventing excessive automation and respecting manual control preferences.