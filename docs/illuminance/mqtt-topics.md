# MQTT Topics Guide - Illuminance Agent

This guide explains the MQTT communication patterns for the Illuminance Agent and how to integrate with its topic structure.

## Topics the Agent Listens To

### Sensor Trigger Topics: `automation/sensor/illuminance/{location}`

The Illuminance Agent receives **trigger messages** that tell it when new sensor data is available for analysis.

**Examples of topics it listens to**:
- `automation/sensor/illuminance/living_room`
- `automation/sensor/illuminance/bedroom`  
- `automation/sensor/illuminance/study`
- `automation/sensor/illuminance/kitchen`

**What happens when a trigger arrives**:
1. Agent identifies the location from the topic (e.g., "living_room")
2. Retrieves historical sensor data for that location from Redis
3. Performs illuminance analysis and trend calculation
4. Publishes context message if state changed or enough time has passed

**Important**: The trigger message itself doesn't contain sensor data - it's just a notification. The agent reads actual sensor values from Redis storage.

---

## Topics the Agent Publishes To

### Context Topics: `automation/context/illuminance/{location}`

The Illuminance Agent publishes **analysis results** that other agents and automations can use.

**Examples of topics it publishes to**:
- `automation/context/illuminance/living_room`
- `automation/context/illuminance/bedroom`
- `automation/context/illuminance/study`
- `automation/context/illuminance/kitchen`

**When messages are published**:
- ✅ **State change**: Room lighting category changes (dark→dim, bright→moderate, etc.)
- ✅ **Periodic updates**: Every 5 minutes minimum, even if unchanged
- ✅ **New sensor data**: When triggered by fresh sensor readings

**When messages are NOT published**:
- ❌ State unchanged AND less than 5 minutes since last update
- ❌ Analysis fails due to missing data
- ❌ Agent is unhealthy or disconnected

---

## How to Use These Topics

### For Other Agents (Subscribing to Context)

If you're building automation that needs lighting context:

```go
package main

import (
    "encoding/json"
    "fmt"
    "log"
    "strings"
    mqtt "github.com/eclipse/paho.mqtt.golang"
)

type IlluminanceContext struct {
    Source    string `json:"source"`
    Type      string `json:"type"`
    Location  string `json:"location"`
    State     string `json:"state"`
    Message   string `json:"message"`
    Data      struct {
        CurrentLux int    `json:"current_lux"`
        Trend      string `json:"trend"`
        // ... other fields as needed
    } `json:"data"`
    Timestamp string `json:"timestamp"`
}

func setupIlluminanceSubscription(client mqtt.Client) {
    // Subscribe to all illuminance context
    client.Subscribe("automation/context/illuminance/+", 0, handleIlluminanceMessage)
    
    // Or subscribe to specific room only
    // client.Subscribe("automation/context/illuminance/living_room", 0, handleIlluminanceMessage)
}

func handleIlluminanceMessage(client mqtt.Client, msg mqtt.Message) {
    topic := msg.Topic()
    parts := strings.Split(topic, "/")
    location := parts[len(parts)-1] // Extract location
    
    var context IlluminanceContext
    if err := json.Unmarshal(msg.Payload(), &context); err != nil {
        log.Printf("Error parsing illuminance context: %v", err)
        return
    }
    
    // Use context for automation decisions
    if context.State == "dim" && context.Data.Trend == "dimming" {
        // Room is getting darker - maybe turn on lights
        fmt.Printf("%s is getting dim, consider lighting\n", location)
        // Trigger lighting automation here
    }
}
```

### For Publishing Sensor Data (Integration)

If you have illuminance sensors and want them analyzed:

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "time"
    
    mqtt "github.com/eclipse/paho.mqtt.golang"
    "github.com/go-redis/redis/v8"
)

var ctx = context.Background()

type SensorReading struct {
    Timestamp       string  `json:"timestamp"`
    Illuminance     float64 `json:"illuminance"`
    IlluminanceUnit string  `json:"illuminance_unit"`
}

type TriggerMessage struct {
    Source    string `json:"source"`
    Timestamp string `json:"timestamp"`
    Action    string `json:"action"`
}

