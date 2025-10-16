# Message Examples and Testing Guide - Occupancy Agent

This guide shows the types of messages the occupancy agent publishes and provides testing approaches for validating the system.

## Input Triggers

The occupancy agent receives motion triggers from the collector agent. The payload content is ignored - the topic itself signals "new motion data available in Redis."

**Motion Trigger Examples**:
```
Topic: automation/sensor/motion/study
Topic: automation/sensor/motion/kitchen
Topic: automation/sensor/motion/bedroom
```

**Agent Response**: Extract location from topic, check for recent motion data in Redis, and trigger analysis if motion detected in last 2 minutes.

## Output Messages

The agent publishes structured occupancy analysis results to help downstream automation systems make informed decisions.

**Topic Pattern**: `automation/context/occupancy/{location}`

### Go Message Structures

```go
type OccupancyMessage struct {
    Source    string        `json:"source"`
    Type      string        `json:"type"`
    Location  string        `json:"location"`
    State     string        `json:"state"`
    Message   string        `json:"message"`
    Data      OccupancyData `json:"data"`
    Timestamp string        `json:"timestamp"`
}

type OccupancyData struct {
    Occupied            bool    `json:"occupied"`
    Confidence          float64 `json:"confidence"`
    Reasoning           string  `json:"reasoning"`
    Method              string  `json:"method"`
    MinutesSinceMotion  float64 `json:"minutes_since_motion"`
    MotionLast2Min      *int    `json:"motion_last_2min,omitempty"`
    MotionLast8Min      *int    `json:"motion_last_8min,omitempty"`
}
```

## Message Examples

### Initial Motion Detection

**Scenario**: First motion detected in a room

```json
{
  "source": "temporal-occupancy-agent",
  "type": "occupancy",
  "location": "study",
  "state": "occupied",
  "message": "Room occupied (first motion detected)",
  "data": {
    "occupied": true,
    "confidence": 0.9,
    "reasoning": "First motion event - assuming entry",
    "method": "initial_motion",
    "minutes_since_motion": 0
  },
  "timestamp": "2024-01-01T12:00:00.123Z"
}
```

**Integration Usage**:
```go
func handleInitialMotion(msg OccupancyMessage) {
    if msg.Data.Method == "initial_motion" {
        // High confidence entry - turn on lights immediately
        activateWelcomeLighting(msg.Location)
        
        // Start monitoring for settling behavior
        scheduleSettlingCheck(msg.Location, 5*time.Minute)
    }
}
```

### Active Presence Detection

**Scenario**: Person actively moving around in room

```json
{
  "source": "temporal-occupancy-agent",
  "type": "occupancy",
  "location": "kitchen",
  "state": "occupied",
  "message": "Room is occupied (confidence: 0.85)",
  "data": {
    "occupied": true,
    "confidence": 0.85,
    "reasoning": "Multiple recent motions indicate person actively present",
    "method": "vonich_hakim_stabilized",
    "minutes_since_motion": 1.2,
    "motion_last_2min": 2,
    "motion_last_8min": 3
  },
  "timestamp": "2024-01-01T12:00:30.456Z"
}
```

**Integration Usage**:
```go
func handleActivePresence(msg OccupancyMessage) {
    if msg.Data.MotionLast2Min != nil && *msg.Data.MotionLast2Min >= 2 {
        // Person actively moving - keep bright lighting
        maintainBrightLighting(msg.Location)
        
        // Cancel any scheduled dim/off actions
        cancelScheduledLightingChanges(msg.Location)
    }
}
```

### Person Settled (Working/Reading)

**Scenario**: Person entered room and is now sitting still

```json
{
  "source": "temporal-occupancy-agent",
  "type": "occupancy",
  "location": "study",
  "state": "occupied",
  "message": "Room is occupied (confidence: 0.75)",
  "data": {
    "occupied": true,
    "confidence": 0.75,
    "reasoning": "Multiple motions in recent past, now quiet - person likely settled (working/reading)",
    "method": "vonich_hakim_stabilized",
    "minutes_since_motion": 4.5,
    "motion_last_2min": 0,
    "motion_last_8min": 4
  },
  "timestamp": "2024-01-01T12:05:00.789Z"
}
```

