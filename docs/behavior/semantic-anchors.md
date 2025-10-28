# Semantic Anchors System

## Overview

The semantic anchor system creates 128-dimensional vector embeddings for behavioral events, enabling flexible pattern recognition beyond fixed episode boundaries.

## Files Created

### Core Implementation

1. **`internal/behavior/types/anchor.go`** - Type definitions
   - `SemanticAnchor` - Core anchor with 128-dim pgvector embedding
   - `ActivitySignal` - Observed signals (motion, lighting, media)
   - `ActivityInterpretation` - Parallel activity detection
   - `BehavioralPattern` - Weight-based pattern tracking
   - `AnchorDistance` - Pre-computed semantic distances

2. **`internal/behavior/storage/anchor_storage.go`** - Storage layer
   - `CreateAnchor()` / `GetAnchor()` - Basic CRUD
   - `FindSimilarAnchors()` - Vector similarity search (cosine distance)
   - `StoreDistance()` / `GetDistance()` - Distance caching
   - `CreateInterpretation()` / `GetInterpretations()` - Activity interpretations
   - `CreatePattern()` / `UpdatePattern()` / `GetTopPatterns()` - Pattern management

3. **`internal/behavior/embedding/semantic_embedding.go`** - Embedding computation
   - `ComputeSemanticEmbedding()` - Pure function (128-dim output)
   - Dimensions:
     - `[0-3]` - Temporal cyclical (hour, day of week)
     - `[4-7]` - Seasonal cyclical (day of year, month)
     - `[8-11]` - Day type (weekday/weekend/holiday)
     - `[12-27]` - Spatial (location embedding)
     - `[28-43]` - Weather context
     - `[44-59]` - Lighting context
     - `[60-79]` - Activity signals
     - `[80-95]` - Household rhythm
     - `[96-127]` - Reserved for learned features

4. **`internal/behavior/context/gatherer.go`** - Context gathering
   - `GatherContext()` - Collects semantic dimensions
   - Weather (best effort from Redis `weather:current`)
   - Lighting state (recent events from `sensor:lighting:{location}`)
   - Time-based context (always available)

5. **`internal/behavior/anchor/creator.go`** - Anchor creation
   - `CreateAnchor()` - Main entry point
   - `CreateAnchorFromEvent()` - Convenience method for single events
   - `detectInterpretations()` - Parallel activity detection
   - Supports: watching_media, reading, cooking, sleeping, dining, working

6. **`internal/behavior/anchor_integration.go`** - Integration helpers
   - `initializeAnchorCreator()` - Setup during agent start
   - `createAnchorFromEvent()` - Create anchor from event struct
   - `buildSignalValue()` - Convert event to signal value

### Tests

7. **`internal/behavior/embedding/semantic_embedding_test.go`** - 20 comprehensive tests
   - Determinism (same inputs → same output)
   - Normalization (vector length = 1.0)
   - Cyclical encoding (hour 23 and hour 0 are close)
   - Context sensitivity (weather, lighting affect embedding)
   - Location encoding consistency

8. **`internal/behavior/storage/anchor_storage_test.go`** - Integration test stubs
   - Tests require PostgreSQL with pgvector
   - Use with testcontainers or test database

### Database Schema

9. **`e2e/init-scripts/02_semantic_anchors_schema.sql`** - Schema migration
   - Creates 4 tables with pgvector extension
   - IVFFlat index for vector similarity search

## Integration Steps

### Step 1: Enable pgvector Extension

The database migration `02_semantic_anchors_schema.sql` will run automatically with other init scripts. Verify pgvector is enabled:

```sql
SELECT * FROM pg_extension WHERE extname = 'vector';
```

### Step 2: Implement Database Connection Helper

In `internal/behavior/anchor_integration.go`, implement `getDBConnection()`:

