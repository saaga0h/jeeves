# Behavior Agent - Message Examples

This document provides real-world examples of messages the Behavior Agent sends and receives, helping you integrate with the behavioral intelligence system.

---

## Input Messages (What The Agent Receives)

### Consolidation Trigger

**Topic**: `automation/behavior/consolidate`

**Basic Consolidation** (Last 2 hours):
```json
{
  "lookback_hours": 2,
  "location": "universe"
}
```

**Extended Analysis** (Last 24 hours):
```json
{
  "lookback_hours": 24,
  "location": "universe"
}
```

**Location-Specific** (Future):
```json
{
  "lookback_hours": 4,
  "location": "kitchen"
}
```

---

### Virtual Time Configuration

**Topic**: `automation/test/time_config`

**Standard Test Scenario** (6x time acceleration):
```json
{
  "virtual_start": "2025-10-17T07:00:00Z",
  "time_scale": 6
}
```

**Real-Time Test** (No acceleration):
```json
{
  "virtual_start": "2025-10-17T07:00:00Z",
  "time_scale": 1
}
```

**Ultra-Fast Test** (60x acceleration):
```json
{
  "virtual_start": "2025-10-17T07:00:00Z",
  "time_scale": 60
}
```

---

## Output Messages (What The Agent Publishes)

### Consolidation Completion

**Topic**: `automation/behavior/consolidation/completed`

**Successful Consolidation**:
```json
{
  "completed_at": "2025-10-17T07:50:24Z",
  "episodes_created": 7,
  "vectors_detected": 1,
  "macros_created": 1
}
```

**No Pattern Detection**:
```json
{
  "completed_at": "2025-10-17T14:30:00Z",
  "episodes_created": 0,
  "vectors_detected": 0,
  "macros_created": 0
}
```

**Large Pattern Set**:
```json
{
  "completed_at": "2025-10-18T08:00:00Z",
  "episodes_created": 45,
  "vectors_detected": 8,
  "macros_created": 3
}
```

---

## PostgreSQL Data Examples

These examples show the data stored in PostgreSQL after consolidation.

### Behavioral Episodes

**Query**:
```sql
SELECT id, location, started_at_text, ended_at_text,
       activity_type, (jsonld->>'jeeves:triggerType') as trigger_type
FROM behavioral_episodes
WHERE started_at_text::timestamptz >= '2025-10-17T07:00:00Z'
ORDER BY started_at_text;
```

**Example Results**:
```
id                                   | location    | started_at_text                | ended_at_text                | activity_type | trigger_type
-------------------------------------|-------------|--------------------------------|------------------------------|---------------|---------------
8357600f-ae6a-4098-adbf-3375c5cbd00b | bedroom     | 2025-10-17T07:00:36.061257516Z | 2025-10-17T07:05:36Z        | adl:Present   | motion_transition
c385811c-99a0-4bea-8e74-de7cba38300b | bathroom    | 2025-10-17T07:05:36.098478906Z | 2025-10-17T07:08:36Z        | adl:Present   | motion_transition
1e304d04-6ccf-4666-a817-fe3e167ab9d9 | kitchen     | 2025-10-17T07:08:36.134364486Z | 2025-10-17T07:22:36Z        | adl:Present   | temporal_gap
d29b8dba-0946-4150-8ff8-2ab8b2a69e8c | dining_room | 2025-10-17T07:22:36.078924618Z | 2025-10-17T07:37:36Z        | adl:Present   | lighting_off
e9583397-a5b7-4144-8f47-e6836adc00a3 | kitchen     | 2025-10-17T07:39:36.098422836Z | 2025-10-17T07:42:36Z        | adl:Present   | motion_transition
c542f571-c4e8-4d53-8fce-e65f8a8f4b5f | hallway     | 2025-10-17T07:42:36.098427414Z | 2025-10-17T07:43:36Z        | adl:Present   | motion_transition
005704a3-1734-4f22-9c58-71d63bf02c43 | study       | 2025-10-17T07:43:36.059424444Z | 2025-10-17T07:50:24Z        | adl:Present   | motion_transition
```

