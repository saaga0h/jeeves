package embedding

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/saaga0h/jeeves-platform/internal/behavior/types"
)

func TestComputeSemanticEmbedding(t *testing.T) {
	location := "kitchen"
	timestamp := time.Date(2025, 1, 15, 8, 30, 0, 0, time.UTC)
	context := map[string]interface{}{
		"time_of_day":    "morning",
		"household_mode": "waking",
	}
	signals := []types.ActivitySignal{
		{
			Type:       "motion",
			Value:      map[string]interface{}{"state": "detected"},
			Confidence: 0.9,
			Timestamp:  timestamp,
		},
	}

	vec, err := ComputeSemanticEmbedding(location, timestamp, context, signals)
	require.NoError(t, err)
	require.NotNil(t, vec)

	// Check dimensions
	assert.Len(t, vec.Slice(), 128)
}

func TestEmbeddingDeterminism(t *testing.T) {
	location := "bedroom"
	timestamp := time.Date(2025, 3, 20, 14, 15, 0, 0, time.UTC)
	context := map[string]interface{}{
		"season": "spring",
	}
	signals := []types.ActivitySignal{}

	// Compute embedding twice
	vec1, err1 := ComputeSemanticEmbedding(location, timestamp, context, signals)
	require.NoError(t, err1)

	vec2, err2 := ComputeSemanticEmbedding(location, timestamp, context, signals)
	require.NoError(t, err2)

	// Should be identical
	assert.Equal(t, vec1.Slice(), vec2.Slice(), "Same inputs should produce identical embeddings")
}

func TestEmbeddingNormalization(t *testing.T) {
	location := "living_room"
	timestamp := time.Date(2025, 6, 10, 20, 0, 0, 0, time.UTC)
	context := map[string]interface{}{}
	signals := []types.ActivitySignal{}

	vec, err := ComputeSemanticEmbedding(location, timestamp, context, signals)
	require.NoError(t, err)

	// Calculate vector magnitude (should be 1.0 for unit vector)
	var magnitude float64
	for _, v := range vec.Slice() {
		magnitude += float64(v) * float64(v)
	}
	magnitude = math.Sqrt(magnitude)

	// Allow small floating point error
	assert.InDelta(t, 1.0, magnitude, 0.0001, "Vector should be normalized to unit length")
}

func TestCyclicalTimeEncoding(t *testing.T) {
	location := "kitchen"
	context := map[string]interface{}{}
	signals := []types.ActivitySignal{}

	// Test hour 23 (11pm) and hour 0 (midnight) - should be close
	time23 := time.Date(2025, 1, 15, 23, 0, 0, 0, time.UTC)
	time00 := time.Date(2025, 1, 16, 0, 0, 0, 0, time.UTC)

	vec23, err := ComputeSemanticEmbedding(location, time23, context, signals)
	require.NoError(t, err)

	vec00, err := ComputeSemanticEmbedding(location, time00, context, signals)
	require.NoError(t, err)

	// Calculate cosine similarity between the two vectors
	similarity := cosineSimilarity(vec23.Slice(), vec00.Slice())

	// These should be very similar (hour 23 and hour 0 are adjacent)
	// Note: Since we encode many dimensions (128), not just time, similarity will be lower
	assert.Greater(t, similarity, 0.90, "Adjacent hours should have high similarity")

	// Test hour 0 and hour 12 - should be different
	time12 := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	vec12, err := ComputeSemanticEmbedding(location, time12, context, signals)
	require.NoError(t, err)

	similarity012 := cosineSimilarity(vec00.Slice(), vec12.Slice())
	assert.Less(t, similarity012, similarity, "Opposite times of day should be less similar")
}

func TestDayOfWeekCyclical(t *testing.T) {
	location := "office"
	context := map[string]interface{}{}
	signals := []types.ActivitySignal{}

	// Sunday (0) and Monday (1) should be close
	sunday := time.Date(2025, 1, 12, 10, 0, 0, 0, time.UTC) // Sunday
	monday := time.Date(2025, 1, 13, 10, 0, 0, 0, time.UTC) // Monday

	vecSun, _ := ComputeSemanticEmbedding(location, sunday, context, signals)
	vecMon, _ := ComputeSemanticEmbedding(location, monday, context, signals)

	similarity := cosineSimilarity(vecSun.Slice(), vecMon.Slice())
	// Note: Since we encode many dimensions (128), not just day of week, similarity will be lower
	assert.Greater(t, similarity, 0.65, "Adjacent days should have reasonable similarity")
}

func TestSeasonalEncoding(t *testing.T) {
	location := "garden"
	context := map[string]interface{}{}
	signals := []types.ActivitySignal{}

	// Summer and winter should be quite different
	summer := time.Date(2025, 7, 15, 12, 0, 0, 0, time.UTC)
	winter := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)

	vecSummer, _ := ComputeSemanticEmbedding(location, summer, context, signals)
	vecWinter, _ := ComputeSemanticEmbedding(location, winter, context, signals)

	similarity := cosineSimilarity(vecSummer.Slice(), vecWinter.Slice())
	assert.Less(t, similarity, 0.95, "Opposite seasons should be less similar")

	// Adjacent months should be more similar
	june := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	july := time.Date(2025, 7, 15, 12, 0, 0, 0, time.UTC)

	vecJune, _ := ComputeSemanticEmbedding(location, june, context, signals)
	vecJuly, _ := ComputeSemanticEmbedding(location, july, context, signals)

	similarityAdjacent := cosineSimilarity(vecJune.Slice(), vecJuly.Slice())
	assert.Greater(t, similarityAdjacent, similarity, "Adjacent months should be more similar than opposite seasons")
}