```go
func (a *Agent) getDBConnection() (*sql.DB, error) {
    // Option A: If postgres.Client exposes underlying DB
    return a.pgClient.DB(), nil

    // Option B: If you need to create new connection
    // connStr := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s",
    //     a.cfg.Database.Host, a.cfg.Database.Port,
    //     a.cfg.Database.User, a.cfg.Database.Password,
    //     a.cfg.Database.Name)
    // return sql.Open("postgres", connStr)
}
```

### Step 3: Implement Redis Client Helper

In `internal/behavior/anchor_integration.go`, implement `getRedisClient()`:

```go
func (a *Agent) getRedisClient() *redis.Client {
    // Option A: If redis.Client is already *redis.Client
    return a.redis

    // Option B: If redis.Client wraps *redis.Client
    // return a.redis.Client()
}
```

### Step 4: Initialize Anchor Creator in Agent Start

In `internal/behavior/agent.go`, add initialization in `Start()` method:

```go
func (a *Agent) Start(ctx context.Context) error {
    a.logger.Info("Starting behavior agent")

    // ... existing MQTT connection ...

    // Initialize semantic anchor system (optional, non-fatal)
    if err := a.initializeAnchorCreator(a.cfg); err != nil {
        a.logger.Warn("Semantic anchor system not initialized", "error", err)
        // Continue without anchors - this is optional functionality
    } else {
        a.logger.Info("Semantic anchor system ready")
    }

    // ... rest of Start() ...
}
```

### Step 5: Create Anchors During Episode Detection

In `internal/behavior/agent.go`, within the `createEpisodesFromSensors()` function, add anchor creation:

```go
// Around line 633, after processing motion ON or lighting ON events
if (event.Type == "motion" && event.State == "on") ||
   (event.Type == "lighting" && event.State == "on") {

    // Create semantic anchor for this event (non-blocking)
    if err := a.createAnchorFromEvent(ctx, event); err != nil {
        // Log but don't fail episode creation
        a.logger.Debug("Failed to create anchor", "error", err)
    }

    // ... rest of existing episode detection logic ...
}
```

## Usage Example

Once integrated, anchors will be created automatically during episode detection:

```go
// Motion event at 8:30 AM in kitchen
event := Event{
    Location:  "kitchen",
    Timestamp: time.Date(2025, 1, 15, 8, 30, 0, 0, time.UTC),
    Type:      "motion",
    State:     "on",
}

// Anchor creator will:
// 1. Gather context (time_of_day="morning", household_mode="waking", weather, lighting)
// 2. Compute 128-dim embedding (temporal, spatial, weather, signals, rhythm)
// 3. Store anchor in semantic_anchors table
// 4. Detect interpretations (e.g., "cooking" if frequent motion in kitchen)
// 5. Store interpretations in anchor_interpretations table
```

## Querying Semantic Anchors

### Find Similar Behaviors

```sql
-- Find anchors similar to a given anchor (vector similarity search)
SELECT
    id,
    location,
    timestamp,
    context->>'time_of_day' as time_of_day,
    semantic_embedding <=> (
        SELECT semantic_embedding
        FROM semantic_anchors
        WHERE id = '...'
    ) AS distance
FROM semantic_anchors
ORDER BY semantic_embedding <=> (
    SELECT semantic_embedding
    FROM semantic_anchors
    WHERE id = '...'
)
LIMIT 10;
```

### Find Morning Kitchen Activities

```sql
SELECT
    a.id,
    a.timestamp,
    a.location,
    a.context->>'time_of_day' as time_of_day,
    i.activity_type,
    i.confidence
FROM semantic_anchors a
LEFT JOIN anchor_interpretations i ON i.anchor_id = a.id
WHERE
    a.location = 'kitchen'
    AND a.context->>'time_of_day' = 'morning'
ORDER BY a.timestamp DESC
LIMIT 20;
```

### Get Pattern Statistics

