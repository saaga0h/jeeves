# J.E.E.V.E.S. Test Scenarios

This directory contains E2E test scenarios for the J.E.E.V.E.S. agent system. Each scenario is defined in YAML format and tests a specific user story or interaction pattern.

## Scenario Structure

A test scenario consists of:

- **name**: Human-readable scenario name
- **description**: Brief description of what's being tested
- **setup**: Initial state configuration
  - **location**: The location being tested (e.g., "hallway", "study", "bedroom")
  - **initial_state**: Key-value pairs for initial Redis state (optional)
- **events**: Sensor events to publish during the test
- **wait**: Wait periods to allow system processing
- **expectations**: Expected outcomes grouped by layer

## Event Timing

All times are specified in **seconds from test start**:

```yaml
events:
  - time: 0       # Immediately when test starts
    sensor: "motion:hallway"
    value: true
    description: "Person enters"

  - time: 30      # 30 seconds after test starts
    sensor: "motion:hallway"
    value: true
    description: "Still moving"
```

## Sensor Format

Sensors are specified as `type:location`:

- `motion:hallway` → Motion sensor in hallway
- `illuminance:study` → Light sensor in study
- `temperature:bedroom` → Temperature sensor in bedroom

These are automatically converted to MQTT topics following the J.E.E.V.E.S. specification:
- Published to: `automation/raw/{type}/{location}`
- Example: `motion:hallway` → `automation/raw/motion/hallway`

## Wait Periods

Wait periods allow the system time to process events:

```yaml
wait:
  - time: 120
    description: "No further motion detected"
```

This is useful for testing occupancy detection after motion stops.

## Expectations

Expectations verify system behavior. They're grouped by **layer** to organize checks:

```yaml
expectations:
  collector:
    - time: 1
      topic: "automation/sensor/motion/hallway"
      payload:
        sensorType: "motion"
        location: "hallway"
        value: true

  occupancy_decision:
    - time: 180
      topic: "automation/context/occupancy/hallway"
      payload:
        location: "hallway"
        occupied: false
        confidence: ">0.7"
```

### Special Matchers

Payload values support special matching syntax:

#### Exact Match
```yaml
payload:
  location: "hallway"      # Must exactly match "hallway"
  occupied: false          # Must be exactly false
```

#### Numeric Comparison
```yaml
payload:
  confidence: ">0.7"       # Must be greater than 0.7
  temperature: "<25.0"     # Must be less than 25.0
  humidity: ">=50"         # Greater than or equal to 50
  brightness: "<=100"      # Less than or equal to 100
```

#### Regex Pattern
```yaml
payload:
  reasoning: "~pass.*through|single.*motion~"  # Matches regex pattern
  status: "~active|pending~"                   # Matches "active" OR "pending"
```

**Pattern syntax**: Wrap regex in `~pattern~`

## J.E.E.V.E.S. MQTT Topology

The system uses a hierarchical MQTT topic structure:

### Topic Flow
```
1. Raw Sensor Data    → automation/raw/{type}/{location}
2. Processed Triggers → automation/sensor/{type}/{location}
3. Context Messages   → automation/context/{type}/{location}
4. Command Messages   → automation/command/{type}/{location}
```

### Expected Topics by Layer

**collector layer:**
- Input: `automation/raw/{type}/{location}` (subscribed)
- Output: `automation/sensor/{type}/{location}` (published)

**occupancy_decision layer:**
- Input: `automation/sensor/motion/{location}` (subscribed)
- Output: `automation/context/occupancy/{location}` (published)

**illuminance_analysis layer:**
- Input: `automation/sensor/illuminance/{location}` (subscribed)
- Output: `automation/context/illuminance/{location}` (published)

**light_control layer:**
- Input: `automation/context/occupancy/{location}` (subscribed)
- Output: `automation/command/light/{location}` (published)
- Output: `automation/context/lighting/{location}` (published)

### Redis Expectations

You can also check Redis state directly:

```yaml
expectations:
  redis_state:
    - time: 5
      redis_key: "sensor:motion:hallway"
      redis_field: "value"
      expected: "true"
```

## Timing Considerations

