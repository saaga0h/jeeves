# J.E.E.V.E.S. E2E Testing Framework

End-to-end testing framework for the J.E.E.V.E.S. distributed agent system.

## Overview

This framework enables **scenario-driven testing** of the complete agent system. Instead of manually watching logs and publishing MQTT messages, you define user stories in YAML files, and the framework:

- Orchestrates sensor events
- Captures all MQTT traffic
- Validates expected outcomes
- Generates human-readable timeline reports

## Architecture

```
e2e/
├── cmd/
│   ├── observer/          # MQTT traffic capture service
│   └── test-runner/       # Test orchestration
├── internal/
│   ├── observer/          # MQTT message capture
│   ├── scenario/          # YAML parsing and validation
│   ├── executor/          # Test execution
│   ├── checker/           # Expectation matching
│   └── reporter/          # Timeline generation
├── docker-compose.test.yml
├── run-test.sh            # Run single scenario
├── run-all-tests.sh       # Run all scenarios
└── test-output/           # Generated artifacts
    ├── captures/          # MQTT traffic JSON
    ├── timelines/         # Human-readable reports
    └── summaries/         # Structured results
```

## Quick Start

### Prerequisites

1. **Docker and Docker Compose** installed
2. **Go 1.21+** for building agents
3. **Ollama** running on host machine with `mixtral:8x7b` model
4. **Make** for building agents

### Run a Test

```bash
cd e2e
./run-test.sh hallway_passthrough
```

This will:
1. Build all agents
2. Start test environment (MQTT, Redis, agents)
3. Run the scenario
4. Display results
5. Clean up

### Run All Tests

```bash
cd e2e
./run-all-tests.sh
```

## Test Scenarios

Scenarios are defined in `../test-scenarios/` as YAML files.

**Example**: `hallway_passthrough.yaml`

```yaml
name: "Hallway Pass-Through"
description: "Single motion event, person walks through"

setup:
  location: "hallway"

events:
  - time: 0
    sensor: "motion:hallway-sensor-1"
    value: true
    description: "Person enters"

wait:
  - time: 120
    description: "No further motion"

expectations:
  occupancy_decision:
    - time: 180
      topic: "occupancy/status/hallway"
      payload:
        occupied: false
        confidence: ">0.7"
```

See [`../test-scenarios/README.md`](../test-scenarios/README.md) for complete documentation on writing scenarios.

## Components

### MQTT Observer

Passively captures all MQTT traffic during test execution.

**Features:**
- Subscribes to all topics (`#`)
- Thread-safe message storage
- Real-time logging with timestamps
- JSON export for post-analysis

**Standalone usage:**
```bash
docker-compose -f docker-compose.test.yml up observer
```

### Test Runner

Orchestrates test execution:
1. Loads scenario YAML
2. Starts observer
3. Publishes sensor events at specified times
4. Checks expectations against captured messages
5. Generates reports

**Manual usage:**
```bash
docker-compose -f docker-compose.test.yml run --rm test-runner \
  --scenario /scenarios/hallway_passthrough.yaml \
  --output-dir /output
```

### Expectation Matcher

Validates outcomes with flexible matching:

- **Exact match**: `occupied: false`
- **Numeric comparison**: `confidence: ">0.7"`
- **Regex pattern**: `reasoning: "~pass.*through~"`
- **Nested structures**: Recursive matching on JSON objects

### Timeline Reporter

Generates human-readable test reports:

```
╔══════════════════════════════════════════════════════════╗
║  Scenario: Hallway Pass-Through                          ║
║  Duration: 3m 2.5s                                       ║
╚══════════════════════════════════════════════════════════╝

[  0.00s] → sensor       : motion:hallway-sensor-1 = true
[  0.12s] → wait         : No further motion (120.0s)
[120.05s] → occupancy    : analyzing hallway
[180.12s] ✓ occupancy    : hallway occupied=false

=== Expectations ===
Layer: occupancy_decision
  ✓ occupancy/status/hallway: occupied=false, confidence>0.7

╔══════════════════════════════════════════════════════════╗
║  SUMMARY                                                 ║
║  Passed: 1                                               ║
║  Failed: 0                                               ║
║  Status: ✓ ALL TESTS PASSED                              ║
╚══════════════════════════════════════════════════════════╝
```

## Docker Compose Configuration

The test environment includes:

- **mosquitto**: MQTT broker (port 1883)
- **redis**: State storage (port 6379)
- **collector**: Routes sensor data
- **illuminance-agent**: Monitors light levels
- **light-agent**: Controls lighting
- **occupancy-agent**: Detects occupancy patterns
- **observer**: Captures MQTT traffic
- **test-runner**: Executes scenarios (run manually)

### Environment Variables

**Occupancy Agent:**
- `LLM_URL=http://host.docker.internal:11434` - Points to host Ollama
- `LLM_MODEL=mixtral:8x7b` - Best performing model for reasoning
- `ANALYSIS_INTERVAL=60s` - Faster testing (default: 5m)

**All Agents:**
- `LOG_LEVEL=debug` - Verbose logging for debugging
- `MQTT_BROKER=tcp://mosquitto:1883`
- `REDIS_ADDR=redis:6379`

## Output Files

After each test run:

### Timeline Report
`test-output/timelines/<scenario>.txt`

Human-readable timeline showing:
- Events with timestamps
- Agent actions
- Expectation results
- Pass/fail summary