```sql
SELECT
    name,
    pattern_type,
    weight,
    observations,
    predictions,
    acceptances,
    ROUND(acceptances::numeric / NULLIF(predictions, 0), 2) as accuracy
FROM behavioral_patterns
ORDER BY weight DESC
LIMIT 10;
```

## Testing

### Run Embedding Tests

```bash
go test ./internal/behavior/embedding/... -v
```

Expected output:
- All tests should pass
- Tests verify determinism, normalization, cyclical encoding
- Tests verify context sensitivity

### Run Storage Tests (Requires PostgreSQL)

```bash
# Start PostgreSQL with pgvector using Docker
docker-compose up -d postgres

# Run migration scripts
psql -h localhost -U jeeves -d jeeves -f e2e/init-scripts/01_schema.sql
psql -h localhost -U jeeves -d jeeves -f e2e/init-scripts/02_semantic_anchors_schema.sql

# Run tests
go test ./internal/behavior/storage/... -v
```

## Performance Considerations

### Vector Index Tuning

The IVFFlat index uses `lists = 100` by default. Adjust based on data size:

- Small dataset (<10K anchors): `lists = 100`
- Medium dataset (10K-100K): `lists = sqrt(rows)`
- Large dataset (>100K): `lists = rows / 1000`

```sql
-- Rebuild index with different list count
DROP INDEX idx_semantic_similarity;
CREATE INDEX idx_semantic_similarity
ON semantic_anchors
USING ivfflat (semantic_embedding vector_cosine_ops)
WITH (lists = 500);
```

### Async Anchor Creation

For high-volume scenarios, create anchors asynchronously:

```go
// In createEpisodesFromSensors, use goroutine
go func(event Event) {
    ctx := context.Background()
    if err := a.createAnchorFromEvent(ctx, event); err != nil {
        a.logger.Debug("Failed to create anchor", "error", err)
    }
}(event)
```

### Debouncing

Avoid creating too many anchors for rapid-fire events:

```go
// Add debounce tracking to Agent
lastAnchorTime map[string]time.Time

// In createAnchorFromEvent
if lastTime, exists := a.lastAnchorTime[event.Location]; exists {
    if time.Since(lastTime) < 30*time.Second {
        return nil // Skip, too soon since last anchor
    }
}
a.lastAnchorTime[event.Location] = event.Timestamp
```

## Next Steps (Phase 4) - COMPLETED

Phase 4 pattern discovery has been implemented:

1. ✅ **Pattern Discovery** - DBSCAN clustering with multi-stage pipeline
2. ✅ **LLM Integration** - LLM-computed semantic distances for complex patterns
3. ✅ **Multi-Stage Clustering** - Detects and separates parallel activities
4. ⏳ **Learned Features** - Update dimensions `[96-127]` with learned embeddings (future)
5. ⏳ **Prediction** - Use weight-based patterns for activity prediction (future)

**See**: The "Multi-Stage Clustering and Pattern Discovery" section in [agent-behaviors.md](agent-behaviors.md) for details on parallel activity detection.

## Troubleshooting

### Anchor Creation Fails

Check logs for:
```
WARN Failed to create semantic anchor location=kitchen error=...
```

Common issues:
- Database connection not available
- pgvector extension not installed
- Context gathering timeout (weather service down)

### Vector Search Slow

If similarity queries are slow:
1. Verify index exists: `\d semantic_anchors`
2. Analyze query plan: `EXPLAIN ANALYZE SELECT ...`
3. Increase `lists` parameter in index
4. Consider approximate search with lower `probes`

### Tests Fail

Embedding tests should never fail (pure functions). If they do:
- Check for floating-point precision issues
- Verify pgvector-go version matches go.mod

Storage tests require PostgreSQL:
- Ensure pgvector extension installed
- Run migration scripts first
- Check connection credentials

## Questions?

For implementation questions, see:
- Phase 1-2 implementation (database schema, storage layer)
- Phase 3 implementation (this document)
- Existing behavior agent documentation in `docs/behavior/`
