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

### Event Types

The E2E framework supports three types of events:

1. **Sensor Events** - Raw sensor data (motion, temperature, illuminance)
   - Format: `sensor: "type:location"` + `value: <data>`
   - Published to: `automation/raw/{type}/{location}`

2. **Context Events** - Semantic context from agents (occupancy, lighting)
   - Format: `type: "occupancy|lighting"` + `location: "room"` + `data: {...}`
   - Published to: `automation/context/{type}/{location}`

3. **Media Events** - Media playback state (playing, paused, stopped)
   - Format: `type: "media"` + `location: "room"` + `data: {state: "playing", ...}`
   - Published to: `automation/media/{state}/{location}`

### Basic Scenario (Raw Sensors Only)

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

### Advanced Scenario (Context + Media Events + Postgres)

```yaml
# test-scenarios/movie_night.yaml
name: "Movie Night"
description: "Person watches movie with lights dimming"

setup:
  location: "living_room"
  initial_state:
    occupancy: null

events:
  # Raw sensor event (existing format)
  - time: 0
    sensor: "motion:living_room"
    value: true
    description: "Person enters living room"

  # Context event - occupancy (new format)
  - time: 2
    type: occupancy
    location: living_room
    data:
      state: "occupied"
      confidence: 0.85
    description: "Occupancy detected"

  # Context event - lighting (new format)
  - time: 5
    type: lighting
    location: living_room
    data:
      state: "on"
      brightness: 80
      color_temp: 4000
      source: "automated"
    description: "Lights on"

  # Media event (new format)
  - time: 10
    type: media
    location: living_room
    data:
      state: "playing"
      media_type: "video"
      source: "apple_tv"
    description: "Start movie"

  # Manual lighting adjustment
  - time: 12
    type: lighting
    location: living_room
    data:
      state: "on"
      brightness: 15
      color_temp: 2700
      source: "manual"
    description: "Dim lights for movie"

  # Movie ends
  - time: 600  # 10 minutes for quick test
    type: media
    location: living_room
    data:
      state: "stopped"
      media_type: "video"
    description: "Movie ends"

  # Person leaves
  - time: 620
    type: occupancy
    location: living_room
    data:
      state: "empty"
      confidence: 0.9
    description: "Person leaves"

expectations:
  # Behavior agent should publish episode events
  behavior_events:
    - time: 5
      topic: "automation/behavior/episode/started"
      payload:
        location: "living_room"

    - time: 625
      topic: "automation/behavior/episode/closed"
      payload:
        location: "living_room"
        end_reason: "occupancy_empty"

  # Postgres database checks (new)
  postgres:
    - time: 630
      postgres_query: "SELECT COUNT(*) FROM behavioral_episodes WHERE location = 'living_room'"
      postgres_expected: 1

    - time: 630
      postgres_query: |
        SELECT
        EXTRACT(EPOCH FROM (
          (jsonld->>'jeeves:endedAt')::timestamptz - (jsonld->>'jeeves:startedAt')::timestamptz
        ))::int / 60 as duration_minutes
        FROM behavioral_episodes
        ORDER BY id DESC
        LIMIT 1
      postgres_expected: "~10"  # Approximate match (±20%)
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

  postgres:
    image: pgvector/pgvector:pg16
    environment:
      POSTGRES_DB: jeeves_behavior
      POSTGRES_USER: jeeves
      POSTGRES_PASSWORD: jeeves_test
    ports:
      - "5432:5432"
    volumes:
      - ./init-scripts:/docker-entrypoint-initdb.d
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U jeeves"]
      interval: 5s
      timeout: 3s
      retries: 5

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

  behavior-agent:
    build:
      context: ..
      dockerfile: cmd/behavior-agent/Dockerfile
    depends_on:
      mosquitto:
        condition: service_healthy
      redis:
        condition: service_healthy
      postgres:
        condition: service_healthy
    environment:
      JEEVES_MQTT_BROKER: "mosquitto"
      JEEVES_MQTT_PORT: 1883
      JEEVES_REDIS_HOST: "redis"
      JEEVES_REDIS_PORT: 6379
      JEEVES_POSTGRES_HOST: "postgres"
      JEEVES_POSTGRES_DB: "jeeves_behavior"
      JEEVES_POSTGRES_USER: "jeeves"
      JEEVES_POSTGRES_PASSWORD: "jeeves_test"
      JEEVES_POSTGRES_PORT: 5432
      JEEVES_LOG_LEVEL: "debug"

  # Test runner - orchestrates scenarios
  test-runner:
    build:
      context: .
      dockerfile: Dockerfile.test-runner
    depends_on:
      - mosquitto
      - redis
      - postgres
      - collector
      - occupancy-agent
      - behavior-agent
    volumes:
      - ../test-scenarios:/scenarios
      - ./test-output:/output
    environment:
      MQTT_BROKER: "mosquitto:1883"
      REDIS_HOST: "redis:6379"
      POSTGRES_HOST: "postgres:5432"
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

## Postgres Checker (Database Validation)

The PostgresChecker validates database state for agents that use Postgres (e.g., Behavior Agent).

```go
// e2e/internal/checker/postgres_checker.go
package checker

