# MQTT Integration Guide - Occupancy Agent

The occupancy agent integrates with the home automation system through MQTT messaging. It receives motion triggers and publishes intelligent occupancy analysis results.

## What The Agent Listens For

### Motion Sensor Triggers

**Topic Pattern**: `automation/sensor/motion/{location}`

**Examples**:
- `automation/sensor/motion/study`
- `automation/sensor/motion/kitchen`  
- `automation/sensor/motion/bedroom`

**How It Works**:
- Collector agent stores motion data in Redis and sends trigger
- Occupancy agent receives trigger and checks for recent motion (last 2 minutes)
- If recent motion found, triggers immediate intelligent analysis
- If no recent motion, ignores trigger (prevents processing stale events)

**Important**: The MQTT message payload is ignored. The topic itself signals "new motion data available in Redis for this location."

## What The Agent Publishes

### Occupancy Analysis Results

**Topic Pattern**: `automation/context/occupancy/{location}`

**Examples**:
- `automation/context/occupancy/study`
- `automation/context/occupancy/kitchen`
- `automation/context/occupancy/bedroom`

**When Messages Are Sent**:
1. **Initial Motion Detection**: First motion in unknown room (immediate response)
2. **State Changes**: Room transitions from occupied to empty or vice versa
3. **Confidence Updates**: Periodic analysis confirms current state with sufficient confidence
4. **All Gates Pass**: Only publishes when confidence and timing requirements are met

**Message Quality**: Messages are only sent when the system has sufficient confidence and appropriate timing to prevent rapid oscillation.

## Message Integration Examples

### Go Code Examples

**Simple Occupancy Subscriber**:
```go
package main

import (
    "encoding/json"
    "fmt"
    "log"
    
    mqtt "github.com/eclipse/paho.mqtt.golang"
)

type OccupancyMessage struct {
    Source    string                 `json:"source"`
    Type      string                 `json:"type"`
    Location  string                 `json:"location"`
    State     string                 `json:"state"`
    Message   string                 `json:"message"`
    Data      OccupancyData         `json:"data"`
    Timestamp string                 `json:"timestamp"`
}

type OccupancyData struct {
    Occupied              bool    `json:"occupied"`
    Confidence           float64  `json:"confidence"`
    Reasoning            string   `json:"reasoning"`
    Method               string   `json:"method"`
    MinutesSinceMotion   float64  `json:"minutes_since_motion"`
}

func handleOccupancyMessage(client mqtt.Client, msg mqtt.Message) {
    var occupancy OccupancyMessage
    
    if err := json.Unmarshal(msg.Payload(), &occupancy); err != nil {
        log.Printf("Error parsing occupancy message: %v", err)
        return
    }
    
    fmt.Printf("Room %s is %s (confidence: %.2f)\n", 
        occupancy.Location, 
        occupancy.State, 
        occupancy.Data.Confidence)
    fmt.Printf("Reasoning: %s\n", occupancy.Data.Reasoning)
}

func main() {
    opts := mqtt.NewClientOptions().AddBroker("tcp://localhost:1883")
    client := mqtt.NewClient(opts)
    
    if token := client.Connect(); token.Wait() && token.Error() != nil {
        log.Fatal(token.Error())
    }
    
    // Subscribe to all occupancy updates
    client.Subscribe("automation/context/occupancy/+", 0, handleOccupancyMessage)
    
    // Keep running
    select {}
}
```

**Light Control Integration**:
```go
func handleOccupancyForLighting(client mqtt.Client, msg mqtt.Message) {
    var occupancy OccupancyMessage
    json.Unmarshal(msg.Payload(), &occupancy)
    
    // Only act on high-confidence decisions
    if occupancy.Data.Confidence < 0.7 {
        log.Printf("Ignoring low confidence occupancy update: %.2f", 
            occupancy.Data.Confidence)
        return
    }
    
    // Different behavior based on room and state
    switch occupancy.Location {
    case "study":
        if occupancy.Data.Occupied {
            // Turn on work lighting
            publishLightCommand(client, "study", "work_scene")
        } else {
            // Turn off after delay
            publishDelayedOff(client, "study", 300) // 5 minutes
        }
        
    case "kitchen":
        if occupancy.Data.Occupied {
            // Immediate bright lighting
            publishLightCommand(client, "kitchen", "bright")
        } else {
            // Quick off for pass-through areas
            publishDelayedOff(client, "kitchen", 60) // 1 minute
        }
    }
}

func publishLightCommand(client mqtt.Client, location, scene string) {
    topic := fmt.Sprintf("automation/command/light/%s", location)
    payload := fmt.Sprintf(`{"scene": "%s", "source": "occupancy"}`, scene)
    client.Publish(topic, 0, false, payload)
}
```

