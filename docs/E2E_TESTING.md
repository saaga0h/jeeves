# E2E Testing Framework for J.E.E.V.E.S. Agents

## Core Philosophy

**Test complete user scenarios, not individual agents**. Examples:
- "Person walks through hallway" 
- "Person sits down to work in study"
- "Person leaves bedroom in morning"

Each scenario is a **time-series of sensor events** with **expected outcomes** at each agent layer.

## Architecture Overview

```
test-scenarios/
  ├── hallway_passthrough.yaml      # Scenario definition
  ├── study_working.yaml
  └── bedroom_morning.yaml

e2e/
  ├── docker-compose.test.yml       # Infrastructure + agents
  ├── test_runner.go                # Orchestrates tests
  ├── mqtt_observer.go              # Captures all MQTT traffic
  ├── redis_checker.go              # Validates Redis state
  └── timeline_reporter.go          # Human-readable output
```

## Scenario Definition Format (YAML)

```yaml
# test-scenarios/hallway_passthrough.yaml
name: "Hallway Pass-Through"
description: "Single motion event, person walks through, doesn't linger"

setup:
  location: "hallway"
  initial_state:
    occupancy: null
    last_motion: null

# Time-series of sensor events (relative time in seconds)
events:
  - time: 0
    sensor: "motion:hallway-sensor-1"
    value: true
    description: "Person enters hallway"

# Wait periods with no events
wait:
  - time: 120  # 2 minutes
    description: "No further motion detected"

# Expected outcomes at each layer
expectations:
  collector:
    - time: 0
      topic: "sensor/motion/hallway-sensor-1"
      payload:
        sensorId: "hallway-sensor-1"
        sensorType: "motion"
        value: true
    
  redis_storage:
    - time: 1
      key: "sensor:motion:hallway-sensor-1"
      field: "value"
      expected: "true"
    
  occupancy_decision:
    - time: 180  # After 3 min periodic check
      topic: "occupancy/status/hallway"
      payload:
        location: "hallway"
        occupied: false  # Pass-through detected
        confidence: ">0.7"
        reasoning: "~Single motion|pass.*through~"  # Regex match
```

```yaml
# test-scenarios/study_working.yaml
name: "Study - Person Working"
description: "Multiple motions, then settling in to work"

setup:
  location: "study"
  initial_state:
    occupancy: null

events:
  - time: 0
    sensor: "motion:study-sensor-1"
    value: true
    description: "Enter room"
  
  - time: 30
    sensor: "motion:study-sensor-1"
    value: true
    description: "Moving around"
  
  - time: 60
    sensor: "motion:study-sensor-1"
    value: true
    description: "Sitting down at desk"

wait:
  - time: 480  # 8 minutes quiet
    description: "Working quietly at desk"

expectations:
  occupancy_decision:
    - time: 90  # After 3rd motion
      topic: "occupancy/status/study"
      payload:
        occupied: true
        confidence: ">0.7"
        reasoning: "~settling.*in|multiple.*motion~"
    
    - time: 480  # Still occupied after quiet period
      topic: "occupancy/status/study"
      payload:
        occupied: true  # Should remain occupied
        confidence: ">0.6"
```

## Docker Compose Test Environment

