package embedding

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/pgvector/pgvector-go"

	"github.com/saaga0h/jeeves-platform/internal/behavior/types"
)

// ActivityFingerprint represents a normalized activity pattern for embedding lookup
type ActivityFingerprint struct {
	Location   string   `json:"location"`
	TimePeriod string   `json:"time_period"` // morning, afternoon, evening, night
	DayType    string   `json:"day_type"`    // weekday, weekend, holiday
	Signals    []string `json:"signals"`     // sorted list of signal types
}

// ActivityEmbeddingAgent handles progressive learning of activity embeddings
type ActivityEmbeddingAgent struct {
	Storage ActivityEmbeddingStorage
	LLM     LLMEmbeddingGenerator
}

// LLMEmbeddingGenerator interface for generating embeddings via LLM
type LLMEmbeddingGenerator interface {
	GenerateActivityEmbedding(ctx context.Context, fingerprint ActivityFingerprint) ([]float32, error)
}

// ActivityEmbeddingStorage interface for caching activity embeddings
type ActivityEmbeddingStorage interface {
	GetActivityEmbedding(ctx context.Context, fingerprintHash string) ([]float32, error)
	StoreActivityEmbedding(ctx context.Context, fingerprintHash string, fingerprint ActivityFingerprint, embedding []float32) error
}

// GenerateActivityFingerprint creates a stable fingerprint from activity context
func GenerateActivityFingerprint(
	location string,
	timestamp time.Time,
	context map[string]interface{},
	signals []types.ActivitySignal,
) ActivityFingerprint {
	// Normalize time to period
	timePeriod := getTimePeriod(timestamp)

	// Normalize day type
	dayType := getDayType(timestamp, context)

	// Extract and normalize signal types (sorted for consistency)
	signalTypes := extractSignalTypes(signals)

	return ActivityFingerprint{
		Location:   location,
		TimePeriod: timePeriod,
		DayType:    dayType,
		Signals:    signalTypes,
	}
}

// Hash generates a stable hash of the fingerprint for cache lookup
func (fp ActivityFingerprint) Hash() string {
	// Serialize to JSON for stable hashing
	data, _ := json.Marshal(fp)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// Describe generates a human-readable description for LLM prompting
func (fp ActivityFingerprint) Describe() string {
	var parts []string

	// Add time context
	parts = append(parts, fmt.Sprintf("during %s", fp.TimePeriod))

	// Add day type
	if fp.DayType != "weekday" {
		parts = append(parts, fmt.Sprintf("on %s", fp.DayType))
	}

	// Add location
	parts = append(parts, fmt.Sprintf("in %s", fp.Location))

	// Add signals
	if len(fp.Signals) > 0 {
		parts = append(parts, fmt.Sprintf("with signals: %s", strings.Join(fp.Signals, ", ")))
	}

	return strings.Join(parts, " ")
}

// getTimePeriod converts timestamp to broad time period
func getTimePeriod(t time.Time) string {
	hour := t.Hour()
	switch {
	case hour >= 5 && hour < 12:
		return "morning"
	case hour >= 12 && hour < 17:
		return "afternoon"
	case hour >= 17 && hour < 22:
		return "evening"
	default:
		return "night"
	}
}

// getDayType determines if it's a weekday, weekend, or holiday
func getDayType(t time.Time, context map[string]interface{}) string {
	// Check for holiday in context
	if isHoliday, ok := context["is_holiday"].(bool); ok && isHoliday {
		return "holiday"
	}

	// Check weekday vs weekend
	if t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
		return "weekend"
	}

	return "weekday"
}

// extractSignalTypes extracts and normalizes signal types from activity signals
func extractSignalTypes(signals []types.ActivitySignal) []string {
	typeSet := make(map[string]bool)
	var types []string

	for _, signal := range signals {
		// Normalize signal type to semantic category
		normalized := normalizeSignalType(signal)
		if normalized != "" && !typeSet[normalized] {
			typeSet[normalized] = true
			types = append(types, normalized)
		}
	}

	// Sort for consistency in hash generation
	sort.Strings(types)
	return types
}

