# Behavior Agent - How It Works

The **Behavior Agent** analyzes sensor data to detect behavioral patterns, track activities, and understand the routines of people in the home. It converts raw sensor events into meaningful behavioral episodes and identifies recurring patterns (vectors) that represent daily routines.

## What the Behavior Agent Does

The Behavior Agent serves as the **behavioral intelligence layer** for the Jeeves smart home system. It:

1. **Detects Episodes** - Identifies discrete periods of presence in specific locations
2. **Analyzes Patterns** - Finds recurring sequences of locations (behavioral vectors)
3. **Consolidates Activities** - Groups related episodes into meaningful macro-episodes
4. **Learns Routines** - Recognizes patterns like "morning routine" or "bedtime preparation"
5. **Provides Context** - Helps other agents understand what the person is doing

### Key Responsibilities

- **Episode Detection**: Creates behavioral episodes from motion and lighting sensor events
- **Vector Analysis**: Identifies sequential location patterns (e.g., bedroom → bathroom → kitchen)
- **Pattern Recognition**: Uses LLM to understand semantic meaning of behavioral sequences
- **Data Storage**: Maintains episode history in PostgreSQL for long-term analysis
- **Consolidation**: Periodically combines micro-episodes into meaningful macro-episodes

---

## How the Agent Starts Up

### Initialization Process

1. **Configuration Loading**
   - MQTT broker connection details
   - Redis connection for sensor data access
   - PostgreSQL connection for episode storage
   - LLM endpoint for intelligent pattern analysis
   - Consolidation parameters (lookback windows, gap thresholds)

2. **Database Setup**
   - Connects to PostgreSQL database
   - Verifies/creates required tables:
     - `behavioral_episodes` - Individual presence episodes
     - `behavioral_vectors` - Detected location patterns
     - `behavioral_vector_edges` - Pattern transition statistics
     - `macro_episodes` - Consolidated behavioral routines

3. **Service Connections**
   - Establishes Redis connection for reading sensor history
   - Connects to MQTT broker for consolidation triggers
   - Tests LLM connection for pattern analysis

4. **Topic Subscriptions**
   - Subscribes to `automation/behavior/consolidate` for manual triggers
   - Subscribes to `automation/test/time_config` for virtual time support
   - Does NOT subscribe to real-time sensor events (batch processing only)

5. **Ready State**
   - Waits for consolidation triggers
   - Processes sensor data in batch mode
   - Publishes behavioral insights to other agents

---

## How Episode Detection Works

Episodes are discrete periods when a person is present in a specific location. The agent creates episodes from two types of sensor events:

### Motion-Based Episodes

**Detection Method**:
- Motion sensor ON event in a location starts tracking
- Episode continues until motion detected in different location OR significant time gap
- Motion in new location closes previous episode and starts new one

**Example**:
```
07:00 - Motion in bedroom (episode start)
07:05 - Motion in bathroom (bedroom episode ends, bathroom episode starts)
07:08 - Motion in kitchen (bathroom episode ends, kitchen episode starts)
```

**Temporal Gap Detection**:
- If > 5 minutes pass with no motion in same location, episode is split
- This prevents one long episode when person leaves and returns
- Example: Kitchen prep (07:08-07:20) and kitchen cleanup (07:39-07:42) are separate episodes

### Lighting-Based Episodes

**Detection Method**:
- Manual lighting ON event can start an episode
- Manual lighting OFF event ends the episode
- Automated lighting events are ignored (status updates, not occupancy changes)

**Use Case**:
- Rooms without motion sensors (e.g., dining room during meals)
- Person sitting still (reading, eating) with minimal motion
- Manual lighting control indicates intentional presence/absence

**Example**:
```
07:22 - Dining room light turned ON manually (episode start)
07:37 - Dining room light turned OFF manually (episode end)
Result: 15-minute dining room episode
```

### Combined Processing

The agent merges motion and lighting events into a single timeline, sorted by timestamp:

1. **Gather all sensor events** from Redis within consolidation time window
2. **Sort chronologically** to understand actual sequence of activities
3. **Process events** using both motion transitions and lighting changes
4. **Create episodes** with appropriate start/end times and trigger types

**Episode End Times**:
- **Location transition**: End when new location's activity begins
- **Temporal gap**: End at last event before the gap
- **Manual lighting OFF**: End at exact time light turned off
- **Consolidation end**: End at current virtual time

