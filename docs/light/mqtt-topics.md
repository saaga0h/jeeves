# MQTT Topics Guide - Light Agent

This guide explains the MQTT communication patterns for the Light Agent and how to integrate with its intelligent lighting automation.

## Topics the Agent Listens To

The Light Agent subscribes to context from other agents to make intelligent lighting decisions.

### Occupancy Context: `automation/context/occupancy/{location}`

This is the **primary trigger** for lighting decisions. The agent responds immediately to occupancy changes.

**What the agent does with these messages**:
- 🔄 **Updates room status**: Tracks current occupancy state and confidence
- ⚡ **Immediate response**: State changes trigger instant lighting decisions  
- 📊 **Maintains history**: Compares new state to previous to detect changes

**Example locations**:
- `automation/context/occupancy/living_room`
- `automation/context/occupancy/bedroom`
- `automation/context/occupancy/study`
- `automation/context/occupancy/kitchen`

**Expected message format**:
```go
type OccupancyContext struct {
    Source     string  `json:"source"`     // "occupancy-agent"
    Type       string  `json:"type"`       // "occupancy"  
    Location   string  `json:"location"`   // "living_room"
    State      string  `json:"state"`      // "empty", "occupied", "likely"
    Confidence float64 `json:"confidence"` // 0.0-1.0
    Timestamp  string  `json:"timestamp"`  // ISO 8601
}
```

### Illuminance Context: `automation/context/illuminance/{location}`

Currently **received but not actively used** for decisions. The agent reads illuminance data directly from Redis for better historical analysis.

**Future potential**: Could trigger re-evaluation when dramatic lighting changes occur.

**Topics monitored**:
- `automation/context/illuminance/living_room`
- `automation/context/illuminance/bedroom`
- `automation/context/illuminance/study`

---

## Topics the Agent Publishes To

The Light Agent publishes commands and status that other systems can use.

### Light Commands: `automation/command/light/{location}`

These are the **control messages** that tell physical lights what to do.

**When commands are published**:
- ✅ **Occupancy changes**: Room becomes occupied or empty (immediate)
- ✅ **Periodic adjustments**: Every 30 seconds if conditions change
- ✅ **Manual triggers**: API calls to force decisions
- ❌ **Not published**: When decision is "maintain" (no change needed)

**Command topics**:
- `automation/command/light/living_room`
- `automation/command/light/bedroom`  
- `automation/command/light/study`
- `automation/command/light/kitchen`

**Command message format**:
```go
type LightCommand struct {
    Action     string  `json:"action"`      // "on", "off"
    Brightness int     `json:"brightness"`  // 0-100
    ColorTemp  *int    `json:"color_temp"`  // Kelvin (2400-5500), null for off
    Reason     string  `json:"reason"`      // Why this decision was made
    Confidence float64 `json:"confidence"`  // 0.0-1.0
    Timestamp  string  `json:"timestamp"`   // ISO 8601
}
```

### Lighting Context: `automation/context/lighting/{location}`

Published **immediately after every command** for behavior tracking and integration with other agents.

**Context topics**:
- `automation/context/lighting/living_room`
- `automation/context/lighting/bedroom`
- `automation/context/lighting/study`
- `automation/context/lighting/kitchen`

**Context message format**:
```go
type LightingContext struct {
    Type         string  `json:"type"`         // "lighting"
    Location     string  `json:"location"`     // "living_room"
    State        string  `json:"state"`        // "on", "off"
    Illuminating bool    `json:"illuminating"` // true when lights are on
    Brightness   int     `json:"brightness"`   // 0-100
    ColorTemp    *int    `json:"color_temp"`   // Kelvin or null
    Automated    bool    `json:"automated"`    // true (this agent)
    Reason       string  `json:"reason"`       // Decision explanation
    Confidence   float64 `json:"confidence"`   // 0.0-1.0
    Timestamp    string  `json:"timestamp"`    // ISO 8601
    Source       string  `json:"source"`       // "light-agent"
}
```

---

## How to Use These Topics

### For Light Controllers (Subscribing to Commands)

If you're building a system that controls physical lights:

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

func setupLightControl(client mqtt.Client) {
    // Subscribe to all light commands
    client.Subscribe("automation/command/light/+", 0, handleLightCommand)
}