// normalizeSignalType converts raw signal types to semantic categories
func normalizeSignalType(signal types.ActivitySignal) string {
	switch signal.Type {
	case "motion":
		return "motion"
	case "media":
		// Check if media is playing
		if state, ok := signal.Value["state"].(string); ok && (state == "playing" || state == "on") {
			return "media_playing"
		}
		return "media_available"
	case "presence":
		return "presence"
	case "lighting":
		// Normalize lighting by brightness level
		if brightness, ok := signal.Value["brightness"].(float64); ok {
			if brightness > 0.7 {
				return "bright_lights"
			} else if brightness > 0.3 {
				return "medium_lights"
			} else {
				return "dim_lights"
			}
		}
		return "lights_on"
	case "temperature":
		// Could normalize to comfort zones, but skip for now
		return ""
	case "sound":
		if level, ok := signal.Value["level"].(float64); ok {
			if level > 0.5 {
				return "loud_environment"
			}
			return "quiet_environment"
		}
		return ""
	default:
		// Unknown signal types are ignored
		return ""
	}
}

// GetOrGenerateActivityEmbedding retrieves cached embedding or generates new one via LLM
func (a *ActivityEmbeddingAgent) GetOrGenerateActivityEmbedding(
	ctx context.Context,
	fingerprint ActivityFingerprint,
) ([]float32, error) {
	hash := fingerprint.Hash()

	// Try to get from cache first
	embedding, err := a.Storage.GetActivityEmbedding(ctx, hash)
	if err == nil {
		return embedding, nil
	}

	// Cache miss - generate via LLM
	embedding, err = a.LLM.GenerateActivityEmbedding(ctx, fingerprint)
	if err != nil {
		return nil, fmt.Errorf("failed to generate activity embedding: %w", err)
	}

	// Store in cache for future use
	if err := a.Storage.StoreActivityEmbedding(ctx, hash, fingerprint, embedding); err != nil {
		// Log error but don't fail - we have the embedding
		// TODO: Add logging
	}

	return embedding, nil
}

// ComputeSemanticEmbeddingProgressive generates embedding with progressive activity learning
func (a *ActivityEmbeddingAgent) ComputeSemanticEmbeddingProgressive(
	ctx context.Context,
	location string,
	timestamp time.Time,
	contextData map[string]interface{},
	signals []types.ActivitySignal,
) (pgvector.Vector, error) {
	// Start with base embedding (temporal, spatial, weather, etc.)
	embedding := make([]float32, 128)

	// [0-3]: Temporal cyclical encoding
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

	month := float64(timestamp.Month())
	embedding[6] = float32(math.Sin(2 * math.Pi * month / 12))
	embedding[7] = float32(math.Cos(2 * math.Pi * month / 12))

	// [8-11]: Day type encoding
	embedding[8] = encodeDayType(timestamp)
	embedding[9] = encodeHoliday(contextData)
	embedding[10] = encodeTimeOfDay(timestamp)
	embedding[11] = 0.0

	// [12-27]: Spatial encoding (location) - use existing encodeLocation
	locationVec := encodeLocation(location)
	copy(embedding[12:28], locationVec)

	// [28-43]: Weather context
	weatherVec := encodeWeather(contextData)
	copy(embedding[28:44], weatherVec)

	// [44-59]: Lighting context
	lightingVec := encodeLighting(contextData, signals)
	copy(embedding[44:60], lightingVec)

	// [60-79]: Activity signals - PROGRESSIVE LEARNING
	// Generate activity fingerprint
	fingerprint := GenerateActivityFingerprint(location, timestamp, contextData, signals)

	// Get or generate LLM-based activity embedding
	activityEmbedding, err := a.GetOrGenerateActivityEmbedding(ctx, fingerprint)
	if err != nil {
		// Fallback to rule-based encoding if LLM fails
		// TODO: Add proper logging here
		// fmt.Printf("WARNING: LLM embedding failed, using fallback for %s: %v\n", fingerprint.Hash(), err)
		activityEmbedding = encodeSignals(signals)
	}
	copy(embedding[60:80], activityEmbedding)

	// [80-95]: Household rhythm
	rhythmVec := encodeHouseholdRhythm(timestamp, location)
	copy(embedding[80:96], rhythmVec)

	// [96-127]: Reserved for learned features

	// Normalize to unit length
	normalized := normalize(embedding)

	return pgvector.NewVector(normalized), nil
}
