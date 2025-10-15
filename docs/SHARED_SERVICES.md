# J.E.E.V.E.S. Shared Services

Documentation for the `pkg/` infrastructure packages used across all agents.

## Table of Contents
- [Overview](#overview)
- [MQTT Package](#mqtt-package)
- [Redis Package](#redis-package)
- [PostgreSQL Package](#postgresql-package)
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
├── postgres/       # PostgreSQL client abstraction
│   ├── interfaces.go  # Testable interfaces
│   ├── client.go      # lib/pq wrapper
│   └── queries.go     # Common query patterns
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

## PostgreSQL Package

**Location**: [`pkg/postgres/`](../pkg/postgres/)
**Purpose**: Abstraction layer for PostgreSQL with testable interfaces for long-term semantic data storage

### Overview

PostgreSQL serves as the **long-term semantic storage** layer for J.E.E.V.E.S., complementing Redis's time-series capabilities. While Redis handles high-frequency, short-lived data (< 24 hours), PostgreSQL stores:

- **Behavioral episodes** (months/years of retention)
- **JSON-LD semantic documents** for interoperability
- **Complex relational data** requiring ACID guarantees
- **Analytics and reporting data**

### Interface

```go
// Client is the main PostgreSQL interface
type Client interface {
    // Connection management
    Connect(ctx context.Context) error
    Close() error
    Ping(ctx context.Context) error
    
    // Transaction support
    Begin(ctx context.Context) (Tx, error)
    
    // Basic operations
    Exec(ctx context.Context, query string, args ...interface{}) (Result, error)
    Query(ctx context.Context, query string, args ...interface{}) (Rows, error)
    QueryRow(ctx context.Context, query string, args ...interface{}) Row
    
    // JSON-LD specific operations
    StoreJSONLD(ctx context.Context, table string, document interface{}) (string, error)
    QueryJSONLD(ctx context.Context, table string, filter JSONLDFilter) ([]map[string]interface{}, error)
    
    // Health and monitoring
    Stats() ConnectionStats
}

// Transaction interface for atomic operations
type Tx interface {
    Exec(ctx context.Context, query string, args ...interface{}) (Result, error)
    Query(ctx context.Context, query string, args ...interface{}) (Rows, error)
    QueryRow(ctx context.Context, query string, args ...interface{}) Row
    Commit(ctx context.Context) error
    Rollback(ctx context.Context) error
}

// JSONLDFilter for semantic queries
type JSONLDFilter struct {
    Type     string                 `json:"@type,omitempty"`
    Location string                 `json:"location,omitempty"`
    TimeFrom *time.Time             `json:"time_from,omitempty"`
    TimeTo   *time.Time             `json:"time_to,omitempty"`
    Custom   map[string]interface{} `json:"custom,omitempty"`
    Limit    int                    `json:"limit,omitempty"`
}

// ConnectionStats for monitoring
type ConnectionStats struct {
    OpenConnections     int
    InUseConnections    int
    IdleConnections     int
    WaitCount          int64
    WaitDuration       time.Duration
    MaxIdleClosed      int64
    MaxLifetimeClosed  int64
}
```

### Implementation

Wraps [lib/pq](https://github.com/lib/pq) with connection pooling and JSON-LD helpers:

```go
func NewClient(cfg *config.Config, logger *slog.Logger) (Client, error) {
    if !cfg.PostgresEnabled() {
        return &noopClient{}, nil  // Return no-op implementation
    }

    db, err := sql.Open("postgres", cfg.PostgresConnectionString())
    if err != nil {
        return nil, fmt.Errorf("failed to open postgres: %w", err)
    }

    // Configure connection pool
    db.SetMaxOpenConns(cfg.PostgresMaxOpenConns)     // Default: 10
    db.SetMaxIdleConns(cfg.PostgresMaxIdleConns)     // Default: 5
    db.SetConnMaxLifetime(cfg.PostgresConnMaxLife)   // Default: 30 minutes

    return &postgresClient{
        db:     db,
        cfg:    cfg,
        logger: logger,
    }, nil
}

type postgresClient struct {
    db     *sql.DB
    cfg    *config.Config
    logger *slog.Logger
}

func (c *postgresClient) Connect(ctx context.Context) error {
    return c.db.PingContext(ctx)
}

func (c *postgresClient) Close() error {
    return c.db.Close()
}

func (c *postgresClient) Ping(ctx context.Context) error {
    return c.db.PingContext(ctx)
}
```

### JSON-LD Specific Operations

#### Store JSON-LD Documents

```go
// StoreJSONLD stores a JSON-LD document with automatic ID generation
func (c *postgresClient) StoreJSONLD(ctx context.Context, table string, document interface{}) (string, error) {
    jsonld, err := json.Marshal(document)
    if err != nil {
        return "", fmt.Errorf("failed to marshal document: %w", err)
    }

    query := fmt.Sprintf("INSERT INTO %s (jsonld) VALUES ($1) RETURNING id", table)
    
    var id string
    err = c.db.QueryRowContext(ctx, query, jsonld).Scan(&id)
    if err != nil {
        return "", fmt.Errorf("failed to insert document: %w", err)
    }

    c.logger.Debug("Stored JSON-LD document",
        "table", table,
        "id", id,
        "size", len(jsonld))

    return id, nil
}

// UpdateJSONLD updates specific fields in a JSON-LD document
func (c *postgresClient) UpdateJSONLD(ctx context.Context, table string, id string, updates map[string]interface{}) error {
    if len(updates) == 0 {
        return nil
    }

    // Build JSONB update operations
    var setParts []string
    var args []interface{}
    argIndex := 1

    for path, value := range updates {
        jsonValue, err := json.Marshal(value)
        if err != nil {
            return fmt.Errorf("failed to marshal update value for path %s: %w", path, err)
        }

        setParts = append(setParts, fmt.Sprintf("jsonld = jsonb_set(jsonld, '{%s}', $%d)", path, argIndex))
        args = append(args, jsonValue)
        argIndex++
    }

    query := fmt.Sprintf(
        "UPDATE %s SET %s WHERE id = $%d",
        table,
        strings.Join(setParts, ", "),
        argIndex,
    )
    args = append(args, id)

    result, err := c.db.ExecContext(ctx, query, args...)
    if err != nil {
        return fmt.Errorf("failed to update document: %w", err)
    }

    rowsAffected, _ := result.RowsAffected()
    if rowsAffected == 0 {
        return fmt.Errorf("document not found: %s", id)
    }

    return nil
}
```

#### Query JSON-LD Documents

```go
// QueryJSONLD performs semantic queries on JSON-LD documents
func (c *postgresClient) QueryJSONLD(ctx context.Context, table string, filter JSONLDFilter) ([]map[string]interface{}, error) {
    var conditions []string
    var args []interface{}
    argIndex := 1

    // Build WHERE conditions based on filter
    if filter.Type != "" {
        conditions = append(conditions, fmt.Sprintf("jsonld->>'@type' = $%d", argIndex))
        args = append(args, filter.Type)
        argIndex++
    }

    if filter.Location != "" {
        conditions = append(conditions, fmt.Sprintf("location = $%d", argIndex))
        args = append(args, filter.Location)
        argIndex++
    }

    if filter.TimeFrom != nil {
        conditions = append(conditions, fmt.Sprintf("(jsonld->>'jeeves:startedAt')::timestamptz >= $%d", argIndex))
        args = append(args, filter.TimeFrom.Format(time.RFC3339))
        argIndex++
    }

    if filter.TimeTo != nil {
        conditions = append(conditions, fmt.Sprintf("(jsonld->>'jeeves:startedAt')::timestamptz <= $%d", argIndex))
        args = append(args, filter.TimeTo.Format(time.RFC3339))
        argIndex++
    }

    // Custom JSON-LD field filters
    for path, value := range filter.Custom {
        conditions = append(conditions, fmt.Sprintf("jsonld->>$%d = $%d", argIndex, argIndex+1))
        args = append(args, path, fmt.Sprintf("%v", value))
        argIndex += 2
    }

    // Build query
    query := fmt.Sprintf("SELECT id, jsonld FROM %s", table)
    if len(conditions) > 0 {
        query += " WHERE " + strings.Join(conditions, " AND ")
    }
    query += " ORDER BY created_at DESC"

    if filter.Limit > 0 {
        query += fmt.Sprintf(" LIMIT %d", filter.Limit)
    }

    rows, err := c.db.QueryContext(ctx, query, args...)
    if err != nil {
        return nil, fmt.Errorf("failed to query documents: %w", err)
    }
    defer rows.Close()

    var results []map[string]interface{}
    for rows.Next() {
        var id string
        var jsonld []byte

        if err := rows.Scan(&id, &jsonld); err != nil {
            return nil, fmt.Errorf("failed to scan row: %w", err)
        }

        var document map[string]interface{}
        if err := json.Unmarshal(jsonld, &document); err != nil {
            c.logger.Warn("Failed to unmarshal document", "id", id, "error", err)
            continue
        }

        // Add metadata
        document["_id"] = id
        results = append(results, document)
    }

    return results, nil
}
```

### Configuration

PostgreSQL configuration is **optional** - agents continue to work without it:

```go
// Config fields
type Config struct {
    // ... existing fields

    // PostgreSQL configuration (optional)
    PostgresHost        string
    PostgresPort        int
    PostgresDB          string
    PostgresUser        string
    PostgresPassword    string
    PostgresSSLMode     string
    PostgresMaxOpenConns int
    PostgresMaxIdleConns int
    PostgresConnMaxLife  time.Duration
}

func (c *Config) PostgresConnectionString() string {
    if c.PostgresHost == "" {
        return ""  // PostgreSQL disabled
    }

    sslMode := c.PostgresSSLMode
    if sslMode == "" {
        sslMode = "disable"  // Default for development
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

### Environment Variables

```bash
# PostgreSQL configuration (optional)
JEEVES_POSTGRES_HOST=postgres.service.consul
JEEVES_POSTGRES_PORT=5432
JEEVES_POSTGRES_DB=jeeves_behavior
JEEVES_POSTGRES_USER=jeeves
JEEVES_POSTGRES_PASSWORD=secret
JEEVES_POSTGRES_SSLMODE=require  # For production

# Connection pool settings
JEEVES_POSTGRES_MAX_OPEN_CONNS=10
JEEVES_POSTGRES_MAX_IDLE_CONNS=5
JEEVES_POSTGRES_CONN_MAX_LIFE=30m
```

### Usage Example

```go
// In agent initialization
postgresClient, err := postgres.NewClient(cfg, logger)
if err != nil {
    return fmt.Errorf("failed to initialize postgres: %w", err)
}

// Verify connection (only if enabled)
if cfg.PostgresEnabled() {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    
    if err := postgresClient.Connect(ctx); err != nil {
        return fmt.Errorf("failed to connect to postgres: %w", err)
    }
    
    logger.Info("Connected to PostgreSQL", "host", cfg.PostgresHost)
}

// Store behavioral episode
episode := &ontology.BehavioralEpisode{
    // ... episode data
}

id, err := postgresClient.StoreJSONLD(ctx, "behavioral_episodes", episode)
if err != nil {
    logger.Error("Failed to store episode", "error", err)
    // Graceful degradation - continue without storage
} else {
    logger.Info("Episode stored", "id", id)
}

// Query episodes by location
filter := postgres.JSONLDFilter{
    Location: "living_room",
    TimeFrom: &twentyFourHoursAgo,
    Limit:    10,
}

episodes, err := postgresClient.QueryJSONLD(ctx, "behavioral_episodes", filter)
if err != nil {
    logger.Error("Failed to query episodes", "error", err)
} else {
    logger.Info("Found episodes", "count", len(episodes))
}

// Cleanup
defer postgresClient.Close()
```

### Schema Management

#### Standard Table Structure

```sql
-- Behavioral episodes (primary use case)
CREATE TABLE behavioral_episodes (
    id SERIAL PRIMARY KEY,
    jsonld JSONB NOT NULL,
    location TEXT GENERATED ALWAYS AS (jsonld->'adl:activity'->'adl:location'->>'name') STORED,
    episode_type TEXT GENERATED ALWAYS AS (jsonld->>'@type') STORED,
    started_at TIMESTAMP GENERATED ALWAYS AS ((jsonld->>'jeeves:startedAt')::timestamptz) STORED,
    ended_at TIMESTAMP GENERATED ALWAYS AS ((jsonld->>'jeeves:endedAt')::timestamptz) STORED,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

-- Indexes for efficient querying
CREATE INDEX idx_episodes_location ON behavioral_episodes(location);
CREATE INDEX idx_episodes_type ON behavioral_episodes(episode_type);
CREATE INDEX idx_episodes_started_at ON behavioral_episodes(started_at);
CREATE INDEX idx_episodes_ended_at ON behavioral_episodes(ended_at);
CREATE INDEX idx_episodes_jsonld ON behavioral_episodes USING gin(jsonld);

-- Update timestamp trigger
CREATE OR REPLACE FUNCTION update_modified_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TRIGGER update_episodes_modtime
    BEFORE UPDATE ON behavioral_episodes
    FOR EACH ROW
    EXECUTE FUNCTION update_modified_column();
```

#### Extension Requirements

```sql
-- Enable JSON-LD and semantic querying
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";  -- UUID generation
CREATE EXTENSION IF NOT EXISTS "btree_gin";  -- GIN indexes on scalars
```

### Best Practices

#### 1. **Graceful Degradation**

Always handle PostgreSQL being unavailable:

```go
func (a *Agent) storeEpisode(episode *ontology.BehavioralEpisode) {
    if !a.cfg.PostgresEnabled() {
        a.logger.Debug("PostgreSQL not configured, skipping episode storage")
        return
    }

    id, err := a.postgresClient.StoreJSONLD(ctx, "behavioral_episodes", episode)
    if err != nil {
        a.logger.Error("Failed to store episode", "error", err)
        // Agent continues - PostgreSQL failure doesn't break real-time automation
        return
    }

    a.logger.Info("Episode stored", "id", id, "location", episode.Activity.Location.Name)
}
```

#### 2. **Connection Management**

```go
// Use context with timeout for operations
func (a *Agent) queryWithTimeout(filter postgres.JSONLDFilter) ([]map[string]interface{}, error) {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    return a.postgresClient.QueryJSONLD(ctx, "behavioral_episodes", filter)
}

// Check connection health
func (a *Agent) healthCheck() error {
    if !a.cfg.PostgresEnabled() {
        return nil  // PostgreSQL optional
    }

    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()

    return a.postgresClient.Ping(ctx)
}
```

#### 3. **Transaction Usage**

```go
// Atomic operations with transactions
func (a *Agent) updateEpisodeAtomic(episodeID string, updates map[string]interface{}) error {
    tx, err := a.postgresClient.Begin(ctx)
    if err != nil {
        return fmt.Errorf("failed to begin transaction: %w", err)
    }
    defer tx.Rollback(ctx)  // Safe to call even after commit

    // Update episode
    if err := a.updateEpisodeInTx(tx, episodeID, updates); err != nil {
        return err
    }

    // Update related data
    if err := a.updateMetricsInTx(tx, episodeID); err != nil {
        return err
    }

    return tx.Commit(ctx)
}
```

### Performance Considerations

#### Connection Pooling

- **Max Open Connections**: 10 (sufficient for agent workloads)
- **Max Idle Connections**: 5 (keep connections warm)
- **Connection Lifetime**: 30 minutes (rotate to handle network issues)

#### Query Optimization

- **Use GIN indexes** on JSONB columns for semantic queries
- **Generated columns** for frequently queried JSON-LD fields
- **Limit result sets** to prevent memory issues
- **Prepared statements** for repeated queries (handled by lib/pq)

#### Storage Efficiency

- **JSONB compression**: PostgreSQL automatically compresses large JSON documents
- **Partition tables** by time for large datasets (future enhancement)
- **Regular VACUUM** to maintain performance

### Monitoring

Monitor PostgreSQL health and performance:

```go
// Connection statistics
func (a *Agent) logConnectionStats() {
    if !a.cfg.PostgresEnabled() {
        return
    }

    stats := a.postgresClient.Stats()
    a.logger.Info("PostgreSQL connection stats",
        "open_connections", stats.OpenConnections,
        "in_use", stats.InUseConnections,
        "idle", stats.IdleConnections,
        "wait_count", stats.WaitCount,
        "wait_duration", stats.WaitDuration)
}

// Query performance monitoring
func (a *Agent) queryWithMetrics(filter postgres.JSONLDFilter) ([]map[string]interface{}, error) {
    start := time.Now()
    defer func() {
        duration := time.Since(start)
        a.logger.Debug("PostgreSQL query completed",
            "duration", duration,
            "filter", filter)
    }()

    return a.postgresClient.QueryJSONLD(ctx, "behavioral_episodes", filter)
}
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

# Postgres
JEEVES_POSTGRES_HOST="postgres"
JEEVES_POSTGRES_DB="jeeves_behavior"
JEEVES_POSTGRES_USER="jeeves"
JEEVES_POSTGRES_PASSWORD="jeeves_test"
JEEVES_POSTGRES_PORT=5432

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
