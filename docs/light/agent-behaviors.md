# Light Agent - How It Works

This guide explains how the Light Agent automatically manages lighting based on room occupancy and illumination conditions.

## What the Light Agent Does

The Light Agent is the "brain" that decides when to turn lights on or off and how bright they should be. It watches room occupancy and lighting conditions, then makes intelligent decisions about lighting control.

**Key Responsibilities**:
- 🏠 **Monitors rooms**: Tracks which rooms are occupied and their current lighting levels
- 💡 **Makes lighting decisions**: Automatically determines if lights should be on/off and how bright
- 🌅 **Adapts to time of day**: Uses warmer colors in evening, cooler during day
- 🔆 **Considers natural light**: Reduces artificial lighting when natural light is available
- ⏰ **Prevents flickering**: Rate limits decisions to avoid rapid on/off cycling
- 🎛️ **Respects manual overrides**: Allows temporary manual control

---

## How It Makes Decisions

### Decision Process Overview

```
1. Room becomes occupied → Immediate lighting evaluation
2. Every 30 seconds → Check all rooms for needed adjustments  
3. Room becomes empty → Immediate lights off
4. Manual override → Pause automation for that room
```

### Core Decision Rules

**Rule 1: Empty Room = Lights OFF** (Always)
- Any room detected as empty gets lights turned off immediately
- Highest priority rule - saves energy

**Rule 2: Uncertain Occupancy = Wait**
- If occupancy detection is unclear, don't change lighting
- Prevents false triggers from sensor noise

**Rule 3: Low Confidence = Maintain Current State**
- If occupancy confidence is below 50%, don't make changes
- Avoids mistakes from unreliable sensor readings

**Rule 4: Occupied Room = Smart Lighting**
- Analyze current lighting conditions
- Consider time of day and natural light
- Calculate appropriate brightness and color temperature

### Smart Lighting Calculation

When a room is occupied with good confidence, the agent:

**Assesses Current Lighting**:
- Checks recent sensor readings (preferred - most accurate)
- Falls back to historical patterns for same time/day
- Uses time-based defaults as last resort

**Determines Natural Light**:
- Daytime + bright conditions = likely natural light
- Natural light reduces need for artificial lighting

**Calculates Brightness**:
- **Dark** (< 5 lux): 50-80% brightness depending on time
- **Dim** (5-50 lux): 40-60% brightness  
- **Moderate** (50-200 lux): 20-40% brightness
- **Bright** (> 200 lux): 0-10% brightness (minimal artificial)

**Sets Color Temperature**:
- **Morning/Midday**: 4500-5500K (cool, energizing)
- **Evening**: 2700K (warm, relaxing)
- **Night**: 2400K (very warm, sleep-friendly)

---

## Configuration

### Environment Variables

```bash
# MQTT Connection
JEEVES_MQTT_BROKER=localhost
JEEVES_MQTT_PORT=1883
JEEVES_MQTT_USER=light_agent
JEEVES_MQTT_PASSWORD=secret

# Redis Connection  
JEEVES_REDIS_HOST=localhost
JEEVES_REDIS_PORT=6379

# Behavior Settings
JEEVES_LIGHT_DECISION_INTERVAL=30     # Seconds between automatic checks
JEEVES_LIGHT_OVERRIDE_DURATION=30     # Default manual override duration (minutes)
JEEVES_LIGHT_RATE_LIMIT=10           # Minimum seconds between decisions per room

# API Server
JEEVES_HEALTH_PORT=8080
```

### How to Start the Agent

```bash
# Using environment variables
export JEEVES_MQTT_BROKER=mqtt.local
./bin/light-agent

# Using command line flags
./bin/light-agent -mqtt-broker mqtt.local -redis-host redis.local -log-level debug
```

---

## Manual Override System

Sometimes you want to control lights manually without automation interfering.

### Setting an Override

```bash
# Override living room for 30 minutes (default)
curl -X POST http://localhost:8080/override/living_room

# Override bedroom for 2 hours
curl -X POST http://localhost:8080/override/bedroom \
  -H "Content-Type: application/json" \
  -d '{"duration": 120}'
```

### Checking Override Status

```bash
# See all active overrides
curl http://localhost:8080/contexts | jq '.overrides'
```

### Clearing an Override

```bash
# Remove override for living room
curl -X DELETE http://localhost:8080/override/living_room
```

**What Override Does**:
- ✅ Stops all automatic lighting decisions for that room
- ✅ Manual light controls work normally
- ✅ Automatically expires after specified duration
- ✅ Agent resumes automation when override expires

---

## Integration with Other Agents

### Receives Context From

**Occupancy Agent**: 
```go
// Topic: automation/context/occupancy/{location}
type OccupancyContext struct {
    State      string  `json:"state"`      // "empty", "occupied", "likely"
    Confidence float64 `json:"confidence"` // 0.0 - 1.0
    Timestamp  string  `json:"timestamp"`
}
```

**Illuminance Agent**:
```go  
// Topic: automation/context/illuminance/{location}
type IlluminanceContext struct {
    State string `json:"state"`         // "dark", "dim", "moderate", "bright"
    Data  struct {
        CurrentLux int    `json:"current_lux"`
        Trend      string `json:"trend"`
        IsDaytime  bool   `json:"is_daytime"`
    } `json:"data"`
}
```

### Publishes Commands To