### Agent Startup
- Tests wait 5 seconds after starting for agents to initialize
- First events should be at `time: 0` or later

### Occupancy Agent
- Analyzes every 60 seconds (configured as `ANALYSIS_INTERVAL=60s`)
- Expectations should account for this periodic analysis
- Example: If last event is at 60s, check at 120s (next analysis cycle)

### MQTT Delivery
- All sensor events use QoS 1 (at least once delivery)
- Typically delivered within milliseconds
- Collector expectations can be checked at `time: 1` (1 second after event)

## Example Scenarios

### Hallway Pass-Through
Tests detection of transient occupancy (person walks through, doesn't linger).

**Key aspects:**
- Single motion event
- Long wait period (120s)
- Expected outcome: NOT occupied

### Study Working
Tests detection of sustained occupancy (person settles in to work).

**Key aspects:**
- Multiple motion events (entering, moving, settling)
- Long work period (480s = 8 minutes)
- Expected outcome: OCCUPIED

### Bedroom Morning
Tests transition from occupied to unoccupied.

**Key aspects:**
- Multiple motion events (activity)
- Final motion event (leaving)
- Long wait period after leaving
- Expected outcomes: Occupied initially, then unoccupied

## Writing New Scenarios

### Step 1: Define User Story
What real-world behavior are you testing?

Example: "Person works in office, takes short break, returns to work"

### Step 2: Plan Events
What sensor events represent this behavior?

```yaml
events:
  - time: 0
    sensor: "motion:office-sensor-1"
    value: true
    description: "Arrive at office"

  - time: 300  # 5 minutes later
    sensor: "motion:office-sensor-1"
    value: true
    description: "Leave for break"

  - time: 600  # 10 minutes total
    sensor: "motion:office-sensor-1"
    value: true
    description: "Return from break"
```

### Step 3: Add Wait Periods
When should the system analyze?

```yaml
wait:
  - time: 120   # After 2 minutes
    description: "Working at desk"

  - time: 720   # After 12 minutes
    description: "Back at work"
```

### Step 4: Define Expectations
What should the system conclude?

```yaml
expectations:
  occupancy_decision:
    - time: 120
      topic: "occupancy/status/office"
      payload:
        occupied: true
        confidence: ">0.7"

    - time: 360   # During break
      topic: "occupancy/status/office"
      payload:
        occupied: false

    - time: 720   # After returning
      topic: "occupancy/status/office"
      payload:
        occupied: true
```

### Step 5: Test and Refine
Run the scenario and adjust timing/expectations based on actual system behavior.

## Troubleshooting

### Expectation Fails: "No messages found for topic"
- Check MQTT topic format in expectations matches what agents publish
- Verify agents are running (`docker-compose logs <agent-name>`)
- Check timing - may need to wait longer for agent to process

### Expectation Fails: "Type mismatch" or "Value mismatch"
- Check actual payload in test output capture: `e2e/test-output/captures/<scenario>.json`
- Adjust expected values or use matchers (`>`, `~regex~`)

### Test Times Out
- Check Ollama is running on host: `curl http://localhost:11434`
- Check `mixtral:8x7b` model is available: `ollama list`
- Increase wait times if system is slow

### Flaky Results
- Occupancy agent uses LLM, which may give slightly different results
- Use confidence thresholds: `">0.6"` instead of exact values
- Use regex patterns: `"~occupied|likely.*occupied~"` for reasoning

## Best Practices

1. **Descriptive names**: Use clear, specific descriptions for events
2. **Realistic timing**: Match real-world behavior patterns
3. **Robust matchers**: Use `>` and `~regex~` for LLM-based outputs
4. **Layer organization**: Group expectations logically (collector, occupancy, lighting)
5. **Comments**: Add inline comments for complex scenarios

## Running Scenarios

```bash
# Run single scenario
cd e2e
./run-test.sh hallway_passthrough

# Run all scenarios
./run-all-tests.sh
```

## Output Files

After running, check:
- `e2e/test-output/timelines/<scenario>.txt` - Human-readable timeline
- `e2e/test-output/captures/<scenario>.json` - All MQTT messages
- `e2e/test-output/summaries/<scenario>.json` - Structured test results
