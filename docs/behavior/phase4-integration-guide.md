# Phase 4 Integration Guide: Distance Computation & Pattern Discovery

## Overview

Phase 4 is now complete! This guide shows how to integrate the distance computation and pattern discovery systems into your behavior agent.

## Components Implemented

### ✅ Core Components

1. **Distance Computation Agent** - `internal/behavior/distance/computation_agent.go`
   - Three strategies: `llm_first`, `learned_first`, `vector_first`
   - Test mode (trigger-based) and production mode (interval-based)
   - Learned distance caching
   - MQTT completion events

2. **DBSCAN Clustering Engine** - `internal/behavior/clustering/dbscan.go`
   - Configurable epsilon and minPoints
   - Distance matrix loading
   - Noise point detection

3. **Pattern Interpreter** - `internal/behavior/patterns/interpreter.go`
   - LLM-based cluster interpretation
   - Pattern creation with initial weight 0.1
   - Context extraction

4. **Pattern Discovery Orchestrator** - `internal/behavior/patterns/discovery_agent.go`
   - Coordinates clustering + interpretation
   - Test and production modes
   - MQTT completion events

5. **Storage Methods** - `internal/behavior/storage/anchor_storage.go` (updated)
   - `UpdateAnchorPattern()` - Link anchors to patterns
   - `GetAnchorsWithDistances()` - Get anchors for clustering
   - `GetAnchorsByIDs()` - Bulk anchor retrieval
   - `UpdatePatternWeight()` - Weight evolution
   - `UpdatePatternObserved()` - Track observations
   - `UpdatePatternPrediction()` - Track predictions

## Integration Steps

### Step 1: Update Agent Struct

Add the new agents to your behavior agent struct in `internal/behavior/agent.go`:

```go
type Agent struct {
	// ... existing fields ...

	// Phase 3: Semantic anchors
	anchorCreator *anchor.AnchorCreator

	// Phase 4: Pattern discovery
	distanceAgent  *distance.ComputationAgent
	discoveryAgent *patterns.DiscoveryAgent
}
```

### Step 2: Add Imports

Add the necessary imports:

```go
import (
	// ... existing imports ...

	"github.com/saaga0h/jeeves-platform/internal/behavior/clustering"
	"github.com/saaga0h/jeeves-platform/internal/behavior/distance"
	"github.com/saaga0h/jeeves-platform/internal/behavior/patterns"
)
```

### Step 3: Initialize Agents in NewAgent

Update the `NewAgent` function or add initialization logic:

```go
func (a *Agent) initializePatternDiscovery(cfg *config.Config) error {
	// Get database connection and storage
	db, err := a.getDBConnection()
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}

	anchorStorage := storage.NewAnchorStorage(db)

	// Create distance computation agent
	distanceConfig := distance.ComputationConfig{
		Strategy:      "vector_first", // production default
		Interval:      6 * time.Hour,
		BatchSize:     100,
		LookbackHours: 24,
	}

	// Check if test mode (from config or environment)
	if cfg.TestMode {
		distanceConfig.Strategy = "llm_first" // Build learned library in tests
	}

	a.distanceAgent = distance.NewComputationAgent(
		distanceConfig,
		anchorStorage,
		a.llm,  // Your LLM client
		a.mqtt,
		a.logger,
	)

	if cfg.TestMode {
		a.distanceAgent.EnableTestMode()
	}

	// Create clustering engine
	clusteringConfig := clustering.DBSCANConfig{
		Epsilon:   0.3,
		MinPoints: 5,
	}

	clusteringEngine := clustering.NewClusteringEngine(
		clusteringConfig,
		anchorStorage,
		a.logger,
	)

	// Create pattern interpreter
	patternInterpreter := patterns.NewPatternInterpreter(
		anchorStorage,
		a.llm,
		a.logger,
	)

	// Create pattern discovery agent
	discoveryConfig := patterns.DiscoveryConfig{
		Interval:      24 * time.Hour,
		MinAnchors:    10,
		LookbackHours: 48,
	}

	a.discoveryAgent = patterns.NewDiscoveryAgent(
		discoveryConfig,
		anchorStorage,
		clusteringEngine,
		patternInterpreter,
		a.mqtt,
		a.logger,
	)

	if cfg.TestMode {
		a.discoveryAgent.EnableTestMode()
	}

	a.logger.Info("Pattern discovery system initialized",
		"distance_strategy", distanceConfig.Strategy,
		"test_mode", cfg.TestMode)

	return nil
}
```

