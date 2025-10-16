# Advanced Algorithms Overview

The occupancy agent uses two sophisticated algorithms to provide reliable room occupancy detection from basic motion sensors. This document provides an overview of how they work.

## Temporal Abstraction Algorithm

**Purpose**: Transform noisy motion sensor data into meaningful patterns that capture human behavior.

**The Challenge**: Raw motion sensors only give binary on/off signals. This doesn't tell you whether someone just walked through a room vs. settled down to work.

**The Solution**: Analyze motion patterns across multiple time scales to understand what's actually happening.

**Time Window Analysis**:
- **0-2 minutes**: Is someone moving right now?
- **2-8 minutes**: What happened recently?
- **8-20 minutes**: Sustained presence or just passing through?
- **20-60 minutes**: Overall room usage level

**Key Innovation**: Uses exclusive (non-overlapping) time windows to properly distinguish temporal patterns. This enables detection of behaviors like "person entered and settled down" vs "person walked through and left."

**Output**: Semantic labels like "active_motion", "settling_in", "pass_through", and "sustained_presence" that describe human-understandable patterns.

*For implementation details, see the `generateTemporalAbstraction()` function in the source code.*

## Vonich-Hakim Stabilization Algorithm

**Purpose**: Prevent rapid oscillation between OCCUPIED and EMPTY states when sensors are at boundary conditions.

**The Problem**: Motion sensors near room boundaries can flicker, causing lights and automation to rapidly turn on/off. This is especially common in doorways and open spaces.

**The Solution**: Monitor prediction history to detect instability, then automatically increase confidence requirements when the system shows signs of oscillation.

**How It Works**:
1. **Tracks Stability Metrics**: Monitors confidence consistency and state change frequency
2. **Detects Patterns**: Identifies when system is becoming unstable or oscillating
3. **Adaptive Dampening**: Automatically increases confidence thresholds for unstable situations
4. **Self-Correction**: Gradually stabilizes without requiring manual tuning

**Example**: If a room keeps switching between OCCUPIED and EMPTY every 30 seconds, the algorithm will:
- Detect the oscillation pattern
- Increase the confidence required to change state (from 60% to potentially 90%+)
- Only allow very confident predictions to trigger state changes
- System naturally stabilizes

**Benefits**:
- No manual threshold tuning required
- Adapts to different sensor qualities and placements
- Maintains responsiveness for clear cases while preventing flicker
- Provides transparent reasoning in logs

*For implementation details, see the `computeVonichHakimStabilization()` function in the source code.*

## Machine Learning Integration

**LLM-Powered Analysis**: The agent uses a local large language model (typically Ollama) to interpret motion patterns and make occupancy decisions.

**Why LLM**: 
- Excellent at pattern recognition and reasoning
- Can handle complex temporal relationships
- Provides human-readable explanations
- Adapts to context (time of day, room type)

**Reliability**: Complete fallback system implements the same decision logic in code, ensuring the system works even without AI components.

**Performance**: Uses small, fast models (3B parameters) optimized for deterministic responses at low latency.

## Integration and Workflow

1. **Motion Detection**: Collector agent stores motion events in Redis, sends trigger
2. **Pattern Analysis**: Temporal abstraction converts raw data to semantic patterns  
3. **AI Analysis**: LLM interprets patterns and predicts occupancy with confidence
4. **Stability Check**: Vonich-Hakim algorithm validates prediction consistency
5. **Decision Gates**: Confidence and time thresholds prevent unwanted changes
6. **Publication**: Final occupancy status published to home automation system

This multi-layered approach provides both immediate responsiveness and long-term stability, making reliable automation possible from basic motion sensors.

## Implementation Notes

**All Algorithm Details**: Complete mathematical formulations, parameter values, and optimization strategies are implemented in the source code functions referenced above.

**Extensibility**: The pattern recognition framework is designed to support future sensor types (presence detection, mmWave radar) and multi-sensor fusion.

**Testing**: Comprehensive test suites validate both individual algorithms and integrated behavior across various real-world scenarios.

For specific implementation questions, refer to the source code where all algorithms are fully documented with comments and examples.