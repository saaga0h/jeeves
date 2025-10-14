# J.E.E.V.E.S. Shared Services

Documentation for the `pkg/` infrastructure packages used across all agents.

## Table of Contents
- [Overview](#overview)
- [MQTT Package](#mqtt-package)
- [Redis Package](#redis-package)
- [Config Package](#config-package)
- [Health Package](#health-package)
- [Ontology Package](#ontology-package)
- [Usage Patterns](#usage-patterns)

---

## Overview

The `pkg/` directory contains shared infrastructure code that is reusable across all agents. These packages provide:

1. **Abstraction**: Clean interfaces for testing
2. **Consistency**: Standard patterns for all agents
3. **Configuration**: Unified config hierarchy
4. **Observability**: Health checks and logging

```
pkg/
├── config/         # Configuration management
│   └── config.go   # Hierarchical config (defaults → env → flags)
├── mqtt/           # MQTT client abstraction
│   ├── interfaces.go  # Testable interfaces
│   ├── client.go      # Paho wrapper
│   └── topics.go      # Topic helpers
├── redis/          # Redis client abstraction
│   ├── interfaces.go  # Testable interfaces
│   ├── client.go      # go-redis wrapper
│   └── keys.go        # Key construction helpers
├── health/         # Health check primitives
│   └── health.go      # HTTP health endpoint
└── ontology/       # Semantic web types (JSON-LD)
    ├── context.go     # JSON-LD context definitions
    └── episode.go     # BehavioralEpisode types
```

---

## MQTT Package

**Location**: [`pkg/mqtt/`](../pkg/mqtt/)
**Purpose**: Abstraction layer for MQTT communication with testable interfaces

### Interface

```go
// Client is the main MQTT interface
type Client interface {
    Connect(ctx context.Context) error
    Disconnect()
    Subscribe(topic string, qos byte, handler MessageHandler) error
    Publish(topic string, qos byte, retained bool, payload []byte) error
    IsConnected() bool
}

// MessageHandler is a callback for incoming messages
type MessageHandler func(Message)

// Message represents an MQTT message
type Message interface {
    Topic() string
    Payload() []byte
    Ack()
}
```

### Implementation

Wraps [Eclipse Paho MQTT](https://github.com/eclipse/paho.mqtt.golang):

```go
func NewClient(cfg *config.Config, logger *slog.Logger) Client {
    opts := pahomqtt.NewClientOptions()
    opts.AddBroker(cfg.MQTTAddress())
    opts.SetClientID(...)
    opts.SetCleanSession(true)
    opts.SetAutoReconnect(true)
    opts.SetConnectRetry(true)

    // Connection handlers
    opts.OnConnect = func(c pahomqtt.Client) {
        logger.Info("Connected to MQTT broker")
    }

    opts.OnConnectionLost = func(c pahomqtt.Client, err error) {
        logger.Warn("MQTT connection lost", "error", err)
    }

    return &mqttClient{client: pahomqtt.NewClient(opts), ...}
}
```

### Topic Helpers

```go
// Topic constants
const (
    TopicRawSensors   = "automation/raw/+/+"
    TopicRawMotion    = "automation/raw/motion/+"
    TopicSensorMotion = "automation/sensor/motion/+"
)

// Topic construction functions
func RawSensorTopic(sensorType, location string) string {
    return fmt.Sprintf("automation/raw/%s/%s", sensorType, location)
}

func ProcessedSensorTopic(sensorType, location string) string {
    return fmt.Sprintf("automation/sensor/%s/%s", sensorType, location)
}

// Topic conversion
func ConvertRawToProcessed(rawTopic string) string {
    return rawTopic[0:14] + "sensor" + rawTopic[17:]
}
```

### Usage Example

```go
// In agent initialization
mqttClient := mqtt.NewClient(cfg, logger)

// Connect with timeout
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
if err := mqttClient.Connect(ctx); err != nil {
    return fmt.Errorf("failed to connect: %w", err)
}

// Subscribe to topics
handler := func(msg mqtt.Message) {
    logger.Info("Received", "topic", msg.Topic(), "payload", string(msg.Payload()))
}
if err := mqttClient.Subscribe("automation/sensor/+/+", 0, handler); err != nil {
    return err
}

// Publish messages
payload := []byte(`{"occupied": true}`)
if err := mqttClient.Publish("automation/context/occupancy/study", 0, false, payload); err != nil {
    return err
}

// Cleanup
defer mqttClient.Disconnect()
```

### Connection Management

- **Auto-Reconnect**: Enabled by default
- **Connect Retry**: 5-second initial interval
- **Max Reconnect Interval**: 30 seconds
- **Clean Session**: true (don't persist subscriptions)

### QoS Levels

All agents use **QoS 0** (at most once):
- Sensors re-send data periodically
- Redis is the source of truth
- Simpler, faster than QoS 1/2

---

## Redis Package

**Location**: [`pkg/redis/`](../pkg/redis/)
**Purpose**: Abstraction layer for Redis with testable interfaces

### Interface

```go
type Client interface {
    // Strings
    Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
    Get(ctx context.Context, key string) (string, error)

    // Hashes
    HSet(ctx context.Context, key string, field string, value interface{}) error
    HGet(ctx context.Context, key string, field string) (string, error)
    HGetAll(ctx context.Context, key string) (map[string]string, error)

    // Sorted Sets
    ZAdd(ctx context.Context, key string, score float64, member interface{}) error
    ZRemRangeByScore(ctx context.Context, key string, min, max string) error
    ZCard(ctx context.Context, key string) (int64, error)
    ZRangeByScoreWithScores(ctx context.Context, key string, min, max float64) ([]ZMember, error)

    // Lists
    LPush(ctx context.Context, key string, values ...interface{}) error
    LTrim(ctx context.Context, key string, start, stop int64) error
    LLen(ctx context.Context, key string) (int64, error)
    LRange(ctx context.Context, key string, start, stop int64) ([]string, error)

    // Keys
    Keys(ctx context.Context, pattern string) ([]string, error)
    Expire(ctx context.Context, key string, ttl time.Duration) error

    // Connection
    Ping(ctx context.Context) error
    Close() error
}

type ZMember struct {
    Score  float64
    Member string
}
```

### Implementation

Wraps [go-redis](https://github.com/redis/go-redis):

```go
func NewClient(cfg *config.Config, logger *slog.Logger) Client {
    opts := &redis.Options{
        Addr:     cfg.RedisAddress(),
        Password: cfg.RedisPassword,
        DB:       cfg.RedisDB,
    }

    client := redis.NewClient(opts)

    return &redisClient{
        client: client,
        cfg:    cfg,
        logger: logger,
    }
}
```

### Key Construction Helpers

```go
// Motion sensor keys
func MotionSensorKey(location string) string {
    return fmt.Sprintf("sensor:motion:%s", location)
}

func MotionMetaKey(location string) string {
    return fmt.Sprintf("meta:motion:%s", location)
}

// Environmental sensor keys
func EnvironmentalSensorKey(location string) string {
    return fmt.Sprintf("sensor:environmental:%s", location)
}

// Generic sensor keys
func GenericSensorKey(sensorType, location string) string {
    return fmt.Sprintf("sensor:%s:%s", sensorType, location)
}

func GenericMetaKey(sensorType, location string) string {
    return fmt.Sprintf("meta:%s:%s", sensorType, location)
}
```

### Usage Example

```go
// In agent initialization
redisClient := redis.NewClient(cfg, logger)

// Verify connection
ctx := context.Background()
if err := redisClient.Ping(ctx); err != nil {
    return fmt.Errorf("redis ping failed: %w", err)
}

// Store motion event (sorted set)
key := redis.MotionSensorKey("study")
timestamp := float64(time.Now().UnixMilli())
data := `{"state": "on", "timestamp": "2024-01-01T12:00:00Z"}`

if err := redisClient.ZAdd(ctx, key, timestamp, data); err != nil {
    return err
}

// Set TTL
if err := redisClient.Expire(ctx, key, 24*time.Hour); err != nil {
    return err
}

// Query time range
now := time.Now()
twoMinAgo := now.Add(-2 * time.Minute)
members, err := redisClient.ZRangeByScoreWithScores(
    ctx,
    key,
    float64(twoMinAgo.UnixMilli()),
    float64(now.UnixMilli()),
)
if err != nil {
    return err
}

// Update metadata (hash)
metaKey := redis.MotionMetaKey("study")
if err := redisClient.HSet(ctx, metaKey, "lastMotionTime", timestamp); err != nil {
    return err
}

// Cleanup
defer redisClient.Close()
```

### Data Structure Patterns

#### Sorted Sets (Time-Series)
```go
// Best for: Motion events, illuminance readings
// Key: sensor:motion:{location}
// Score: Unix timestamp (milliseconds)
// Member: JSON data

// Add event
redisClient.ZAdd(ctx, key, float64(time.Now().UnixMilli()), jsonData)

// Query range
members, _ := redisClient.ZRangeByScoreWithScores(ctx, key, minTimestamp, maxTimestamp)

// Cleanup old data
redisClient.ZRemRangeByScore(ctx, key, "-inf", fmt.Sprintf("%d", oldestAllowed))
```

#### Hashes (Metadata)
```go
// Best for: Last known values, configuration
// Key: meta:motion:{location}

// Set fields
redisClient.HSet(ctx, key, "lastMotionTime", "1704110400000")
redisClient.HSet(ctx, key, "state", "on")

// Get field
value, _ := redisClient.HGet(ctx, key, "lastMotionTime")

// Get all fields
allFields, _ := redisClient.HGetAll(ctx, key)
```

#### Lists (FIFO Queues)
```go
// Best for: Generic sensors, recent N events
// Key: sensor:{type}:{location}

// Push to head
redisClient.LPush(ctx, key, jsonData)

// Trim to max size
redisClient.LTrim(ctx, key, 0, 999)  // Keep newest 1000

// Get range
items, _ := redisClient.LRange(ctx, key, 0, 9)  // Get 10 newest
```

---

## Config Package

**Location**: [`pkg/config/`](../pkg/config/)
**Purpose**: Hierarchical configuration management

### Configuration Hierarchy

```
1. Defaults (hardcoded in NewConfig())
   ↓
2. Environment Variables (JEEVES_* prefix)
   ↓
3. CLI Flags (--flag-name)
```

### Config Struct

```go
type Config struct {
    // MQTT configuration
    MQTTBroker   string
    MQTTPort     int
    MQTTUser     string
    MQTTPassword string
    MQTTClientID string

    // Redis configuration
    RedisHost     string
    RedisPort     int
    RedisPassword string
    RedisDB       int

    // Service configuration
    ServiceName string
    HealthPort  int
    LogLevel    string

    // Agent-specific (extend as needed)
    MaxSensorHistory          int
    OccupancyAnalysisIntervalSec int
    LLMEndpoint               string
    LLMModel                  string
    // ... more fields
}
```

### Usage Example

```go
// In main.go
func main() {
    // 1. Create config with defaults
    cfg := config.NewConfig()
    cfg.ServiceName = "collector-agent"  // Override service name

    // 2. Load from environment
    cfg.LoadFromEnv()

    // 3. Load from CLI flags
    cfg.LoadFromFlags()

    // 4. Validate
    if err := cfg.Validate(); err != nil {
        fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
        os.Exit(1)
    }

    // 5. Use throughout agent
    mqttClient := mqtt.NewClient(cfg, logger)
    redisClient := redis.NewClient(cfg, logger)
}
```

### Environment Variables

All environment variables use the `JEEVES_` prefix:

```bash
# MQTT
JEEVES_MQTT_BROKER=mqtt.service.consul
JEEVES_MQTT_PORT=1883
JEEVES_MQTT_USER=agent
JEEVES_MQTT_PASSWORD=secret

# Redis
JEEVES_REDIS_HOST=redis.service.consul
JEEVES_REDIS_PORT=6379
JEEVES_REDIS_PASSWORD=secret
JEEVES_REDIS_DB=0

# Service
JEEVES_SERVICE_NAME=collector-agent
JEEVES_HEALTH_PORT=8080
JEEVES_LOG_LEVEL=info

# Agent-specific
JEEVES_MAX_SENSOR_HISTORY=1000
JEEVES_OCCUPANCY_ANALYSIS_INTERVAL_SEC=60
JEEVES_LLM_ENDPOINT=http://localhost:11434/api/generate
JEEVES_LLM_MODEL=mixtral:8x7b
```

### CLI Flags

```bash
./collector-agent \
  --mqtt-broker mqtt.service.consul \
  --mqtt-port 1883 \
  --redis-host redis.service.consul \
  --redis-port 6379 \
  --log-level debug \
  --health-port 8080
```

### Helper Methods

```go
// MQTTAddress returns full broker URL
func (c *Config) MQTTAddress() string {
    return fmt.Sprintf("tcp://%s:%d", c.MQTTBroker, c.MQTTPort)
}

// RedisAddress returns full Redis address
func (c *Config) RedisAddress() string {
    return fmt.Sprintf("%s:%d", c.RedisHost, c.RedisPort)
}

// Validate checks required fields
func (c *Config) Validate() error {
    if c.MQTTBroker == "" {
        return fmt.Errorf("MQTT broker is required")
    }
    // ... more validation
}
```

---

## Health Package

**Location**: [`pkg/health/`](../pkg/health/)
**Purpose**: HTTP health check endpoints for Nomad/Consul

### Interface

```go
type Checker struct {
    mqtt   mqtt.Client
    redis  redis.Client
    logger *slog.Logger
}

func NewChecker(mqttClient mqtt.Client, redisClient redis.Client, logger *slog.Logger) *Checker
```

### Health Endpoints

#### Simple Health Check (Default)

**Endpoint**: `/health`
**Method**: `GET`
**Response**: 200 OK if process is alive

```json
{
  "status": "ok",
  "timestamp": "2024-01-01T12:00:00.000Z"
}
```

**Philosophy**: Fast health checks for Nomad/Consul. Don't check dependencies.

```go
func (h *Checker) HandlerFunc() http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        response := HealthResponse{
            Status:    "ok",
            Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
        }
        json.NewEncoder(w).Encode(response)
    }
}
```

#### Detailed Health Check (Optional)

**Endpoint**: `/health/detailed`
**Method**: `GET`
**Response**: Includes dependency status

```json
{
  "status": "healthy",
  "timestamp": "2024-01-01T12:00:00.000Z",
  "services": {
    "mqtt": "connected",
    "redis": "connected"
  }
}
```

```go
func (h *Checker) DetailedHandlerFunc() http.HandlerFunc {
    // Checks MQTT and Redis connections
    // Returns 503 if any dependency is down
}
```

### Usage Example

```go
// In main.go
func main() {
    // ... init mqtt, redis, agent ...

    // Create health checker
    healthChecker := health.NewChecker(mqttClient, redisClient, logger)

    // Start health server
    mux := http.NewServeMux()
    mux.HandleFunc("/health", healthChecker.HandlerFunc())
    mux.HandleFunc("/health/detailed", healthChecker.DetailedHandlerFunc())

    server := &http.Server{
        Addr:    fmt.Sprintf(":%d", cfg.HealthPort),
        Handler: mux,
    }

    go func() {
        logger.Info("Starting health check server", "port", cfg.HealthPort)
        if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            logger.Error("Health server error", "error", err)
        }
    }()

    // ... agent logic ...

    // Graceful shutdown
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    server.Shutdown(shutdownCtx)
}
```

---

## Ontology Package

**Location**: [`pkg/ontology/`](../pkg/ontology/)
**Purpose**: Semantic web types using JSON-LD for interoperable behavioral episode storage

### Overview

The ontology package provides JSON-LD types for representing behavioral episodes using standard semantic web vocabularies. This enables interoperability with other smart home systems and future AI/ML analysis tools.

### JSON-LD Context

**File**: `pkg/ontology/context.go`

```go
package ontology

// GetDefaultContext returns the standard JSON-LD context
func GetDefaultContext() map[string]interface{} {
    return map[string]interface{}{
        "@vocab": "https://saref.etsi.org/core#",
        "jeeves": "https://jeeves.home/vocab#",
        "adl":    "http://purl.org/adl#",
        "sosa":   "http://www.w3.org/ns/sosa/",
        "prov":   "http://www.w3.org/ns/prov#",
        "xsd":    "http://www.w3.org/2001/XMLSchema#",
    }
}
```

**Vocabularies Used**:
- **SAREF** (Smart Applications REFerence): Base ontology for IoT devices and smart home concepts
- **ADL** (Activities of Daily Living): Ontology for human activities (eating, sleeping, working, etc.)
- **SSN/SOSA** (Semantic Sensor Network): Sensor observations and actuations
- **PROV** (Provenance): Data lineage and causality
- **jeeves**: Custom vocabulary for J.E.E.V.E.S.-specific concepts

### Behavioral Episode Types

**File**: `pkg/ontology/episode.go`

```go
package ontology

import (
    "fmt"
    "time"

    "github.com/google/uuid"
)

// BehavioralEpisode is the root JSON-LD document
type BehavioralEpisode struct {
    Context    map[string]interface{} `json:"@context"`
    Type       string                 `json:"@type"`
    ID         string                 `json:"@id"`
    StartedAt  time.Time              `json:"jeeves:startedAt"`
    EndedAt    *time.Time             `json:"jeeves:endedAt,omitempty"`
    DayOfWeek  string                 `json:"jeeves:dayOfWeek"`
    TimeOfDay  string                 `json:"jeeves:timeOfDay"`
    Duration   string                 `json:"jeeves:duration,omitempty"`
    Activity   Activity               `json:"adl:activity"`
    EnvContext EnvironmentalContext   `json:"jeeves:hadEnvironmentalContext"`
}

type Activity struct {
    Type     string   `json:"@type"`
    Name     string   `json:"name"`
    Location Location `json:"adl:location"`
}

type Location struct {
    Type string `json:"@type"`
    ID   string `json:"@id"`
    Name string `json:"name"`
}

type EnvironmentalContext struct {
    Type string `json:"@type"`
    ID   string `json:"@id"`
}

// NewEpisode creates a new behavioral episode
func NewEpisode(activity Activity, location Location) *BehavioralEpisode {
    now := time.Now()

    return &BehavioralEpisode{
        Context:   GetDefaultContext(),
        Type:      "jeeves:BehavioralEpisode",
        ID:        fmt.Sprintf("urn:uuid:%s", uuid.New().String()),
        StartedAt: now,
        DayOfWeek: now.Weekday().String(),
        TimeOfDay: getTimeOfDay(now),
        Activity: Activity{
            Type:     activity.Type,
            Name:     activity.Name,
            Location: location,
        },
        EnvContext: EnvironmentalContext{
            Type: "jeeves:EnvironmentalContext",
            ID:   fmt.Sprintf("urn:uuid:%s", uuid.New().String()),
        },
    }
}

func getTimeOfDay(t time.Time) string {
    hour := t.Hour()
    switch {
    case hour < 6:
        return "night"
    case hour < 12:
        return "morning"
    case hour < 17:
        return "afternoon"
    case hour < 21:
        return "evening"
    default:
        return "night"
    }
}
```

### Example Episode JSON-LD

When stored in Postgres, a behavioral episode looks like:

```json
{
  "@context": {
    "@vocab": "https://saref.etsi.org/core#",
    "jeeves": "https://jeeves.home/vocab#",
    "adl": "http://purl.org/adl#",
    "sosa": "http://www.w3.org/ns/sosa/",
    "prov": "http://www.w3.org/ns/prov#",
    "xsd": "http://www.w3.org/2001/XMLSchema#"
  },
  "@type": "jeeves:BehavioralEpisode",
  "@id": "urn:uuid:550e8400-e29b-41d4-a716-446655440000",
  "jeeves:startedAt": "2025-10-14T14:30:00Z",
  "jeeves:endedAt": "2025-10-14T15:45:00Z",
  "jeeves:dayOfWeek": "Tuesday",
  "jeeves:timeOfDay": "afternoon",
  "adl:activity": {
    "@type": "adl:Present",
    "name": "Present",
    "adl:location": {
      "@type": "saref:Room",
      "@id": "urn:room:living_room",
      "name": "living_room"
    }
  },
  "jeeves:hadEnvironmentalContext": {
    "@type": "jeeves:EnvironmentalContext",
    "@id": "urn:uuid:650e8400-e29b-41d4-a716-446655440001"
  }
}
```

### Usage in Agents

**Behavior Agent** ([internal/behavior/agent.go](../internal/behavior/agent.go:132-144)):

```go
import (
    "github.com/saaga0h/jeeves-platform/pkg/ontology"
)

func (a *Agent) startEpisode(location string) {
    // Create minimal episode
    episode := ontology.NewEpisode(
        ontology.Activity{
            Type: "adl:Present",
            Name: "Present",
        },
        ontology.Location{
            Type: "saref:Room",
            ID:   fmt.Sprintf("urn:room:%s", location),
            Name: location,
        },
    )

    // Store in Postgres
    jsonld, _ := json.Marshal(episode)

    var id string
    err := a.db.QueryRow(
        "INSERT INTO behavioral_episodes (jsonld) VALUES ($1) RETURNING id",
        jsonld,
    ).Scan(&id)

    if err != nil {
        a.logger.Error("Failed to create episode", "error", err)
        return
    }

    a.activeEpisodes[location] = id
    a.logger.Info("Episode started", "location", location, "id", id)
}
```

### Why JSON-LD?

1. **Interoperability**: Other smart home systems can understand our data
2. **Semantic Queries**: Can query by activity type using standard vocabularies
3. **Future-Proof**: AI/ML tools can leverage semantic relationships
4. **Standards-Based**: SAREF is an ETSI standard for smart home IoT
5. **Flexible**: Easy to add new fields while maintaining compatibility

### Postgres Storage

Episodes are stored as JSONB in Postgres for efficient querying:

```sql
CREATE TABLE behavioral_episodes (
    id SERIAL PRIMARY KEY,
    jsonld JSONB NOT NULL,
    location TEXT GENERATED ALWAYS AS (jsonld->'adl:activity'->'adl:location'->>'name') STORED,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_episodes_location ON behavioral_episodes(location);
CREATE INDEX idx_episodes_jsonld ON behavioral_episodes USING gin(jsonld);
```

**Query Examples**:

```sql
-- Find all episodes in living_room
SELECT * FROM behavioral_episodes WHERE location = 'living_room';

-- Find episodes by time of day
SELECT * FROM behavioral_episodes WHERE jsonld->>'jeeves:timeOfDay' = 'evening';

-- Find episodes by day of week
SELECT * FROM behavioral_episodes WHERE jsonld->>'jeeves:dayOfWeek' = 'Monday';

-- Calculate average episode duration
SELECT
  location,
  AVG(EXTRACT(EPOCH FROM (
    (jsonld->>'jeeves:endedAt')::timestamptz -
    (jsonld->>'jeeves:startedAt')::timestamptz
  )) / 60) as avg_duration_minutes
FROM behavioral_episodes
WHERE jsonld->>'jeeves:endedAt' IS NOT NULL
GROUP BY location;
```

### Dependencies

- **github.com/google/uuid**: UUID generation for episode IDs
- **encoding/json**: JSON marshaling for storage

### Future Enhancements

Future versions may include:
- **Activity Classification**: Detect watching TV, working, cooking, etc.
- **Environmental Enrichment**: Add lighting levels, temperature, etc. to episodes
- **Pattern Recognition**: Identify routines from episode history
- **Preference Learning**: Infer preferred settings for different activities

---

## Usage Patterns

### Standard Agent Bootstrap

Every agent follows this pattern in `cmd/{agent}/main.go`:

```go
func main() {
    // 1. Configuration
    cfg := config.NewConfig()
    cfg.ServiceName = "my-agent"
    cfg.LoadFromEnv()
    cfg.LoadFromFlags()
    if err := cfg.Validate(); err != nil {
        fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
        os.Exit(1)
    }

    // 2. Logging
    logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
        Level: parseLogLevel(cfg.LogLevel),
    }))
    slog.SetDefault(logger)

    // 3. Context & signals
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

    // 4. Dependencies
    mqttClient := mqtt.NewClient(cfg, logger)
    redisClient := redis.NewClient(cfg, logger)

    // 5. Agent
    agent := internal.NewAgent(mqttClient, redisClient, cfg, logger)

    // 6. Health server
    healthChecker := health.NewChecker(mqttClient, redisClient, logger)
    httpServer := startHealthServer(cfg.HealthPort, healthChecker, logger)

    // 7. Start agent
    agentErr := make(chan error, 1)
    go func() {
        if err := agent.Start(ctx); err != nil {
            agentErr <- err
        }
    }()

    // 8. Wait for shutdown
    select {
    case <-sigChan:
        logger.Info("Shutdown signal received")
    case err := <-agentErr:
        logger.Error("Agent failed", "error", err)
    }

    // 9. Graceful shutdown
    cancel()
    agent.Stop()
    httpServer.Shutdown(context.Background())
}
```

### Testing with Mocks

Interfaces enable easy testing:

```go
// Mock MQTT client
type MockMQTTClient struct {
    messages []struct{ topic string; payload []byte }
}

func (m *MockMQTTClient) Publish(topic string, qos byte, retained bool, payload []byte) error {
    m.messages = append(m.messages, struct{topic string; payload []byte}{topic, payload})
    return nil
}

// Test agent with mock
func TestAgent(t *testing.T) {
    mockMQTT := &MockMQTTClient{}
    mockRedis := &MockRedisClient{}
    cfg := config.NewConfig()
    logger := slog.Default()

    agent := internal.NewAgent(mockMQTT, mockRedis, cfg, logger)

    // ... test logic ...

    // Assert published messages
    assert.Equal(t, 1, len(mockMQTT.messages))
    assert.Equal(t, "automation/context/occupancy/study", mockMQTT.messages[0].topic)
}
```

### Error Handling Patterns

```go
// Connection errors
if err := mqttClient.Connect(ctx); err != nil {
    return fmt.Errorf("failed to connect to MQTT: %w", err)
}

// Redis errors with logging
if err := redisClient.ZAdd(ctx, key, score, member); err != nil {
    logger.Error("Failed to store data", "key", key, "error", err)
    // Continue or return based on criticality
}

// MQTT publish errors
if err := mqttClient.Publish(topic, 0, false, payload); err != nil {
    logger.Error("Failed to publish", "topic", topic, "error", err)
    // Usually continue - MQTT is best-effort
}
```

### Logging Standards

```go
// Structured logging with slog
logger.Info("Agent started",
    "service_name", cfg.ServiceName,
    "mqtt_broker", cfg.MQTTAddress(),
    "redis_host", cfg.RedisAddress())

logger.Debug("Processing message",
    "topic", msg.Topic(),
    "size", len(msg.Payload()))

logger.Warn("Retrying operation",
    "attempt", attempt,
    "max_attempts", maxAttempts,
    "error", err)

logger.Error("Operation failed",
    "operation", "store_sensor_data",
    "sensor_type", sensorType,
    "location", location,
    "error", err)
```

---

## Package Philosophy

### Why Interfaces?

1. **Testability**: Mock dependencies in unit tests
2. **Flexibility**: Swap implementations (e.g., different MQTT library)
3. **Decoupling**: Agents don't depend on concrete types

### Why Wrappers?

1. **Consistent API**: Standard methods across all agents
2. **Error Handling**: Wrap errors with context
3. **Logging**: Built-in logging of operations
4. **Abstractions**: Hide library-specific details

### Why Minimal?

1. **Low Overhead**: No performance penalty
2. **Easy to Understand**: Simple, obvious code
3. **Focused**: Only what agents actually need

---

## Related Documentation

- [ARCHITECTURE.md](./ARCHITECTURE.md) - System overview
- [AGENTS.md](./AGENTS.md) - Agent catalog
- [AGENT_DEVELOPMENT.md](./AGENT_DEVELOPMENT.md) - Building new agents
