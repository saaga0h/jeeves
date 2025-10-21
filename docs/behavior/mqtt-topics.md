# MQTT Integration Guide - Behavior Agent

The behavior agent integrates with the home automation system through MQTT messaging for consolidation triggers and behavioral insights publication.

## What The Agent Listens For

### Consolidation Triggers

**Topic**: `automation/behavior/consolidate`

**Purpose**: Triggers batch processing to detect episodes, vectors, and macro-episodes from sensor data

**Message Format**:
```json
{
  "lookback_hours": 2,
  "location": "universe"
}
```

**Fields**:
- `lookback_hours`: How far back to analyze sensor data (hours)
- `location`: Which location to analyze ("universe" = all locations)

**When To Trigger**:
- After scenario completion (testing)
- End of day for daily summaries
- Manual trigger for debugging/analysis
- Periodic trigger (e.g., every hour) for real-time insights

**How It Works**:
1. Agent receives consolidation trigger
2. Queries Redis for sensor data in time window
3. Detects episodes from motion/lighting events
4. Builds behavioral vectors from episode sequences
5. Uses LLM to consolidate into macro-episodes
6. Publishes completion notification

### Virtual Time Configuration (Testing)

**Topic**: `automation/test/time_config`

**Purpose**: Configures virtual time for testing and simulation

**Message Format**:
```json
{
  "virtual_start": "2025-10-17T07:00:00Z",
  "time_scale": 6
}
```

**Fields**:
- `virtual_start`: Simulated start time (ISO 8601 format)
- `time_scale`: Time acceleration factor (6 = 6x faster than real time)

**Use Cases**:
- Automated testing with compressed time
- Scenario replay with historical timestamps
- Deterministic test execution
- Development and validation

---

## What The Agent Publishes

### Consolidation Completion

**Topic**: `automation/behavior/consolidation/completed`

**Purpose**: Notifies other agents that consolidation has finished

**Message Format**:
```json
{
  "completed_at": "2025-10-17T07:50:24Z",
  "episodes_created": 7,
  "vectors_detected": 1,
  "macros_created": 1
}
```

**Fields**:
- `completed_at`: Timestamp when consolidation finished
- `episodes_created`: Number of micro-episodes detected
- `vectors_detected`: Number of behavioral vectors found
- `macros_created`: Number of macro-episodes consolidated

**Subscribers**:
- Observer agent (for visualization)
- Test framework (for validation)
- Future automation agents (for pattern-based rules)

### Episode Events (Future)

**Topic Pattern**: `automation/behavior/episode/{event_type}/{location}`

**Event Types**:
- `started` - New episode beginning
- `closed` - Episode ending
- `updated` - Episode details changed

**Use Cases**:
- Real-time behavioral monitoring
- Triggering automations when routines begin
- Alerting on unusual patterns

**Current Status**: Not yet implemented (consolidation only for now)

---

## Message Integration Examples

### Go Code Examples

**Triggering Consolidation**:
```go
package main

import (
    "encoding/json"
    "log"

    mqtt "github.com/eclipse/paho.mqtt.golang"
)

type ConsolidationTrigger struct {
    LookbackHours int    `json:"lookback_hours"`
    Location      string `json:"location"`
}

func triggerConsolidation(client mqtt.Client, hours int) error {
    trigger := ConsolidationTrigger{
        LookbackHours: hours,
        Location:      "universe",
    }

    payload, err := json.Marshal(trigger)
    if err != nil {
        return err
    }

    token := client.Publish(
        "automation/behavior/consolidate",
        0,  // QoS
        false,  // Retained
        payload,
    )

    token.Wait()
    return token.Error()
}
```

**Subscribing to Consolidation Completion**:
```go
package main

import (
    "encoding/json"
    "fmt"
    "log"

    mqtt "github.com/eclipse/paho.mqtt.golang"
)

type ConsolidationResult struct {
    CompletedAt     string `json:"completed_at"`
    EpisodesCreated int    `json:"episodes_created"`
    VectorsDetected int    `json:"vectors_detected"`
    MacrosCreated   int    `json:"macros_created"`
}

func handleConsolidationComplete(client mqtt.Client, msg mqtt.Message) {
    var result ConsolidationResult

    if err := json.Unmarshal(msg.Payload(), &result); err != nil {
        log.Printf("Error parsing consolidation result: %v", err)
        return
    }

    fmt.Printf("Consolidation completed at %s\n", result.CompletedAt)
    fmt.Printf("  Episodes: %d\n", result.EpisodesCreated)
    fmt.Printf("  Vectors:  %d\n", result.VectorsDetected)
    fmt.Printf("  Macros:   %d\n", result.MacrosCreated)
}

func main() {
    opts := mqtt.NewClientOptions()
    opts.AddBroker("tcp://localhost:1883")

    client := mqtt.NewClient(opts)
    if token := client.Connect(); token.Wait() && token.Error() != nil {
        log.Fatal(token.Error())
    }

    client.Subscribe(
        "automation/behavior/consolidation/completed",
        0,
        handleConsolidationComplete,
    )

    select {} // Wait forever
}
```

### Python Code Examples

**Triggering Consolidation**:
```python
import paho.mqtt.client as mqtt
import json

def trigger_consolidation(client, hours=2):
    payload = json.dumps({
        "lookback_hours": hours,
        "location": "universe"
    })

    client.publish(
        "automation/behavior/consolidate",
        payload,
        qos=0
    )
    print(f"Triggered consolidation for last {hours} hours")

# Setup
client = mqtt.Client()
client.connect("localhost", 1883)

# Trigger
trigger_consolidation(client, hours=2)
```