```yaml
# e2e/docker-compose.test.yml
version: '3.8'

services:
  # Infrastructure
  mosquitto:
    image: eclipse-mosquitto:2
    ports:
      - "1883:1883"
    volumes:
      - ./mosquitto.conf:/mosquitto/config/mosquitto.conf
    healthcheck:
      test: ["CMD", "mosquitto_pub", "-t", "test", "-m", "health"]
      interval: 5s
      timeout: 3s
      retries: 3

  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 3s
      retries: 3

  # Test observer - captures all MQTT traffic
  mqtt_observer:
    build:
      context: .
      dockerfile: Dockerfile.observer
    depends_on:
      mosquitto:
        condition: service_healthy
    environment:
      MQTT_BROKER: "mosquitto:1883"
      REDIS_HOST: "redis:6379"
    volumes:
      - ./test-output:/output

  # J.E.E.V.E.S. Agents
  collector:
    build:
      context: ..
      dockerfile: cmd/collector/Dockerfile
    depends_on:
      - mosquitto
      - redis
    environment:
      MQTT_BROKER: "mosquitto:1883"
      REDIS_HOST: "redis:6379"
      LOG_LEVEL: "debug"

  illuminance-agent:
    build:
      context: ..
      dockerfile: cmd/illuminance-agent/Dockerfile
    depends_on:
      - mosquitto
      - redis
      - collector

  light-agent:
    build:
      context: ..
      dockerfile: cmd/light-agent/Dockerfile
    depends_on:
      - mosquitto
      - redis

  occupancy-agent:
    build:
      context: ..
      dockerfile: cmd/occupancy-agent/Dockerfile
    depends_on:
      - mosquitto
      - redis
      - collector
    environment:
      MQTT_BROKER: "mosquitto:1883"
      REDIS_HOST: "redis:6379"
      LLM_URL: "http://host.docker.internal:11434"  # Ollama on host
      LLM_MODEL: "deepseek-coder:6.7b"
      ANALYSIS_INTERVAL: "60s"  # Faster for testing

  # Test runner - orchestrates scenarios
  test-runner:
    build:
      context: .
      dockerfile: Dockerfile.test-runner
    depends_on:
      - mosquitto
      - redis
      - collector
      - occupancy-agent
    volumes:
      - ../test-scenarios:/scenarios
      - ./test-output:/output
    environment:
      MQTT_BROKER: "mosquitto:1883"
      REDIS_HOST: "redis:6379"
    command: ["--scenario", "/scenarios/hallway_passthrough.yaml"]
```

## MQTT Observer (Passive Traffic Capture)

```go
// e2e/mqtt_observer.go
package main

import (
    "encoding/json"
    "fmt"
    "os"
    "time"
    mqtt "github.com/eclipse/paho.mqtt.golang"
)

type CapturedMessage struct {
    Timestamp time.Time   `json:"timestamp"`
    Topic     string      `json:"topic"`
    Payload   interface{} `json:"payload"`
    QoS       byte        `json:"qos"`
}

type Observer struct {
    client   mqtt.Client
    messages []CapturedMessage
    startTime time.Time
}

func NewObserver(broker string) *Observer {
    opts := mqtt.NewClientOptions().AddBroker(broker)
    opts.SetClientID("e2e-observer")
    
    client := mqtt.NewClient(opts)
    if token := client.Connect(); token.Wait() && token.Error() != nil {
        panic(token.Error())
    }
    
    return &Observer{
        client:    client,
        messages:  make([]CapturedMessage, 0),
        startTime: time.Now(),
    }
}

func (o *Observer) Start() {
    // Subscribe to ALL topics
    o.client.Subscribe("#", 0, func(client mqtt.Client, msg mqtt.Message) {
        var payload interface{}
        json.Unmarshal(msg.Payload(), &payload)
        
        captured := CapturedMessage{
            Timestamp: time.Now(),
            Topic:     msg.Topic(),
            Payload:   payload,
            QoS:       msg.Qos(),
        }
        
        o.messages = append(o.messages, captured)
        
        // Real-time logging
        elapsed := time.Since(o.startTime).Seconds()
        fmt.Printf("[%6.2fs] %s: %s\n", 
            elapsed, 
            msg.Topic(), 
            string(msg.Payload()),
        )
    })
}

func (o *Observer) SaveCapture(filename string) error {
    f, err := os.Create(filename)
    if err != nil {
        return err
    }
    defer f.Close()
    
    encoder := json.NewEncoder(f)
    encoder.SetIndent("", "  ")
    return encoder.Encode(o.messages)
}

func (o *Observer) GetMessagesByTopic(topic string) []CapturedMessage {
    matches := make([]CapturedMessage, 0)
    for _, msg := range o.messages {
        if msg.Topic == topic {
            matches = append(matches, msg)
        }
    }
    return matches
}
```

## Test Runner (Orchestrates Scenarios)