### Step 4: Start Agents in Agent.Start()

Update your `Start()` method to launch the new agents:

```go
func (a *Agent) Start(ctx context.Context) error {
	a.logger.Info("Starting behavior agent")

	// ... existing MQTT connection and time manager setup ...

	// Initialize semantic anchor system (Phase 3)
	if err := a.initializeAnchorCreator(a.cfg); err != nil {
		a.logger.Warn("Semantic anchor system not initialized", "error", err)
	}

	// Initialize pattern discovery system (Phase 4)
	if err := a.initializePatternDiscovery(a.cfg); err != nil {
		a.logger.Warn("Pattern discovery system not initialized", "error", err)
	} else {
		// Start distance computation agent
		go func() {
			if err := a.distanceAgent.Start(ctx); err != nil {
				a.logger.Error("Distance agent failed", "error", err)
			}
		}()

		// Start pattern discovery agent
		go func() {
			if err := a.discoveryAgent.Start(ctx); err != nil {
				a.logger.Error("Discovery agent failed", "error", err)
			}
		}()

		a.logger.Info("Pattern discovery agents started")
	}

	// ... existing consolidation trigger subscription ...

	<-ctx.Done()
	return nil
}
```

### Step 5: Add Test Mode Support

Ensure your config supports test mode. Add to `pkg/config/config.go`:

```go
type Config struct {
	// ... existing fields ...

	TestMode bool  // Enable test mode for agents
}
```

Or detect test mode from environment:

```go
testMode := os.Getenv("TEST_MODE") == "true"
```

## MQTT Topics

The new agents publish and subscribe to these topics:

### Distance Computation

**Subscribe:**
- `automation/behavior/compute_distances` - Trigger distance computation
  ```json
  {
    "lookback_hours": 6
  }
  ```

**Publish:**
- `automation/behavior/distances/completed` - Computation complete
  ```json
  {
    "distances_computed": 42,
    "timestamp": "2025-01-15T10:30:00Z"
  }
  ```

### Pattern Discovery

**Subscribe:**
- `automation/behavior/discover_patterns` - Trigger pattern discovery
  ```json
  {
    "min_anchors": 10,
    "lookback_hours": 6
  }
  ```

**Publish:**
- `automation/behavior/patterns/discovered` - Discovery complete
  ```json
  {
    "patterns_created": 3,
    "timestamp": "2025-01-15T10:35:00Z"
  }
  ```

## Test Scenario Example

Here's how to use the pattern discovery system in E2E tests:

```yaml
name: "Pattern Discovery Test"

# Configure pattern discovery
pattern_discovery:
  clustering:
    epsilon: 0.3
    min_points: 5

  distance_computation:
    strategy: llm_first  # Use LLM in tests
    batch_size: 100
    lookback_hours: 6

events:
  # Create events that generate anchors (0-240 seconds)
  - time: 0
    type: motion
    location: bedroom
    data: { state: "on" }

  - time: 30
    type: motion
    location: bathroom
    data: { state: "on" }

  # ... more events creating anchors ...

  # Trigger consolidation to create episodes (240 seconds)
  - time: 240
    type: behavior
    location: universe
    data:
      action: consolidate
      lookback_minutes: 5

  # Wait for episodes to be created
  - time: 250
    description: "Wait for episode processing"

  # Trigger distance computation (250 seconds)
  - time: 250
    type: mqtt
    topic: automation/behavior/compute_distances
    payload:
      lookback_hours: 1

  # Wait for distance computation
  - time: 260
    description: "Wait for distance computation"

  # Trigger pattern discovery (260 seconds)
  - time: 260
    type: mqtt
    topic: automation/behavior/discover_patterns
    payload:
      min_anchors: 5
      lookback_hours: 1

  # Wait for pattern discovery
  - time: 270
    description: "Wait for pattern discovery"

expectations:
  # Verify distances computed
  behavior_events:
    - topic: automation/behavior/distances/completed
      payload:
        distances_computed: "5+"
      timeout: 15

  # Verify patterns discovered
  behavior_events:
    - topic: automation/behavior/patterns/discovered
      payload:
        patterns_created: "1+"
      timeout: 15

  # Verify patterns in database
  postgres:
    - postgres_query: "SELECT COUNT(*) FROM behavioral_patterns"
      postgres_expected: "1+"
      description: "At least one pattern discovered"

    - postgres_query: "SELECT weight FROM behavioral_patterns LIMIT 1"
      postgres_expected: 0.1
      description: "Initial pattern weight is 0.1"

    - postgres_query: "SELECT COUNT(*) FROM anchor_distances"
      postgres_expected: "5+"
      description: "Distances computed and stored"
```