import (
    "database/sql"
    "fmt"
    "log"
    "strconv"
    "strings"

    _ "github.com/lib/pq"
)

// PostgresChecker validates database state
type PostgresChecker struct {
    db     *sql.DB
    logger *log.Logger
}

// NewPostgresChecker creates a new Postgres checker
func NewPostgresChecker(connStr string, logger *log.Logger) (*PostgresChecker, error) {
    db, err := sql.Open("postgres", connStr)
    if err != nil {
        return nil, fmt.Errorf("failed to connect to postgres: %w", err)
    }

    if err := db.Ping(); err != nil {
        return nil, fmt.Errorf("failed to ping postgres: %w", err)
    }

    return &PostgresChecker{db: db, logger: logger}, nil
}

// CheckQuery executes a query and compares result
func (p *PostgresChecker) CheckQuery(query string, expected interface{}) error {
    var result interface{}
    err := p.db.QueryRow(query).Scan(&result)
    if err != nil {
        return fmt.Errorf("query failed: %w", err)
    }

    return p.compareResults(result, expected)
}

func (p *PostgresChecker) compareResults(actual, expected interface{}) error {
    // Handle approximate matches: "~10" means 8-12 (±20%)
    if expectedStr, ok := expected.(string); ok {
        if strings.HasPrefix(expectedStr, "~") {
            return p.compareApproximate(actual, expectedStr)
        }
    }

    // Exact match
    actualStr := fmt.Sprintf("%v", actual)
    expectedStr := fmt.Sprintf("%v", expected)

    if actualStr == expectedStr {
        return nil
    }

    return fmt.Errorf("mismatch: expected %v, got %v", expected, actual)
}

func (p *PostgresChecker) compareApproximate(actual interface{}, expectedStr string) error {
    // Parse "~10" as target 10 with ±20% tolerance
    targetStr := strings.TrimPrefix(expectedStr, "~")
    target, err := strconv.ParseFloat(targetStr, 64)
    if err != nil {
        return fmt.Errorf("invalid approximate value: %s", expectedStr)
    }

    // Convert actual to float
    var actualFloat float64
    switch v := actual.(type) {
    case int64:
        actualFloat = float64(v)
    case float64:
        actualFloat = v
    case string:
        actualFloat, err = strconv.ParseFloat(v, 64)
        if err != nil {
            return fmt.Errorf("cannot convert actual value to number: %v", actual)
        }
    default:
        return fmt.Errorf("unsupported type for approximate comparison: %T", actual)
    }

    // 20% tolerance
    tolerance := target * 0.2
    if actualFloat >= (target-tolerance) && actualFloat <= (target+tolerance) {
        return nil
    }

    return fmt.Errorf("value %.2f not within ±20%% of %.0f", actualFloat, target)
}
```

### Postgres Expectations in Scenarios

```yaml
expectations:
  # Standard MQTT expectations
  behavior_events:
    - time: 5
      topic: "automation/behavior/episode/started"
      payload:
        location: "living_room"

  # Postgres database checks
  postgres:
    # Exact match
    - time: 630
      postgres_query: "SELECT COUNT(*) FROM behavioral_episodes WHERE location = 'living_room'"
      postgres_expected: 1

    # Approximate match (±20% tolerance)
    - time: 630
      postgres_query: |
        SELECT
        EXTRACT(EPOCH FROM (
          (jsonld->>'jeeves:endedAt')::timestamptz - (jsonld->>'jeeves:startedAt')::timestamptz
        ))::int / 60 as duration_minutes
        FROM behavioral_episodes
        ORDER BY id DESC
        LIMIT 1
      postgres_expected: "~10"  # Accepts 8-12 minutes