```go
// e2e/test_runner.go
package main

import (
    "fmt"
    "time"
    mqtt "github.com/eclipse/paho.mqtt.golang"
    "gopkg.in/yaml.v3"
)

type TestScenario struct {
    Name        string                 `yaml:"name"`
    Description string                 `yaml:"description"`
    Setup       SetupConfig            `yaml:"setup"`
    Events      []SensorEvent          `yaml:"events"`
    Wait        []WaitPeriod           `yaml:"wait"`
    Expectations map[string][]Expectation `yaml:"expectations"`
}

type SensorEvent struct {
    Time        int         `yaml:"time"`
    Sensor      string      `yaml:"sensor"`
    Value       interface{} `yaml:"value"`
    Description string      `yaml:"description"`
}

type Expectation struct {
    Time    int                    `yaml:"time"`
    Topic   string                 `yaml:"topic"`
    Payload map[string]interface{} `yaml:"payload"`
}

func RunScenario(scenario *TestScenario, broker string, observer *Observer) *TestReport {
    client := connectMQTT(broker)
    report := NewTestReport(scenario.Name)
    
    fmt.Printf("\n=== Running Scenario: %s ===\n", scenario.Name)
    fmt.Printf("%s\n\n", scenario.Description)
    
    startTime := time.Now()
    
    // Play events
    for _, event := range scenario.Events {
        // Wait until event time
        waitUntil := startTime.Add(time.Duration(event.Time) * time.Second)
        time.Sleep(time.Until(waitUntil))
        
        elapsed := time.Since(startTime).Seconds()
        fmt.Printf("[%6.2fs] EVENT: %s → %v (%s)\n",
            elapsed, event.Sensor, event.Value, event.Description)
        
        // Publish sensor event
        topic := fmt.Sprintf("sensor/%s", event.Sensor)
        payload := map[string]interface{}{
            "sensor": event.Sensor,
            "value":  event.Value,
            "time":   time.Now().Unix(),
        }
        publishJSON(client, topic, payload)
    }
    
    // Wait periods
    for _, wait := range scenario.Wait {
        waitUntil := startTime.Add(time.Duration(wait.Time) * time.Second)
        remaining := time.Until(waitUntil)
        
        if remaining > 0 {
            elapsed := time.Since(startTime).Seconds()
            fmt.Printf("[%6.2fs] WAIT: %s (%.0fs)\n",
                elapsed, wait.Description, remaining.Seconds())
            time.Sleep(remaining)
        }
    }
    
    // Check expectations
    fmt.Printf("\n=== Checking Expectations ===\n")
    
    for layer, expectations := range scenario.Expectations {
        fmt.Printf("\nLayer: %s\n", layer)
        
        for _, expect := range expectations {
            waitUntil := startTime.Add(time.Duration(expect.Time) * time.Second)
            time.Sleep(time.Until(waitUntil))
            
            // Find matching message in observer
            messages := observer.GetMessagesByTopic(expect.Topic)
            
            if len(messages) == 0 {
                report.AddFailure(layer, fmt.Sprintf(
                    "No messages on topic %s", expect.Topic))
                continue
            }
            
            // Check latest message
            latest := messages[len(messages)-1]
            
            if matchesExpectation(latest.Payload, expect.Payload) {
                report.AddSuccess(layer, expect.Topic)
                fmt.Printf("  ✓ %s: %v\n", expect.Topic, latest.Payload)
            } else {
                report.AddFailure(layer, fmt.Sprintf(
                    "Mismatch on %s: expected %v, got %v",
                    expect.Topic, expect.Payload, latest.Payload))
                fmt.Printf("  ✗ %s: expected %v, got %v\n",
                    expect.Topic, expect.Payload, latest.Payload)
            }
        }
    }
    
    return report
}
```

## Timeline Reporter (Human-Readable Output)

