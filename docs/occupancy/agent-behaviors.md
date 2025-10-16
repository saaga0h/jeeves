# How the Occupancy Agent Works

The occupancy agent analyzes motion sensor data to determine if rooms are occupied or empty. It uses advanced pattern recognition and machine learning to provide reliable occupancy detection from basic motion sensors.

## What This Agent Does

**Primary Function**: Converts unreliable motion sensor signals into reliable room occupancy status

**Key Capabilities**:
- Detects when someone enters or leaves a room
- Distinguishes between someone working quietly vs. an empty room
- Identifies pass-through traffic vs. sustained presence
- Prevents false alarms from sensor noise or boundary conditions
- Provides confidence scores and reasoning for all decisions

**Technical Approach**: The agent uses temporal pattern analysis and machine learning (LLM) to understand motion patterns across multiple time scales, combined with stabilization algorithms to prevent oscillating predictions.

## How It Starts Up

**Configuration Loading**:
- MQTT broker connection details
- Redis database connection for motion data
- Analysis frequency (default: every 30 seconds)
- LLM endpoint for intelligent analysis (typically local Ollama server)

**Connection Setup**:
1. Establishes Redis connection for reading motion sensor data
2. Connects to MQTT broker for receiving triggers and publishing results
3. Subscribes to motion sensor triggers from all rooms
4. Tests LLM connection for intelligent analysis

**Ready State**:
- Monitors for motion sensor triggers from any room
- Runs periodic analysis to catch state changes
- Publishes occupancy updates to home automation system

## How Motion Detection Works

### Real-Time Motion Triggers

**When Motion is Detected**:
1. Motion sensor data gets stored in Redis by the collector agent
2. Collector sends trigger message: `automation/sensor/motion/{location}`
3. Occupancy agent receives trigger and checks if motion is recent (last 2 minutes)
4. If motion is recent, triggers immediate analysis

**First Motion in Unknown Room** (Fast Path):
- Immediately sets room to "occupied" with high confidence (0.9)
- Publishes occupancy status without complex analysis
- Stores initial prediction for future pattern analysis
- Reasoning: First motion is almost always someone entering

**Motion in Known Room**:
- Runs full intelligent analysis using motion history
- Considers multiple time windows and movement patterns
- Uses machine learning to interpret occupancy state
- May confirm current state or trigger state change

### Periodic Background Analysis

**Every 30 seconds** (configurable):
1. Checks all rooms that have motion sensor data
2. Skips rooms analyzed within last 25 seconds (rate limiting)
3. For each eligible room, runs full pattern analysis
4. Updates occupancy state if analysis indicates change needed

**Purpose of Periodic Analysis**:
- Catches transitions that triggers might miss (e.g., someone sitting still too long)
- Provides regular updates for rooms with subtle activity patterns
- Ensures system doesn't miss occupancy changes during quiet periods

## How Pattern Analysis Works

The agent analyzes motion patterns across four time windows to understand what's happening:

**Time Window Analysis**:
- **0-2 minutes**: Is someone moving right now?
- **2-8 minutes**: What happened in the recent past?
- **8-20 minutes**: Was this sustained presence or just passing through?
- **20-60 minutes**: What's the overall activity level in this space?

*For detailed implementation of pattern analysis algorithms, see the `generateTemporalAbstraction()` function in the source code.*

**Example Pattern Recognition**:
- **Active Presence**: Motion right now + recent activity = Person actively in room
- **Settling In**: Quiet now + recent activity = Person entered and sat down (working/reading)
- **Pass-Through**: Single motion event, now quiet for 5+ minutes = Person walked through
- **Extended Absence**: No motion for 10+ minutes = Room is empty

## How Intelligent Analysis Works

### Machine Learning Integration

**LLM-Powered Decision Making**:
- Builds detailed motion pattern description for AI analysis
- Includes time-of-day context and historical patterns
- Asks LLM to interpret patterns and provide reasoning
- Gets back: occupied status, confidence level, and human-readable explanation

**Fallback System**:
- If LLM is unavailable, uses deterministic rule-based analysis
- Implements same decision patterns as LLM in code
- Ensures system continues working even without AI component

### Stability and Anti-Oscillation

**The Problem**: Motion sensors at boundaries can flicker, causing rapid OCCUPIED ↔ EMPTY changes

**The Solution**: Vonich-Hakim Stabilization (see implementation in `computeVonichHakimStabilization()`)
- Monitors prediction history for instability patterns
- Detects when system is oscillating between states
- Automatically increases confidence requirements for unstable situations
- Makes system more conservative when patterns are unclear