```

### Usage in Test Runner

The test runner automatically detects Postgres expectations and uses the PostgresChecker:

```go
// Execute Postgres checks if present
if postgresExpectations, ok := s.Expectations["postgres"]; ok {
    for _, expect := range postgresExpectations {
        if expect.PostgresQuery != "" {
            err := r.postgresChecker.CheckQuery(
                expect.PostgresQuery,
                expect.PostgresExpected,
            )
            if err != nil {
                // Record failure
            } else {
                // Record success
            }
        }
    }
}
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
9. **Multi-Backend**: Supports Redis, Postgres, and MQTT validation

## Adding New Test Scenarios

Just create a new YAML file with any combination of event types:

### Simple Sensor-Only Scenario

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

### Advanced Scenario with Context and Media Events

```yaml
# test-scenarios/study_deep_work.yaml
name: "Study - Deep Work Session"
description: "Person works with focus mode and controlled lighting"

events:
  # Raw sensor
  - time: 0
    sensor: "motion:study"
    value: true
    description: "Person enters"

  # Context event - occupancy
  - time: 2
    type: occupancy
    location: study
    data:
      state: "occupied"
      confidence: 0.9
    description: "Occupancy confirmed"

  # Context event - lighting adjustment
  - time: 5
    type: lighting
    location: study
    data:
      state: "on"
      brightness: 60
      color_temp: 4500
      source: "automated"
    description: "Work lighting activated"

  # Media event - focus music
  - time: 10
    type: media
    location: study
    data:
      state: "playing"
      media_type: "audio"
      source: "spotify"
    description: "Focus music starts"

  # Manual lighting adjustment
  - time: 15
    type: lighting
    location: study
    data:
      state: "on"
      brightness: 40
      color_temp: 3000
      source: "manual"
    description: "User dims for comfort"

expectations:
  behavior_events:
    - time: 5
      topic: "automation/behavior/episode/started"
      payload:
        location: "study"

  postgres:
    - time: 30
      postgres_query: "SELECT COUNT(*) FROM behavioral_episodes WHERE location = 'study'"
      postgres_expected: 1
```

This keeps complexity in the framework (write once) and scenarios simple (YAML declarations).

## Virtual Time for Long Scenarios

For testing long-duration scenarios (movies, work sessions) without waiting hours, the E2E framework supports **virtual time compression**. This allows a 90-minute movie scenario to run in 1.5 minutes while maintaining realistic agent behavior.

### How Virtual Time Works

The framework operates with two time domains:

1. **Real Time**: Actual clock time for test execution (compressed)
2. **Virtual Time**: Simulated time that agents perceive (full duration)

Virtual time is achieved through:
- **Time Scale Factor**: Real seconds × scale = virtual seconds (e.g., 60x acceleration)
- **MQTT Time Configuration**: Published to `automation/test/time_config`
- **Agent Integration**: Agents subscribe to time config and use virtual timestamps
- **Database Consistency**: All stored timestamps reflect virtual time, not compressed test time

### Configuration in Scenarios

Add a `test_mode` section to enable virtual time:

