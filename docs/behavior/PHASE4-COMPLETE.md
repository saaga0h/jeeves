# Phase 4 Implementation - COMPLETE ✅

## Summary

Phase 4 of the Semantic Anchor System has been successfully implemented! This adds autonomous pattern discovery through distance computation, clustering, and LLM-based interpretation.

## Components Implemented

### 1. Distance Computation Agent
**File:** `internal/behavior/distance/computation_agent.go`

**Features:**
- ✅ Three computation strategies:
  - `llm_first` - Always use LLM (test mode, builds learned library)
  - `learned_first` - Check learned patterns, fallback to LLM then vector
  - `vector_first` - Fast cosine distance (production default)
- ✅ Test mode (MQTT trigger-based) and production mode (interval-based)
- ✅ Learned distance caching with pattern keys
- ✅ MQTT completion events for test synchronization
- ✅ Cosine distance computation for vector strategy
- ✅ LLM prompt for semantic distance rating (0-1 scale)

**Configuration:**
```go
distanceConfig := distance.ComputationConfig{
    Strategy:      "vector_first",  // or "llm_first", "learned_first"
    Interval:      6 * time.Hour,   // production: 6h
    BatchSize:     100,             // pairs per batch
    LookbackHours: 24,              // how far back to look
}
```

### 2. DBSCAN Clustering Engine
**File:** `internal/behavior/clustering/dbscan.go`

**Features:**
- ✅ Full DBSCAN implementation
- ✅ Configurable epsilon (distance threshold) and minPoints (min cluster size)
- ✅ Distance matrix loading from storage
- ✅ Noise point detection
- ✅ Cluster expansion algorithm
- ✅ Detailed logging for debugging

**Configuration:**
```go
clusteringConfig := clustering.DBSCANConfig{
    Epsilon:   0.3,  // max distance for neighbors
    MinPoints: 5,    // min points to form cluster
}
```

### 3. Pattern Interpreter
**File:** `internal/behavior/patterns/interpreter.go`

**Features:**
- ✅ LLM-based cluster interpretation
- ✅ Extracts common context from anchors
- ✅ Creates BehavioralPattern with initial weight 0.1
- ✅ Identifies pattern types (routine, activity, transition)
- ✅ JSON response parsing with validation
- ✅ Handles clusters of any size

**Pattern Types Identified:**
- `morning_routine`, `evening_wind_down`
- `meal_preparation`, `work_session`, `leisure_time`
- `waking_up`, `going_to_bed`
- Custom types based on LLM analysis

### 4. Pattern Discovery Orchestrator
**File:** `internal/behavior/patterns/discovery_agent.go`

**Features:**
- ✅ Coordinates clustering + interpretation
- ✅ Test mode (MQTT trigger-based) and production mode (interval-based)
- ✅ Filters out noise and small clusters
- ✅ Stores patterns and links anchors
- ✅ MQTT completion events
- ✅ Configurable thresholds

**Configuration:**
```go
discoveryConfig := patterns.DiscoveryConfig{
    Interval:      24 * time.Hour,  // daily in production
    MinAnchors:    10,              // min anchors to analyze
    LookbackHours: 48,              // last 2 days
}
```

### 5. Storage Methods (Enhanced)
**File:** `internal/behavior/storage/anchor_storage.go`

**New Methods Added:**
```go
// Anchor-Pattern linking
UpdateAnchorPattern(ctx, anchorID, patternID) error

// Bulk retrieval
GetAnchorsWithDistances(ctx, since) ([]*SemanticAnchor, error)
GetAnchorsByIDs(ctx, ids) ([]*SemanticAnchor, error)

// Pattern weight evolution
UpdatePatternWeight(ctx, patternID, delta) error
UpdatePatternObserved(ctx, patternID) error
UpdatePatternPrediction(ctx, patternID, accepted bool) error
```

## Architecture Flow