func handleLightCommand(client mqtt.Client, msg mqtt.Message) {
    topic := msg.Topic()
    parts := strings.Split(topic, "/")
    location := parts[len(parts)-1] // Extract room name
    
    var command LightCommand
    if err := json.Unmarshal(msg.Payload(), &command); err != nil {
        log.Printf("Error parsing light command: %v", err)
        return
    }
    
    // Execute the lighting command
    switch command.Action {
    case "on":
        fmt.Printf("💡 Turning on %s: %d%% brightness", location, command.Brightness)
        if command.ColorTemp != nil {
            fmt.Printf(", %dK color temp", *command.ColorTemp)
        }
        fmt.Printf(" (reason: %s)\n", command.Reason)
        
        // Call your light control API here
        // controlLight(location, true, command.Brightness, command.ColorTemp)
        
    case "off":
        fmt.Printf("🌙 Turning off %s (reason: %s)\n", location, command.Reason)
        
        // Call your light control API here  
        // controlLight(location, false, 0, nil)
    }
}
```

### For Publishing Occupancy Context

If you're building an occupancy detection system:

```go
func publishOccupancyUpdate(client mqtt.Client, location string, state string, confidence float64) error {
    context := OccupancyContext{
        Source:     "your-occupancy-system",
        Type:       "occupancy",
        Location:   location,
        State:      state,      // "empty", "occupied", "likely"
        Confidence: confidence, // 0.0-1.0
        Timestamp:  time.Now().Format(time.RFC3339),
    }
    
    payload, err := json.Marshal(context)
    if err != nil {
        return fmt.Errorf("failed to marshal occupancy context: %w", err)
    }
    
    topic := fmt.Sprintf("automation/context/occupancy/%s", location)
    token := client.Publish(topic, 0, false, payload)
    token.Wait()
    
    if token.Error() != nil {
        return fmt.Errorf("failed to publish occupancy: %w", token.Error())
    }
    
    return nil
}

// Usage examples
publishOccupancyUpdate(client, "living_room", "occupied", 0.95)
publishOccupancyUpdate(client, "bedroom", "empty", 0.88)
publishOccupancyUpdate(client, "study", "likely", 0.65)
```

### For Monitoring Lighting Activity

```go
func monitorLightingActivity(client mqtt.Client) {
    // Monitor all lighting context for activity tracking
    client.Subscribe("automation/context/lighting/+", 0, handleLightingContext)
}

func handleLightingContext(client mqtt.Client, msg mqtt.Message) {
    topic := msg.Topic()
    parts := strings.Split(topic, "/")
    location := parts[len(parts)-1]
    
    var context LightingContext
    if err := json.Unmarshal(msg.Payload(), &context); err != nil {
        log.Printf("Error parsing lighting context: %v", err)
        return
    }
    
    // Track lighting patterns for analytics
    if context.State == "on" {
        fmt.Printf("📊 %s lights activated: %d%% brightness, %s\n", 
            location, context.Brightness, context.Reason)
    } else {
        fmt.Printf("📊 %s lights deactivated: %s\n", location, context.Reason)
    }
    
    // Store in database, send to analytics, etc.
    // trackLightingEvent(context)
}
```

---

## Message Flow Examples

### Room Becomes Occupied

```
1. 📥 Motion sensor detects activity
2. 📤 Occupancy Agent → automation/context/occupancy/living_room
   {"state": "occupied", "confidence": 0.95}
3. 🧠 Light Agent receives context, detects state change
4. ⚡ Light Agent makes immediate decision:
   - Checks current illuminance (45 lux = dim)
   - Considers time (19:30 = evening)
   - Calculates 60% brightness, 2700K warm light
5. 📤 Light Agent → automation/command/light/living_room
   {"action": "on", "brightness": 60, "color_temp": 2700}
6. 📤 Light Agent → automation/context/lighting/living_room
   {"state": "on", "illuminating": true, "brightness": 60}
7. 💡 Light controller receives command and executes
```

### Room Becomes Empty

```
1. 📥 No motion detected for threshold period
2. 📤 Occupancy Agent → automation/context/occupancy/living_room
   {"state": "empty", "confidence": 0.90}
3. 🧠 Light Agent receives context, detects state change
4. ⚡ Light Agent applies immediate rule: Empty room = lights off
5. 📤 Light Agent → automation/command/light/living_room
   {"action": "off", "brightness": 0, "reason": "room_empty"}
6. 📤 Light Agent → automation/context/lighting/living_room
   {"state": "off", "illuminating": false, "brightness": 0}
7. 🌙 Light controller turns off lights
```

### Periodic Adjustment (Natural Light Increase)

```
1. ⏰ Light Agent periodic timer (every 30 seconds)
2. 🔍 Checks all rooms with occupancy context
3. 📊 Living room: Still occupied, but illuminance now 650 lux (bright)
4. 🧠 Analyzes: Natural light sufficient, artificial lights not needed
5. 📤 Light Agent → automation/command/light/living_room
   {"action": "off", "reason": "occupied_bright_midday_recent_reading"}
6. 💡 Lights turn off automatically due to sufficient natural light
```

---

## Topic Patterns

### Wildcard Subscriptions

```go
// Subscribe to all occupancy context
client.Subscribe("automation/context/occupancy/+", 0, handler)

// Subscribe to all light commands (if you're a light controller)
client.Subscribe("automation/command/light/+", 0, handler)

