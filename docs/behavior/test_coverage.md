# Behavior System Test Coverage

This document provides comprehensive overview of test coverage for the behavioral pattern recognition system.

## Test Structure

Tests are organized into three categories:
1. **E2E Behavioral Tests** - End-to-end scenario testing
2. **Unit Tests** - Component-level testing
3. **Benchmark Tests** - Performance and quality baselines

## E2E Behavioral Tests

Location: `/test-scenarios/`

### Active Production Tests

#### 1. `test_parallel_activities.yaml`
**Purpose**: Test location-temporal clustering with simultaneous activities in different locations

**Configuration**:
- Strategy: `progressive_learned`
- Location-Temporal Clustering: Enabled
- Progressive Activity Embeddings: Enabled

**Scenario**: Simulates parallel activities (TV watching in living room + working in study)

**Validates**:
- Location-temporal clustering correctly separates parallel activities
- Back-and-forth pattern detection works
- Patterns are location-specific
- Semantic validation prevents mixed-location patterns

**Key Expectations**:
- Multiple location-specific patterns discovered (4-6 patterns)
- Patterns should NOT mix locations
- Outlier rate acceptable

**Current Status**: ✅ Active - Used for validation of location-temporal clustering

---

#### 2. `test_progressive_learning.yaml`
**Purpose**: Validate progressive learning strategy and temporal decay

**Configuration**:
- Strategy: `progressive_learned`
- Batch Processing: Disabled (single discovery run)

**Scenario**: Three repeated morning routines (bedroom → bathroom → kitchen → dining)

**Validates**:
- LLM call reduction over time as patterns are learned
- Learned pattern reuse with confidence scores
- Similarity caching effectiveness
- Pattern observation accumulation
- Temporal decay of learned patterns

**Key Expectations**:
- Batch 1: Heavy LLM seeding (>=20 LLM distances)
- Batch 2: Learned patterns emerging (>=10 learned distances)
- Batch 3: High learned usage (>=30 learned/cached distances)
- Final: LLM calls < 50% of total
- Multiple patterns discovered (>=5)
- High confidence patterns (>=3 with confidence >= 0.7)

**Current Status**: ✅ Active - Primary test for progressive learning

---

### Benchmark Tests

#### 3. `test_llm_first_benchmark.yaml` (NEW)
**Purpose**: Quality baseline using LLM-first strategy

**Configuration**:
- Strategy: `llm_first` (benchmark/reference)
- All distances computed via LLM

**Scenario**: Two distinct activities (TV watching + study session)

**Validates**:
- LLM-first strategy is functional
- All distances use LLM (no vector fallback)
- High-quality semantic distances produced
- Pattern discovery works with LLM distances

**Key Expectations**:
- Most distances use LLM (>=10)
- No vector fallback (0 vector distances)
- Patterns discovered (>=2)
- Sufficient anchors created (>=10)

**Use Case**:
- Development benchmark for best possible results
- Establishing quality baselines
- Validating LLM integration is working
- Building learned pattern library

**Current Status**: ✅ Created - Ready for validation

---

### Legacy/Archive Tests

The following scenarios exist but may be outdated or superseded:

- `hallway_passthrough.yaml` - Occupancy detection (collector/occupancy focus)
- `study_working.yaml` - Single location session (collector/occupancy focus)
- `bedroom_morning.yaml` - Morning routine (collector/occupancy focus)
- `movie_night.yaml` - Entertainment scenario (collector/occupancy focus)
- `weekend.yaml` - Weekend patterns (collector/occupancy focus)
- `consolidation_test.yaml` - Anchor consolidation testing
- `pattern_discovery_test.yaml` - Basic pattern discovery
- `parallel_activities.yaml` - Earlier parallel activities test
- `saturday_parallel_activities.yaml` - Weekend parallel activities
- `test_sliding_window_parallel.yaml` - Sliding window tests
- `test_sliding_window_short.yaml` - Short window tests

