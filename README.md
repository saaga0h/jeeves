# J.E.E.V.E.S. Platform 2.0

**Just Excellently Executing Various Environmental Services**

A distributed home automation platform built in Go, following the Minestrone Soup Architecture with independently deployable agents communicating via MQTT message bus.

## Overview

J.E.E.V.E.S. Platform 2.0 is a complete rewrite of the Node.js-based home automation system in idiomatic Go. The platform consists of multiple specialized agents that work together to provide intelligent home automation:

- **Collector Agent**: Receives raw sensor data, stores in Redis, publishes triggers
- **Illuminance Agent**: Monitors light levels and adjusts lighting automatically
- **Light Agent**: Bridges MQTT commands with physical lights
- **Occupancy Agent**: Uses advanced pattern recognition and AI to detect room occupancy from motion sensors

## Architecture Principles

1. **Minestrone Soup Architecture**: Composable primitives that are independently deployable
2. **Single Responsibility**: Each agent does one thing well
3. **Event-Driven**: Agents communicate via MQTT pub/sub patterns
4. **Stateless Where Possible**: State stored in Redis, not in agent memory
5. **Configuration Hierarchy**: Defaults → Environment Variables → CLI Parameters

## Repository Structure

```
jeeves/
├── cmd/                        # Agent entry points (bootstrap code)
│   ├── collector-agent/
│   ├── illuminance-agent/
│   ├── light-agent/
│   └── occupancy-agent/
├── internal/                   # Agent-specific implementations
│   ├── collector/             # Fully implemented
│   ├── illuminance/           # Fully implemented
│   ├── light/                 # Fully implemented
│   ├── occupancy/             # Fully implemented
│   └── behavior/              # work-in-progress
├── pkg/                       # Shared infrastructure packages
│   ├── config/               # Configuration management
│   ├── mqtt/                 # MQTT client wrapper
│   ├── redis/                # Redis client wrapper
│   └── health/               # Health check primitives
├── deploy/nomad/             # Nomad job definitions
├── docs/                     # Specifications and documentation
├── Makefile                  # Build automation
├── go.mod
└── README.md
```

## Quick Start

### Prerequisites

- Go 1.25+
- MQTT broker (e.g., Mosquitto)
- Redis server

### Build

```bash
# Build all agents for current platform
make build

# Build for multiple architectures
make build-all

# Run tests
make test
```

### Configuration

Configuration follows a hierarchy: defaults → environment variables → CLI flags.

#### Environment Variables

All environment variables use the `JEEVES_` prefix:

```bash
# MQTT configuration
export JEEVES_MQTT_BROKER=mqtt.service.consul
export JEEVES_MQTT_PORT=1883
export JEEVES_MQTT_USER=collector
export JEEVES_MQTT_PASSWORD=secret

# Redis configuration
export JEEVES_REDIS_HOST=redis.service.consul
export JEEVES_REDIS_PORT=6379

# Service configuration
export JEEVES_LOG_LEVEL=info
export JEEVES_HEALTH_PORT=8080
```

#### CLI Flags

```bash
./bin/collector-agent \
  -mqtt-broker mqtt.service.consul \
  -redis-host redis.service.consul \
  -log-level debug \
  -health-port 8080
```

### Running Locally

```bash
# Run collector agent
make run-collector

# Run illuminance agent
make run-illuminance

# Run light agent
make run-light

# Run occupancy agent
make run-occupancy
```

## Agent Details

### Collector Agent

The Collector Agent is the **central data hub** that receives all sensor data and makes it available to other agents in the system.

**What it does:**
- Acts as the data gateway between raw sensors and the rest of the platform
- Receives sensor data from motion, temperature, illuminance, and other sensors
- Stores data efficiently in Redis with 24-hour automatic cleanup
- Notifies other agents when new sensor data is available
- Handles 1-2 messages/second with minimal resource usage

**Key Features:**
- **Smart Storage**: Uses optimized Redis data structures for different sensor types
- **Reliable Processing**: Gracefully handles malformed messages and connection issues
- **Event Distribution**: Publishes trigger messages so other agents know when to act
- **Auto-Cleanup**: Prevents memory growth with automatic data expiration
- **High Performance**: Processes sensor data with minimal latency