## Production Configuration

For production use, configure longer intervals:

```go
// Production distance computation
distanceConfig := distance.ComputationConfig{
	Strategy:      "learned_first",  // Use learned patterns first
	Interval:      6 * time.Hour,    // Every 6 hours
	BatchSize:     500,              // Process more at once
	LookbackHours: 72,               // Last 3 days
}

// Production pattern discovery
discoveryConfig := patterns.DiscoveryConfig{
	Interval:      24 * time.Hour,   // Daily
	MinAnchors:    20,               // Require more data
	LookbackHours: 168,              // Last week
}
```

## Weight Evolution

Patterns evolve their weight through use:

```go
// When pattern is observed in real behavior
storage.UpdatePatternObserved(ctx, patternID)
// Weight += 0.1

// When pattern used for prediction (accepted)
storage.UpdatePatternPrediction(ctx, patternID, true)
// Weight += 0.5

// When pattern used for prediction (rejected)
storage.UpdatePatternPrediction(ctx, patternID, false)
// Weight unchanged (patterns only grow)
```

## Querying Patterns

### Get Top Patterns by Weight

```go
patterns, err := storage.GetTopPatterns(ctx, 10)
// Returns top 10 patterns by weight
```

### SQL Queries

```sql
-- Get patterns with high success rate
SELECT
    name,
    pattern_type,
    weight,
    ROUND(acceptances::numeric / NULLIF(predictions, 0), 2) as accuracy
FROM behavioral_patterns
WHERE predictions > 0
ORDER BY weight DESC
LIMIT 10;

-- Get morning routines
SELECT
    name,
    weight,
    observations,
    typical_duration_minutes
FROM behavioral_patterns
WHERE pattern_type = 'morning_routine'
ORDER BY weight DESC;

-- Get anchors in a pattern
SELECT
    a.location,
    a.timestamp,
    a.context->>'time_of_day' as time_of_day
FROM semantic_anchors a
WHERE a.pattern_id = 'pattern-uuid-here'
ORDER BY a.timestamp;
```

## Troubleshooting

### No Patterns Discovered

**Check:**
1. Are anchors being created? `SELECT COUNT(*) FROM semantic_anchors`
2. Are distances computed? `SELECT COUNT(*) FROM anchor_distances`
3. Check clustering epsilon - may be too strict
4. Check min_points - may be too high

### LLM Errors

**Check:**
1. LLM client is configured and working
2. Check logs for LLM response parsing errors
3. Verify LLM returns valid JSON
4. Check prompt size limits

### Distance Computation Slow

**Solutions:**
1. Use `vector_first` strategy (fastest)
2. Reduce `batch_size`
3. Reduce `lookback_hours`
4. Check LLM response time

### Clustering Produces Only Noise

**Solutions:**
1. Decrease `epsilon` (more lenient distance threshold)
2. Decrease `min_points` (smaller clusters acceptable)
3. Check if distances are reasonable (0-1 range)
4. Verify distance computation strategy

## Next Steps

After Phase 4 is integrated:

1. **Monitor Pattern Quality** - Review discovered patterns
2. **Tune Clustering** - Adjust epsilon/minPoints for your data
3. **Implement Predictions** - Use patterns for activity prediction
4. **Pattern Merging** - Identify and merge similar patterns
5. **Pattern Archival** - Archive unused patterns

## Files Modified/Created

### Created:
- `internal/behavior/distance/computation_agent.go`
- `internal/behavior/clustering/dbscan.go`
- `internal/behavior/patterns/interpreter.go`
- `internal/behavior/patterns/discovery_agent.go`

### Modified:
- `internal/behavior/storage/anchor_storage.go` (added 7 new methods)

### To Modify:
- `internal/behavior/agent.go` (add initialization and start logic)
- `pkg/config/config.go` (add TestMode field, optional)

## Summary

Phase 4 adds autonomous pattern discovery to your behavior detection system:

1. **Distance Computation** - Computes semantic distances between anchors
2. **Clustering** - Groups similar anchors using DBSCAN
3. **Interpretation** - LLM identifies patterns from clusters
4. **Weight Evolution** - Patterns prove usefulness over time

The system is fully functional and ready for integration. All components support both test mode (trigger-based) and production mode (interval-based).