**Subscribing to Results**:
```python
import paho.mqtt.client as mqtt
import json

def on_consolidation_complete(client, userdata, msg):
    result = json.loads(msg.payload)

    print(f"Consolidation completed: {result['completed_at']}")
    print(f"  Episodes created: {result['episodes_created']}")
    print(f"  Vectors detected: {result['vectors_detected']}")
    print(f"  Macros created:   {result['macros_created']}")

# Setup
client = mqtt.Client()
client.on_message = on_consolidation_complete
client.connect("localhost", 1883)

client.subscribe("automation/behavior/consolidation/completed")
client.loop_forever()
```

---

## Topic Naming Conventions

### Input Topics (What Agent Subscribes To)

- `automation/behavior/*` - Behavior-specific commands
- `automation/test/*` - Testing and simulation controls

**Design Principles**:
- Action-oriented naming (consolidate, configure)
- Agent-specific namespace prevents cross-talk
- Test topics clearly separated from production

### Output Topics (What Agent Publishes)

- `automation/behavior/consolidation/*` - Consolidation lifecycle events
- `automation/behavior/episode/*` - Episode lifecycle events (future)
- `automation/behavior/vector/*` - Vector detection events (future)

**Design Principles**:
- Event-oriented naming (completed, started, detected)
- Hierarchical structure for filtering
- Rich event types for specialized subscribers

---

## Testing Integration

### E2E Test Scenario Example

```yaml
events:
  # ... sensor events ...

  # Trigger consolidation at end
  - time: 2990
    type: behavior
    location: universe
    data:
      action: consolidate
      lookback_hours: 2
    description: "Consolidation triggered after morning routine"

wait:
  # Wait for processing
  - time: 3040
    description: "Wait for consolidation to complete"

expectations:
  postgres:
    # Verify episodes created
    - time: 3040
      postgres_query: |
        SELECT COUNT(*) FROM behavioral_episodes
        WHERE started_at_text::timestamptz >= '2025-10-17T07:00:00Z'
      postgres_expected: 6
      description: "Six micro-episodes created"

    # Verify vectors detected
    - time: 3040
      postgres_query: "SELECT COUNT(*) FROM behavioral_vectors"
      postgres_expected: ">=1"
      description: "At least one behavioral vector detected"
```

---

## Quality of Service (QoS) Recommendations

### Consolidation Triggers
**QoS 0** (At most once)
- Consolidation is idempotent (safe to re-run)
- Missed trigger not critical (can trigger again)
- Reduces MQTT broker overhead

### Consolidation Results
**QoS 0** (At most once)
- Results stored in database (durable)
- Subscribers can query database if they miss message
- Not time-critical

### Future Episode Events
**QoS 1** (At least once) - Recommended
- Episode lifecycle important for automations
- Duplicates can be handled by episode ID
- Ensures critical behavioral events aren't lost

---

## Message Retention

### Current Implementation
**Retained Messages**: Not used
- Behavior agent state is in PostgreSQL
- New subscribers should query database
- MQTT only for real-time notifications

### Future Considerations
**Potential Retained Topics**:
- `automation/behavior/status` - Agent health and readiness
- `automation/behavior/config` - Current configuration
- Last consolidation result for late-joining subscribers

---

## Security Considerations

### Topic Access Control

**Consolidation Triggers**:
- Should be restricted to authorized agents/users
- Consolidation has computational cost
- Excessive triggers could impact performance

**Behavioral Data**:
- Contains personal behavior patterns
- Consider topic-level ACLs for privacy
- Encrypt MQTT connection (TLS) in production

### Data Privacy

**Behavioral Insights**:
- Episodes reveal presence patterns
- Vectors reveal routine sequences
- Consider anonymization for logging
- Secure database access controls

---

## Monitoring and Debugging

### Useful MQTT Subscriptions

**Monitor All Behavior Events**:
```
mosquitto_sub -t "automation/behavior/#" -v
```

**Monitor Consolidation Only**:
```
mosquitto_sub -t "automation/behavior/consolidation/#" -v
```

**Trigger Test Consolidation**:
```
mosquitto_pub -t "automation/behavior/consolidate" \
  -m '{"lookback_hours": 2, "location": "universe"}'
```

### Troubleshooting

**"Consolidation not triggering"**:
1. Check MQTT connection in agent logs
2. Verify topic subscription succeeded
3. Test with manual mosquitto_pub
4. Check JSON payload format

**"No completion message received"**:
1. Check consolidation logs for errors
2. Verify episodes were created in database
3. Look for MQTT publish errors in logs
4. Check subscriber is connected when message sent

**"Virtual time not working"**:
1. Verify time_config message sent before sensor data
2. Check time_config payload format
3. Review agent logs for "Test mode configured"
4. Ensure collector also receives time_config

---

## Future Enhancements

### Planned MQTT Topics

**Real-Time Episode Events**:
- `automation/behavior/episode/started/{location}`
- `automation/behavior/episode/closed/{location}`
- Enable real-time automation triggers

**Pattern Detection Alerts**:
- `automation/behavior/pattern/detected`
- `automation/behavior/anomaly/detected`
- Alert on unusual behavioral patterns

**Learning Updates**:
- `automation/behavior/routine/learned`
- `automation/behavior/routine/changed`
- Notify when new routines discovered or existing routines evolve

**Query API**:
- `automation/behavior/query/episode`
- `automation/behavior/query/vector`
- Request-response pattern for behavioral data access