**Note**: These tests were created during earlier development phases and may not reflect current architecture (location-temporal clustering, progressive learning).

---

## Unit Tests

Location: `/internal/behavior/`

### Existing Unit Tests

#### 1. `storage/anchor_storage_test.go`
**Coverage**: Anchor storage operations
- Creating anchors
- Querying anchors
- Pattern associations

#### 2. `embedding/semantic_embedding_test.go`
**Coverage**: Semantic embedding generation
- Structured tensor encoding
- Dimension validation
- Component encoding (temporal, spatial, etc.)

### Missing Unit Test Coverage

**High Priority**:
- ❌ `distance/computation_agent_test.go` - Strategy switching, distance computation
- ❌ `patterns/location_temporal_clustering_test.go` - Clustering logic, back-and-forth detection
- ❌ `patterns/semantic_validation_test.go` - Sequence validation, splitting
- ❌ `clustering/dbscan_test.go` - DBSCAN algorithm, epsilon selection
- ❌ `patterns/discovery_agent_test.go` - Pattern discovery orchestration

**Medium Priority**:
- ❌ `embedding/activity_embedding_test.go` - Activity fingerprints, caching
- ❌ `embedding/activity_llm_test.go` - LLM-based embeddings
- ❌ `distance/learned_patterns_test.go` - Learned pattern storage/retrieval

**Low Priority**:
- ❌ `patterns/pattern_interpreter_test.go` - Pattern interpretation
- ❌ `patterns/temporal_grouping_test.go` - Temporal grouping logic

---

## Test Coverage by Strategy

### llm_first Strategy
**E2E Tests**:
- ✅ `test_llm_first_benchmark.yaml` (NEW)

**Unit Tests**:
- ❌ None

**Status**: Functional, used as benchmark

**Recommendation**: Keep one E2E test to verify LLM integration remains functional

---

### progressive_learned Strategy
**E2E Tests**:
- ✅ `test_progressive_learning.yaml`
- ✅ `test_parallel_activities.yaml`

**Unit Tests**:
- ❌ None

**Status**: Production strategy, well tested via E2E

**Recommendation**: Add unit tests for learned pattern storage and retrieval logic

---

### vector_first Strategy
**E2E Tests**:
- ❌ None

**Unit Tests**:
- ❌ None

**Status**: Fast path, minimal testing

**Recommendation**: Low priority - simple mathematical computation

---

### learned_first Strategy (Deprecated)
**E2E Tests**:
- ❌ None

**Unit Tests**:
- ❌ None

**Status**: Legacy, superseded by progressive_learned

**Recommendation**: No new tests needed; remove strategy in future cleanup

---

### hybrid Strategy (Experimental)
**E2E Tests**:
- ❌ None

**Unit Tests**:
- ❌ None

**Status**: Experimental, not production-ready

**Recommendation**: Add E2E test if strategy is promoted to production

---

## Test Execution

### Running E2E Tests

**Single test**:
```bash
cd e2e
./run-progressive-test.sh  # Runs test_parallel_activities.yaml
```

**Specific scenario**:
```bash
cd e2e
./run-test.sh test_llm_first_benchmark
```

**All tests**:
```bash
cd e2e
./run-all-tests.sh
```

### Running Unit Tests

```bash
cd internal/behavior
go test ./...
```

### Test Requirements

**Infrastructure**:
- Docker and Docker Compose
- Ollama running on host with `mixtral:8x7b` model
- PostgreSQL with pgvector extension
- MQTT broker (Mosquitto)
- Redis

**Environment Variables**:
See `e2e/docker-compose.test.yml` for complete configuration

---

## Test Quality Metrics

### Current Coverage

**E2E Tests**:
- Location-Temporal Clustering: ✅ Good
- Progressive Learning: ✅ Good
- LLM-First (Benchmark): ✅ New
- Multi-Strategy Comparison: ❌ Missing

**Unit Tests**:
- Core Algorithms: ❌ Missing
- Strategy Implementation: ❌ Missing
- Storage Layer: ✅ Basic