```go
// e2e/timeline_reporter.go
package main

import (
    "fmt"
    "time"
)

type TestReport struct {
    ScenarioName string
    StartTime    time.Time
    Duration     time.Duration
    Events       []ReportEvent
}

type ReportEvent struct {
    Timestamp   time.Time
    Layer       string
    EventType   string  // "sensor", "mqtt", "redis", "expectation"
    Description string
    Status      string  // "info", "success", "failure"
}

func (r *TestReport) GenerateTimeline() string {
    output := fmt.Sprintf("\n╔══════════════════════════════════════════════════════════╗\n")
    output += fmt.Sprintf("║  Scenario: %-45s ║\n", r.ScenarioName)
    output += fmt.Sprintf("║  Duration: %-45s ║\n", r.Duration)
    output += fmt.Sprintf("╚══════════════════════════════════════════════════════════╝\n\n")
    
    for _, event := range r.Events {
        elapsed := event.Timestamp.Sub(r.StartTime).Seconds()
        
        var icon string
        switch event.Status {
        case "success":
            icon = "✓"
        case "failure":
            icon = "✗"
        default:
            icon = "→"
        }
        
        output += fmt.Sprintf("[%6.2fs] %s %-15s: %s\n",
            elapsed, icon, event.Layer, event.Description)
    }
    
    return output
}

func (r *TestReport) GenerateSummary() string {
    successes := 0
    failures := 0
    
    for _, event := range r.Events {
        switch event.Status {
        case "success":
            successes++
        case "failure":
            failures++
        }
    }
    
    output := "\n╔══════════════════════════════════════════════════════════╗\n"
    output += fmt.Sprintf("║  SUMMARY                                                 ║\n")
    output += fmt.Sprintf("║  Passed: %-3d                                            ║\n", successes)
    output += fmt.Sprintf("║  Failed: %-3d                                            ║\n", failures)
    
    if failures == 0 {
        output += "║  Status: ✓ ALL TESTS PASSED                              ║\n"
    } else {
        output += "║  Status: ✗ SOME TESTS FAILED                             ║\n"
    }
    
    output += "╚══════════════════════════════════════════════════════════╝\n"
    
    return output
}
```

## Running Tests

```bash
# Build all agents
make build-all

# Run single scenario
cd e2e
docker-compose -f docker-compose.test.yml run test-runner \
  --scenario /scenarios/hallway_passthrough.yaml

# Run all scenarios
for scenario in ../test-scenarios/*.yaml; do
  docker-compose -f docker-compose.test.yml run test-runner \
    --scenario /scenarios/$(basename $scenario)
done

# Clean up
docker-compose -f docker-compose.test.yml down -v
```

## Example Test Output

```
=== Running Scenario: Hallway Pass-Through ===
Single motion event, person walks through, doesn't linger

[  0.00s] EVENT: motion:hallway-sensor-1 → true (Person enters hallway)
[  0.05s] sensor/motion/hallway-sensor-1: {"sensor":"hallway-sensor-1","value":true}
[  0.12s] collector/processed: {"sensorId":"hallway-sensor-1","location":"hallway"}
[  0.12s] WAIT: No further motion detected (120.0s)
[120.00s] occupancy/status/hallway: {"location":"hallway","occupied":false,"confidence":0.75}

=== Checking Expectations ===

Layer: collector
  ✓ sensor/motion/hallway-sensor-1: {sensor:hallway-sensor-1 value:true}

Layer: redis_storage
  ✓ sensor:motion:hallway-sensor-1: value=true

Layer: occupancy_decision
  ✓ occupancy/status/hallway: {location:hallway occupied:false confidence:0.75}

╔══════════════════════════════════════════════════════════╗
║  SUMMARY                                                 ║
║  Passed: 3                                               ║
║  Failed: 0                                               ║
║  Status: ✓ ALL TESTS PASSED                              ║
╚══════════════════════════════════════════════════════════╝
```

## Advantages of This Approach

1. **Scenario-Driven**: Test real user stories, not infrastructure
2. **Declarative**: YAML scenarios are easy to read and write
3. **Observable**: MQTT observer captures everything passively
4. **Timeline-Based**: See exactly when each event occurred
5. **Self-Contained**: Docker Compose handles all orchestration
6. **No Test Code in Agents**: Agents remain production code
7. **Easy to Add Cases**: New YAML file = new test
8. **Visual Output**: Timeline shows the complete flow

## Adding New Test Scenarios

Just create a new YAML file:

```yaml
# test-scenarios/bedroom_morning.yaml
name: "Bedroom Morning Routine"
description: "Person wakes up, moves around, then leaves"

events:
  - time: 0
    sensor: "motion:bedroom-sensor-1"
    value: true
    description: "Person wakes up"
  
  - time: 120
    sensor: "motion:bedroom-sensor-1"
    value: true
    description: "Getting dressed"
  
  - time: 240
    sensor: "motion:bedroom-sensor-1"
    value: true
    description: "Leaving room"

wait:
  - time: 600
    description: "Person left bedroom"

expectations:
  occupancy_decision:
    - time: 300
      topic: "occupancy/status/bedroom"
      payload:
        occupied: false
        confidence: ">0.7"
```

This keeps complexity in the framework (write once) and scenarios simple (YAML declarations).