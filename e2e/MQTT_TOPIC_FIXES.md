# MQTT Topic Fixes - E2E Testing Framework

## Summary

Fixed E2E test framework to match the actual J.E.E.V.E.S. MQTT topic topology as specified in agent documentation.

## Problem

Tests were using incorrect MQTT topics that didn't match the agent specifications:
- Test published to: `sensor/motion/hallway-sensor-1`
- Agents subscribe to: `automation/raw/+/+`
- **Result**: Agents never received test events

## J.E.E.V.E.S. MQTT Topology (Correct)

```
┌─────────────────┐
│ Raw Sensor Data │  automation/raw/{type}/{location}
└────────┬────────┘
         │
         ▼
    ┌──────────┐
    │Collector │
    └────┬─────┘
         │
         ▼
┌──────────────────┐
│Processed Triggers│  automation/sensor/{type}/{location}
└────────┬─────────┘
         │
         ▼
   ┌──────────┐
   │  Agents  │  (Occupancy, Illuminance, Light)
   └────┬─────┘
        │
        ▼
┌─────────────────┐
│Context Messages │  automation/context/{type}/{location}
└─────────────────┘
        │
        ▼
┌─────────────────┐
│Command Messages │  automation/command/{type}/{location}
└─────────────────┘
```

## Changes Made

### 1. MQTT Player (`e2e/internal/executor/mqtt_player.go`)

**Before:**
```go
topic := fmt.Sprintf("sensor/%s/%s", sensorType, sensorID)
payload := map[string]interface{}{
    "sensorType": sensorType,
    "sensorId":   sensorID,
    "value":      event.Value,
}
```

**After:**
```go
topic := fmt.Sprintf("automation/raw/%s/%s", sensorType, location)
payload := map[string]interface{}{
    "sensorType": sensorType,
    "location":   location,
    "value":      event.Value,
}
```

### 2. Test Scenarios (All `.yaml` files)

#### Sensor Format
**Before:** `sensor: "motion:hallway-sensor-1"`
**After:** `sensor: "motion:hallway"`

**Rationale:** Location is the abstraction, not individual sensor IDs

#### Collector Expectations
**Before:**
```yaml
collector:
  - topic: "sensor/motion/hallway-sensor-1"
    payload:
      sensorType: "motion"
      value: true
```

**After:**
```yaml
collector:
  - topic: "automation/sensor/motion/hallway"
    payload:
      sensorType: "motion"
      location: "hallway"
      value: true
```

#### Occupancy Expectations
**Before:**
```yaml
occupancy_decision:
  - topic: "occupancy/status/hallway"
    payload:
      location: "hallway"
      occupied: false
```

**After:**
```yaml
occupancy_decision:
  - topic: "automation/context/occupancy/hallway"
    payload:
      location: "hallway"
      occupied: false
```

### 3. Documentation (`test-scenarios/README.md`)

Added section explaining J.E.E.V.E.S. MQTT topology with:
- Topic flow diagram
- Expected topics by layer (collector, occupancy, illuminance, light)
- Corrected all examples

## Files Modified

1. **Code:**
   - `e2e/internal/executor/mqtt_player.go` - Topic publishing logic

2. **Test Scenarios:**
   - `test-scenarios/hallway_passthrough.yaml`
   - `test-scenarios/study_working.yaml`
   - `test-scenarios/bedroom_morning.yaml`

3. **Documentation:**
   - `test-scenarios/README.md` - Added MQTT topology section

## Verification

### Before Fixes
```
Test published to: sensor/motion/hallway-sensor-1
Collector subscribed to: automation/raw/+/+
Result: ✗ No match → Test failed
```

### After Fixes
```
Test publishes to: automation/raw/motion/hallway
Collector subscribed to: automation/raw/+/+
Result: ✓ Match → Collector processes event
```

## Testing the Fixes

```bash
cd e2e
./run-test.sh hallway_passthrough
```

Expected behavior:
1. Test publishes to `automation/raw/motion/hallway`
2. Collector receives event
3. Collector publishes to `automation/sensor/motion/hallway`
4. Occupancy agent receives trigger
5. Occupancy agent analyzes and publishes to `automation/context/occupancy/hallway`
6. Test expectations match actual topics

## MQTT Topic Reference

### Raw Sensor Data (Test → Collector)
- **Format**: `automation/raw/{type}/{location}`
- **Examples**:
  - `automation/raw/motion/hallway`
  - `automation/raw/illuminance/study`
  - `automation/raw/temperature/bedroom`

### Processed Triggers (Collector → Agents)
- **Format**: `automation/sensor/{type}/{location}`
- **Examples**:
  - `automation/sensor/motion/hallway`
  - `automation/sensor/illuminance/study`

### Context Messages (Agents → System)
- **Format**: `automation/context/{type}/{location}`
- **Examples**:
  - `automation/context/occupancy/hallway`
  - `automation/context/illuminance/study`
  - `automation/context/lighting/hallway`

### Command Messages (Agents → Actuators)
- **Format**: `automation/command/{type}/{location}`
- **Examples**:
  - `automation/command/light/hallway`
  - `automation/command/lights/study`

## Agent Documentation Reference

For detailed specifications, see:
- `docs/collector/mqtt-topics.md` - Collector topic spec
- `docs/occupancy/mqtt-topics.md` - Occupancy agent spec
- `docs/illuminance/mqtt-topics.md` - Illuminance agent spec
- `docs/light/mqtt-topics.md` - Light agent spec

## Key Learnings

1. **Follow the spec**: Always refer to agent documentation for topic structure
2. **Location abstraction**: Use location names, not sensor IDs
3. **Hierarchical topics**: J.E.E.V.E.S. uses `automation/` prefix for all topics
4. **Trigger-based**: Agents read from Redis, MQTT is for triggers only
5. **Test last**: Build tests after understanding agent behavior