**JSONLD Episode Example**:
```json
{
  "@context": "https://jeeves.local/contexts/behavioral-episode",
  "@type": "jeeves:BehavioralEpisode",
  "adl:activity": {
    "@type": "adl:Present",
    "name": "Present",
    "adl:location": {
      "name": "bedroom"
    }
  },
  "jeeves:startedAt": "2025-10-17T07:00:36.061257516Z",
  "jeeves:endedAt": "2025-10-17T07:05:36Z",
  "jeeves:triggerType": "motion_transition"
}
```

---

### Behavioral Vectors

**Query**:
```sql
SELECT id, timestamp, sequence, quality_score,
       (context->>'time_of_day') as time_of_day,
       (context->>'total_duration_sec')::int as duration_sec
FROM behavioral_vectors
WHERE timestamp >= '2025-10-17T07:00:00Z'
ORDER BY timestamp;
```

**Morning Routine Vector**:
```json
{
  "id": "6513355c-a0b0-4b6c-b0c2-bfab867741c2",
  "timestamp": "2025-10-17T07:00:36.061258Z",
  "sequence": [
    {
      "sensors": ["motion"],
      "location": "bedroom",
      "gap_to_next": 0,
      "duration_sec": 299
    },
    {
      "sensors": ["motion"],
      "location": "bathroom",
      "gap_to_next": 0,
      "duration_sec": 179
    },
    {
      "sensors": ["motion"],
      "location": "kitchen",
      "gap_to_next": 0,
      "duration_sec": 839
    },
    {
      "sensors": ["motion"],
      "location": "dining_room",
      "gap_to_next": 120,
      "duration_sec": 899
    },
    {
      "sensors": ["motion"],
      "location": "kitchen",
      "gap_to_next": 0,
      "duration_sec": 179
    },
    {
      "sensors": ["motion"],
      "location": "hallway",
      "gap_to_next": 0,
      "duration_sec": 59
    },
    {
      "sensors": ["motion"],
      "location": "study",
      "gap_to_next": 0,
      "duration_sec": 407
    }
  ],
  "context": {
    "day_of_week": "Friday",
    "time_of_day": "morning",
    "location_count": 6,
    "transition_count": 6,
    "total_duration_sec": 2981
  },
  "quality_score": 1.00,
  "consolidated": false,
  "macro_episode_id": null
}
```

**Short Vector Example** (2 locations):
```json
{
  "sequence": [
    {
      "sensors": ["motion"],
      "location": "living_room",
      "gap_to_next": 0,
      "duration_sec": 1200
    },
    {
      "sensors": ["motion"],
      "location": "kitchen",
      "gap_to_next": 0,
      "duration_sec": 600
    }
  ],
  "context": {
    "time_of_day": "evening",
    "day_of_week": "Saturday",
    "location_count": 2,
    "transition_count": 1,
    "total_duration_sec": 1800
  },
  "quality_score": 1.00
}
```

---

### Macro Episodes

**Query**:
```sql
SELECT id, pattern_type, start_time, end_time, duration_minutes,
       locations, summary
FROM macro_episodes
WHERE start_time >= '2025-10-17T07:00:00Z'
ORDER BY start_time;
```

**Morning Routine Macro**:
```json
{
  "id": "afe1feca-8113-409b-a921-70e6293daf43",
  "pattern_type": "morning_routine",
  "start_time": "2025-10-17T07:00:36.061258Z",
  "end_time": "2025-10-17T07:50:24Z",
  "duration_minutes": 49,
  "locations": ["bedroom", "bathroom", "kitchen", "dining_room", "hallway", "study"],
  "micro_episode_ids": [
    "8357600f-ae6a-4098-adbf-3375c5cbd00b",
    "c385811c-99a0-4bea-8e74-de7cba38300b",
    "1e304d04-6ccf-4666-a817-fe3e167ab9d9",
    "d29b8dba-0946-4150-8ff8-2ab8b2a69e8c",
    "e9583397-a5b7-4144-8f47-e6836adc00a3",
    "c542f571-c4e8-4d53-8fce-e65f8a8f4b5f",
    "005704a3-1734-4f22-9c58-71d63bf02c43"
  ],
  "summary": "Morning routine activities: Temporal proximity is less than 2 hours, location sequence makes sense, and it's all in the morning. Quick transitions between activities support this being a single continuous activity pattern.",
  "semantic_tags": ["morning", "routine", "preparation"],
  "context_features": {
    "time_of_day": "morning",
    "day_of_week": "Friday",
    "total_locations": 6,
    "total_transitions": 6
  }
}
```