### MQTT Capture
`test-output/captures/<scenario>.json`

Complete MQTT traffic dump:
```json
[
  {
    "timestamp": "2024-01-15T10:30:00Z",
    "topic": "sensor/motion/hallway-sensor-1",
    "payload": {"sensorType": "motion", "value": true},
    "qos": 1
  }
]
```

### Test Summary
`test-output/summaries/<scenario>.json`

Structured test results for programmatic analysis.

## Debugging Failed Tests

### Step 1: Check Timeline
```bash
cat test-output/timelines/<scenario>.txt
```

Look for:
- When events occurred
- Which expectations failed
- Error messages

### Step 2: Inspect MQTT Capture
```bash
cat test-output/captures/<scenario>.json | jq '.[] | select(.topic | contains("occupancy"))'
```

Find actual messages published by agents.

### Step 3: Check Agent Logs
```bash
docker-compose -f docker-compose.test.yml logs occupancy-agent
```

Look for errors or unexpected behavior.

### Step 4: Verify Ollama
```bash
curl http://localhost:11434/api/tags
```

Ensure `mixtral:8x7b` is available.

### Step 5: Run Observer Standalone
```bash
docker-compose -f docker-compose.test.yml up mosquitto redis collector occupancy-agent observer
```

Manually publish events and watch real-time logs.

## Common Issues

### "Failed to connect to MQTT broker"
- Check Mosquitto is running: `docker-compose -f docker-compose.test.yml ps`
- Check health: `docker-compose -f docker-compose.test.yml logs mosquitto`

### "No messages found for topic"
- Verify topic format matches agent output
- Check timing - may need to increase wait time
- Look at capture file for actual topics

### "LLM connection failed"
- Ensure Ollama is running on host: `curl http://localhost:11434`
- Check model exists: `ollama list | grep mixtral`
- Pull model if needed: `ollama pull mixtral:8x7b`

### Flaky Test Results
- LLM-based reasoning may vary slightly between runs
- Use flexible matchers: `">0.6"` instead of `"0.75"`
- Use regex: `"~occupied|likely~"` instead of exact strings

### Tests Run Slowly
- First run builds Docker images (takes time)
- Subsequent runs are faster (cached images)
- Consider reducing `ANALYSIS_INTERVAL` in docker-compose

## Development

### Adding New Internal Packages

1. Create package in `e2e/internal/<name>/`
2. Update Dockerfiles to copy new code
3. Rebuild: `docker-compose -f docker-compose.test.yml build`

### Running Tests Locally (without Docker)

```bash
# Start infrastructure
docker-compose -f docker-compose.test.yml up mosquitto redis

# Build and run agents locally
cd agents
go run cmd/collector/main.go &
go run cmd/occupancy-agent/main.go &

# Run test-runner locally
cd e2e
go run cmd/test-runner/main.go \
  --scenario ../test-scenarios/hallway_passthrough.yaml \
  --mqtt-broker tcp://localhost:1883 \
  --redis-host localhost:6379 \
  --output-dir ./test-output
```

## CI/CD Integration

To run tests in CI:

```yaml
# .github/workflows/e2e-tests.yml
name: E2E Tests

on: [push, pull_request]

jobs:
  e2e:
    runs-on: ubuntu-latest

    steps:
      - uses: actions/checkout@v3

      - name: Set up Ollama
        run: |
          # Install and start Ollama
          # Pull mixtral model

      - name: Run E2E Tests
        run: |
          cd e2e
          ./run-all-tests.sh
```

## Performance

Typical test durations:
- **hallway_passthrough**: ~3 minutes
- **study_working**: ~9 minutes
- **bedroom_morning**: ~11 minutes

Total suite runtime: ~25 minutes

## Best Practices

1. **Clean state**: Always start with fresh Docker environment
2. **Timing margins**: Add buffer time for LLM processing
3. **Flexible matchers**: Use `>` and `~regex~` for LLM outputs
4. **Descriptive names**: Clear scenario and event descriptions
5. **Incremental development**: Test one expectation at a time
6. **Version control**: Commit passing test scenarios

## Future Enhancements

Potential improvements:
- [ ] Web UI for viewing test results
- [ ] Parallel scenario execution
- [ ] Historical test comparison
- [ ] Performance metrics collection
- [ ] Snapshot testing (record/replay)
- [ ] Test flakiness detection
- [ ] Custom assertions DSL

## Architecture Decisions

### Why MQTT Observer?
- Passive observation doesn't affect system behavior
- Captures complete picture of agent interactions
- Enables post-test analysis and debugging

### Why YAML Scenarios?
- Human-readable and writable
- Version control friendly
- Easy to share and review
- Non-programmers can write tests

### Why Timeline Reports?
- Cognitive load reduction - see everything at once
- Easy to spot timing issues
- Shareable with non-technical stakeholders

### Why Docker Compose?
- Self-contained test environment
- Reproducible across machines
- Isolated from development environment
- Easy cleanup

## Contributing

When adding new test scenarios:

1. Create YAML file in `test-scenarios/`
2. Run the test: `./run-test.sh <scenario-name>`
3. Verify it passes consistently (run 3+ times)
4. Document any special requirements in test scenario README
5. Submit PR with scenario and any framework improvements

## License

Same as J.E.E.V.E.S. project.