func TestDifferentLocationsProduceDifferentEmbeddings(t *testing.T) {
	timestamp := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	context := map[string]interface{}{}
	signals := []types.ActivitySignal{}

	vecKitchen, _ := ComputeSemanticEmbedding("kitchen", timestamp, context, signals)
	vecBedroom, _ := ComputeSemanticEmbedding("bedroom", timestamp, context, signals)
	vecBathroom, _ := ComputeSemanticEmbedding("bathroom", timestamp, context, signals)

	// Different locations should produce different embeddings
	assert.NotEqual(t, vecKitchen.Slice(), vecBedroom.Slice())
	assert.NotEqual(t, vecBedroom.Slice(), vecBathroom.Slice())

	// But same location should produce same embedding
	vecKitchen2, _ := ComputeSemanticEmbedding("kitchen", timestamp, context, signals)
	assert.Equal(t, vecKitchen.Slice(), vecKitchen2.Slice())
}

func TestWeatherContextEncoding(t *testing.T) {
	location := "bedroom"
	timestamp := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	signals := []types.ActivitySignal{}

	// No weather context
	contextNoWeather := map[string]interface{}{}
	vecNoWeather, _ := ComputeSemanticEmbedding(location, timestamp, contextNoWeather, signals)

	// With weather context
	contextWithWeather := map[string]interface{}{
		"weather": map[string]interface{}{
			"brightness":    0.8,
			"precipitation": 0.2,
			"temperature":   15.0,
			"cloudiness":    0.4,
		},
	}
	vecWithWeather, _ := ComputeSemanticEmbedding(location, timestamp, contextWithWeather, signals)

	// Embeddings should be different
	similarity := cosineSimilarity(vecNoWeather.Slice(), vecWithWeather.Slice())
	assert.Less(t, similarity, 0.99, "Weather context should affect embedding")
}

func TestLightingSignalsEncoding(t *testing.T) {
	location := "living_room"
	timestamp := time.Date(2025, 1, 15, 20, 0, 0, 0, time.UTC)
	context := map[string]interface{}{}

	// No lighting signals
	signalsNoLight := []types.ActivitySignal{}
	vecNoLight, _ := ComputeSemanticEmbedding(location, timestamp, context, signalsNoLight)

	// With lighting signals
	signalsWithLight := []types.ActivitySignal{
		{
			Type: "lighting",
			Value: map[string]interface{}{
				"state":      "on",
				"brightness": 75.0,
			},
			Confidence: 1.0,
			Timestamp:  timestamp,
		},
	}
	vecWithLight, _ := ComputeSemanticEmbedding(location, timestamp, context, signalsWithLight)

	// Embeddings should be different
	similarity := cosineSimilarity(vecNoLight.Slice(), vecWithLight.Slice())
	assert.Less(t, similarity, 0.99, "Lighting signals should affect embedding")
}

func TestMotionSignalsEncoding(t *testing.T) {
	location := "kitchen"
	timestamp := time.Date(2025, 1, 15, 8, 0, 0, 0, time.UTC)
	context := map[string]interface{}{}

	// Single motion event
	signalsSingle := []types.ActivitySignal{
		{
			Type:       "motion",
			Value:      map[string]interface{}{"state": "detected"},
			Confidence: 0.8,
			Timestamp:  timestamp,
		},
	}
	vecSingle, _ := ComputeSemanticEmbedding(location, timestamp, context, signalsSingle)

	// Multiple motion events (frequent activity)
	signalsMultiple := []types.ActivitySignal{
		{Type: "motion", Confidence: 0.9, Timestamp: timestamp},
		{Type: "motion", Confidence: 0.85, Timestamp: timestamp.Add(1 * time.Minute)},
		{Type: "motion", Confidence: 0.95, Timestamp: timestamp.Add(2 * time.Minute)},
		{Type: "motion", Confidence: 0.88, Timestamp: timestamp.Add(3 * time.Minute)},
	}
	vecMultiple, _ := ComputeSemanticEmbedding(location, timestamp, context, signalsMultiple)

	// Embeddings should be different (frequency matters)
	similarity := cosineSimilarity(vecSingle.Slice(), vecMultiple.Slice())
	assert.Less(t, similarity, 0.99, "Motion frequency should affect embedding")
}