**Overall Coverage**: ~30%

### Coverage Goals

**Phase 1** (Current):
- ✅ E2E tests for production strategies
- ✅ Benchmark test for quality baseline
- ❌ Unit tests for critical algorithms

**Phase 2** (Next):
- ❌ Unit tests for location-temporal clustering
- ❌ Unit tests for semantic validation
- ❌ Unit tests for distance strategies

**Phase 3** (Future):
- ❌ Multi-strategy comparison tests
- ❌ Performance benchmarks
- ❌ Regression test suite

---

## Test Maintenance

### When to Update Tests

1. **Architecture changes**: Update E2E tests when clustering strategy changes
2. **New strategies**: Add benchmark test for new distance strategies
3. **Bug fixes**: Add regression test when fixing bugs
4. **Configuration changes**: Update test expectations when thresholds change

### Test Ownership

- **E2E Behavioral Tests**: Owned by behavior system team
- **Unit Tests**: Owned by respective component developers
- **Benchmark Tests**: Owned by ML/algorithm team

---

## Known Test Gaps

### Critical Gaps

1. **No unit tests for location-temporal clustering**
   - Impact: High - Core algorithm untested at unit level
   - Recommendation: Add tests for session detection, sequence building, back-and-forth detection

2. **No unit tests for semantic validation**
   - Impact: High - Critical path for pattern quality
   - Recommendation: Add tests for validation thresholds, sequence splitting

3. **No strategy comparison tests**
   - Impact: Medium - Can't objectively compare strategy quality
   - Recommendation: Add multi-strategy benchmark on same scenario

4. **No performance regression tests**
   - Impact: Medium - Can't detect performance degradation
   - Recommendation: Add timing benchmarks for distance computation

### Non-Critical Gaps

1. **Limited unit test coverage**
   - Impact: Low - E2E tests provide good coverage
   - Recommendation: Add over time as bugs are found

2. **No stress/load testing**
   - Impact: Low - Production load is relatively low
   - Recommendation: Add when scaling becomes a concern

---

## Test Debugging

### Common Issues

**"Pattern mix locations"**:
- Check: Location-temporal clustering enabled
- Check: Back-and-forth detection working
- Solution: Verify `JEEVES_USE_LOCATION_TEMPORAL_CLUSTERING=true`

**"LLM calls not reducing"**:
- Check: Progressive learning enabled
- Check: Learned pattern storage working
- Solution: Verify database tables populated

**"No patterns discovered"**:
- Check: Minimum anchor count
- Check: Semantic validation thresholds
- Solution: Lower `pattern_min_anchors_for_discovery`

### Debugging Commands

**Check anchors**:
```sql
SELECT location, COUNT(*) FROM semantic_anchors GROUP BY location;
```

**Check distances**:
```sql
SELECT source, COUNT(*) FROM anchor_distances GROUP BY source;
```

**Check patterns**:
```sql
SELECT id, description, anchor_count FROM behavioral_patterns;
```

**Check learned patterns**:
```sql
SELECT pattern_key, confidence_score, observation_count FROM learned_patterns;
```

---

## Contributing

### Adding New E2E Tests

1. Create scenario YAML in `/test-scenarios/`
2. Follow naming convention: `test_<feature>_<description>.yaml`
3. Include comprehensive expectations
4. Test locally 3+ times to verify stability
5. Document in this file

### Adding Unit Tests

1. Create `*_test.go` file alongside implementation
2. Use table-driven tests for multiple cases
3. Mock external dependencies (LLM, database)
4. Aim for >80% coverage of critical paths
5. Run `go test -v` to verify

---

## References

- E2E Framework: `/e2e/README.md`
- Test Scenarios: `/test-scenarios/README.md`
- Distance Strategies: `/internal/behavior/distance/computation_agent.go` (package docs)
- Location-Temporal Clustering: `/internal/behavior/patterns/location_temporal_clustering.go`