**Integration Points:**
- **Input**: Raw sensor data via MQTT (`automation/raw/+/+`)
- **Output**: Trigger notifications via MQTT (`automation/sensor/+/+`)
- **Storage**: Sensor data buffered in Redis for fast access by other agents
- **Monitoring**: Health check endpoint for container orchestration

See [docs/collector/](docs/collector/) for complete documentation.

### Illuminance Agent

Analyzes room lighting conditions and provides intelligent context for other automation agents.

**Key Features:**
- **Lighting Analysis**: Categorizes lighting (dark/dim/moderate/bright/very_bright) with trend analysis
- **Context Publishing**: Publishes rich lighting context via MQTT for other agents to use
- **Temporal Intelligence**: Tracks lighting patterns, stability, and typical levels over time
- **Smart Fallbacks**: Uses astronomical calculations when sensor data is unavailable
- **Integration Ready**: Powers smart lighting decisions for the Light Agent

See [docs/illuminance/](docs/illuminance/) for complete guides on how it works, message formats, MQTT integration, and Redis data requirements.

### Light Agent

Intelligent lighting automation that responds to occupancy and environmental conditions.

**Key Features:**
- **Smart Decision Making**: Automatically turns lights on/off based on room occupancy and current lighting conditions
- **Context-Aware Brightness**: Adjusts brightness based on time of day, natural light availability, and lighting history
- **Circadian Color Temperature**: Uses warmer colors in evening (2400K-2700K) and cooler during day (4500K-5500K)
- **Manual Override System**: Temporarily disables automation when manual control is detected
- **Rate Limiting**: Prevents light flickering with intelligent decision timing
- **Multi-Strategy Analysis**: Uses recent sensor data, historical patterns, or time-based fallbacks for reliable operation

See [docs/light/](docs/light/) for complete guides on how it works, decision logic, MQTT integration, manual override API, and troubleshooting.

### Occupancy Agent

Provides intelligent room occupancy detection using advanced temporal pattern analysis and machine learning. Converts unreliable motion sensor signals into confident occupancy predictions.

**Key Features:**
- **AI-Powered Analysis**: Uses local LLM (Ollama) to interpret complex motion patterns with human-readable reasoning
- **Temporal Pattern Recognition**: Analyzes motion across multiple time scales to distinguish "person working" from "person left room"
- **Anti-Oscillation Technology**: Vonich-Hakim stabilization prevents rapid state changes from boundary sensor conditions
- **Pass-Through Detection**: Identifies when someone walked through vs. stayed in a room
- **Confidence Scoring**: Provides reliability metrics for downstream automation decisions
- **Settling Behavior Recognition**: Detects when someone enters and sits down (working, reading, watching TV)

**Smart Capabilities:**
- Immediate response to room entries (< 1 second)
- Distinguishes active presence from quiet presence
- Handles sensor noise and environmental factors
- Adapts to different room types and usage patterns
- Falls back gracefully when AI components unavailable

See [docs/occupancy/](docs/occupancy/) for complete guides on how the intelligent analysis works, pattern recognition algorithms, MQTT integration, and troubleshooting.

## Deployment

### Nomad

Job definitions are available in [deploy/nomad/](deploy/nomad/).

```bash
# Deploy collector agent
nomad job run deploy/nomad/collector-agent.nomad.hcl

# Deploy illuminance agent
nomad job run deploy/nomad/illuminance-agent.nomad.hcl

# Deploy light agent
nomad job run deploy/nomad/light-agent.nomad.hcl

# Deploy occupancy agent
nomad job run deploy/nomad/occupancy-agent.nomad.hcl
```

Each agent includes:
- Vault integration for secrets management
- Consul service registration
- Health check monitoring
- Resource constraints (100 MHz CPU, 128 MB RAM)

### Docker (Future)

Docker support is planned but not yet implemented.

## Development

### Code Organization