**Confidence-Based Automation**:
```go
func handleSmartAutomation(client mqtt.Client, msg mqtt.Message) {
    var occupancy OccupancyMessage
    json.Unmarshal(msg.Payload(), &occupancy)
    
    confidence := occupancy.Data.Confidence
    location := occupancy.Location
    
    // Adjust automation behavior based on confidence
    if confidence >= 0.9 {
        // High confidence - immediate action
        log.Printf("High confidence occupancy change in %s", location)
        executeImmediateAutomation(client, location, occupancy.Data.Occupied)
        
    } else if confidence >= 0.7 {
        // Medium confidence - delayed action
        log.Printf("Medium confidence occupancy change in %s", location)
        scheduleDelayedAutomation(client, location, occupancy.Data.Occupied, 30)
        
    } else {
        // Low confidence - log only
        log.Printf("Low confidence occupancy update in %s (%.2f): %s", 
            location, confidence, occupancy.Data.Reasoning)
    }
}
```

## Data Flow Architecture

### How Information Flows

1. **Motion Detection**:
   - Physical motion sensor triggers
   - Collector receives raw sensor data on `automation/raw/motion/{location}`
   - Collector stores timestamped events in Redis database
   - Collector publishes trigger on `automation/sensor/motion/{location}`

2. **Intelligent Analysis**:
   - Occupancy agent receives motion trigger
   - Agent queries Redis for motion history across multiple time windows
   - Agent runs temporal pattern analysis and LLM interpretation
   - Agent applies stabilization algorithms to prevent oscillation

3. **Decision Making**:
   - System evaluates confidence and timing gates
   - Only high-quality decisions are published
   - Result published on `automation/context/occupancy/{location}`

4. **Home Automation**:
   - Light agent, behavior agent, and other systems subscribe to occupancy
   - Each system interprets confidence and reasoning for its own needs
   - Coordinated automation based on reliable occupancy data

### Trigger-Based Architecture Benefits

**Efficiency**: Agents only analyze when new data is available
**Responsiveness**: Immediate triggers for entering rooms
**Reliability**: Periodic background analysis catches missed transitions
**Scalability**: System scales to many rooms without polling overhead

## Advanced Integration Patterns

### Multi-Room Coordination

```go
type RoomTracker struct {
    Rooms map[string]*RoomState
    mu    sync.RWMutex
}

type RoomState struct {
    Occupied   bool
    Confidence float64
    LastUpdate time.Time
    Reasoning  string
}

func (rt *RoomTracker) HandleOccupancy(msg OccupancyMessage) {
    rt.mu.Lock()
    defer rt.mu.Unlock()
    
    rt.Rooms[msg.Location] = &RoomState{
        Occupied:   msg.Data.Occupied,
        Confidence: msg.Data.Confidence,
        LastUpdate: time.Now(),
        Reasoning:  msg.Data.Reasoning,
    }
    
    // Implement house-wide logic
    rt.analyzeHouseOccupancy()
}

func (rt *RoomTracker) analyzeHouseOccupancy() {
    occupiedRooms := 0
    totalConfidence := 0.0
    
    for room, state := range rt.Rooms {
        if state.Occupied && state.Confidence > 0.7 {
            occupiedRooms++
            totalConfidence += state.Confidence
        }
    }
    
    if occupiedRooms == 0 {
        // House appears empty - activate away mode
        log.Println("House appears empty, activating away mode")
        // publishAwayMode()
    } else {
        log.Printf("House occupied: %d rooms with avg confidence %.2f", 
            occupiedRooms, totalConfidence/float64(occupiedRooms))
    }
}
```

