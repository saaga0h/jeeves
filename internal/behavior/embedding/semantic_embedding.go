package embedding

import (
	"math"
	"time"

	"github.com/pgvector/pgvector-go"

	"github.com/saaga0h/jeeves-platform/internal/behavior/types"
)

// ComputeSemanticEmbedding generates a 128-dimensional semantic tensor
// representing an anchor's position in behavioral space.
//
// Dimensions breakdown:
// [0-3]:   Temporal cyclical (hour of day, day of week)
// [4-7]:   Seasonal cyclical (day of year, month)
// [8-11]:  Day type (weekday/weekend/holiday, time period)
// [12-27]: Spatial (location embedding)
// [28-43]: Weather context
// [44-59]: Lighting context
// [60-79]: Activity signals
// [80-95]: Household rhythm
// [96-127]: Reserved for learned features
func ComputeSemanticEmbedding(
	location string,
	timestamp time.Time,
	context map[string]interface{},
	signals []types.ActivitySignal,
) (pgvector.Vector, error) {
	embedding := make([]float32, 128)

	// [0-3]: Temporal cyclical encoding
	// Use sin/cos to ensure continuity (23:59 and 00:00 are close)
	hour := float64(timestamp.Hour())
	embedding[0] = float32(math.Sin(2 * math.Pi * hour / 24))
	embedding[1] = float32(math.Cos(2 * math.Pi * hour / 24))

	dayOfWeek := float64(timestamp.Weekday())
	embedding[2] = float32(math.Sin(2 * math.Pi * dayOfWeek / 7))
	embedding[3] = float32(math.Cos(2 * math.Pi * dayOfWeek / 7))

	// [4-7]: Seasonal cyclical encoding
	dayOfYear := float64(timestamp.YearDay())
	embedding[4] = float32(math.Sin(2 * math.Pi * dayOfYear / 365))
	embedding[5] = float32(math.Cos(2 * math.Pi * dayOfYear / 365))

	// Month encoding (additional seasonal signal)
	month := float64(timestamp.Month())
	embedding[6] = float32(math.Sin(2 * math.Pi * month / 12))
	embedding[7] = float32(math.Cos(2 * math.Pi * month / 12))

	// [8-11]: Day type encoding
	embedding[8] = encodeDayType(timestamp)    // weekday=1.0, weekend=-1.0
	embedding[9] = encodeHoliday(context)      // holiday=1.0, normal=0.0
	embedding[10] = encodeTimeOfDay(timestamp) // morning/afternoon/evening/night
	embedding[11] = 0.0                        // reserved

	// [12-27]: Spatial encoding (location)
	locationVec := encodeLocation(location)
	copy(embedding[12:28], locationVec)

	// [28-43]: Weather context
	weatherVec := encodeWeather(context)
	copy(embedding[28:44], weatherVec)

	// [44-59]: Lighting context
	lightingVec := encodeLighting(context, signals)
	copy(embedding[44:60], lightingVec)

	// [60-79]: Activity signals
	signalVec := encodeSignals(signals)
	copy(embedding[60:80], signalVec)

	// [80-95]: Household rhythm (derived from time patterns)
	rhythmVec := encodeHouseholdRhythm(timestamp, location)
	copy(embedding[80:96], rhythmVec)

	// [96-127]: Reserved for learned features (future use)
	// These will be updated through pattern learning

	// Normalize to unit length
	normalized := normalize(embedding)

	return pgvector.NewVector(normalized), nil
}

// encodeDayType converts day of week to a scalar
// Weekday = 1.0, Weekend = -1.0
func encodeDayType(t time.Time) float32 {
	if t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
		return -1.0
	}
	return 1.0
}

// encodeHoliday checks if the day is a holiday
func encodeHoliday(context map[string]interface{}) float32 {
	if isHoliday, ok := context["is_holiday"].(bool); ok && isHoliday {
		return 1.0
	}
	return 0.0
}

// encodeTimeOfDay categorizes hour into time periods
// morning: 5-12 → 1.0
// afternoon: 12-17 → 0.5
// evening: 17-21 → 0.0
// night: 21-5 → -1.0
func encodeTimeOfDay(t time.Time) float32 {
	hour := t.Hour()
	switch {
	case hour >= 5 && hour < 12:
		return 1.0
	case hour >= 12 && hour < 17:
		return 0.5
	case hour >= 17 && hour < 21:
		return 0.0
	default:
		return -1.0
	}
}

// encodeLocation converts location string to 16-dimensional vector
// Uses FNV-1a hash for deterministic encoding
func encodeLocation(location string) []float32 {
	vec := make([]float32, 16)

	// FNV-1a hash for deterministic location encoding
	hash := fnv1aHash(location)

	// Extract 16 values from hash, normalized to [-0.5, 0.5]
	for i := 0; i < 16; i++ {
		vec[i] = float32((hash>>(i*4))&15)/15.0 - 0.5
	}

	return vec
}

// fnv1aHash computes FNV-1a hash (deterministic, good distribution)
func fnv1aHash(s string) uint64 {
	var h uint64 = 14695981039346656037 // FNV offset basis
	for _, c := range s {
		h ^= uint64(c)
		h *= 1099511628211 // FNV prime
	}
	return h
}