// Subscribe to all lighting context (for monitoring)
client.Subscribe("automation/context/lighting/+", 0, handler)
```

### Location Extraction

```go
func extractLocation(topic string) string {
    parts := strings.Split(topic, "/")
    return parts[len(parts)-1] // Last segment is always location
}

// Examples:
// automation/context/occupancy/living_room → "living_room"
// automation/command/light/bedroom → "bedroom"
// automation/context/lighting/study → "study"
```

---

## Integration Patterns

### Home Assistant Integration

```yaml
# configuration.yaml
mqtt:
  light:
    - name: "Living Room Automated"
      command_topic: "automation/command/light/living_room"
      state_topic: "automation/context/lighting/living_room"
      brightness_command_topic: "automation/command/light/living_room"
      color_temp_command_topic: "automation/command/light/living_room"
      schema: json

# automation.yaml  
automation:
  - alias: "Log lighting decisions"
    trigger:
      platform: mqtt
      topic: automation/context/lighting/+
    action:
      service: system_log.write
      data:
        message: "Light automation: {{ trigger.topic.split('/')[-1] }} {{ trigger.payload_json.state }} ({{ trigger.payload_json.reason }})"
```

### Node-RED Integration

```json
[
  {
    "id": "light-monitor",
    "type": "mqtt in",
    "topic": "automation/context/lighting/+",
    "outputs": 1
  },
  {
    "id": "command-relay", 
    "type": "mqtt in",
    "topic": "automation/command/light/+",
    "outputs": 1
  },
  {
    "id": "process-command",
    "type": "function",
    "func": "const location = msg.topic.split('/').pop();\nconst command = msg.payload;\n// Forward to your light system\nreturn {topic: `lights/${location}/set`, payload: command};"
  }
]
```

---

## Troubleshooting MQTT Issues

### Light Agent Not Responding to Occupancy

**Symptoms**: 
- Occupancy changes published but no light commands issued
- Health check shows `mqtt_connected: false`

**Check MQTT connectivity**:
```bash
# Test occupancy publishing  
mosquitto_pub -h mqtt-broker -t 'automation/context/occupancy/test_room' \
  -m '{"source":"test","type":"occupancy","location":"test_room","state":"occupied","confidence":0.95,"timestamp":"2025-01-01T12:00:00Z"}'

# Monitor for light commands
mosquitto_sub -h mqtt-broker -t 'automation/command/light/+' -v
```

**Common fixes**:
- Verify MQTT broker connectivity
- Check agent configuration (broker address, credentials)
- Ensure occupancy confidence is ≥ 0.5

### Light Commands Not Reaching Controllers

**Symptoms**:
- Light Agent publishes commands but physical lights don't respond
- Commands visible in MQTT but not executed

**Debug MQTT flow**:
```bash
# Monitor all light-related topics
mosquitto_sub -h mqtt-broker -t 'automation/+/light/+' -v

# Check specific room
mosquitto_sub -h mqtt-broker -t 'automation/command/light/living_room' -v
```

**Common fixes**:
- Verify light controller is subscribed to command topics
- Check topic name formatting (should match exactly)
- Ensure JSON parsing works in light controller
- Test with manual command injection

### No Periodic Updates

**Symptoms**:
- Only responds to occupancy changes, no automatic adjustments
- No commands during lighting condition changes

**Check**:
```bash
# Verify agent is running and healthy
curl http://light-agent:8080/health

# Force manual decision
curl -X POST http://light-agent:8080/decide/living_room
```

**Common issues**:
- Periodic timer not running (check agent logs)
- No occupancy context stored (requires initial occupancy message)
- Rate limiting preventing decisions (10-second minimum interval)

---

## Topic Testing Commands

### Simulate Occupancy Changes

```bash
# Room becomes occupied
mosquitto_pub -h localhost -t 'automation/context/occupancy/living_room' \
  -m '{"source":"test","type":"occupancy","location":"living_room","state":"occupied","confidence":0.95,"timestamp":"'$(date -Iseconds)'"}'

# Room becomes empty
mosquitto_pub -h localhost -t 'automation/context/occupancy/living_room' \
  -m '{"source":"test","type":"occupancy","location":"living_room","state":"empty","confidence":0.90,"timestamp":"'$(date -Iseconds)'"}'
```

### Monitor All Light Activity

```bash
# See all light commands
mosquitto_sub -h localhost -t 'automation/command/light/+' -v

# See all lighting context
mosquitto_sub -h localhost -t 'automation/context/lighting/+' -v

# See complete light-related activity
mosquitto_sub -h localhost -t 'automation/+/light/+' -v
```

### Test Command Format

```bash
# Example command that light controllers should handle
mosquitto_pub -h localhost -t 'automation/command/light/test_room' \
  -m '{"action":"on","brightness":75,"color_temp":3000,"reason":"test","confidence":1.0,"timestamp":"'$(date -Iseconds)'"}'
```

This MQTT structure enables seamless integration between occupancy detection, intelligent lighting decisions, and physical light control systems throughout your smart home.