### State History Tracking

```go
func trackOccupancyHistory(client mqtt.Client, msg mqtt.Message) {
    var occupancy OccupancyMessage
    json.Unmarshal(msg.Payload(), &occupancy)
    
    // Store in time-series database or log
    event := OccupancyEvent{
        Location:   occupancy.Location,
        Occupied:   occupancy.Data.Occupied,
        Confidence: occupancy.Data.Confidence,
        Reasoning:  occupancy.Data.Reasoning,
        Timestamp:  time.Now(),
    }
    
    // This enables:
    // - Behavioral pattern analysis
    // - System performance monitoring
    // - Debugging sensor issues
    // - Energy usage correlation
    storeOccupancyEvent(event)
}
```

## Message Quality and Reliability

### Confidence Interpretation Guide

**0.9 - 1.0**: Extremely confident
- Act immediately on these decisions
- Suitable for immediate lighting changes
- Can trigger security or energy-saving actions

**0.7 - 0.9**: High confidence
- Good for most automation decisions
- May want brief delay for state changes
- Suitable for most home automation actions

**0.5 - 0.7**: Moderate confidence  
- Use with caution for important actions
- Good for logging and monitoring
- May want longer delays or confirmation

**0.3 - 0.5**: Low confidence
- Primarily for debugging and analysis
- Avoid triggering important automation
- Indicates sensor boundary conditions

### Timing Considerations

**Immediate Publishing** (< 1 second):
- First motion detection in unknown room
- High-confidence state changes with clear patterns

**Delayed Publishing** (30-60 seconds):
- Complex pattern analysis requiring LLM
- Stabilization processing for uncertain cases

**No Publishing**:
- Low confidence predictions that don't meet thresholds
- Rapid changes blocked by timing gates
- System instability detected by stabilization algorithm

## Troubleshooting Integration Issues

### No Occupancy Messages

**Check Motion Data**:
```bash
# Verify motion data reaching Redis
redis-cli KEYS 'sensor:motion:*'
redis-cli ZRANGE sensor:motion:study 0 -1 WITHSCORES
```

**Check Agent Subscriptions**:
- Verify agent is subscribed to `automation/sensor/motion/+`
- Check MQTT connection status in agent logs
- Confirm collector is publishing triggers

### Delayed Occupancy Updates

**Normal Delays**:
- LLM analysis: 5-30 seconds depending on model
- Stabilization processing: Additional 1-5 seconds
- Pattern analysis: 1-2 seconds

**Excessive Delays**:
- Check LLM endpoint connectivity and performance
- Monitor Redis query performance
- Verify MQTT broker is not overloaded

### Inconsistent Occupancy Detection

**Common Causes**:
- Sensor placement at room boundaries
- Multiple sensors in overlapping coverage areas
- Pets or environmental factors triggering sensors

**Debugging**:
- Enable debug-level logging for full pattern data
- Monitor confidence trends over time
- Check stabilization activation in logs

### Missing State Changes

**Confidence Gates**:
- Check if predictions meet confidence thresholds
- Look for stabilization dampening in logs
- Verify system isn't in high-dampening mode

**Timing Gates**:
- Ensure 45+ seconds between state changes
- Check if rapid changes are being blocked
- Monitor time since last state change

## Future Sensor Integration

### Presence Sensor Support

The MQTT architecture is designed to support future sensor types:

**Potential Topics**:
- `automation/sensor/presence/{location}` - mmWave radar sensors
- `automation/sensor/environmental/{location}` - Temperature, humidity, CO2
- `automation/sensor/vision/{location}` - Computer vision analysis

**Unified Processing**:
- Same occupancy output topics and message format
- Enhanced confidence through sensor fusion
- Backward compatibility with existing automation

### Multi-Sensor Fusion

```go
// Future capability example
type SensorFusion struct {
    Motion      MotionData      `json:"motion"`
    Presence    PresenceData    `json:"presence"`
    Environment EnvironmentData `json:"environment"`
}

// Combined analysis would produce higher confidence
// and more nuanced occupancy detection
```

This integration guide provides the foundation for building reliable home automation using the occupancy agent's intelligent room detection capabilities.