**Bedtime Routine Example**:
```json
{
  "pattern_type": "bedtime_routine",
  "start_time": "2025-10-17T22:15:00Z",
  "end_time": "2025-10-17T22:45:00Z",
  "duration_minutes": 30,
  "locations": ["kitchen", "bathroom", "bedroom"],
  "summary": "Evening bedtime preparation: sequential visits to kitchen (late snack), bathroom (preparing for bed), and bedroom (settling in for sleep). Typical evening pattern.",
  "semantic_tags": ["evening", "bedtime", "routine"]
}
```

**Cooking Session Example**:
```json
{
  "pattern_type": "cooking",
  "start_time": "2025-10-17T18:00:00Z",
  "end_time": "2025-10-17T19:15:00Z",
  "duration_minutes": 75,
  "locations": ["kitchen", "dining_room", "kitchen"],
  "summary": "Cooking and dining session: extended kitchen presence for meal preparation, dining room for eating, return to kitchen for cleanup.",
  "semantic_tags": ["cooking", "dining", "meal_preparation"]
}
```

---

## Integration Examples

### Python: Query Recent Episodes

```python
import psycopg2
import json
from datetime import datetime, timedelta

# Connect to database
conn = psycopg2.connect(
    host="localhost",
    database="jeeves_behavior",
    user="jeeves",
    password="jeeves_test"
)

# Query episodes from last 2 hours
since = datetime.now() - timedelta(hours=2)
cursor = conn.cursor()

cursor.execute("""
    SELECT location, started_at_text, ended_at_text,
           EXTRACT(EPOCH FROM (ended_at_text::timestamptz - started_at_text::timestamptz))/60 as duration_minutes
    FROM behavioral_episodes
    WHERE started_at_text::timestamptz >= %s
    ORDER BY started_at_text
""", (since,))

episodes = cursor.fetchall()
for location, start, end, duration in episodes:
    print(f"{location}: {duration:.1f} minutes ({start} to {end})")

cursor.close()
conn.close()
```

**Output**:
```
bedroom: 5.0 minutes (2025-10-17T07:00:36.061257516Z to 2025-10-17T07:05:36Z)
bathroom: 3.0 minutes (2025-10-17T07:05:36.098478906Z to 2025-10-17T07:08:36Z)
kitchen: 14.0 minutes (2025-10-17T07:08:36.134364486Z to 2025-10-17T07:22:36Z)
dining_room: 15.0 minutes (2025-10-17T07:22:36.078924618Z to 2025-10-17T07:37:36Z)
```

---

### Go: Query Behavioral Vectors

```go
package main

import (
    "database/sql"
    "encoding/json"
    "fmt"
    "log"

    _ "github.com/lib/pq"
)

type VectorSequence struct {
    Location   string   `json:"location"`
    DurationSec int     `json:"duration_sec"`
    GapToNext   int     `json:"gap_to_next"`
    Sensors     []string `json:"sensors"`
}

type Context struct {
    TimeOfDay       string `json:"time_of_day"`
    DayOfWeek       string `json:"day_of_week"`
    LocationCount   int    `json:"location_count"`
    TransitionCount int    `json:"transition_count"`
}

func main() {
    db, err := sql.Open("postgres", "user=jeeves dbname=jeeves_behavior sslmode=disable")
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    rows, err := db.Query(`
        SELECT sequence, context, quality_score
        FROM behavioral_vectors
        WHERE timestamp >= NOW() - INTERVAL '24 hours'
        ORDER BY timestamp DESC
    `)
    if err != nil {
        log.Fatal(err)
    }
    defer rows.Close()

    for rows.Next() {
        var sequenceJSON, contextJSON []byte
        var qualityScore float64

        err := rows.Scan(&sequenceJSON, &contextJSON, &qualityScore)
        if err != nil {
            log.Fatal(err)
        }

        var sequence []VectorSequence
        var context Context

        json.Unmarshal(sequenceJSON, &sequence)
        json.Unmarshal(contextJSON, &context)

        fmt.Printf("Vector (quality: %.2f) at %s:\n", qualityScore, context.TimeOfDay)
        for _, step := range sequence {
            fmt.Printf("  -> %s (%ds)\n", step.Location, step.DurationSec)
        }
        fmt.Println()
    }
}
```

---

### JavaScript: Listen for Consolidation Results