---

## How Vector Detection Works

Behavioral vectors are sequential patterns of location visits that represent meaningful routines.

### What is a Vector?

A **vector** is a sequence of 2 or more locations visited in order, with timing information:

**Example Vector**:
```json
{
  "sequence": [
    {"location": "bedroom", "duration_sec": 299, "gap_to_next": 0},
    {"location": "bathroom", "duration_sec": 179, "gap_to_next": 0},
    {"location": "kitchen", "duration_sec": 839, "gap_to_next": 0}
  ],
  "quality_score": 1.0,
  "context": {
    "time_of_day": "morning",
    "day_of_week": "Friday",
    "location_count": 3,
    "transition_count": 2,
    "total_duration_sec": 1317
  }
}
```

This vector represents a "morning preparation" routine: wake up in bedroom, freshen up in bathroom, prepare breakfast in kitchen.

### Vector Detection Algorithm

**Step 1: Identify Sequential Episodes**
- Take episodes sorted by start time
- Look for consecutive episodes forming a sequence

**Step 2: Build Vectors**
- Start with each episode as potential vector start
- Extend vector if next episode has small gap (< 5 minutes)
- Stop extending when:
  - Gap is too large (> 5 minutes)
  - Would revisit same location in wrong context
  - End of episode list reached

**Step 3: Calculate Quality Score**
```
quality_score = average of all edge proximity scores
proximity_score = 1.0 - (gap_seconds / max_gap_seconds)
```

**Step 4: Store Vector Edges**
- Track each location transition (e.g., bedroom → bathroom)
- Record gap duration and temporal proximity
- Build statistics about common transitions

### Vector Context

Each vector includes contextual information:

- **Time of Day**: morning, afternoon, evening, night
- **Day of Week**: For weekly pattern recognition
- **Location Count**: Number of unique locations in sequence
- **Transition Count**: Number of location changes
- **Total Duration**: How long the entire vector took

This context helps the LLM understand what type of routine the vector represents.

---

## How Consolidation Works

Consolidation is the process of combining micro-episodes into meaningful macro-episodes that represent complete behavioral routines.

### Consolidation Trigger

**Manual Trigger**:
```
Topic: automation/behavior/consolidate
Payload: {
  "lookback_hours": 2,
  "location": "universe"
}
```

**Automatic Trigger** (future):
- Periodic consolidation (e.g., every hour)
- End-of-day consolidation for daily summaries
- Wake-up consolidation for overnight patterns

### Consolidation Process

**Phase 0: Episode Creation**
```
1. Query Redis for motion/lighting events in time window
2. Merge and sort all events chronologically
3. Detect episodes using location transitions and temporal gaps
4. Store episodes in PostgreSQL database
```

**Phase 1: Vector Detection**
```
1. Retrieve all episodes in time window
2. Build sequential location patterns
3. Calculate quality scores and edge statistics
4. Store vectors in database
```

**Phase 2: Rule-Based Consolidation**
```
1. Group episodes by location
2. Look for episodes in same location with small gaps (< 2 hours)
3. Merge into macro-episodes (e.g., multiple kitchen episodes → cooking session)
4. Store rule-based macro-episodes
```

**Phase 3: LLM Consolidation**
```
1. Take remaining unconsolidated episodes
2. Group by time window (< 2 hour gaps)
3. Ask LLM: "Do these activities form a meaningful routine?"
4. LLM provides:
   - Should merge: yes/no
   - Pattern type: morning_routine, bedtime_prep, etc.
   - Confidence: 0.0 to 1.0
   - Reasoning: Human-readable explanation
5. Create macro-episodes with semantic labels
6. Store LLM-generated insights
```

### Example Consolidation Output

**Micro Episodes** (7 detected):
- Bedroom: 5 minutes
- Bathroom: 3 minutes
- Kitchen: 14 minutes (prep)
- Dining room: 15 minutes
- Kitchen: 3 minutes (cleanup)
- Hallway: 1 minute
- Study: 7 minutes

**Vectors Detected** (1):
- bedroom → bathroom → kitchen → dining_room → kitchen → hallway → study