```
Events → Anchors (Phase 3)
    ↓
Distance Computation Agent
    ├─ Vector: Cosine distance (fast)
    ├─ Learned: Check cache first
    └─ LLM: Semantic rating (accurate)
    ↓
Clustering Engine (DBSCAN)
    ├─ Load distance matrix
    ├─ Group similar anchors
    └─ Filter noise
    ↓
Pattern Interpreter (LLM)
    ├─ Analyze cluster characteristics
    ├─ Identify pattern type
    └─ Create BehavioralPattern (weight=0.1)
    ↓
Storage
    └─ behavioral_patterns table
    ↓
Weight Evolution (usage-based)
    ├─ Observed: +0.1
    ├─ Prediction accepted: +0.5
    └─ Prediction rejected: no change
```

## MQTT Topics

### Distance Computation

**Trigger:**
```bash
Topic: automation/behavior/compute_distances
Payload: {"lookback_hours": 6}
```

**Completion:**
```bash
Topic: automation/behavior/distances/completed
Payload: {"distances_computed": 42, "timestamp": "..."}
```

### Pattern Discovery

**Trigger:**
```bash
Topic: automation/behavior/discover_patterns
Payload: {"min_anchors": 10, "lookback_hours": 6}
```

**Completion:**
```bash
Topic: automation/behavior/patterns/discovered
Payload: {"patterns_created": 3, "timestamp": "..."}
```

## Integration Checklist

To integrate Phase 4 into your behavior agent:

- [ ] Add agent fields to Agent struct
- [ ] Add imports for distance, clustering, patterns
- [ ] Create `initializePatternDiscovery()` method
- [ ] Call initialization in `NewAgent()` or `Start()`
- [ ] Start agents in goroutines in `Start()`
- [ ] Add TestMode support to config (optional)
- [ ] Update test scenarios with pattern discovery triggers

See [phase4-integration-guide.md](phase4-integration-guide.md) for detailed integration instructions.

## Test Scenario Template

```yaml
name: "Pattern Discovery Test"

pattern_discovery:
  clustering:
    epsilon: 0.3
    min_points: 5
  distance_computation:
    strategy: llm_first
    batch_size: 100
    lookback_hours: 6

events:
  # ... events creating anchors (0-240s) ...

  - time: 250
    type: mqtt
    topic: automation/behavior/compute_distances
    payload: {lookback_hours: 1}

  - time: 260
    type: mqtt
    topic: automation/behavior/discover_patterns
    payload: {min_anchors: 5, lookback_hours: 1}

expectations:
  behavior_events:
    - topic: automation/behavior/distances/completed
      payload: {distances_computed: "5+"}
    - topic: automation/behavior/patterns/discovered
      payload: {patterns_created: "1+"}

  postgres:
    - postgres_query: "SELECT COUNT(*) FROM behavioral_patterns"
      postgres_expected: "1+"
    - postgres_query: "SELECT weight FROM behavioral_patterns LIMIT 1"
      postgres_expected: 0.1
```

## Production Configuration

```go
// Distance computation - every 6 hours
distanceConfig := distance.ComputationConfig{
    Strategy:      "learned_first",  // Check cache, then LLM, then vector
    Interval:      6 * time.Hour,
    BatchSize:     500,
    LookbackHours: 72,  // Last 3 days
}

// Pattern discovery - daily
discoveryConfig := patterns.DiscoveryConfig{
    Interval:      24 * time.Hour,
    MinAnchors:    20,
    LookbackHours: 168,  // Last week
}

// Clustering - moderate strictness
clusteringConfig := clustering.DBSCANConfig{
    Epsilon:   0.3,   // Adjust based on your data
    MinPoints: 5,     // Minimum cluster size
}
```

## Database Queries

### Get Top Patterns
```sql
SELECT
    name,
    pattern_type,
    weight,
    observations,
    ROUND(acceptances::numeric / NULLIF(predictions, 0), 2) as accuracy
FROM behavioral_patterns
ORDER BY weight DESC
LIMIT 10;
```

### Get Pattern Details
```sql
SELECT
    p.name,
    p.weight,
    COUNT(a.id) as anchor_count,
    p.context->>'typical_time_of_day' as time_of_day
FROM behavioral_patterns p
LEFT JOIN semantic_anchors a ON a.pattern_id = p.id
WHERE p.id = 'pattern-uuid-here'
GROUP BY p.id;
```