```javascript
const mqtt = require('mqtt');

const client = mqtt.connect('mqtt://localhost:1883');

client.on('connect', () => {
    console.log('Connected to MQTT broker');

    // Subscribe to consolidation results
    client.subscribe('automation/behavior/consolidation/completed', (err) => {
        if (err) {
            console.error('Subscription error:', err);
        } else {
            console.log('Listening for consolidation results...');
        }
    });
});

client.on('message', (topic, message) => {
    if (topic === 'automation/behavior/consolidation/completed') {
        const result = JSON.parse(message.toString());

        console.log('\n=== Consolidation Completed ===');
        console.log(`Time: ${result.completed_at}`);
        console.log(`Episodes: ${result.episodes_created}`);
        console.log(`Vectors:  ${result.vectors_detected}`);
        console.log(`Macros:   ${result.macros_created}`);
        console.log('===============================\n');
    }
});
```

---

## Test Scenario Integration

### Complete Morning Routine Test

**Consolidation Trigger in Scenario**:
```yaml
events:
  # ... sensor events for morning routine ...

  # Trigger consolidation at end
  - time: 2990
    type: behavior
    location: universe
    data:
      action: consolidate
      lookback_hours: 2
    description: "Consolidation triggered after morning routine"

wait:
  - time: 3040
    description: "Wait for consolidation to complete"
```

**Expected PostgreSQL Results**:
```yaml
expectations:
  postgres:
    # Verify episode count
    - postgres_query: |
        SELECT COUNT(*) FROM behavioral_episodes
        WHERE started_at_text::timestamptz >= '2025-10-17T07:00:00Z'
      postgres_expected: 7
      description: "Seven micro-episodes created"

    # Verify vector detection
    - postgres_query: |
        SELECT COUNT(*) FROM behavioral_vectors
        WHERE timestamp >= '2025-10-17T07:00:00Z'
      postgres_expected: 1
      description: "One comprehensive vector detected"

    # Verify macro consolidation
    - postgres_query: |
        SELECT pattern_type FROM macro_episodes
        WHERE start_time >= '2025-10-17T07:00:00Z'
      postgres_expected: "morning_routine"
      description: "Macro-episode identified as morning routine"
```

---

## Common Query Patterns

### Find All Episodes in Location

```sql
SELECT started_at_text, ended_at_text,
       EXTRACT(EPOCH FROM (ended_at_text::timestamptz - started_at_text::timestamptz))/60 as duration_minutes
FROM behavioral_episodes
WHERE location = 'kitchen'
  AND started_at_text::timestamptz >= NOW() - INTERVAL '7 days'
ORDER BY started_at_text DESC;
```

### Find Vectors Containing Location

```sql
SELECT id, sequence, context
FROM behavioral_vectors
WHERE sequence @> '[{"location": "dining_room"}]'::jsonb
ORDER BY timestamp DESC;
```

### Find Morning Routines

```sql
SELECT start_time, duration_minutes, locations, summary
FROM macro_episodes
WHERE pattern_type = 'morning_routine'
  AND start_time >= NOW() - INTERVAL '30 days'
ORDER BY start_time DESC;
```

### Episode Duration Statistics

```sql
SELECT location,
       COUNT(*) as episode_count,
       AVG(EXTRACT(EPOCH FROM (ended_at_text::timestamptz - started_at_text::timestamptz))/60) as avg_duration_min,
       MAX(EXTRACT(EPOCH FROM (ended_at_text::timestamptz - started_at_text::timestamptz))/60) as max_duration_min
FROM behavioral_episodes
WHERE started_at_text::timestamptz >= NOW() - INTERVAL '7 days'
GROUP BY location
ORDER BY avg_duration_min DESC;
```

---

## Message Format Evolution

### Version Compatibility

**Current Format** (v1):
- JSONB storage in PostgreSQL
- Flexible schema evolution
- Backward compatible additions

**Future Enhancements**:
- Episode confidence scores
- Multi-person tracking
- Activity classification beyond "Present"
- Predictive next-location field

### Schema Migration Strategy

**Adding New Fields**:
- Use JSONB for flexible schema
- New fields are optional
- Old queries continue working
- Graceful degradation

**Example**:
```json
{
  "existing_field": "value",
  "new_field": "new_value"  // Added without breaking changes
}
```