**Macro Episode Created**:
```json
{
  "pattern_type": "morning_routine",
  "duration_minutes": 49,
  "locations": ["bedroom", "bathroom", "kitchen", "dining_room", "hallway", "study"],
  "confidence": 0.95,
  "summary": "Morning routine: temporal proximity maintained, location sequence makes sense, typical morning activities"
}
```

---

## LLM Integration

The behavior agent uses a Large Language Model to understand behavioral patterns semantically.

### When LLM is Used

1. **Macro-Episode Consolidation**
   - Determining if episodes should be grouped together
   - Identifying pattern types (morning_routine, cooking, etc.)
   - Providing confidence scores and reasoning

2. **Pattern Type Classification**
   - Understanding what the behavioral sequence represents
   - Distinguishing between different types of routines
   - Learning person-specific patterns over time

### LLM Prompt Structure

The agent provides the LLM with:

**Episode Information**:
- Location sequence with durations
- Time gaps between episodes
- Time of day and day of week
- Total duration of sequence

**Analysis Request**:
```
Given these sequential episodes, determine:
1. Should they be merged into a single macro-episode?
2. What pattern type does this represent?
3. How confident are you (0.0 to 1.0)?
4. What is your reasoning?
```

**LLM Response Format**:
```json
{
  "should_merge": true,
  "pattern_type": "morning_routine",
  "confidence": 0.95,
  "reasoning": "Temporal proximity maintained, typical morning sequence"
}
```

### Fallback Behavior

If LLM is unavailable:
- Rule-based consolidation still works
- Vector detection continues normally
- Only semantic labeling is missing
- System remains functional for basic pattern tracking

---

## Virtual Time Support

The behavior agent supports virtual time for testing and simulation.

### How Virtual Time Works

**Configuration**:
```
Topic: automation/test/time_config
Payload: {
  "virtual_start": "2025-10-17T07:00:00Z",
  "time_scale": 6
}
```

- `virtual_start`: The simulated starting time
- `time_scale`: 6x = 1 real minute = 6 virtual minutes

### Impact on Behavior Agent

**Timestamp Handling**:
- All sensor data stored with virtual timestamps
- Episode start/end times use virtual clock
- Consolidation queries use virtual time ranges
- Redis queries filter by virtual timestamp scores

**Why It Matters**:
- Enables automated testing with compressed time
- Allows replay of historical scenarios
- Makes tests deterministic and repeatable
- Critical for development and validation

**Production Mode**:
- When no time_config received, uses real wall-clock time
- Seamlessly switches between test and production modes

---

## Data Storage

### PostgreSQL Schema

**behavioral_episodes**:
- Stores individual presence episodes
- JSONB format for flexible schema
- Indexed by location, time, and activity type
- Supports semantic queries and pattern matching

**behavioral_vectors**:
- Stores detected location sequences
- Includes timing, quality scores, and context
- Links to constituent micro-episodes
- Tracks consolidation status

**behavioral_vector_edges**:
- Statistics about location transitions
- Gap durations and proximity scores
- Builds knowledge about common patterns
- Helps refine future vector detection

**macro_episodes**:
- Consolidated behavioral routines
- Semantic labels from LLM analysis
- Links to micro-episodes and vectors
- Duration, confidence, and reasoning

### Why PostgreSQL?

- **Complex Queries**: Semantic search and pattern matching
- **JSONB Support**: Flexible schema for evolving ontology
- **Vector Extension**: Future: pgvector for similarity search
- **Relational Links**: Episodes → Vectors → Macro-Episodes
- **Long-term Storage**: Historical pattern analysis and learning

---

## Configuration Guide

### Essential Settings

```bash
# Required: Database connections
JEEVES_POSTGRES_HOST=postgres
JEEVES_POSTGRES_DB=jeeves_behavior
JEEVES_POSTGRES_USER=jeeves
JEEVES_POSTGRES_PASSWORD=secure_password

# Required: Service connections
JEEVES_MQTT_BROKER=mosquitto
JEEVES_REDIS_HOST=redis

# Required: LLM for pattern analysis
JEEVES_LLM_ENDPOINT=http://localhost:11434
JEEVES_LLM_MODEL=mixtral:8x7b

# Optional: Consolidation tuning
BEHAVIOR_MAX_GAP_MINUTES=5           # Episode gap threshold
BEHAVIOR_VECTOR_MAX_GAP_SECONDS=300  # Vector continuity threshold
BEHAVIOR_MACRO_MAX_GAP_MINUTES=120   # Macro-episode grouping threshold
```