func publishSensorData(redisClient *redis.Client, mqttClient mqtt.Client, location string, luxValue float64) error {
    // Step 1: Store sensor data (typically done by collector agent)
    timestamp := time.Now()
    
    reading := SensorReading{
        Timestamp:       timestamp.Format(time.RFC3339),
        Illuminance:     luxValue,
        IlluminanceUnit: "lux",
    }
    
    readingJSON, err := json.Marshal(reading)
    if err != nil {
        return fmt.Errorf("failed to marshal reading: %w", err)
    }
    
    // Store in Redis sorted set
    key := fmt.Sprintf("sensor:environmental:%s", location)
    score := float64(timestamp.UnixMilli())
    err = redisClient.ZAdd(ctx, key, &redis.Z{
        Score:  score,
        Member: string(readingJSON),
    }).Err()
    if err != nil {
        return fmt.Errorf("failed to store in Redis: %w", err)
    }
    
    // Step 2: Trigger analysis
    trigger := TriggerMessage{
        Source:    "your-sensor-system",
        Timestamp: timestamp.Format(time.RFC3339),
        Action:    "new_data",
    }
    
    triggerJSON, err := json.Marshal(trigger)
    if err != nil {
        return fmt.Errorf("failed to marshal trigger: %w", err)
    }
    
    topic := fmt.Sprintf("automation/sensor/illuminance/%s", location)
    token := mqttClient.Publish(topic, 0, false, triggerJSON)
    token.Wait()
    
    if token.Error() != nil {
        return fmt.Errorf("failed to publish trigger: %w", token.Error())
    }
    
    // Agent will analyze and publish context automatically
    return nil
}
```

---

## Topic Structure Explained

### Wildcard Patterns

The `+` wildcard matches any single level:

- `automation/sensor/illuminance/+` matches all sensor triggers
- `automation/context/illuminance/+` matches all context publications

### Location Extraction

Both trigger and context topics follow the same pattern:
```
automation/{type}/illuminance/{location}
```

Where:
- `{type}` is either "sensor" (trigger) or "context" (result)
- `{location}` is the room/area identifier

**Valid location examples**: `living_room`, `bedroom`, `study`, `kitchen`, `hallway`, `office`

### Message Flow Example

Here's a complete flow from sensor reading to context publication:

```
1. 📊 Sensor reading: 350 lux in living room
2. 💾 Collector stores in Redis: sensor:environmental:living_room
3. 📢 Collector publishes: automation/sensor/illuminance/living_room
4. 🔍 Illuminance Agent triggered, analyzes data
5. 📤 Agent publishes: automation/context/illuminance/living_room
   Content: {"state": "bright", "data": {...}}
6. 🏠 Other agents receive context and make decisions
```

---

## Integration Patterns

### For Home Assistant

```yaml
# automation.yaml
- alias: "Respond to dim lighting"
  trigger:
    platform: mqtt
    topic: automation/context/illuminance/+
  condition:
    condition: template
    value_template: "{{ trigger.payload_json.state == 'dim' }}"
  action:
    service: light.turn_on
    target:
      entity_id: "light.{{ trigger.topic.split('/')[-1] }}"
    data:
      brightness: 180
```

### For Node-RED

```json
[
  {
    "id": "mqtt-in",
    "type": "mqtt in",
    "topic": "automation/context/illuminance/+",
    "outputs": 1
  },
  {
    "id": "filter-dim",
    "type": "switch",
    "property": "payload.state",
    "rules": [
      {"t": "eq", "v": "dim"}
    ]
  }
]
```

### For Custom Applications

```go
package main

import (
    "encoding/json"
    "fmt"
    "log"
    "strings"
    "time"
    
    mqtt "github.com/eclipse/paho.mqtt.golang"
)

type IlluminanceData struct {
    CurrentLux int  `json:"current_lux"`
    IsDaytime  bool `json:"is_daytime"`
    // ... other fields as needed
}

type IlluminanceContext struct {
    State string          `json:"state"`
    Data  IlluminanceData `json:"data"`
    // ... other fields as needed
}