- **cmd/**: Bootstrap code only (~100-150 lines)
  - Configuration loading
  - Signal handling
  - Dependency initialization
  - Agent startup

- **internal/**: Business logic
  - Agent orchestration
  - Message processing
  - Storage operations
  - Unit tests

- **pkg/**: Shared infrastructure
  - Reusable across all agents
  - Interfaces for testability
  - Public API

### Testing

### Unit Tests

```bash
# Run all tests
make test

# Run tests with coverage
make test-coverage

# Lint code
make lint

# Format code
make fmt
```

### E2E Testing

A comprehensive end-to-end testing framework is available for testing complete system behavior:

```bash
# Run a single scenario
cd e2e
make test SCENARIO=hallway_passthrough

# Run all scenarios
make test-all

# List available scenarios
make list
```

The E2E framework provides:
- **Scenario-driven testing**: Define user stories in YAML
- **Complete observability**: Capture all MQTT traffic
- **Flexible validation**: Support for exact matches, comparisons, and regex
- **Human-readable reports**: Timeline-based test output
- **Self-contained**: Docker Compose orchestrates all services

See [e2e/README.md](e2e/README.md) for complete documentation and [e2e/QUICKSTART.md](e2e/QUICKSTART.md) for getting started.

### Adding a New Agent

1. Create `internal/{agent}/agent.go` with business logic
2. Create `cmd/{agent}-agent/main.go` with bootstrap code
3. Add agent to `AGENTS` variable in Makefile
4. Create Nomad job definition in `deploy/nomad/`
5. Write unit tests in `internal/{agent}/`

## MQTT Topic Taxonomy

### Raw Sensor Data (Input)
- `automation/raw/{sensor_type}/{location}` - Raw sensor readings

### Processed Sensor Data (Output)
- `automation/sensor/{sensor_type}/{location}` - Processed sensor triggers

### Examples
- `automation/raw/motion/study` → `automation/sensor/motion/study`
- `automation/raw/temperature/living_room` → `automation/sensor/temperature/living_room`
- `automation/raw/illuminance/bedroom` → `automation/sensor/illuminance/bedroom`

## Redis Schema

### Motion Sensors
- Key: `sensor:motion:{location}` (Sorted Set)
- Metadata: `meta:motion:{location}` (Hash with lastMotionTime)
- TTL: 24 hours

### Environmental Sensors
- Key: `sensor:environmental:{location}` (Sorted Set)
- Combined temperature and illuminance readings
- TTL: 24 hours

### Generic Sensors
- Key: `sensor:{type}:{location}` (List)
- Metadata: `meta:{type}:{location}` (Hash)
- Max entries: 1000 (configurable)
- TTL: 24 hours

## Health Checks

All agents expose a health check endpoint:

```bash
curl http://localhost:8080/health
```

Response:
```json
{
  "status": "ok",
  "timestamp": "2024-01-01T12:00:00.000Z"
}
```

Health checks are intentionally minimal (no dependency checks) to keep them fast for Nomad/Consul.

## Performance

- **Target Load**: 10-50 sensors, 1-2 messages/second
- **Memory Usage**: ~128 MB per agent
- **CPU Usage**: ~100 MHz per agent
- **Storage**: Bounded by 24-hour TTL

## Roadmap

### Phase 1: Foundation ✅
- [x] Repository structure
- [x] Shared packages (config, mqtt, redis, health)
- [x] Collector agent (fully implemented)
- [x] Build system (Makefile)
- [x] Nomad job definitions

### Phase 2: Agent Stubs ✅
- [x] Illuminance agent stub
- [x] Light agent stub
- [x] Occupancy agent stub

### Phase 3: Illuminance Agent 🚧
- [ ] Implement light level monitoring
- [ ] Implement automated light adjustment
- [ ] Add unit tests

### Phase 4: Occupancy Agent 🚧
- [ ] Implement occupancy detection
- [ ] Track behavioral patterns
- [ ] Add unit tests

### Phase 5: Light Agent 🚧
- [ ] Implement Matter protocol integration
- [ ] Handle light commands
- [ ] Add unit tests

### Phase 6: Observability 📅
- [ ] Add metrics collection
- [ ] Implement distributed tracing
- [ ] Enhanced logging

## Contributing

This is a personal home automation project, if want to contribute:

1. Follow the architecture patterns established in `docs/`
2. Write idiomatic Go code
3. Add unit tests for business logic
4. Update documentation as needed

## Acknowledgments

Built with:
- [Eclipse Paho MQTT](https://github.com/eclipse/paho.mqtt.golang) - MQTT client
- [go-redis](https://github.com/redis/go-redis) - Redis client
- [pflag](https://github.com/spf13/pflag) - CLI flag parsing