### Production Considerations

**Performance**:
- Batch processing reduces real-time overhead
- Consolidation runs on-demand or scheduled
- Redis queries optimized with time ranges
- PostgreSQL indexes for fast pattern queries

**Scalability**:
- Episodes stored long-term in PostgreSQL
- Redis only needs recent sensor data (24 hours)
- Consolidation can process weeks of data
- LLM calls are rate-limited and cached

**Reliability**:
- Graceful handling of missing sensor data
- Falls back to rule-based if LLM unavailable
- Transaction safety for database operations
- Idempotent consolidation (can re-run safely)

---

## Monitoring and Troubleshooting

### Key Metrics

**Episode Detection**:
- Episodes created per consolidation run
- Episode duration distribution
- Trigger type breakdown (motion, lighting, temporal gap)

**Vector Detection**:
- Vectors detected per consolidation
- Vector length distribution
- Quality score distribution

**LLM Consolidation**:
- Macro-episodes created
- LLM confidence scores
- Analysis duration
- LLM availability

### Common Issues

**"No episodes being created"**:
1. Check Redis sensor data exists in time window
2. Verify virtual time configuration if testing
3. Check motion/lighting events have correct timestamps
4. Review consolidation trigger payload

**"Episodes have wrong durations"**:
1. Verify sensor timestamps are correct (virtual vs wall-clock)
2. Check temporal gap threshold configuration
3. Review lighting source field (manual vs automated)
4. Examine episode creation logs for trigger types

**"Missing dining room episodes"**:
1. Confirm lighting events are being stored in Redis
2. Check lighting source is "manual" not "automated"
3. Verify lighting ON/OFF event pairing
4. Review lighting-based episode creation logs

**"LLM not consolidating properly"**:
1. Check LLM endpoint connectivity
2. Verify LLM model is loaded (mixtral:8x7b)
3. Review LLM prompt and response in logs
4. Check confidence threshold settings

---

## Integration with Other Agents

### Collector Agent
- **Provides**: Sensor data in Redis (motion, lighting, environmental)
- **Publishes**: Trigger messages after storing data
- **Behavior Uses**: Historical sensor data for episode detection

### Occupancy Agent
- **Provides**: Room occupancy predictions
- **Note**: Behavior agent does NOT use occupancy predictions for episodes
- **Reason**: Occupancy is non-deterministic and doesn't work with virtual time

### Observer Agent
- **Consumes**: Episode and vector data for visualization
- **Displays**: Behavioral patterns, routine timelines, location sequences
- **Purpose**: Human-readable insights from behavioral analysis

### Future: Automation Agents
- **Will Use**: Behavioral patterns to predict next actions
- **Example**: Pre-warm coffee when morning routine vector starts
- **Context**: Macro-episodes provide semantic context for automations

---

## Example Behavioral Flow

A complete morning routine example:

```
07:00:36 - Motion in bedroom
         → Episode start: bedroom (motion event)

07:05:36 - Motion in bathroom
         → Episode end: bedroom (5 min)
         → Episode start: bathroom (motion transition)

07:08:36 - Motion in kitchen
         → Episode end: bathroom (3 min)
         → Episode start: kitchen (motion transition)

07:22:36 - Dining room light ON (manual)
         → Episode end: kitchen (14 min - temporal gap)
         → Episode start: dining_room (lighting event)

07:37:36 - Dining room light OFF (manual)
         → Episode end: dining_room (15 min - lighting off)

07:39:36 - Motion in kitchen
         → Episode start: kitchen (motion event)

07:42:36 - Motion in hallway
         → Episode end: kitchen (3 min)
         → Episode start: hallway (motion transition)

07:43:36 - Motion in study
         → Episode end: hallway (1 min)
         → Episode start: study (motion transition)

07:50:24 - Consolidation triggered
         → Episode end: study (7 min - consolidation boundary)
         → Vector detected: bedroom→bathroom→kitchen→dining_room→kitchen→hallway→study
         → LLM analysis: morning_routine (confidence: 0.95)
         → Macro-episode created: Morning routine, 49 minutes
```

**Result**:
- 7 micro-episodes detected
- 1 comprehensive behavioral vector
- 1 macro-episode: "morning_routine"
- Ready for automation and learning