```yaml
# test-scenarios/movie_night.yaml
name: "Movie Night - Virtual Time"
description: "90-minute movie compressed to 1.5 minutes"

# Virtual time configuration
test_mode:
  enabled: true
  virtual_start: "2024-12-19T20:00:00Z"    # When the scenario "virtually" starts
  time_scale: 60                           # 1 real second = 60 virtual seconds
  # This means 90 virtual minutes = 1.5 real minutes

setup:
  location: "living_room"
  initial_state:
    occupancy: null

events:
  # Events use virtual time offsets
  - time: 0
    sensor: "motion:living_room"
    value: true
    description: "Person enters at virtual 8:00 PM"

  - time: 60                               # 1 virtual minute
    type: media
    location: living_room
    data:
      state: "playing"
      media_type: "video"
    description: "Movie starts at virtual 8:01 PM"

  # Periodic motion to maintain occupancy
  - time: 600                              # 10 virtual minutes
    sensor: "motion:living_room"
    value: true
    description: "Small movement during movie"

  - time: 1800                             # 30 virtual minutes
    sensor: "motion:living_room"
    value: true
    description: "Shift position during movie"

  - time: 3600                             # 60 virtual minutes
    sensor: "motion:living_room"
    value: true
    description: "Bathroom break motion"

  - time: 5400                             # 90 virtual minutes (movie ends)
    type: media
    location: living_room
    data:
      state: "stopped"
    description: "Movie ends at virtual 9:30 PM"

  - time: 5460                             # 91 virtual minutes
    type: occupancy
    location: living_room
    data:
      state: "empty"
    description: "Person leaves at virtual 9:31 PM"

expectations:
  behavior_events:
    - time: 60
      topic: "automation/behavior/episode/started"
      payload:
        location: "living_room"

    - time: 5470
      topic: "automation/behavior/episode/closed"
      payload:
        location: "living_room"

  # Database should show realistic virtual timestamps
  postgres:
    - time: 5480
      postgres_query: |
        SELECT 
          (jsonld->>'jeeves:startedAt')::timestamptz as start_time,
          (jsonld->>'jeeves:endedAt')::timestamptz as end_time,
          EXTRACT(EPOCH FROM (
            (jsonld->>'jeeves:endedAt')::timestamptz - 
            (jsonld->>'jeeves:startedAt')::timestamptz
          ))::int / 60 as duration_minutes
        FROM behavioral_episodes 
        WHERE location = 'living_room'
        ORDER BY id DESC LIMIT 1
      postgres_expected: "~91"               # Should be ~91 virtual minutes
```

### Agent Integration Example

The Behavior Agent demonstrates virtual time integration:

```go
// internal/behavior/agent.go
func (a *Agent) startEpisode(location string) {
    episode := &BehavioralEpisode{
        Location:  location,
        StartedAt: a.timeManager.Now(),  // Uses virtual time if configured
        Events:    make([]interface{}, 0),
    }
    // ... store to database with virtual timestamp
}

func (a *Agent) endEpisode(location string, reason string) {
    episode.EndedAt = a.timeManager.Now()  // Virtual time
    // ... update database
}
```

The TimeManager handles both modes transparently:

```go
// internal/behavior/time_manager.go
func (tm *TimeManager) Now() time.Time {
    if !tm.virtualTimeEnabled {
        return time.Now()  // Real time
    }
    
    elapsed := time.Since(tm.testStartTime)
    virtualElapsed := time.Duration(float64(elapsed) * tm.timeScale)
    return tm.virtualStartTime.Add(virtualElapsed)  // Virtual time
}
```

### Time Scale Guidelines

Choose time scales based on scenario duration:

| Virtual Duration | Recommended Scale | Real Duration |
|------------------|-------------------|---------------|
| 5 minutes        | 5x               | 1 minute      |
| 30 minutes       | 30x              | 1 minute      |
| 2 hours          | 60x              | 2 minutes     |
| 8 hours (workday)| 120x             | 4 minutes     |
| 24 hours         | 300x             | 4.8 minutes   |

**Maximum recommended scale: 300x** - Higher scales can cause timing issues.

### Test Runner Implementation

The test runner automatically handles virtual time:

```go
// e2e/internal/executor/runner.go
func (r *Runner) runScenario(scenario *Scenario) error {
    // Check if virtual time is enabled
    if scenario.TestMode != nil && scenario.TestMode.Enabled {
        // Publish time configuration to agents
        timeConfig := TimeConfig{
            VirtualStart: scenario.TestMode.VirtualStart,
            TimeScale:    scenario.TestMode.TimeScale,
            TestStartTime: time.Now(),
        }
        
        r.publishTimeConfig(timeConfig)
        
        // Use time scaling for event timing
        for _, event := range scenario.Events {
            // Convert virtual time to real time
            realWaitTime := time.Duration(event.Time) * time.Second / time.Duration(scenario.TestMode.TimeScale)
            time.Sleep(realWaitTime)
            // ... publish event
        }
    } else {
        // Standard real-time execution
        // ... normal event timing
    }
}

func (r *Runner) publishTimeConfig(config TimeConfig) {
    payload, _ := json.Marshal(config)
    r.mqttClient.Publish("automation/test/time_config", 0, false, payload)
}
```