// encodeWeather extracts weather dimensions from context
func encodeWeather(context map[string]interface{}) []float32 {
	vec := make([]float32, 16)

	// Extract weather info from context
	if weather, ok := context["weather"].(map[string]interface{}); ok {
		// Brightness level (0.0-1.0)
		if brightness, ok := weather["brightness"].(float64); ok {
			vec[0] = float32(brightness)
		}

		// Rain/snow (0.0-1.0)
		if precip, ok := weather["precipitation"].(float64); ok {
			vec[1] = float32(precip)
		}

		// Temperature normalized (-1.0 to 1.0, -20°C to 40°C)
		if temp, ok := weather["temperature"].(float64); ok {
			vec[2] = float32((temp+20)/60*2 - 1)
		}

		// Cloudiness (0.0-1.0)
		if clouds, ok := weather["cloudiness"].(float64); ok {
			vec[3] = float32(clouds)
		}
	}

	// Rest zeros for now (future: wind, humidity, etc.)
	return vec
}

// encodeLighting extracts lighting dimensions from context and signals
func encodeLighting(context map[string]interface{}, signals []types.ActivitySignal) []float32 {
	vec := make([]float32, 16)

	// Check lighting context
	if lighting, ok := context["lighting_state"].(map[string]interface{}); ok {
		if brightness, ok := lighting["brightness"].(float64); ok {
			vec[0] = float32(brightness / 100) // normalize to 0-1
		}

		if colorTemp, ok := lighting["color_temp"].(float64); ok {
			// Normalize color temperature (2000-8500K range)
			vec[1] = float32((colorTemp - 2000) / 6500)
		}

		if source, ok := lighting["source"].(string); ok {
			vec[2] = encodeSource(source) // manual=1.0, automated=-1.0
		}
	}

	// Check lighting signals (overrides context if present)
	for _, signal := range signals {
		if signal.Type == "lighting" {
			if brightness, ok := signal.Value["brightness"].(float64); ok {
				vec[3] = float32(brightness / 100)
			}
			if state, ok := signal.Value["state"].(string); ok {
				if state == "on" {
					vec[4] = 1.0
				} else {
					vec[4] = -1.0
				}
			}
		}
	}

	return vec
}

// encodeSource converts lighting source to scalar
func encodeSource(source string) float32 {
	switch source {
	case "manual":
		return 1.0
	case "automated":
		return -1.0
	default:
		return 0.0
	}
}

// encodeSignals extracts activity signal dimensions
func encodeSignals(signals []types.ActivitySignal) []float32 {
	vec := make([]float32, 20)

	motionCount := 0
	mediaActive := false
	presenceDetected := false
	maxMotionConfidence := float32(0.0)

	for _, signal := range signals {
		switch signal.Type {
		case "motion":
			motionCount++
			if float32(signal.Confidence) > maxMotionConfidence {
				maxMotionConfidence = float32(signal.Confidence)
			}

		case "media":
			if state, ok := signal.Value["state"].(string); ok && state == "playing" {
				mediaActive = true
				vec[1] = 1.0

				// Extract media type
				if mediaType, ok := signal.Value["type"].(string); ok {
					vec[10] = encodeMediaType(mediaType)
				}
			}

		case "presence":
			presenceDetected = true
			vec[2] = float32(signal.Confidence)

		case "lighting":
			// Already handled in encodeLighting
			continue
		}
	}

	// Motion confidence
	vec[0] = maxMotionConfidence

	// Motion frequency (normalized to 0-1, assuming 5+ events = 1.0)
	vec[3] = min(float32(motionCount)/5.0, 1.0)

	// Boolean flags
	if mediaActive {
		vec[4] = 1.0
	}
	if presenceDetected {
		vec[5] = 1.0
	}

	// Signal diversity (how many different signal types)
	uniqueTypes := make(map[string]bool)
	for _, signal := range signals {
		uniqueTypes[signal.Type] = true
	}
	vec[6] = min(float32(len(uniqueTypes))/4.0, 1.0)

	return vec
}

// encodeMediaType converts media type to scalar
func encodeMediaType(mediaType string) float32 {
	switch mediaType {
	case "tv":
		return 1.0
	case "music":
		return 0.5
	case "podcast":
		return 0.0
	default:
		return -1.0
	}
}

// encodeHouseholdRhythm encodes typical activity patterns by time and location
func encodeHouseholdRhythm(timestamp time.Time, location string) []float32 {
	vec := make([]float32, 16)

	hour := timestamp.Hour()

	// Time-based rhythm encoding
	// Waking period (5-9am)
	if hour >= 5 && hour < 9 {
		vec[0] = 1.0
	}

	// Active day (9am-6pm)
	if hour >= 9 && hour < 18 {
		vec[1] = 1.0
	}

	// Evening wind-down (6pm-10pm)
	if hour >= 18 && hour < 22 {
		vec[2] = 1.0
	}

	// Sleep period (10pm-5am)
	if hour >= 22 || hour < 5 {
		vec[3] = 1.0
	}

	// Location-specific rhythm hints
	switch location {
	case "bedroom":
		// Bedrooms are active during waking and sleep periods
		if (hour >= 5 && hour < 9) || (hour >= 22 || hour < 5) {
			vec[4] = 1.0
		}
	case "kitchen":
		// Kitchens are active during meal times
		if (hour >= 7 && hour < 9) || (hour >= 12 && hour < 14) || (hour >= 18 && hour < 20) {
			vec[5] = 1.0
		}
	case "living_room":
		// Living rooms are active during evening
		if hour >= 18 && hour < 23 {
			vec[6] = 1.0
		}
	case "bathroom":
		// Bathrooms are active during morning and evening routines
		if (hour >= 6 && hour < 9) || (hour >= 21 && hour < 23) {
			vec[7] = 1.0
		}
	}

	return vec
}

// normalize converts vector to unit length
func normalize(vec []float32) []float32 {
	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	norm = math.Sqrt(norm)

	if norm == 0 {
		return vec
	}

	normalized := make([]float32, len(vec))
	for i, v := range vec {
		normalized[i] = float32(float64(v) / norm)
	}

	return normalized
}

// min returns the smaller of two float32 values
func min(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}