**Light Command Topic**: `automation/command/light/{location}`
```go
type LightCommand struct {
    Action     string  `json:"action"`      // "on", "off"
    Brightness int     `json:"brightness"`  // 0-100
    ColorTemp  *int    `json:"color_temp"`  // Kelvin (2400-5500), null for off
    Reason     string  `json:"reason"`      // Why this decision was made
    Confidence float64 `json:"confidence"`  // 0.0-1.0
    Timestamp  string  `json:"timestamp"`
}
```

**Example Commands**:
```json
// Turn on living room lights
{
  "action": "on",
  "brightness": 75,
  "color_temp": 2700,
  "reason": "occupied_dim_evening_recent_reading",
  "confidence": 0.85,
  "timestamp": "2025-01-01T19:30:00Z"
}

// Turn off bedroom lights  
{
  "action": "off",
  "brightness": 0,
  "color_temp": null,
  "reason": "room_empty",
  "confidence": 0.90,
  "timestamp": "2025-01-01T22:15:00Z"
}
```

---

## Monitoring and Health

### Health Check Endpoint

```bash
curl http://localhost:8080/health
```

```json
{
  "status": "healthy",
  "timestamp": "2025-01-01T12:00:00Z",
  "uptime": 3600,
  "services": {
    "redis": "connected",
    "mqtt": "connected"
  },
  "metrics": {
    "locations_tracked": 5,
    "locations_in_redis": 5,
    "manual_overrides": ["living_room"]
  }
}
```

### Status Meanings

- **healthy**: All services connected, agent operating normally
- **degraded**: Minor issues (some sensors offline) but still functional  
- **unhealthy**: Major issues (Redis/MQTT disconnected)

### API Endpoints for Debugging

```bash
# View current room contexts
curl http://localhost:8080/contexts

# Get specific room context
curl http://localhost:8080/context/living_room

# Force immediate decision for a room
curl -X POST http://localhost:8080/decide/living_room
```

---

## Troubleshooting

### Lights Not Responding to Occupancy

**Symptoms**: Room is occupied but lights don't turn on

**Check**:
1. **Occupancy context**: `curl http://localhost:8080/context/living_room`
   - Verify occupancy state is "occupied" 
   - Check confidence is >= 0.5

2. **Manual override**: Look for override in health check
   - Clear if needed: `curl -X DELETE http://localhost:8080/override/living_room`

3. **Rate limiting**: Force immediate decision
   - `curl -X POST http://localhost:8080/decide/living_room`

4. **MQTT connectivity**: Check health endpoint
   - Should show `"mqtt": "connected"`

### Lights Flickering On/Off

**Symptoms**: Lights rapidly cycling on and off

**Cause**: Usually occupancy sensor being indecisive

**Solutions**:
1. **Check occupancy confidence**: Should be stable above 0.5
2. **Review sensor placement**: May be detecting movement incorrectly
3. **Increase rate limiting**: Adjust `JEEVES_LIGHT_RATE_LIMIT` to 20+ seconds
4. **Use manual override**: Temporarily stop automation while debugging

### Wrong Brightness or Color

**Symptoms**: Lights too bright/dim or wrong color temperature

**Check**:
1. **Illuminance data**: Verify room has recent illuminance readings
   - Agent prefers recent sensor data over time-based defaults
2. **Time zone**: Ensure system time/timezone is correct
   - Color temperature depends on local time of day
3. **Natural light detection**: May need sensor recalibration

**Debug**:
```bash
# Force decision and check reason in logs
curl -X POST http://localhost:8080/decide/living_room

# Check illuminance agent data
curl http://illuminance-agent:8080/health
```

### Agent Won't Start

**Symptoms**: Agent exits or fails to connect

**Check**:
1. **MQTT broker**: Verify broker is running and accessible
2. **Redis server**: Ensure Redis is accessible
3. **Permissions**: Check MQTT username/password if using authentication
4. **Port conflicts**: Ensure health check port (8080) is available

**Debug startup**:
```bash
# Run with debug logging
./bin/light-agent -log-level debug

# Check specific connections
redis-cli -h redis.local ping
mosquitto_pub -h mqtt.local -t test -m "connection test"
```

### No Context from Other Agents

**Symptoms**: Health shows 0 locations tracked

**Check**:
1. **Occupancy Agent**: Must be running and publishing context
2. **MQTT topics**: Verify subscription topics match published topics
3. **Network**: Ensure agents can reach MQTT broker

**Test MQTT flow**:
```bash
# Listen for occupancy context
mosquitto_sub -h mqtt.local -t 'automation/context/occupancy/+' -v

# Listen for light commands  
mosquitto_sub -h mqtt.local -t 'automation/command/light/+' -v
```

---

## Performance Notes

### Decision Frequency

- **Occupancy changes**: Immediate response (< 1 second)
- **Periodic reviews**: Every 30 seconds by default
- **Rate limiting**: Maximum 1 decision per room per 10 seconds

### Memory Usage

- **Context storage**: In-memory map of room states
- **Override tracking**: In-memory expiration times
- **Typical usage**: < 50MB for 10-20 rooms

### MQTT Message Volume

- **Incoming**: ~1-5 messages/minute (occupancy + illuminance context)
- **Outgoing**: ~1-10 messages/minute (light commands + context)
- **Peak**: During activity periods when people move between rooms

This intelligent automation ensures your home lighting responds naturally to how you use your spaces while conserving energy and adapting to your daily routines.