func main() {
    opts := mqtt.NewClientOptions()
    opts.AddBroker("tcp://localhost:1883")
    opts.SetClientID("illuminance-consumer")
    
    client := mqtt.NewClient(opts)
    if token := client.Connect(); token.Wait() && token.Error() != nil {
        log.Fatal(token.Error())
    }
    
    // Subscribe to all illuminance context
    client.Subscribe("automation/context/illuminance/+", 0, onIlluminanceContext)
    
    // Keep the program running
    select {}
}

func onIlluminanceContext(client mqtt.Client, msg mqtt.Message) {
    topic := msg.Topic()
    parts := strings.Split(topic, "/")
    location := parts[len(parts)-1]
    
    var context IlluminanceContext
    if err := json.Unmarshal(msg.Payload(), &context); err != nil {
        log.Printf("Error parsing context: %v", err)
        return
    }
    
    // Custom automation logic
    switch context.State {
    case "very_bright":
        fmt.Printf("%s is very bright - consider closing blinds\n", location)
        // Add automation logic here
        
    case "dark":
        if context.Data.IsDaytime {
            fmt.Printf("%s is dark during daytime - possible sensor issue\n", location)
            // Add alert logic here
        }
    }
}
```

---

## Troubleshooting MQTT Issues

### Agent Not Receiving Triggers

**Symptoms**:
- No context messages published
- Health check shows `mqtt_connected: false`

**Check**:
```bash
# Verify MQTT broker connectivity
mosquitto_sub -h your-broker -t 'automation/sensor/illuminance/+' -v

# Test manual trigger
mosquitto_pub -h your-broker -t 'automation/sensor/illuminance/test_room' -m '{"test": true}'
```

**Common fixes**:
- Verify MQTT broker address in agent configuration
- Check network connectivity
- Confirm topic permissions if using authentication

### Context Messages Not Appearing

**Symptoms**:
- Agent receives triggers but doesn't publish context
- Health check shows locations but no recent messages

**Check**:
```bash
# Monitor context publications
mosquitto_sub -h your-broker -t 'automation/context/illuminance/+' -v

# Check Redis for sensor data
redis-cli zrange sensor:environmental:living_room 0 -1 WITHSCORES
```

**Common fixes**:
- Verify Redis connection (check health endpoint)
- Ensure sensor data exists in Redis before triggering
- Check agent logs for analysis errors

### Wrong Location Names

**Symptoms**:
- Triggers for one room affect different room
- Context published to unexpected topics

**Solution**:
Ensure consistent location naming across:
- Sensor data storage: `sensor:environmental:{location}`
- Trigger topics: `automation/sensor/illuminance/{location}`
- Context topics: `automation/context/illuminance/{location}`

### Message Frequency Issues

**Too frequent**: If getting context messages too often, check if sensors are sending data faster than needed. Agent publishes immediately on state changes.

**Too infrequent**: If not getting periodic updates, verify agent is running and healthy. Should publish at least every 5 minutes per location.

---

## Topic Testing Commands

### Monitor All Illuminance Activity

```bash
# See all sensor triggers
mosquitto_sub -h localhost -t 'automation/sensor/illuminance/+' -v

# See all context publications  
mosquitto_sub -h localhost -t 'automation/context/illuminance/+' -v

# See everything illuminance-related
mosquitto_sub -h localhost -t 'automation/+/illuminance/+' -v
```

### Send Test Triggers

```bash
# Trigger analysis for living room
mosquitto_pub -h localhost -t 'automation/sensor/illuminance/living_room' \
  -m '{"source": "test", "timestamp": "2025-01-01T12:00:00Z"}'

# Test multiple rooms
for room in living_room bedroom study kitchen; do
  mosquitto_pub -h localhost -t "automation/sensor/illuminance/$room" \
    -m '{"source": "test"}'
done
```

### Validate Message Format

```bash
# Check context message structure
mosquitto_sub -h localhost -t 'automation/context/illuminance/+' | \
  jq '.data.current_lux, .state, .location'
```

This MQTT structure enables reliable, scalable illuminance monitoring across your entire smart home system.