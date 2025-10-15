# Agent Development Guide

Practical guide for building new agents in the J.E.E.V.E.S. platform.

## Table of Contents
- [Quick Start](#quick-start)
- [Agent Template](#agent-template)
- [Code Organization](#code-organization)
- [Development Workflow](#development-workflow)
- [Testing Strategy](#testing-strategy)
- [Common Patterns](#common-patterns)
- [PostgreSQL Integration](#postgresql-integration)
- [Virtual Time Support](#virtual-time-support)
- [Best Practices](#best-practices)
- [Deployment](#deployment)

---

## Quick Start

### Prerequisites

- Go 1.23+
- Access to MQTT broker (Mosquitto)
- Access to Redis
- Understanding of the [architecture](./ARCHITECTURE.md)

### Creating a New Agent (5 Steps)

```bash
# 1. Create internal package structure
mkdir -p internal/myagent
touch internal/myagent/agent.go
touch internal/myagent/agent_test.go

# 2. Create cmd entry point
mkdir -p cmd/myagent-agent
touch cmd/myagent-agent/main.go

# 3. Add to Makefile AGENTS variable
# Edit Makefile: AGENTS = collector occupancy illuminance light myagent

# 4. Create documentation
mkdir -p docs/myagent
touch docs/myagent/mqtt-topics.md
touch docs/myagent/redis-schema.md
touch docs/myagent/agent-behaviors.md

# 5. Build and test
make build
./bin/myagent-agent --help
```

---

## Agent Template

### File: `cmd/myagent-agent/main.go`

Bootstrap code (keep minimal, ~150 lines):

```go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/saaga0h/jeeves-platform/internal/myagent"
	"github.com/saaga0h/jeeves-platform/pkg/config"
	"github.com/saaga0h/jeeves-platform/pkg/health"
	"github.com/saaga0h/jeeves-platform/pkg/mqtt"
	"github.com/saaga0h/jeeves-platform/pkg/redis"
)

func main() {
	// Load configuration with hierarchy: defaults → env → flags
	cfg := config.NewConfig()
	cfg.ServiceName = "myagent-agent" // Override default service name
	cfg.LoadFromEnv()
	cfg.LoadFromFlags()

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		os.Exit(1)
	}

	// Set up structured logging
	logLevel := parseLogLevel(cfg.LogLevel)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	logger.Info("Starting J.E.E.V.E.S. MyAgent",
		"version", "2.0",
		"service_name", cfg.ServiceName,
		"mqtt_broker", cfg.MQTTAddress(),
		"redis_host", cfg.RedisAddress(),
		"log_level", cfg.LogLevel)

	// Set up context with cancellation for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Initialize MQTT client
	mqttClient := mqtt.NewClient(cfg, logger)

	// Initialize Redis client
	redisClient := redis.NewClient(cfg, logger)

	// Create agent
	agent := myagent.NewAgent(mqttClient, redisClient, cfg, logger)

	// Start health check server
	healthChecker := health.NewChecker(mqttClient, redisClient, logger)
	httpServer := startHealthServer(cfg.HealthPort, healthChecker, logger)

	// Start agent in a goroutine
	agentErr := make(chan error, 1)
	go func() {
		if err := agent.Start(ctx); err != nil {
			logger.Error("Agent error", "error", err)
			agentErr <- err
		}
	}()

	// Wait for shutdown signal or agent error
	select {
	case <-sigChan:
		logger.Info("Shutdown signal received (SIGTERM/SIGINT)")
	case err := <-agentErr:
		logger.Error("Agent failed", "error", err)
	}

	// Graceful shutdown
	logger.Info("Initiating graceful shutdown")

	// Cancel context to stop agent
	cancel()

	// Stop agent
	if err := agent.Stop(); err != nil {
		logger.Error("Error stopping agent", "error", err)
	}

	// Shutdown health server
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("Error shutting down health server", "error", err)
	}

	logger.Info("MyAgent shutdown complete")
}

// startHealthServer starts the HTTP health check server
func startHealthServer(port int, checker *health.Checker, logger *slog.Logger) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", checker.HandlerFunc())

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go func() {
		logger.Info("Starting health check server", "port", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Health server error", "error", err)
		}
	}()

	return server
}

// parseLogLevel converts string log level to slog.Level
func parseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
```

### File: `internal/myagent/agent.go`

Business logic (this is where the work happens):

```go
package myagent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/saaga0h/jeeves-platform/pkg/config"
	"github.com/saaga0h/jeeves-platform/pkg/mqtt"
	"github.com/saaga0h/jeeves-platform/pkg/redis"
)

// Agent represents the myagent agent
type Agent struct {
	mqtt    mqtt.Client
	redis   redis.Client
	storage *Storage     // Optional: separate storage operations
	cfg     *config.Config
	logger  *slog.Logger

	// Optional: for periodic tasks
	ticker   *time.Ticker
	stopChan chan struct{}
}

// NewAgent creates a new agent with the given dependencies
func NewAgent(mqttClient mqtt.Client, redisClient redis.Client, cfg *config.Config, logger *slog.Logger) *Agent {
	// Optional: create storage helper
	storage := NewStorage(redisClient, cfg, logger)

	return &Agent{
		mqtt:     mqttClient,
		redis:    redisClient,
		storage:  storage,
		cfg:      cfg,
		logger:   logger,
		stopChan: make(chan struct{}),
	}
}

// Start starts the agent and begins processing
func (a *Agent) Start(ctx context.Context) error {
	a.logger.Info("Starting myagent agent",
		"service_name", a.cfg.ServiceName,
		"mqtt_broker", a.cfg.MQTTAddress())

	// Connect to MQTT broker
	if err := a.mqtt.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to MQTT: %w", err)
	}

	// Verify Redis connection
	if err := a.redis.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping Redis: %w", err)
	}

	// Subscribe to topics
	topic := "automation/sensor/mytype/+"
	if err := a.mqtt.Subscribe(topic, 0, a.handleMessage); err != nil {
		return fmt.Errorf("failed to subscribe to %s: %w", topic, err)
	}

	a.logger.Info("Subscribed to topic", "topic", topic)

	// Optional: start periodic tasks
	// a.startPeriodicTask()

	a.logger.Info("MyAgent started and ready")

	// Block until context is cancelled
	<-ctx.Done()
	a.logger.Info("MyAgent stopping")

	return nil
}

// Stop gracefully stops the agent
func (a *Agent) Stop() error {
	a.logger.Info("Stopping myagent agent")

	// Stop periodic tasks if any
	if a.ticker != nil {
		a.ticker.Stop()
	}
	close(a.stopChan)

	// Disconnect from MQTT
	a.mqtt.Disconnect()

	// Close Redis connection
	if err := a.redis.Close(); err != nil {
		a.logger.Error("Error closing Redis connection", "error", err)
		return err
	}

	a.logger.Info("MyAgent stopped")
	return nil
}

// handleMessage processes incoming MQTT messages
func (a *Agent) handleMessage(msg mqtt.Message) {
	topic := msg.Topic()
	payload := msg.Payload()

	a.logger.Debug("Received MQTT message", "topic", topic, "size", len(payload))

	// Extract location from topic
	parts := strings.Split(topic, "/")
	if len(parts) != 4 {
		a.logger.Warn("Invalid topic format", "topic", topic)
		return
	}
	location := parts[3]

	// Process message
	ctx := context.Background()
	if err := a.processMessage(ctx, location, payload); err != nil {
		a.logger.Error("Failed to process message",
			"location", location,
			"error", err)
		return
	}

	a.logger.Info("Message processed", "location", location)
}

// processMessage implements your business logic
func (a *Agent) processMessage(ctx context.Context, location string, payload []byte) error {
	// TODO: Implement your logic here

	// 1. Parse payload
	// 2. Query Redis for historical data if needed
	// 3. Perform analysis/decision making
	// 4. Publish results to MQTT
	// 5. Update Redis state if needed

	return nil
}

// Optional: startPeriodicTask for agents that need periodic execution
func (a *Agent) startPeriodicTask() {
	interval := 30 * time.Second
	a.ticker = time.NewTicker(interval)

	go func() {
		a.logger.Info("Starting periodic task", "interval", interval)
		for {
			select {
			case <-a.ticker.C:
				a.performPeriodicTask()
			case <-a.stopChan:
				return
			}
		}
	}()
}

func (a *Agent) performPeriodicTask() {
	ctx := context.Background()
	// TODO: Implement periodic logic
	a.logger.Debug("Performing periodic task")
}
```

### File: `internal/myagent/storage.go`

Optional: Separate Redis operations for cleaner code:

```go
package myagent

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/saaga0h/jeeves-platform/pkg/config"
	"github.com/saaga0h/jeeves-platform/pkg/redis"
)

// Storage wraps Redis operations for myagent
type Storage struct {
	redis  redis.Client
	cfg    *config.Config
	logger *slog.Logger
}

// NewStorage creates a new storage wrapper
func NewStorage(redisClient redis.Client, cfg *config.Config, logger *slog.Logger) *Storage {
	return &Storage{
		redis:  redisClient,
		cfg:    cfg,
		logger: logger,
	}
}

// Example: Store data
func (s *Storage) StoreData(ctx context.Context, location string, data interface{}) error {
	key := fmt.Sprintf("myagent:data:%s", location)

	// Serialize data
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal data: %w", err)
	}

	// Store with TTL
	if err := s.redis.Set(ctx, key, jsonData, 24*time.Hour); err != nil {
		return fmt.Errorf("failed to store data: %w", err)
	}

	s.logger.Debug("Stored data", "location", location, "key", key)
	return nil
}

// Example: Query data
func (s *Storage) GetData(ctx context.Context, location string) (interface{}, error) {
	key := fmt.Sprintf("myagent:data:%s", location)

	data, err := s.redis.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get data: %w", err)
	}

	// Deserialize
	var result interface{}
	if err := json.Unmarshal([]byte(data), &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal data: %w", err)
	}

	return result, nil
}
```

### File: `internal/myagent/agent_test.go`

Unit tests with mocks:

```go
package myagent

import (
	"context"
	"testing"
	"time"

	"github.com/saaga0h/jeeves-platform/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"log/slog"
	"os"
)

// MockMQTTClient is a mock MQTT client for testing
type MockMQTTClient struct {
	mock.Mock
}

func (m *MockMQTTClient) Connect(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockMQTTClient) Disconnect() {
	m.Called()
}

func (m *MockMQTTClient) Subscribe(topic string, qos byte, handler mqtt.MessageHandler) error {
	args := m.Called(topic, qos, handler)
	return args.Error(0)
}

func (m *MockMQTTClient) Publish(topic string, qos byte, retained bool, payload []byte) error {
	args := m.Called(topic, qos, retained, payload)
	return args.Error(0)
}

func (m *MockMQTTClient) IsConnected() bool {
	args := m.Called()
	return args.Bool(0)
}

// MockRedisClient is a mock Redis client for testing
type MockRedisClient struct {
	mock.Mock
}

func (m *MockRedisClient) Ping(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockRedisClient) Close() error {
	args := m.Called()
	return args.Error(0)
}

// Add other methods as needed...

func TestAgentStart(t *testing.T) {
	mockMQTT := new(MockMQTTClient)
	mockRedis := new(MockRedisClient)
	cfg := config.NewConfig()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Setup expectations
	mockMQTT.On("Connect", mock.Anything).Return(nil)
	mockRedis.On("Ping", mock.Anything).Return(nil)
	mockMQTT.On("Subscribe", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	// Create agent
	agent := NewAgent(mockMQTT, mockRedis, cfg, logger)

	// Start in goroutine
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := agent.Start(ctx)
	assert.NoError(t, err)

	// Verify expectations
	mockMQTT.AssertExpectations(t)
	mockRedis.AssertExpectations(t)
}
```

---

## Code Organization

### Directory Structure

```
jeeves-platform/
├── cmd/
│   └── myagent-agent/
│       └── main.go              # Bootstrap only (~150 lines)
│
├── internal/
│   └── myagent/
│       ├── agent.go             # Core orchestration
│       ├── agent_test.go        # Unit tests
│       ├── storage.go           # Redis operations (optional)
│       ├── processor.go         # Message processing (if complex)
│       ├── analysis.go          # Business logic
│       └── types.go             # Data structures
│
└── docs/
    └── myagent/
        ├── mqtt-topics.md       # MQTT specification
        ├── redis-schema.md      # Redis keys/structure
        ├── agent-behaviors.md   # Behavior documentation
        └── message-examples.md  # Example payloads
```

### File Responsibilities

| File | Purpose | Size Guideline |
|------|---------|----------------|
| `cmd/*/main.go` | Bootstrap, signals, shutdown | ~150 lines |
| `agent.go` | Orchestration, MQTT/Redis wiring | 200-400 lines |
| `storage.go` | Redis operations | 100-300 lines |
| `processor.go` | Message parsing/transformation | 100-200 lines |
| `analysis.go` | Core business logic | Variable |
| `types.go` | Data structures, constants | 50-150 lines |

---

## Development Workflow

### 1. Planning Phase

Before writing code:

```bash
# Create documentation first
mkdir -p docs/myagent

# Document MQTT topics
cat > docs/myagent/mqtt-topics.md <<EOF
# MQTT Topics - MyAgent

## Subscribed Topics
- \`automation/sensor/mytype/+\` - Purpose: ...

## Published Topics
- \`automation/context/mycontext/{location}\` - Purpose: ...
EOF

# Document Redis schema
cat > docs/myagent/redis-schema.md <<EOF
# Redis Schema - MyAgent

## Keys
- \`myagent:data:{location}\` (String/Hash/ZSet) - Purpose: ...
EOF
```

### 2. Implementation Phase

```bash
# Start with types
touch internal/myagent/types.go
# Define your data structures

# Implement core agent
touch internal/myagent/agent.go
# Implement Start(), Stop(), message handlers

# Add storage if needed
touch internal/myagent/storage.go
# Implement Redis operations

# Create bootstrap
touch cmd/myagent-agent/main.go
# Copy template from above
```

### 3. Testing Phase

```bash
# Unit tests
go test ./internal/myagent/... -v

# Build
make build

# Local testing with Docker
docker-compose -f e2e/docker-compose.test.yml up -d mosquitto redis

# Run agent locally
./bin/myagent-agent \
  --mqtt-broker localhost \
  --mqtt-port 1883 \
  --redis-host localhost \
  --redis-port 6379 \
  --log-level debug

# Publish test message
mosquitto_pub -t "automation/sensor/mytype/testloc" -m '{"test": "data"}'

# Monitor logs
# Check agent output for processing
```

### 4. E2E Testing Phase

```bash
# Create test scenario
cat > test-scenarios/myagent_test.yaml <<EOF
name: "MyAgent Basic Test"
description: "Test myagent functionality"

events:
  - time: 0
    sensor: "mytype:testloc"
    value: true

expectations:
  myagent_output:
    - time: 5
      topic: "automation/context/mycontext/testloc"
      payload:
        location: "testloc"
        status: "expected_value"
EOF

# Run E2E test
cd e2e
./run-test.sh myagent_test
```

---

## Testing Strategy

### Unit Tests

Test business logic in isolation:

```go
func TestProcessMessage(t *testing.T) {
	tests := []struct {
		name     string
		location string
		payload  []byte
		want     error
	}{
		{
			name:     "valid payload",
			location: "study",
			payload:  []byte(`{"value": 42}`),
			want:     nil,
		},
		{
			name:     "invalid json",
			location: "study",
			payload:  []byte(`{invalid`),
			want:     ErrInvalidPayload,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := setupTestAgent()
			err := agent.processMessage(context.Background(), tt.location, tt.payload)
			assert.Equal(t, tt.want, err)
		})
	}
}
```

### Integration Tests

Test with real MQTT/Redis (Docker):

```go
// +build integration

func TestAgentIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	// Connect to real services
	cfg := config.NewConfig()
	cfg.MQTTBroker = "localhost"
	cfg.RedisHost = "localhost"

	mqttClient := mqtt.NewClient(cfg, logger)
	redisClient := redis.NewClient(cfg, logger)

	// Test full flow
	// ...
}
```

Run with: `go test -tags=integration ./internal/myagent/...`

### E2E Tests

Test complete user scenarios (see [TESTING.md](./TESTING.md)):

```yaml
# test-scenarios/myagent_scenario.yaml
name: "Complete Flow Test"
events:
  - time: 0
    sensor: "mytype:location"
    value: 123

expectations:
  myagent:
    - time: 5
      topic: "automation/context/mycontext/location"
      payload:
        processed: true
```

---

## Common Patterns

### Pattern 1: Trigger-Based Processing

Agent reacts to MQTT triggers, reads Redis for history:

```go
func (a *Agent) handleTrigger(msg mqtt.Message) {
	location := extractLocation(msg.Topic())

	// Read historical data from Redis
	ctx := context.Background()
	history, err := a.storage.GetHistory(ctx, location, 10*time.Minute)
	if err != nil {
		a.logger.Error("Failed to get history", "error", err)
		return
	}

	// Analyze
	result := a.analyze(history)

	// Publish result
	a.publishResult(location, result)
}
```

### Pattern 2: Periodic Analysis

Agent runs analysis on schedule:

```go
func (a *Agent) startPeriodicAnalysis() {
	interval := time.Duration(a.cfg.AnalysisIntervalSec) * time.Second
	a.ticker = time.NewTicker(interval)

	go func() {
		for {
			select {
			case <-a.ticker.C:
				a.analyzeAllLocations()
			case <-a.stopChan:
				return
			}
		}
	}()
}

func (a *Agent) analyzeAllLocations() {
	ctx := context.Background()
	locations, _ := a.storage.GetAllLocations(ctx)

	for _, location := range locations {
		// Rate limiting, de-duplication, etc.
		if a.shouldAnalyze(location) {
			a.analyzeLocation(ctx, location)
		}
	}
}
```

### Pattern 3: State Tracking

Agent maintains in-memory state for quick decisions:

```go
type Agent struct {
	// ... mqtt, redis, etc.

	stateMux sync.RWMutex
	states   map[string]*LocationState
}

func (a *Agent) updateState(location string, newState string) {
	a.stateMux.Lock()
	defer a.stateMux.Unlock()

	if _, exists := a.states[location]; !exists {
		a.states[location] = &LocationState{}
	}

	a.states[location].State = newState
	a.states[location].LastUpdate = time.Now()
}

func (a *Agent) getState(location string) *LocationState {
	a.stateMux.RLock()
	defer a.stateMux.RUnlock()

	return a.states[location]
}
```

### Pattern 4: Rate Limiting

Prevent spam from rapid triggers:

```go
type RateLimiter struct {
	mutex         sync.RWMutex
	lastDecisions map[string]time.Time
}

func (r *RateLimiter) ShouldProcess(location string, minInterval time.Duration) bool {
	r.mutex.RLock()
	lastTime, exists := r.lastDecisions[location]
	r.mutex.RUnlock()

	if !exists {
		r.recordDecision(location)
		return true
	}

	if time.Since(lastTime) >= minInterval {
		r.recordDecision(location)
		return true
	}

	return false
}

func (r *RateLimiter) recordDecision(location string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.lastDecisions[location] = time.Now()
}
```

### Pattern 5: Graceful Degradation

Handle failures gracefully:

```go
func (a *Agent) processWithFallback(ctx context.Context, data Data) Result {
	// Try primary method
	result, err := a.primaryAnalysis(ctx, data)
	if err == nil {
		return result
	}

	a.logger.Warn("Primary analysis failed, using fallback",
		"error", err,
		"method", "fallback")

	// Fallback to simpler logic
	return a.fallbackAnalysis(data)
}
```

### Pattern 6: Postgres Integration

**For agents that need long-term semantic storage** (like behavioral episodes):

#### Step 1: Import the database driver

In your `cmd/myagent-agent/main.go`:

```go
import (
	"database/sql"

	_ "github.com/lib/pq"  // Import Postgres driver (blank import)

	"github.com/saaga0h/jeeves-platform/internal/myagent"
	// ... other imports
)
```

#### Step 2: Initialize Postgres connection

In your `main()` function:

```go
// Initialize MQTT client
mqttClient := mqtt.NewClient(cfg, logger)

// Initialize Redis client
redisClient := redis.NewClient(cfg, logger)

// Initialize Postgres (if needed)
var db *sql.DB
if cfg.PostgresEnabled() {
	connStr := cfg.PostgresConnectionString()
	var err error
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		logger.Error("Failed to connect to Postgres", "error", err)
		os.Exit(1)
	}

	if err := db.Ping(); err != nil {
		logger.Error("Failed to ping Postgres", "error", err)
		os.Exit(1)
	}

	logger.Info("Connected to Postgres", "host", cfg.PostgresHost)
	defer db.Close()
}

// Create agent
agent := myagent.NewAgent(mqttClient, redisClient, db, cfg, logger)
```

#### Step 3: Use Postgres in agent

In your `internal/myagent/agent.go`:

```go
import (
	"database/sql"
	"encoding/json"
)

type Agent struct {
	mqtt   mqtt.Client
	redis  redis.Client
	db     *sql.DB  // Can be nil if Postgres not needed
	cfg    *config.Config
	logger *slog.Logger
}

func NewAgent(mqttClient mqtt.Client, redisClient redis.Client, db *sql.DB, cfg *config.Config, logger *slog.Logger) (*Agent, error) {
	return &Agent{
		mqtt:   mqttClient,
		redis:  redisClient,
		db:     db,
		cfg:    cfg,
		logger: logger,
	}, nil
}

// Example: Store semantic data
func (a *Agent) storeEpisode(episode *ontology.BehavioralEpisode) error {
	if a.db == nil {
		a.logger.Warn("Postgres not configured, skipping episode storage")
		return nil
	}

	jsonld, err := json.Marshal(episode)
	if err != nil {
		return fmt.Errorf("failed to marshal episode: %w", err)
	}

	var id string
	err = a.db.QueryRow(
		"INSERT INTO behavioral_episodes (jsonld) VALUES ($1) RETURNING id",
		jsonld,
	).Scan(&id)

	if err != nil {
		return fmt.Errorf("failed to insert episode: %w", err)
	}

	a.logger.Info("Episode stored", "id", id)
	return nil
}

// Example: Query semantic data
func (a *Agent) getEpisodesByLocation(location string) ([]Episode, error) {
	if a.db == nil {
		return nil, fmt.Errorf("postgres not configured")
	}

	rows, err := a.db.Query(
		"SELECT id, jsonld FROM behavioral_episodes WHERE location = $1 ORDER BY created_at DESC LIMIT 10",
		location,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var episodes []Episode
	for rows.Next() {
		var id string
		var jsonld []byte
		if err := rows.Scan(&id, &jsonld); err != nil {
			return nil, err
		}

		var episode Episode
		if err := json.Unmarshal(jsonld, &episode); err != nil {
			a.logger.Warn("Failed to unmarshal episode", "id", id, "error", err)
			continue
		}

		episodes = append(episodes, episode)
	}

	return episodes, nil
}
```

#### Step 4: Add Postgres configuration

In your agent's environment variables:

```bash
# Postgres configuration (optional)
JEEVES_POSTGRES_HOST=postgres.service.consul
JEEVES_POSTGRES_PORT=5432
JEEVES_POSTGRES_DB=jeeves_behavior
JEEVES_POSTGRES_USER=jeeves
JEEVES_POSTGRES_PASSWORD=secret
```

#### When to Use Postgres vs Redis

**Use Redis for**:
- Time-series sensor data (sorted sets)
- Short-lived data (< 24 hours)
- High-frequency writes (1000s/sec)
- Real-time lookups
- Cache/temporary state

**Use Postgres for**:
- Long-term semantic records (months/years)
- Complex relational data
- JSON-LD documents for interoperability
- Data that needs ACID guarantees
- Analytics and reporting

**Example**: Behavior Agent uses both:
- Redis: Recent occupancy/lighting state (via other agents)
- Postgres: Historical behavioral episodes for pattern learning

### Pattern 7: Virtual Time Support

**For agents that need to support virtual time compression in E2E testing scenarios**:

#### Step 1: Add TimeManager to agent

In your `internal/myagent/agent.go`:

```go
import (
	"sync"
	"time"
)

// TimeManager handles virtual time for testing scenarios
type TimeManager struct {
	virtualTimeEnabled bool
	virtualStartTime   time.Time
	testStartTime      time.Time
	timeScale          float64
	mutex              sync.RWMutex
}

type Agent struct {
	mqtt   mqtt.Client
	redis  redis.Client
	cfg    *config.Config
	logger *slog.Logger

	timeManager *TimeManager  // Virtual time support
}

func NewAgent(mqttClient mqtt.Client, redisClient redis.Client, cfg *config.Config, logger *slog.Logger) (*Agent, error) {
	return &Agent{
		mqtt:        mqttClient,
		redis:       redisClient,
		cfg:         cfg,
		logger:      logger,
		timeManager: &TimeManager{},
	}, nil
}
```

#### Step 2: Implement time configuration

Subscribe to virtual time configuration and provide time methods:

```go
// ConfigureFromMQTT configures virtual time from MQTT message
func (tm *TimeManager) ConfigureFromMQTT(payload []byte) error {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	var config struct {
		VirtualStart  string  `json:"virtual_start"`
		TimeScale     float64 `json:"time_scale"`
		TestStartTime string  `json:"test_start_time"`
	}

	if err := json.Unmarshal(payload, &config); err != nil {
		return fmt.Errorf("failed to unmarshal time config: %w", err)
	}

	virtualStart, err := time.Parse(time.RFC3339, config.VirtualStart)
	if err != nil {
		return fmt.Errorf("invalid virtual_start time: %w", err)
	}

	testStart, err := time.Parse(time.RFC3339, config.TestStartTime)
	if err != nil {
		return fmt.Errorf("invalid test_start_time: %w", err)
	}

	tm.virtualTimeEnabled = true
	tm.virtualStartTime = virtualStart
	tm.testStartTime = testStart
	tm.timeScale = config.TimeScale

	return nil
}

// Now returns current time (virtual or real)
func (tm *TimeManager) Now() time.Time {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()

	if !tm.virtualTimeEnabled {
		return time.Now()
	}

	// Calculate virtual time
	elapsed := time.Since(tm.testStartTime)
	virtualElapsed := time.Duration(float64(elapsed) * tm.timeScale)
	return tm.virtualStartTime.Add(virtualElapsed)
}

// IsVirtualTimeEnabled returns whether virtual time is active
func (tm *TimeManager) IsVirtualTimeEnabled() bool {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()
	return tm.virtualTimeEnabled
}
```

#### Step 3: Subscribe to time configuration

In your agent's Start method:

```go
func (a *Agent) Start(ctx context.Context) error {
	// ... existing MQTT and Redis setup ...

	// Subscribe to time configuration for E2E testing
	if err := a.mqtt.Subscribe("automation/test/time_config", 0, a.handleTimeConfig); err != nil {
		a.logger.Warn("Failed to subscribe to time config", "error", err)
		// Don't fail - time config is optional
	}

	// Subscribe to your agent's topics
	topic := "automation/sensor/mytype/+"
	if err := a.mqtt.Subscribe(topic, 0, a.handleMessage); err != nil {
		return fmt.Errorf("failed to subscribe to %s: %w", topic, err)
	}

	// ... rest of agent startup
}

// Handle virtual time configuration
func (a *Agent) handleTimeConfig(msg mqtt.Message) {
	if err := a.timeManager.ConfigureFromMQTT(msg.Payload()); err != nil {
		a.logger.Error("Failed to configure virtual time", "error", err)
		return
	}

	a.logger.Info("Virtual time configured",
		"enabled", a.timeManager.IsVirtualTimeEnabled())
}
```

#### Step 4: Use virtual time for timestamps

Use `timeManager.Now()` instead of `time.Now()` for any stored timestamps:

```go
// Good: Use virtual time for stored data
func (a *Agent) processMessage(ctx context.Context, location string, payload []byte) error {
	timestamp := a.timeManager.Now()  // Virtual or real time

	// Store with virtual timestamp
	data := ProcessedData{
		Location:  location,
		Timestamp: timestamp,
		// ... other fields
	}

	if err := a.storage.Store(ctx, data); err != nil {
		return err
	}

	return nil
}

// Good: Use for episode tracking
func (a *Agent) startEpisode(location string) {
	episode := &BehavioralEpisode{
		ID:        generateUUID(),
		Location:  location,
		StartedAt: a.timeManager.Now(),  // Virtual time if testing
		// ... other fields
	}

	// Store in database with virtual timestamp
	a.storeEpisode(episode)
}

// Bad: Don't use for internal operations
func (a *Agent) logPerformance() {
	start := time.Now()  // Use real time for performance timing
	defer func() {
		a.logger.Debug("Operation took", "duration", time.Since(start))
	}()
}
```

#### Step 5: Testing with virtual time

E2E tests can now use virtual time configuration:

```yaml
# test-scenarios/myagent_virtual_time.yaml
name: "MyAgent Virtual Time Test"
description: "Test long scenario in compressed time"

# Enable virtual time compression
test_mode:
  enabled: true
  virtual_start: "2024-12-19T14:00:00Z"
  time_scale: 60  # 1 real second = 60 virtual seconds

events:
  - time: 0
    sensor: "mytype:location"
    value: 123
    description: "Event at virtual 14:00"

  - time: 3600  # 1 virtual hour later
    sensor: "mytype:location"
    value: 456
    description: "Event at virtual 15:00"

expectations:
  myagent:
    - time: 3605
      topic: "automation/context/mycontext/location"
      payload:
        timestamp: "~2024-12-19T15:00"  # Virtual timestamp
```

#### Virtual Time Best Practices

1. **Always use `timeManager.Now()`** for stored timestamps
2. **Use `time.Now()`** for internal operations (performance, logging)
3. **Subscribe to time config early** in agent startup
4. **Handle time config errors gracefully** (don't fail if unavailable)
5. **Test both modes** - ensure agent works with and without virtual time

This enables long-duration scenarios (hours) to run in minutes while maintaining realistic timestamps in your data.

---

## PostgreSQL Integration

For agents that need **long-term semantic storage** beyond Redis's 24-hour TTL, PostgreSQL provides robust relational database capabilities with JSON-LD support.

### When to Use PostgreSQL

**Ideal for**:
- Behavioral episodes and long-term patterns
- JSON-LD semantic documents 
- Data requiring ACID guarantees
- Complex relational queries
- Analytics and reporting

**Example Use Cases**:
- User activity episodes (Behavior Agent)
- Historical energy usage patterns
- Device health and maintenance records
- User preference learning data

### Connection Patterns

#### Connection String Configuration

Add to your `pkg/config/config.go`:

```go
type Config struct {
    // ... existing fields

    // PostgreSQL configuration
    PostgresHost     string
    PostgresPort     int
    PostgresDB       string
    PostgresUser     string
    PostgresPassword string
    PostgresSSLMode  string
}

func (c *Config) PostgresConnectionString() string {
    if c.PostgresHost == "" {
        return ""  // PostgreSQL optional
    }

    sslMode := c.PostgresSSLMode
    if sslMode == "" {
        sslMode = "disable"  // Default for local development
    }

    return fmt.Sprintf(
        "host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
        c.PostgresHost,
        c.PostgresPort,
        c.PostgresDB,
        c.PostgresUser,
        c.PostgresPassword,
        sslMode,
    )
}

func (c *Config) PostgresEnabled() bool {
    return c.PostgresHost != ""
}
```

#### Environment Variables

```bash
# PostgreSQL configuration (optional for most agents)
JEEVES_POSTGRES_HOST=postgres.service.consul
JEEVES_POSTGRES_PORT=5432
JEEVES_POSTGRES_DB=jeeves_behavior
JEEVES_POSTGRES_USER=jeeves
JEEVES_POSTGRES_PASSWORD=secret
JEEVES_POSTGRES_SSLMODE=require  # For production
```

#### Connection Pool Setup

```go
import (
    "database/sql"
    "time"
    _ "github.com/lib/pq"
)

func initPostgres(cfg *config.Config, logger *slog.Logger) (*sql.DB, error) {
    if !cfg.PostgresEnabled() {
        return nil, nil  // PostgreSQL optional
    }

    db, err := sql.Open("postgres", cfg.PostgresConnectionString())
    if err != nil {
        return nil, fmt.Errorf("failed to open postgres: %w", err)
    }

    // Configure connection pool
    db.SetMaxOpenConns(5)                   // Max concurrent connections
    db.SetMaxIdleConns(2)                   // Keep alive for reuse
    db.SetConnMaxLifetime(30 * time.Minute) // Rotate connections

    // Verify connection
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    if err := db.PingContext(ctx); err != nil {
        db.Close()
        return nil, fmt.Errorf("failed to ping postgres: %w", err)
    }

    logger.Info("Connected to PostgreSQL",
        "host", cfg.PostgresHost,
        "database", cfg.PostgresDB)

    return db, nil
}
```

### Semantic Storage Guidelines

#### JSON-LD Document Storage

Use JSONB for efficient JSON-LD storage with semantic querying:

```sql
-- Table for semantic documents
CREATE TABLE behavioral_episodes (
    id SERIAL PRIMARY KEY,
    jsonld JSONB NOT NULL,
    location TEXT GENERATED ALWAYS AS (jsonld->'adl:activity'->'adl:location'->>'name') STORED,
    episode_type TEXT GENERATED ALWAYS AS (jsonld->>'@type') STORED,
    started_at TIMESTAMP GENERATED ALWAYS AS ((jsonld->>'jeeves:startedAt')::timestamptz) STORED,
    created_at TIMESTAMP DEFAULT NOW()
);

-- Indexes for efficient querying
CREATE INDEX idx_episodes_location ON behavioral_episodes(location);
CREATE INDEX idx_episodes_type ON behavioral_episodes(episode_type);
CREATE INDEX idx_episodes_started_at ON behavioral_episodes(started_at);
CREATE INDEX idx_episodes_jsonld ON behavioral_episodes USING gin(jsonld);
```

#### Storage Operations

```go
import (
    "github.com/saaga0h/jeeves-platform/pkg/ontology"
)

// Store semantic episode
func (a *Agent) storeEpisode(episode *ontology.BehavioralEpisode) error {
    if a.db == nil {
        return nil  // PostgreSQL optional
    }

    jsonld, err := json.Marshal(episode)
    if err != nil {
        return fmt.Errorf("failed to marshal episode: %w", err)
    }

    var id string
    err = a.db.QueryRow(
        "INSERT INTO behavioral_episodes (jsonld) VALUES ($1) RETURNING id",
        jsonld,
    ).Scan(&id)

    if err != nil {
        return fmt.Errorf("failed to insert episode: %w", err)
    }

    a.logger.Info("Episode stored",
        "id", id,
        "location", episode.Activity.Location.Name,
        "type", episode.Type)

    return nil
}

// Update existing episode
func (a *Agent) updateEpisode(episodeID string, updates map[string]interface{}) error {
    if a.db == nil {
        return nil
    }

    // Build JSONB update operations
    var setParts []string
    var args []interface{}
    argIndex := 1

    for path, value := range updates {
        jsonValue, err := json.Marshal(value)
        if err != nil {
            return fmt.Errorf("failed to marshal update value: %w", err)
        }

        setParts = append(setParts, fmt.Sprintf("jsonld = jsonb_set(jsonld, '{%s}', $%d)", path, argIndex))
        args = append(args, jsonValue)
        argIndex++
    }

    query := fmt.Sprintf(
        "UPDATE behavioral_episodes SET %s WHERE id = $%d",
        strings.Join(setParts, ", "),
        argIndex,
    )
    args = append(args, episodeID)

    result, err := a.db.Exec(query, args...)
    if err != nil {
        return fmt.Errorf("failed to update episode: %w", err)
    }

    rowsAffected, _ := result.RowsAffected()
    if rowsAffected == 0 {
        return fmt.Errorf("episode not found: %s", episodeID)
    }

    return nil
}
```

#### Semantic Queries

Leverage JSONB for semantic web queries:

```go
// Query episodes by activity type
func (a *Agent) getEpisodesByActivityType(activityType string, limit int) ([]ontology.BehavioralEpisode, error) {
    if a.db == nil {
        return nil, fmt.Errorf("postgres not configured")
    }

    query := `
        SELECT jsonld 
        FROM behavioral_episodes 
        WHERE jsonld->'adl:activity'->>'@type' = $1 
        ORDER BY started_at DESC 
        LIMIT $2
    `

    rows, err := a.db.Query(query, activityType, limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var episodes []ontology.BehavioralEpisode
    for rows.Next() {
        var jsonld []byte
        if err := rows.Scan(&jsonld); err != nil {
            return nil, err
        }

        var episode ontology.BehavioralEpisode
        if err := json.Unmarshal(jsonld, &episode); err != nil {
            a.logger.Warn("Failed to unmarshal episode", "error", err)
            continue
        }

        episodes = append(episodes, episode)
    }

    return episodes, nil
}

// Complex semantic query with time range
func (a *Agent) getEpisodePatterns(location string, timeOfDay string, days int) (*EpisodePattern, error) {
    query := `
        SELECT 
            COUNT(*) as episode_count,
            AVG(EXTRACT(EPOCH FROM (
                (jsonld->>'jeeves:endedAt')::timestamptz - 
                (jsonld->>'jeeves:startedAt')::timestamptz
            ))) as avg_duration_seconds,
            array_agg(DISTINCT jsonld->>'jeeves:dayOfWeek') as days_of_week
        FROM behavioral_episodes 
        WHERE location = $1 
          AND jsonld->>'jeeves:timeOfDay' = $2
          AND started_at >= NOW() - INTERVAL '%d days'
    `

    var pattern EpisodePattern
    var daysOfWeek pq.StringArray

    err := a.db.QueryRow(fmt.Sprintf(query, days), location, timeOfDay).Scan(
        &pattern.EpisodeCount,
        &pattern.AvgDurationSeconds,
        &daysOfWeek,
    )

    if err != nil {
        return nil, err
    }

    pattern.DaysOfWeek = []string(daysOfWeek)
    return &pattern, nil
}
```

### Error Handling and Resilience

#### Graceful Degradation

```go
func (a *Agent) storeEpisodeWithFallback(episode *ontology.BehavioralEpisode) {
    // Primary: Store in PostgreSQL
    if err := a.storeEpisode(episode); err != nil {
        a.logger.Error("Failed to store episode in PostgreSQL", "error", err)

        // Fallback: Store in Redis with shorter TTL
        if err := a.storeEpisodeToRedis(episode); err != nil {
            a.logger.Error("Failed to store episode in Redis fallback", "error", err)
            // Could add to retry queue or drop with metric
        } else {
            a.logger.Warn("Episode stored to Redis fallback")
        }
    }
}
```

#### Retry Logic with Exponential Backoff

```go
func (a *Agent) storeEpisodeWithRetry(episode *ontology.BehavioralEpisode) error {
    backoff := time.Second
    maxRetries := 3

    for attempt := 1; attempt <= maxRetries; attempt++ {
        err := a.storeEpisode(episode)
        if err == nil {
            return nil
        }

        if attempt == maxRetries {
            return fmt.Errorf("failed after %d attempts: %w", maxRetries, err)
        }

        a.logger.Warn("Episode storage failed, retrying",
            "attempt", attempt,
            "backoff", backoff,
            "error", err)

        time.Sleep(backoff)
        backoff *= 2  // Exponential backoff
    }

    return nil
}
```

---

## Virtual Time Support

Virtual time enables **testing long-duration scenarios** (movies, work sessions) in compressed time while maintaining realistic timestamps in stored data.

### Implementation Overview

Virtual time works by:
1. **MQTT Time Configuration**: Test runner publishes time config
2. **TimeManager Component**: Calculates virtual timestamps
3. **Agent Integration**: Uses virtual time for stored data
4. **Database Consistency**: All timestamps reflect virtual time, not real test time

### TimeManager Implementation

#### Basic TimeManager

```go
// pkg/time/manager.go (or in your agent package)
type TimeManager struct {
    virtualTimeEnabled bool
    virtualStartTime   time.Time
    testStartTime      time.Time
    timeScale          float64
    mutex              sync.RWMutex
}

func NewTimeManager() *TimeManager {
    return &TimeManager{
        virtualTimeEnabled: false,
    }
}

// ConfigureFromMQTT sets up virtual time from test configuration
func (tm *TimeManager) ConfigureFromMQTT(payload []byte) error {
    tm.mutex.Lock()
    defer tm.mutex.Unlock()

    var config struct {
        VirtualStart  string  `json:"virtual_start"`
        TimeScale     float64 `json:"time_scale"`
        TestStartTime string  `json:"test_start_time"`
    }

    if err := json.Unmarshal(payload, &config); err != nil {
        return fmt.Errorf("failed to unmarshal time config: %w", err)
    }

    virtualStart, err := time.Parse(time.RFC3339, config.VirtualStart)
    if err != nil {
        return fmt.Errorf("invalid virtual_start time: %w", err)
    }

    testStart, err := time.Parse(time.RFC3339, config.TestStartTime)
    if err != nil {
        return fmt.Errorf("invalid test_start_time: %w", err)
    }

    tm.virtualTimeEnabled = true
    tm.virtualStartTime = virtualStart
    tm.testStartTime = testStart
    tm.timeScale = config.TimeScale

    return nil
}

// Now returns current time (virtual or real)
func (tm *TimeManager) Now() time.Time {
    tm.mutex.RLock()
    defer tm.mutex.RUnlock()

    if !tm.virtualTimeEnabled {
        return time.Now()
    }

    // Calculate virtual time
    elapsed := time.Since(tm.testStartTime)
    virtualElapsed := time.Duration(float64(elapsed) * tm.timeScale)
    return tm.virtualStartTime.Add(virtualElapsed)
}

// IsVirtualTimeEnabled returns whether virtual time is active
func (tm *TimeManager) IsVirtualTimeEnabled() bool {
    tm.mutex.RLock()
    defer tm.mutex.RUnlock()
    return tm.virtualTimeEnabled
}

// Reset disables virtual time (for production)
func (tm *TimeManager) Reset() {
    tm.mutex.Lock()
    defer tm.mutex.Unlock()
    tm.virtualTimeEnabled = false
}
```

### Agent Integration

#### Add TimeManager to Agent

```go
type Agent struct {
    mqtt   mqtt.Client
    redis  redis.Client
    db     *sql.DB
    cfg    *config.Config
    logger *slog.Logger

    timeManager *TimeManager  // Virtual time support
}

func NewAgent(mqttClient mqtt.Client, redisClient redis.Client, db *sql.DB, cfg *config.Config, logger *slog.Logger) *Agent {
    return &Agent{
        mqtt:        mqttClient,
        redis:       redisClient,
        db:          db,
        cfg:         cfg,
        logger:      logger,
        timeManager: NewTimeManager(),
    }
}
```

#### Subscribe to Time Configuration

```go
func (a *Agent) Start(ctx context.Context) error {
    // ... existing setup ...

    // Subscribe to virtual time configuration (E2E testing)
    if err := a.mqtt.Subscribe("automation/test/time_config", 0, a.handleTimeConfig); err != nil {
        a.logger.Warn("Failed to subscribe to time config", "error", err)
        // Don't fail - virtual time is optional
    }

    // ... rest of agent startup
}

func (a *Agent) handleTimeConfig(msg mqtt.Message) {
    if err := a.timeManager.ConfigureFromMQTT(msg.Payload()); err != nil {
        a.logger.Error("Failed to configure virtual time", "error", err)
        return
    }

    a.logger.Info("Virtual time configured",
        "enabled", a.timeManager.IsVirtualTimeEnabled(),
        "scale", a.timeManager.timeScale)
}
```

### Usage Patterns

#### For Stored Timestamps

**Always use virtual time for data that will be stored**:

```go
// Good: Use virtual time for database records
func (a *Agent) createEpisode(location string) {
    episode := &ontology.BehavioralEpisode{
        ID:        generateUUID(),
        StartedAt: a.timeManager.Now(),  // Virtual time if testing
        Location:  location,
        // ... other fields
    }

    a.storeEpisode(episode)
}

// Good: Use virtual time for Redis data
func (a *Agent) recordSensorReading(location string, value float64) {
    timestamp := a.timeManager.Now()
    
    data := SensorReading{
        Value:     value,
        Timestamp: timestamp,  // Virtual time
        Location:  location,
    }

    key := fmt.Sprintf("sensor:readings:%s", location)
    score := float64(timestamp.UnixMilli())
    
    a.redis.ZAdd(context.Background(), key, score, data)
}
```

#### For Internal Operations

**Use real time for performance, logging, and internal operations**:

```go
// Good: Use real time for performance measurement
func (a *Agent) processWithTiming(ctx context.Context, data []byte) error {
    start := time.Now()  // Real time for performance
    defer func() {
        duration := time.Since(start)
        a.logger.Debug("Processing completed", "duration", duration)
    }()

    // Use virtual time for stored results
    result := ProcessingResult{
        Timestamp: a.timeManager.Now(),  // Virtual time
        Data:      processData(data),
    }

    return a.storeResult(result)
}

// Good: Use real time for rate limiting
func (a *Agent) checkRateLimit(location string) bool {
    lastTime := a.lastOperationTime[location]
    if time.Since(lastTime) < 5*time.Second {  // Real time
        return false  // Rate limited
    }
    
    a.lastOperationTime[location] = time.Now()  // Real time
    return true
}
```

### Testing Considerations

#### Test Both Modes

Ensure your agent works with and without virtual time:

```go
func TestAgentWithVirtualTime(t *testing.T) {
    agent := setupTestAgent(t)

    // Configure virtual time
    timeConfig := `{
        "virtual_start": "2024-12-19T20:00:00Z",
        "time_scale": 60,
        "test_start_time": "2024-12-19T10:30:00Z"
    }`

    // Simulate time config message
    msg := &MockMessage{
        topic:   "automation/test/time_config",
        payload: []byte(timeConfig),
    }
    agent.handleTimeConfig(msg)

    // Test that stored timestamps use virtual time
    episode := agent.createEpisode("test_location")
    assert.True(t, agent.timeManager.IsVirtualTimeEnabled())
    
    // Virtual timestamp should be around 2024-12-19T20:00:00Z
    assert.Equal(t, 2024, episode.StartedAt.Year())
    assert.Equal(t, 12, int(episode.StartedAt.Month()))
    assert.Equal(t, 19, episode.StartedAt.Day())
    assert.Equal(t, 20, episode.StartedAt.Hour())
}

func TestAgentWithoutVirtualTime(t *testing.T) {
    agent := setupTestAgent(t)

    // No virtual time configuration
    episode := agent.createEpisode("test_location")
    assert.False(t, agent.timeManager.IsVirtualTimeEnabled())
    
    // Should use real time (approximately now)
    now := time.Now()
    assert.WithinDuration(t, now, episode.StartedAt, 1*time.Second)
}
```

#### E2E Test Scenarios

Virtual time enables testing long scenarios efficiently:

```yaml
# test-scenarios/long_workday.yaml
name: "8-Hour Workday - Compressed"
description: "Full workday in 4 minutes with virtual time"

test_mode:
  enabled: true
  virtual_start: "2024-12-19T09:00:00Z"
  time_scale: 120  # 1 real second = 120 virtual seconds

events:
  - time: 0
    sensor: "motion:home_office"
    value: true
    description: "Start work at virtual 9:00 AM"

  - time: 14400  # 4 virtual hours = 2 real minutes
    sensor: "motion:home_office"
    value: true
    description: "Lunch break at virtual 1:00 PM"

  - time: 28800  # 8 virtual hours = 4 real minutes
    type: occupancy
    location: home_office
    data:
      state: "empty"
    description: "End work at virtual 5:00 PM"

expectations:
  postgres:
    - time: 28810
      postgres_query: |
        SELECT 
          EXTRACT(hour FROM (jsonld->>'jeeves:startedAt')::timestamptz) as start_hour,
          EXTRACT(hour FROM (jsonld->>'jeeves:endedAt')::timestamptz) as end_hour
        FROM behavioral_episodes 
        WHERE location = 'home_office'
        ORDER BY id DESC LIMIT 1
      postgres_expected:
        start_hour: 9
        end_hour: 17  # Virtual 5 PM
```

This comprehensive virtual time system enables realistic long-duration testing while maintaining data integrity and realistic behavioral patterns.

---

## Best Practices

### 1. Logging

```go
// Good: Structured logging with context
a.logger.Info("Processing message",
	"location", location,
	"sensor_type", sensorType,
	"payload_size", len(payload))

// Good: Different log levels
a.logger.Debug("Cache hit", "key", key)  // Verbose
a.logger.Info("State changed", "old", old, "new", new)  // Important events
a.logger.Warn("Retrying operation", "attempt", attempt)  // Warnings
a.logger.Error("Operation failed", "error", err)  // Errors

// Bad: Unstructured logs
log.Printf("Got message: %v", msg)  // Don't use log.Printf

// Bad: Logging in hot path without checking level
for _, item := range millionItems {
	a.logger.Debug("Processing", "item", item)  // This is okay, slog checks level
}
```

### 2. Error Handling

```go
// Good: Wrap errors with context
if err := a.storage.Store(ctx, data); err != nil {
	return fmt.Errorf("failed to store data for location %s: %w", location, err)
}

// Good: Log and continue for non-critical errors
if err := a.mqtt.Publish(topic, 0, false, payload); err != nil {
	a.logger.Error("Failed to publish", "topic", topic, "error", err)
	// Continue - MQTT is best-effort
}

// Good: Fail fast for critical errors
if err := a.mqtt.Connect(ctx); err != nil {
	return fmt.Errorf("failed to connect to MQTT: %w", err)
}

// Bad: Swallowing errors
_ = someOperation()  // Don't ignore errors
```

### 3. Concurrency

```go
// Good: Protect shared state with mutex
func (a *Agent) updateState(key string, value interface{}) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.state[key] = value
}

// Good: Use context for cancellation
func (a *Agent) longOperation(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Do work
		}
	}
}

// Bad: Shared state without protection
a.state[key] = value  // Race condition!

// Bad: Ignoring context
func (a *Agent) longOperation(ctx context.Context) error {
	// Never checks ctx.Done()
	for i := 0; i < 1000000; i++ {
		// work...
	}
}
```

### 4. Configuration

```go
// Good: Use config package
type Agent struct {
	cfg *config.Config
}

func (a *Agent) getInterval() time.Duration {
	return time.Duration(a.cfg.AnalysisIntervalSec) * time.Second
}

// Good: Validate config early
func (a *Agent) Start(ctx context.Context) error {
	if a.cfg.AnalysisIntervalSec < 1 {
		return fmt.Errorf("invalid analysis interval: %d", a.cfg.AnalysisIntervalSec)
	}
	// ... continue
}

// Bad: Hardcoded values
time.Sleep(30 * time.Second)  // Use config!

// Bad: Magic numbers
if count > 42 {  // What is 42?
	// Use named constant or config
}
```

### 5. Testing

```go
// Good: Table-driven tests
func TestAnalyze(t *testing.T) {
	tests := []struct {
		name  string
		input Data
		want  Result
	}{
		{"case1", Data{...}, Result{...}},
		{"case2", Data{...}, Result{...}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Analyze(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Good: Test helpers
func setupTestAgent(t *testing.T) *Agent {
	cfg := config.NewConfig()
	mockMQTT := &MockMQTTClient{}
	mockRedis := &MockRedisClient{}
	return NewAgent(mockMQTT, mockRedis, cfg, slog.Default())
}

// Bad: No tests
// Bad: Tests with no assertions
func TestSomething(t *testing.T) {
	DoSomething()  // Does this work? Who knows!
}
```

---

## Deployment

### 1. Build for Production

```bash
# Build for current platform
make build

# Build for Linux (production)
GOOS=linux GOARCH=amd64 go build -o bin/myagent-agent-linux ./cmd/myagent-agent

# Multi-stage Docker build (use existing Dockerfile)
# The main Dockerfile builds all agents
docker build -t jeeves-platform .
```

### 2. Nomad Job Definition

Create `deploy/nomad/myagent-agent.nomad.hcl`:

```hcl
job "myagent-agent" {
  datacenters = ["dc1"]
  type        = "service"

  group "myagent" {
    count = 1

    network {
      port "health" {
        to = 8080
      }
    }

    task "myagent-agent" {
      driver = "exec"

      config {
        command = "/usr/local/bin/myagent-agent"
      }

      artifact {
        source      = "https://releases.example.com/myagent-agent-linux"
        destination = "local/myagent-agent"
      }

      template {
        data = <<EOH
JEEVES_MQTT_BROKER={{ key "jeeves/mqtt/broker" }}
JEEVES_MQTT_PORT={{ key "jeeves/mqtt/port" }}
JEEVES_REDIS_HOST={{ key "jeeves/redis/host" }}
JEEVES_REDIS_PORT={{ key "jeeves/redis/port" }}
JEEVES_LOG_LEVEL=info
EOH

        destination = "secrets/file.env"
        env         = true
      }

      resources {
        cpu    = 100  # MHz
        memory = 128  # MB
      }

      service {
        name = "myagent"
        port = "health"

        check {
          type     = "http"
          path     = "/health"
          interval = "10s"
          timeout  = "2s"
        }
      }
    }
  }
}
```

Deploy:

```bash
nomad job run deploy/nomad/myagent-agent.nomad.hcl
nomad job status myagent-agent
nomad logs -f -job myagent-agent
```

### 3. Environment Variables

Production configuration:

```bash
# Required
JEEVES_MQTT_BROKER=mqtt.service.consul
JEEVES_MQTT_PORT=1883
JEEVES_REDIS_HOST=redis.service.consul
JEEVES_REDIS_PORT=6379
JEEVES_SERVICE_NAME=myagent-agent

# Optional
JEEVES_MQTT_USER=myagent
JEEVES_MQTT_PASSWORD=secret
JEEVES_REDIS_PASSWORD=secret
JEEVES_LOG_LEVEL=info
JEEVES_HEALTH_PORT=8080

# Agent-specific
JEEVES_ANALYSIS_INTERVAL_SEC=30
# ... add your agent's config
```

---

## Checklist for New Agents

- [ ] Created `internal/myagent/agent.go` with Start/Stop
- [ ] Created `cmd/myagent-agent/main.go` bootstrap
- [ ] Added agent to `Makefile` AGENTS variable
- [ ] Wrote unit tests (`agent_test.go`)
- [ ] Documented MQTT topics (`docs/myagent/mqtt-topics.md`)
- [ ] Documented Redis schema (`docs/myagent/redis-schema.md`)
- [ ] Documented behavior (`docs/myagent/agent-behaviors.md`)
- [ ] Created E2E test scenario (`test-scenarios/myagent_test.yaml`)
- [ ] Tested locally with MQTT/Redis
- [ ] Ran E2E tests successfully
- [ ] Created Nomad job definition
- [ ] Documented configuration options
- [ ] Added logging with appropriate levels
- [ ] Implemented graceful shutdown
- [ ] Added health check endpoint (automatic via pkg/health)

---

## Related Documentation

- [ARCHITECTURE.md](./ARCHITECTURE.md) - System overview
- [AGENTS.md](./AGENTS.md) - Existing agent examples
- [SHARED_SERVICES.md](./SHARED_SERVICES.md) - Using pkg/ infrastructure
- [TESTING.md](./TESTING.md) - E2E testing framework

For specific agent implementations to learn from:
- Simple: [internal/collector/](../internal/collector/) (~400 LOC)
- Medium: [internal/illuminance/](../internal/illuminance/) (~800 LOC)
- Complex: [internal/occupancy/](../internal/occupancy/) (~3,300 LOC)
