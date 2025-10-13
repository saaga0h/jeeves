# E2E Testing Framework - Quick Start

## Prerequisites

```bash
# 1. Install Ollama
brew install ollama

# 2. Pull LLM model
ollama pull mixtral:8x7b

# 3. Verify Ollama is running
curl http://localhost:11434/api/tags
```

## Running Tests

### Single Test
```bash
cd e2e
./run-test.sh hallway_passthrough
```

### All Tests
```bash
cd e2e
./run-all-tests.sh
```

### List Available Tests
```bash
ls -1 test-scenarios/*.yaml | xargs -n1 basename -s .yaml
```

## Viewing Results

### Timeline Report (Human-readable)
```bash
cat e2e/test-output/timelines/hallway_passthrough.txt
```

### MQTT Traffic (JSON)
```bash
cat e2e/test-output/captures/hallway_passthrough.json | jq
```

### Specific Topic
```bash
cat e2e/test-output/captures/hallway_passthrough.json | \
  jq '.[] | select(.topic | contains("occupancy"))'
```

## Writing New Tests

1. **Create YAML file** in `test-scenarios/`:
   ```yaml
   name: "My Test Scenario"
   description: "What I'm testing"

   setup:
     location: "kitchen"

   events:
     - time: 0
       sensor: "motion:kitchen-sensor-1"
       value: true
       description: "Person enters kitchen"

   wait:
     - time: 120
       description: "No further motion"

   expectations:
     occupancy_decision:
       - time: 180
         topic: "occupancy/status/kitchen"
         payload:
           occupied: false
           confidence: ">0.7"
   ```

2. **Run the test:**
   ```bash
   ./run-test.sh my_test_scenario
   ```

3. **Debug if needed:**
   - Check timeline for failures
   - Inspect MQTT capture for actual messages
   - Adjust timing or expectations
   - Re-run

## Special Matchers

### Exact Match
```yaml
occupied: false          # Must be exactly false
location: "hallway"      # Must be exactly "hallway"
```

### Numeric Comparison
```yaml
confidence: ">0.7"       # Greater than 0.7
temperature: "<25"       # Less than 25
humidity: ">=50"         # Greater than or equal to 50
```

### Regex Pattern
```yaml
reasoning: "~pass.*through|single.*motion~"
status: "~active|pending~"
```

## Common Issues

### "Failed to connect to MQTT broker"
```bash
# Check mosquitto is running
docker-compose -f e2e/docker-compose.test.yml ps mosquitto

# Restart if needed
docker-compose -f e2e/docker-compose.test.yml restart mosquitto
```

### "No messages found for topic"
- Check topic name matches agent output
- Increase wait time before expectation
- Look at capture file for actual topics

### "LLM connection failed"
```bash
# Verify Ollama is running
curl http://localhost:11434/api/tags

# Start Ollama if needed
ollama serve
```

### Test is flaky
- Use confidence thresholds: `">0.6"` instead of exact values
- Use regex patterns: `"~occupied|likely~"`
- Add more wait time before expectations

## Timing Guide

```
T+0s     : Test starts, first events published
T+5s     : Agents initialized (automatic)
T+60s    : Occupancy agent first analysis
T+120s   : Occupancy agent second analysis
T+180s   : Occupancy agent third analysis
```

**Rule of thumb:** Check expectations at least 60s after last relevant event.

## Debugging Commands

### Watch agent logs in real-time
```bash
docker-compose -f e2e/docker-compose.test.yml logs -f occupancy-agent
```

### Run observer standalone
```bash
docker-compose -f e2e/docker-compose.test.yml up mosquitto redis observer
```

### Manual event publishing
```bash
docker exec -it jeeves-test-mosquitto mosquitto_pub \
  -t "sensor/motion/hallway-sensor-1" \
  -m '{"sensorType":"motion","value":true}'
```

### Check Redis state
```bash
docker exec -it jeeves-test-redis redis-cli

# In Redis CLI:
HGETALL sensor:motion:hallway-sensor-1
KEYS *
```

## File Locations

```
e2e/
├── test-output/
│   ├── captures/       # Full MQTT traffic dumps
│   ├── timelines/      # Human-readable test reports
│   └── summaries/      # Structured JSON results
│
test-scenarios/
├── *.yaml              # Test scenario definitions
```

## Clean Environment

```bash
# Stop and remove all containers
docker-compose -f e2e/docker-compose.test.yml down -v

# Remove test output
rm -rf e2e/test-output/*
```

## Build from Scratch

```bash
# Build agents
make build

# Build test infrastructure
docker-compose -f e2e/docker-compose.test.yml build

# Run tests
cd e2e && ./run-all-tests.sh
```

## Performance

Typical test durations:
- **hallway_passthrough**: ~3 minutes
- **study_working**: ~9 minutes
- **bedroom_morning**: ~11 minutes
- **Full suite**: ~25 minutes

## Getting Help

1. Check [e2e/README.md](README.md) for full documentation
2. Check [test-scenarios/README.md](../test-scenarios/README.md) for scenario syntax
3. Look at existing test scenarios for examples
4. Review agent logs for unexpected behavior

## Common Scenario Patterns

### Transient Occupancy
```yaml
events:
  - time: 0
    sensor: "motion:location-sensor-1"
    value: true
    description: "Brief motion"

wait:
  - time: 120
    description: "Long pause"

expectations:
  occupancy_decision:
    - time: 180
      payload:
        occupied: false
```

### Sustained Occupancy
```yaml
events:
  - time: 0
    sensor: "motion:location-sensor-1"
    value: true
    description: "Enter"
  - time: 30
    sensor: "motion:location-sensor-1"
    value: true
    description: "Moving around"
  - time: 60
    sensor: "motion:location-sensor-1"
    value: true
    description: "Settling in"

expectations:
  occupancy_decision:
    - time: 90
      payload:
        occupied: true
```

### Transition (Occupied → Empty)
```yaml
events:
  - time: 0
    sensor: "motion:location-sensor-1"
    value: true
    description: "Person present"
  - time: 60
    sensor: "motion:location-sensor-1"
    value: true
    description: "Person leaving"

wait:
  - time: 300
    description: "Long absence"

expectations:
  occupancy_decision:
    - time: 90
      payload:
        occupied: true
    - time: 360
      payload:
        occupied: false
```