### Virtual Time in Database Queries

Postgres expectations automatically use virtual time when checking durations:

```yaml
expectations:
  postgres:
    # This checks virtual duration, not real test duration
    - time: 5480
      postgres_query: |
        SELECT EXTRACT(EPOCH FROM (
          (jsonld->>'jeeves:endedAt')::timestamptz - 
          (jsonld->>'jeeves:startedAt')::timestamptz
        ))::int / 60 as duration_minutes
        FROM behavioral_episodes 
        WHERE location = 'living_room'
        ORDER BY id DESC LIMIT 1
      postgres_expected: "~91"               # Virtual minutes, not real

    # Check realistic virtual timestamps
    - time: 5480
      postgres_query: |
        SELECT EXTRACT(hour FROM (jsonld->>'jeeves:startedAt')::timestamptz) as start_hour
        FROM behavioral_episodes 
        WHERE location = 'living_room'
        ORDER BY id DESC LIMIT 1
      postgres_expected: 20                  # Started at virtual 8 PM
```

### Example Virtual Time Scenarios

#### Workday Scenario (8 hours → 4 minutes)

```yaml
name: "Full Workday - Compressed"
description: "8-hour workday in 4 minutes with periodic activity"

test_mode:
  enabled: true
  virtual_start: "2024-12-19T09:00:00Z"
  time_scale: 120

events:
  - time: 0
    sensor: "motion:home_office"
    value: true
    description: "Start work at 9 AM"

  - time: 7200                             # 2 virtual hours
    sensor: "motion:home_office"
    value: true
    description: "11 AM movement"

  - time: 14400                            # 4 virtual hours  
    sensor: "motion:home_office"
    value: true
    description: "1 PM lunch break"

  - time: 25200                            # 7 virtual hours
    sensor: "motion:home_office"
    value: true
    description: "4 PM afternoon activity"

  - time: 28800                            # 8 virtual hours
    type: occupancy
    location: home_office
    data:
      state: "empty"
    description: "End work at 5 PM"
```

#### Sleep Cycle (8 hours → 2.7 minutes)

```yaml
name: "Sleep Cycle - Compressed"
description: "Full night sleep with minimal interruptions"

test_mode:
  enabled: true
  virtual_start: "2024-12-19T23:00:00Z"
  time_scale: 180

events:
  - time: 0
    sensor: "motion:bedroom"
    value: true
    description: "Go to bed at 11 PM"

  - time: 14400                            # 4 virtual hours
    sensor: "motion:bedroom"
    value: true
    description: "3 AM bathroom break"

  - time: 28800                            # 8 virtual hours
    sensor: "motion:bedroom"
    value: true
    description: "7 AM wake up"

  - time: 29400                            # 8 hours 10 minutes
    type: occupancy
    location: bedroom
    data:
      state: "empty"
    description: "Leave bedroom"
```

### Benefits of Virtual Time

1. **Fast Test Execution**: Long scenarios complete in minutes
2. **Realistic Behavior**: Agents see proper time progression
3. **Database Integrity**: Stored timestamps reflect actual scenario timing
4. **Behavioral Validation**: Can test time-dependent logic (work hours, sleep cycles)
5. **Compressed Logs**: Debug output shows realistic timestamps, not test time

### Virtual Time Best Practices

1. **Include Periodic Motion**: Add motion events throughout long periods to maintain occupancy
2. **Realistic Start Times**: Use appropriate `virtual_start` times (evening for movies, morning for work)
3. **Moderate Scaling**: Keep time scales under 300x for reliability
4. **Test Both Modes**: Verify scenarios work with and without virtual time
5. **Document Expected Duration**: Include both virtual and real durations in descriptions

This virtual time system enables comprehensive testing of long-duration scenarios while maintaining the efficiency needed for continuous integration.