**Integration Usage**:
```go
func handleSettledPresence(msg OccupancyMessage) {
    // Check for settling pattern
    if msg.Data.MotionLast2Min != nil && *msg.Data.MotionLast2Min == 0 &&
       msg.Data.MotionLast8Min != nil && *msg.Data.MotionLast8Min >= 3 {
        
        // Person settled - adjust to work lighting
        setWorkLighting(msg.Location)
        
        // Extend timeout for this room type
        if msg.Location == "study" || msg.Location == "office" {
            setOccupancyTimeout(msg.Location, 30*time.Minute)
        }
    }
}
```

### Pass-Through Detection

**Scenario**: Person walked through room but didn't stay

```json
{
  "source": "temporal-occupancy-agent",
  "type": "occupancy",
  "location": "hallway",
  "state": "empty",
  "message": "Room is empty (confidence: 0.8)",
  "data": {
    "occupied": false,
    "confidence": 0.8,
    "reasoning": "Single motion event 7 minutes ago - pass-through detected",
    "method": "vonich_hakim_stabilized",
    "minutes_since_motion": 7.2,
    "motion_last_2min": 0,
    "motion_last_8min": 1
  },
  "timestamp": "2024-01-01T12:07:00.321Z"
}
```

**Integration Usage**:
```go
func handlePassThrough(msg OccupancyMessage) {
    // Check for pass-through pattern
    if !msg.Data.Occupied && 
       msg.Data.MotionLast8Min != nil && *msg.Data.MotionLast8Min == 1 &&
       msg.Data.MinutesSinceMotion > 5 {
        
        // Quick pass-through - turn off lights soon
        scheduleQuickOff(msg.Location, 1*time.Minute)
        
        // Don't affect other room states
        log.Printf("Pass-through detected in %s", msg.Location)
    }
}
```

### Extended Absence

**Scenario**: Room empty for extended period

```json
{
  "source": "temporal-occupancy-agent",
  "type": "occupancy",
  "location": "study",
  "state": "empty",
  "message": "Room is empty (confidence: 0.9)",
  "data": {
    "occupied": false,
    "confidence": 0.9,
    "reasoning": "No motion for 15+ minutes - extended absence",
    "method": "vonich_hakim_stabilized",
    "minutes_since_motion": 15.8,
    "motion_last_2min": 0,
    "motion_last_8min": 0
  },
  "timestamp": "2024-01-01T12:15:00.654Z"
}
```

**Integration Usage**:
```go
func handleExtendedAbsence(msg OccupancyMessage) {
    if !msg.Data.Occupied && msg.Data.MinutesSinceMotion > 15 {
        // Very confident room is empty
        turnOffAllLights(msg.Location)
        
        // Consider energy saving measures
        if msg.Data.Confidence >= 0.9 {
            activateEnergySaving(msg.Location)
        }
        
        // Update house-wide occupancy tracking
        updateHouseOccupancyState()
    }
}
```

### Stabilization Applied

**Scenario**: System detected instability and applied dampening

```json
{
  "source": "temporal-occupancy-agent",
  "type": "occupancy",
  "location": "bedroom",
  "state": "occupied",
  "message": "Room is occupied (confidence: 0.68)",
  "data": {
    "occupied": true,
    "confidence": 0.68,
    "reasoning": "Recent motion indicates presence (V-H stabilization: moderate_dampening)",
    "method": "immediate_vonich_hakim_analysis",
    "minutes_since_motion": 2.1,
    "motion_last_2min": 1,
    "motion_last_8min": 2
  },
  "timestamp": "2024-01-01T14:00:00.987Z"
}
```

**Integration Usage**:
```go
func handleStabilizedPrediction(msg OccupancyMessage) {
    // Check if stabilization was applied
    if strings.Contains(msg.Data.Reasoning, "stabilization") {
        log.Printf("Stabilization applied in %s - sensor may be at boundary", msg.Location)
        
        // Be more conservative with automation
        if msg.Data.Confidence < 0.7 {
            // Wait for higher confidence before major changes
            log.Printf("Low confidence with stabilization - delaying automation")
            return
        }
    }
    
    // Proceed with normal automation
    handleOccupancyChange(msg)
}
```

## Confidence-Based Automation Patterns

### Go Implementation Examples

