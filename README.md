# J.E.E.V.E.S. Platform 2.0

**Just Excellently Executing Various Environmental Services**

A distributed home automation platform built in Go, following the Minestrone Soup Architecture with independently deployable agents communicating via MQTT message bus.

## Overview

J.E.E.V.E.S. Platform 2.0 is a complete rewrite of the Node.js-based home automation system in idiomatic Go. The platform consists of multiple specialized agents that work together to provide intelligent home automation:

- **Collector Agent**: Receives raw sensor data, stores in Redis, publishes triggers
- **Illuminance Agent**: Monitors light levels and adjusts lighting automatically
- **Light Agent**: Bridges MQTT commands with physical lights
- **Occupancy Agent**: Detects and tracks room occupancy patterns

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
│   ├── illuminance/           # Stub (TODO)
│   ├── light/                 # Stub (TODO)
│   └── occupancy/             # Stub (TODO)
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

### Illuminance Agent (🚧 Stub)

Monitors light levels and automatically adjusts lighting.

**TODO:**
- Subscribe to illuminance sensor topics
- Implement light level monitoring
- Implement automated light adjustment based on thresholds

See [docs/illuminance/](docs/illuminance/) for specifications.

### Light Agent (🚧 Stub)

Bridges MQTT commands with Matter/physical lights.

**TODO:**
- Subscribe to light command topics
- Implement Matter protocol integration
- Handle light state commands (on, off, brightness, color)
- Publish light state updates

See [docs/light/](docs/light/) for specifications.

### Occupancy Agent (🚧 Stub)

Detects and tracks room occupancy patterns.

**TODO:**
- Subscribe to motion/occupancy sensor topics
- Implement occupancy detection logic
- Track behavioral patterns
- Publish occupancy status updates

See [docs/occupancy/](docs/occupancy/) for specifications.

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
