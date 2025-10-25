package types

import (
	"time"

	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
)

// SemanticAnchor represents a point in behavioral space with a high-dimensional embedding
// that captures the contextual meaning of an activity, rather than just its physical coordinates.
type SemanticAnchor struct {
	ID                 uuid.UUID              `json:"id"`
	Timestamp          time.Time              `json:"timestamp"`
	Location           string                 `json:"location"`
	SemanticEmbedding  pgvector.Vector        `json:"semantic_embedding"` // 128-dimensional vector
	Context            map[string]interface{} `json:"context"`
	Signals            []ActivitySignal       `json:"signals"`
	DurationMinutes    *int                   `json:"duration_minutes,omitempty"`
	DurationSource     *string                `json:"duration_source,omitempty"`     // 'measured', 'estimated', 'inferred'
	DurationConfidence *float64               `json:"duration_confidence,omitempty"` // 0.0-1.0
	PrecedingAnchorID  *uuid.UUID             `json:"preceding_anchor_id,omitempty"`
	FollowingAnchorID  *uuid.UUID             `json:"following_anchor_id,omitempty"`
	PatternID          *uuid.UUID             `json:"pattern_id,omitempty"`
	CreatedAt          time.Time              `json:"created_at"`
}

// ActivitySignal represents an observed signal (motion, lighting, etc.) that contributes
// to understanding what activity is happening at this anchor point.
type ActivitySignal struct {
	Type       string                 `json:"type"`       // "motion", "lighting", "temperature", etc.
	Value      map[string]interface{} `json:"value"`      // Flexible value structure
	Confidence float64                `json:"confidence"` // 0.0-1.0
	Timestamp  time.Time              `json:"timestamp"`
}

// ActivityInterpretation represents a possible interpretation of what activity
// is happening at an anchor. Supports parallel activities in the same space.
type ActivityInterpretation struct {
	ID              uuid.UUID  `json:"id"`
	AnchorID        uuid.UUID  `json:"anchor_id"`
	ActivityType    string     `json:"activity_type"` // "cooking", "reading", "working", etc.
	Confidence      float64    `json:"confidence"`    // 0.0-1.0
	Evidence        []string   `json:"evidence"`      // List of supporting signals
	SpawnedAnchorID *uuid.UUID `json:"spawned_anchor_id,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

// BehavioralPattern represents a discovered pattern with weight-based ranking.
// Weight starts at 0.1 and only increases through successful predictions.
type BehavioralPattern struct {
	ID                     uuid.UUID              `json:"id"`
	Name                   string                 `json:"name"`
	Description            string                 `json:"description,omitempty"`  // LLM-generated description
	PatternType            string                 `json:"pattern_type,omitempty"` // 'morning_routine', 'meal_cycle', etc.
	Weight                 float64                `json:"weight"`                 // Starts at 0.1, only increases
	ClusterSize            int                    `json:"cluster_size"`           // Number of anchors in cluster
	Locations              []string               `json:"locations,omitempty"`    // Locations involved in pattern
	Observations           int                    `json:"observations"`           // Times pattern observed
	TimesObserved          int                    `json:"times_observed"`         // Alias for observations
	Predictions            int                    `json:"predictions"`            // Times used for prediction
	Acceptances            int                    `json:"acceptances"`            // Predictions accepted
	Rejections             int                    `json:"rejections"`             // Predictions rejected
	FirstSeen              time.Time              `json:"first_seen"`
	LastSeen               time.Time              `json:"last_seen"`
	LastUseful             *time.Time             `json:"last_useful,omitempty"`             // Last successful prediction
	TypicalDurationMinutes *int                   `json:"typical_duration_minutes,omitempty"` // Expected duration
	Context                map[string]interface{} `json:"context,omitempty"`                 // Typical context (deprecated)
	DominantContext        map[string]interface{} `json:"dominant_context,omitempty"`        // Dominant context from cluster
	CreatedAt              time.Time              `json:"created_at"`
	UpdatedAt              time.Time              `json:"updated_at"`
}

// AnchorDistance represents a pre-computed semantic distance between two anchors.
type AnchorDistance struct {
	Anchor1ID  uuid.UUID `json:"anchor1_id"`
	Anchor2ID  uuid.UUID `json:"anchor2_id"`
	Distance   float64   `json:"distance"`   // 0.0-1.0 (cosine distance)
	Source     string    `json:"source"`     // 'llm', 'learned', 'vector'
	ComputedAt time.Time `json:"computed_at"`
}

// LearnedDistance represents a pattern-based distance in the learned library.
type LearnedDistance struct {
	ID             uuid.UUID  `json:"id"`
	PatternKey     string     `json:"pattern_key"`     // Generated from anchor characteristics
	Distance       float64    `json:"distance"`        // 0.0-1.0
	Interpretation string     `json:"interpretation"`  // LLM's explanation
	TimesUsed      int        `json:"times_used"`      // Usage counter
	LastUsed       *time.Time `json:"last_used,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}