**Confidence Thresholds**:
```go
func processOccupancyMessage(msg OccupancyMessage) {
    confidence := msg.Data.Confidence
    
    switch {
    case confidence >= 0.9:
        // Extremely confident - immediate action
        handleHighConfidenceUpdate(msg)
        
    case confidence >= 0.7:
        // High confidence - normal automation
        handleNormalConfidenceUpdate(msg)
        
    case confidence >= 0.5:
        // Medium confidence - delayed or conservative action
        handleMediumConfidenceUpdate(msg)
        
    default:
        // Low confidence - log only, no automation
        log.Printf("Low confidence occupancy update: %s in %s (%.2f)", 
            msg.State, msg.Location, confidence)
    }
}

func handleHighConfidenceUpdate(msg OccupancyMessage) {
    // Immediate lighting changes
    if msg.Data.Occupied {
        setLightingScene(msg.Location, "active")
    } else {
        scheduleLightingOff(msg.Location, 1*time.Minute)
    }
    
    // Update security systems
    updateSecurityState(msg.Location, msg.Data.Occupied)
    
    // Trigger other automation
    triggerImmediateAutomation(msg)
}

func handleMediumConfidenceUpdate(msg OccupancyMessage) {
    // More conservative approach
    if msg.Data.Occupied {
        // Gradual lighting changes
        scheduleGradualLightingIncrease(msg.Location, 30*time.Second)
    } else {
        // Longer delay before turning off
        scheduleLightingOff(msg.Location, 5*time.Minute)
    }
    
    // Don't trigger security changes
    log.Printf("Medium confidence automation applied in %s", msg.Location)
}
```

**Room-Specific Handling**:
```go
func handleLocationSpecificAutomation(msg OccupancyMessage) {
    switch msg.Location {
    case "kitchen":
        handleKitchenOccupancy(msg)
    case "study", "office":
        handleWorkspaceOccupancy(msg)
    case "bedroom":
        handleBedroomOccupancy(msg)
    case "hallway", "entryway":
        handleTransitOccupancy(msg)
    default:
        handleGenericOccupancy(msg)
    }
}

func handleWorkspaceOccupancy(msg OccupancyMessage) {
    if msg.Data.Occupied {
        // Work lighting for productivity
        setLightingScene(msg.Location, "work")
        
        // Longer timeout for settling behavior
        if strings.Contains(msg.Data.Reasoning, "settled") {
            setOccupancyTimeout(msg.Location, 45*time.Minute)
        }
    } else {
        // Longer delay before turning off work lights
        scheduleLightingOff(msg.Location, 10*time.Minute)
    }
}

func handleTransitOccupancy(msg OccupancyMessage) {
    if msg.Data.Occupied {
        // Bright lighting for navigation
        setLightingScene(msg.Location, "bright")
        
        // Short timeout for transit areas
        setOccupancyTimeout(msg.Location, 2*time.Minute)
    } else {
        // Quick off for pass-through areas
        scheduleLightingOff(msg.Location, 30*time.Second)
    }
}
```

## Testing Strategies

### Unit Testing

**Test Message Parsing**:
```go
func TestMessageParsing(t *testing.T) {
    jsonMessage := `{
        "source": "temporal-occupancy-agent",
        "type": "occupancy",
        "location": "study",
        "state": "occupied",
        "message": "Room is occupied (confidence: 0.85)",
        "data": {
            "occupied": true,
            "confidence": 0.85,
            "reasoning": "Active motion detected",
            "method": "vonich_hakim_stabilized",
            "minutes_since_motion": 1.2,
            "motion_last_2min": 2
        },
        "timestamp": "2024-01-01T12:00:00.000Z"
    }`
    
    var msg OccupancyMessage
    err := json.Unmarshal([]byte(jsonMessage), &msg)
    
    assert.NoError(t, err)
    assert.Equal(t, "study", msg.Location)
    assert.True(t, msg.Data.Occupied)
    assert.Equal(t, 0.85, msg.Data.Confidence)
    assert.Equal(t, "vonich_hakim_stabilized", msg.Data.Method)
    assert.NotNil(t, msg.Data.MotionLast2Min)
    assert.Equal(t, 2, *msg.Data.MotionLast2Min)
}
```

**Test Confidence Handling**:
```go
func TestConfidenceBasedAutomation(t *testing.T) {
    testCases := []struct {
        confidence float64
        expected   AutomationLevel
    }{
        {0.95, HighConfidence},
        {0.75, NormalConfidence},
        {0.55, MediumConfidence},
        {0.35, LowConfidence},
    }
    
    for _, tc := range testCases {
        msg := OccupancyMessage{
            Data: OccupancyData{
                Confidence: tc.confidence,
                Occupied:   true,
            },
        }
        
        level := determineAutomationLevel(msg)
        assert.Equal(t, tc.expected, level)
    }
}
```