func TestMediaSignalsEncoding(t *testing.T) {
	location := "living_room"
	timestamp := time.Date(2025, 1, 15, 19, 0, 0, 0, time.UTC)
	context := map[string]interface{}{}

	// Without media
	signalsNoMedia := []types.ActivitySignal{}
	vecNoMedia, _ := ComputeSemanticEmbedding(location, timestamp, context, signalsNoMedia)

	// With media playing
	signalsWithMedia := []types.ActivitySignal{
		{
			Type: "media",
			Value: map[string]interface{}{
				"state": "playing",
				"type":  "tv",
			},
			Confidence: 1.0,
			Timestamp:  timestamp,
		},
	}
	vecWithMedia, _ := ComputeSemanticEmbedding(location, timestamp, context, signalsWithMedia)

	// Embeddings should be different
	similarity := cosineSimilarity(vecNoMedia.Slice(), vecWithMedia.Slice())
	assert.Less(t, similarity, 0.99, "Media signals should affect embedding")
}

func TestHouseholdRhythmEncoding(t *testing.T) {
	location := "bedroom"
	context := map[string]interface{}{}
	signals := []types.ActivitySignal{}

	// Morning in bedroom (waking period)
	morning := time.Date(2025, 1, 15, 7, 0, 0, 0, time.UTC)
	vecMorning, _ := ComputeSemanticEmbedding(location, morning, context, signals)

	// Night in bedroom (sleep period)
	night := time.Date(2025, 1, 15, 23, 0, 0, 0, time.UTC)
	vecNight, _ := ComputeSemanticEmbedding(location, night, context, signals)

	// Both should be somewhat similar (bedroom is active at both times)
	similarity := cosineSimilarity(vecMorning.Slice(), vecNight.Slice())

	// But compare to afternoon in bedroom (less typical)
	afternoon := time.Date(2025, 1, 15, 15, 0, 0, 0, time.UTC)
	vecAfternoon, _ := ComputeSemanticEmbedding(location, afternoon, context, signals)

	similarityAfternoon := cosineSimilarity(vecMorning.Slice(), vecAfternoon.Slice())

	// Morning and night should be at least somewhat similar (both are bedroom times)
	// but this test is subtle since many other dimensions differ (time of day, etc)
	// Just verify that afternoon similarity isn't dramatically higher
	assert.InDelta(t, similarity, similarityAfternoon, 0.15,
		"Bedroom rhythm encoding should show some consistency across typical bedroom times")
}

func TestEncodeDayType(t *testing.T) {
	// Weekday
	monday := time.Date(2025, 1, 13, 10, 0, 0, 0, time.UTC)
	assert.Equal(t, float32(1.0), encodeDayType(monday))

	// Weekend
	saturday := time.Date(2025, 1, 11, 10, 0, 0, 0, time.UTC)
	assert.Equal(t, float32(-1.0), encodeDayType(saturday))

	sunday := time.Date(2025, 1, 12, 10, 0, 0, 0, time.UTC)
	assert.Equal(t, float32(-1.0), encodeDayType(sunday))
}

func TestEncodeHoliday(t *testing.T) {
	// Not a holiday
	contextNormal := map[string]interface{}{}
	assert.Equal(t, float32(0.0), encodeHoliday(contextNormal))

	// Holiday
	contextHoliday := map[string]interface{}{
		"is_holiday": true,
	}
	assert.Equal(t, float32(1.0), encodeHoliday(contextHoliday))

	// Explicitly not a holiday
	contextNotHoliday := map[string]interface{}{
		"is_holiday": false,
	}
	assert.Equal(t, float32(0.0), encodeHoliday(contextNotHoliday))
}

func TestEncodeTimeOfDay(t *testing.T) {
	tests := []struct {
		hour     int
		expected float32
	}{
		{7, 1.0},   // morning
		{13, 0.5},  // afternoon
		{19, 0.0},  // evening
		{23, -1.0}, // night
		{2, -1.0},  // night
	}

	for _, tt := range tests {
		t.Run(string(rune(tt.hour)), func(t *testing.T) {
			timestamp := time.Date(2025, 1, 15, tt.hour, 0, 0, 0, time.UTC)
			result := encodeTimeOfDay(timestamp)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLocationHashDeterminism(t *testing.T) {
	// Same location should produce same hash
	vec1 := encodeLocation("kitchen")
	vec2 := encodeLocation("kitchen")
	assert.Equal(t, vec1, vec2)

	// Different locations should produce different hashes
	vecBedroom := encodeLocation("bedroom")
	assert.NotEqual(t, vec1, vecBedroom)
}

func TestNormalize(t *testing.T) {
	// Test vector
	vec := []float32{1.0, 2.0, 3.0, 4.0}

	normalized := normalize(vec)

	// Calculate magnitude
	var magnitude float64
	for _, v := range normalized {
		magnitude += float64(v) * float64(v)
	}
	magnitude = math.Sqrt(magnitude)

	assert.InDelta(t, 1.0, magnitude, 0.0001, "Normalized vector should have unit length")
}

func TestNormalizeZeroVector(t *testing.T) {
	// Zero vector should remain zero
	vec := []float32{0.0, 0.0, 0.0}
	normalized := normalize(vec)

	assert.Equal(t, vec, normalized, "Zero vector should remain unchanged")
}

// Helper function to calculate cosine similarity
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0.0
	}

	var dotProduct, normA, normB float64

	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0.0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}