### Get Distance Statistics
```sql
SELECT
    source,
    COUNT(*) as count,
    ROUND(AVG(distance)::numeric, 3) as avg_distance,
    ROUND(MIN(distance)::numeric, 3) as min_distance,
    ROUND(MAX(distance)::numeric, 3) as max_distance
FROM anchor_distances
GROUP BY source
ORDER BY count DESC;
```

## Performance Notes

### Distance Computation
- **Vector strategy**: ~1ms per pair (fast, production default)
- **Learned strategy**: ~1ms per cached pair, falls back to LLM if not cached
- **LLM strategy**: ~500-2000ms per pair (accurate, use in tests to build library)

### Clustering
- **Time complexity**: O(n²) for distance loading, O(n log n) for DBSCAN
- **Memory**: Stores full distance matrix (n*(n-1)/2 distances)
- **Recommendation**: Process in batches if >1000 anchors

### Pattern Interpretation
- **LLM calls**: 1 per cluster
- **Time**: ~1-2 seconds per cluster
- **Cost**: Depends on LLM provider

## Troubleshooting

### Issue: No patterns discovered

**Check:**
1. `SELECT COUNT(*) FROM semantic_anchors` - Are anchors being created?
2. `SELECT COUNT(*) FROM anchor_distances` - Are distances computed?
3. Logs for clustering output - epsilon too strict?
4. Logs for cluster filtering - min_points too high?

**Solutions:**
- Decrease `epsilon` (e.g., 0.3 → 0.4) to allow looser clusters
- Decrease `min_points` (e.g., 5 → 3) for smaller clusters
- Increase `lookback_hours` to analyze more data

### Issue: LLM errors

**Check:**
1. LLM client configuration
2. Logs for JSON parsing errors
3. LLM response format (should be valid JSON)

**Solutions:**
- Add error logging to see full LLM response
- Verify LLM supports JSON format requests
- Switch to `vector_first` strategy temporarily

### Issue: Slow distance computation

**Solutions:**
1. Use `vector_first` strategy (fastest)
2. Reduce `batch_size`
3. Reduce `lookback_hours`
4. Increase computation `interval`

## Weight Evolution Examples

```go
// Pattern observed in actual behavior
storage.UpdatePatternObserved(ctx, patternID)
// weight: 0.1 → 0.2

// Pattern used for prediction (accepted by user)
storage.UpdatePatternPrediction(ctx, patternID, true)
// weight: 0.2 → 0.7

// Pattern used for prediction (rejected by user)
storage.UpdatePatternPrediction(ctx, patternID, false)
// weight: 0.7 → 0.7 (no change, patterns only grow)

// After multiple successful uses
// weight grows: 0.1 → 0.7 → 1.2 → 1.7 → ...
```

## Next Steps

1. **Integrate into behavior agent** - Follow integration guide
2. **Run test scenarios** - Verify pattern discovery works
3. **Tune clustering parameters** - Adjust epsilon/minPoints for your data
4. **Monitor pattern quality** - Review discovered patterns
5. **Implement predictions** - Use patterns for activity prediction (Phase 5)

## Files Created

```
internal/behavior/distance/computation_agent.go
internal/behavior/clustering/dbscan.go
internal/behavior/patterns/interpreter.go
internal/behavior/patterns/discovery_agent.go
docs/behavior/phase4-integration-guide.md
docs/behavior/PHASE4-COMPLETE.md (this file)
```

## Files Modified

```
internal/behavior/storage/anchor_storage.go
    + UpdateAnchorPattern()
    + GetAnchorsWithDistances()
    + GetAnchorsByIDs()
    + UpdatePatternWeight()
    + UpdatePatternObserved()
    + UpdatePatternPrediction()
```

## Status: ✅ COMPLETE

All Phase 4 components are implemented, tested for compilation, and documented. The system is ready for integration into the behavior agent.

**Virtual Time Support:** ✅ Preserved
All agents use the storage layer which respects the behavior agent's virtual time through timestamp-based queries.

**Test Mode Support:** ✅ Implemented
Both distance and discovery agents support test mode (MQTT trigger-based) and production mode (interval-based).

**Documentation:** ✅ Complete
- Integration guide with code examples
- MQTT topics documented
- Test scenario templates
- SQL query examples
- Troubleshooting guide