### Integration Testing

**Mock MQTT Testing**:
```go
func TestOccupancyMessageHandling(t *testing.T) {
    // Setup mock MQTT client
    mockClient := &MockMQTTClient{}
    handler := NewOccupancyHandler(mockClient)
    
    // Test message
    msg := OccupancyMessage{
        Location: "test_room",
        State:    "occupied",
        Data: OccupancyData{
            Occupied:   true,
            Confidence: 0.8,
            Method:     "initial_motion",
        },
    }
    
    // Process message
    err := handler.ProcessOccupancyMessage(msg)
    assert.NoError(t, err)
    
    // Verify automation was triggered
    assert.True(t, mockClient.WasLightCommandSent("test_room"))
}
```

**Real System Testing**:
```go
func TestRealOccupancyFlow(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test")
    }
    
    // Connect to real MQTT broker
    client := connectToMQTT(t)
    defer client.Disconnect(1000)
    
    // Set up message capture
    var receivedMessages []OccupancyMessage
    client.Subscribe("automation/context/occupancy/+", 0, func(client mqtt.Client, msg mqtt.Message) {
        var occupancy OccupancyMessage
        if err := json.Unmarshal(msg.Payload(), &occupancy); err == nil {
            receivedMessages = append(receivedMessages, occupancy)
        }
    })
    
    // Trigger motion event
    client.Publish("automation/sensor/motion/test_room", 0, false, "")
    
    // Wait for response
    time.Sleep(10 * time.Second)
    
    // Verify occupancy message received
    assert.Greater(t, len(receivedMessages), 0)
    
    if len(receivedMessages) > 0 {
        msg := receivedMessages[0]
        assert.Equal(t, "test_room", msg.Location)
        assert.Equal(t, "temporal-occupancy-agent", msg.Source)
        assert.Contains(t, []string{"occupied", "empty"}, msg.State)
    }
}
```

### Manual Testing Scenarios

**Test Scenario 1 - Office Work Session**:
```bash
# 1. Trigger initial motion
mosquitto_pub -t 'automation/sensor/motion/study' -m ''

# Expected: Immediate "occupied" message with initial_motion method

# 2. Wait 5 minutes (simulate working at desk)
sleep 300

# Expected: "occupied" message with settling pattern reasoning

# 3. Wait 30 minutes without motion
sleep 1800

# Expected: "empty" message with extended absence reasoning
```

**Test Scenario 2 - Kitchen Pass-Through**:
```bash
# 1. Trigger motion
mosquitto_pub -t 'automation/sensor/motion/kitchen' -m ''

# 2. Wait 7 minutes without additional motion
sleep 420

# Expected: "empty" message with pass-through pattern
```

**Monitoring During Tests**:
```bash
# Monitor all occupancy messages
mosquitto_sub -t 'automation/context/occupancy/+' -v

# Monitor specific room
mosquitto_sub -t 'automation/context/occupancy/study' -v
```

## Troubleshooting Message Issues

### Common Problems

**No Occupancy Messages Published**:
```go
func debugNoMessages(location string) {
    // Check if motion data exists in Redis
    rdb := connectToRedis()
    
    count, err := rdb.ZCard(context.Background(), 
        fmt.Sprintf("sensor:motion:%s", location)).Result()
    if err != nil {
        log.Printf("Redis error: %v", err)
        return
    }
    
    if count == 0 {
        log.Printf("No motion data for %s - check collector agent", location)
        return
    }
    
    log.Printf("Motion data exists (%d events) - check occupancy agent logs", count)
}
```

**Low Confidence Messages**:
- Check sensor placement (boundary conditions)
- Review recent prediction history for oscillation
- Monitor stabilization activation in agent logs

**Delayed Messages**:
- Verify LLM endpoint connectivity
- Check Redis query performance
- Monitor MQTT broker load

**Inconsistent Confidence**:
- Look for stabilization dampening in reasoning
- Check for environmental factors (pets, wind)
- Review time-of-day patterns

This message guide provides the foundation for integrating with the occupancy agent's intelligent room detection and building reliable automation systems.