**How It Works**:
- Tracks confidence consistency and state change frequency
- When oscillation detected, requires higher confidence to change state
- Gradually stabilizes system without manual tuning
- Logs reasoning when stabilization is applied

*For detailed stabilization mathematics, see the `computeVonichHakimStabilization()` function implementation.*

## Decision Making and Updates

### When Occupancy State Changes

**Confidence Gates**:
- **State Changes** (empty → occupied, or vice versa): Requires 0.6+ confidence
- **State Maintenance** (staying same): Requires 0.3+ confidence
- **With Stabilization**: May require up to 0.9+ confidence if system is unstable

**Time Gates**:
- Minimum 45 seconds between state changes
- Prevents rapid flickering from sensor noise
- Only applies to actual state changes, not confidence updates

**Update Decision Process**:
1. Run pattern analysis and get prediction
2. Check if confidence meets threshold for this type of change
3. Check if enough time has passed since last state change
4. If both pass, update state and publish; otherwise maintain current state

### What Gets Published

**MQTT Topic**: `automation/context/occupancy/{location}`

**Message Contents**:
- Current occupancy status (occupied/empty)
- Confidence level (0.0 to 1.0)
- Human-readable reasoning from analysis
- Motion pattern metrics for debugging
- Timestamp and analysis method used

**When Messages Are Sent**:
- Initial motion detection (immediate)
- State changes that pass confidence and time gates
- Periodic updates when analysis confirms current state with sufficient confidence

## Error Handling and Reliability

### Connection Issues

**MQTT Connection Loss**:
- Client automatically reconnects
- Subscriptions maintained through reconnection
- Logs connection status changes

**Redis Connection Loss**:
- All data queries return safe defaults
- Agent continues operating with fallback behavior
- Errors logged but don't crash system

**LLM Connection Issues**:
- Automatic fallback to deterministic analysis
- Same decision patterns implemented in code
- System remains fully functional without AI component

### Data Quality Issues

**Missing Motion Data**:
- Skips analysis for rooms without motion history
- Logs debug information about skipped rooms
- Continues processing other rooms normally

**Invalid Sensor Data**:
- Parses motion events safely with error handling
- Skips malformed events but continues with valid data
- Logs warnings for debugging sensor issues

### Analysis Errors

**Per-Room Error Isolation**:
- Analysis failures in one room don't affect others
- Errors logged with room context for debugging
- Periodic analysis continues for unaffected rooms

**Graceful Degradation**:
- Falls back to simpler analysis if complex analysis fails
- Maintains basic functionality even with component failures
- Always provides some form of occupancy decision

## System Monitoring and Maintenance

### Performance Monitoring

**Key Metrics to Watch**:
- Analysis response time (should be < 5 seconds)
- Prediction confidence distribution (mostly high confidence indicates healthy system)
- State change frequency (< 2 changes per hour per room indicates stability)
- LLM response time and failure rate

### Debugging Common Issues

**Room Stuck in One State**:
- Check motion sensor data is reaching Redis
- Verify confidence thresholds aren't blocking updates
- Look for stabilization dampening in logs

**Rapid State Changes**:
- Check for boundary sensor placement issues
- Look for Vonich-Hakim stabilization activation
- Monitor prediction history for oscillation patterns

**Low Confidence Predictions**:
- May indicate sensor placement issues
- Check time-of-day patterns (night vs day behavior)
- Consider if room usage patterns match sensor capabilities

### Logging and Diagnostics

**Debug Level Logging**:
- Full motion pattern data
- Complete LLM prompts and responses
- Detailed stabilization calculations
- All Redis queries and timing

**Info Level Logging**:
- Occupancy state changes with reasoning
- Published messages with confidence levels
- Startup and connection status

**Error Level Logging**:
- Connection failures with retry attempts
- LLM failures with fallback activation
- Data parsing errors with context

## Integration with Home Automation

### Downstream Consumers

**Light Agent**:
- Subscribes to occupancy messages
- Uses confidence levels for lighting decisions
- Considers reasoning for context (settling vs active)

**Behavior Agent**:
- Tracks occupancy patterns over time
- Builds behavioral models from occupancy data
- Identifies routine patterns and anomalies

**Other Automation Systems**:
- HVAC control based on room occupancy
- Security systems for presence detection
- Energy management for unoccupied rooms

### Message Format for Integration

The agent publishes structured JSON messages that include:
- Clear occupied/empty status
- Numerical confidence for decision weighting
- Human-readable reasoning for logging/debugging
- Motion metrics for advanced automation logic
- Standardized timestamps for correlation

This format enables both simple automation (just use occupied status) and sophisticated systems (weight decisions by confidence and context).

*For complete message examples and integration patterns, see the Message Examples documentation.*