package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/pgvector/pgvector-go"

	"github.com/saaga0h/jeeves-platform/internal/behavior/types"
)

// AnchorStorage provides persistent storage for semantic anchors using PostgreSQL + pgvector.
type AnchorStorage struct {
	db *sql.DB
}

// NewAnchorStorage creates a new anchor storage instance.
func NewAnchorStorage(db *sql.DB) *AnchorStorage {
	return &AnchorStorage{db: db}
}

// CreateAnchor stores a new semantic anchor in the database.
func (s *AnchorStorage) CreateAnchor(ctx context.Context, anchor *types.SemanticAnchor) error {
	// Marshal context and signals to JSONB
	contextJSON, err := json.Marshal(anchor.Context)
	if err != nil {
		return fmt.Errorf("failed to marshal context: %w", err)
	}

	signalsJSON, err := json.Marshal(anchor.Signals)
	if err != nil {
		return fmt.Errorf("failed to marshal signals: %w", err)
	}

	// Generate UUID if not provided
	if anchor.ID == uuid.Nil {
		anchor.ID = uuid.New()
	}

	// Set created_at if not provided
	if anchor.CreatedAt.IsZero() {
		anchor.CreatedAt = time.Now()
	}

	query := `
		INSERT INTO semantic_anchors (
			id, timestamp, location, semantic_embedding, context, signals,
			duration_minutes, duration_source, duration_confidence,
			preceding_anchor_id, following_anchor_id, pattern_id, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`

	_, err = s.db.ExecContext(ctx, query,
		anchor.ID,
		anchor.Timestamp,
		anchor.Location,
		anchor.SemanticEmbedding,
		contextJSON,
		signalsJSON,
		anchor.DurationMinutes,
		anchor.DurationSource,
		anchor.DurationConfidence,
		anchor.PrecedingAnchorID,
		anchor.FollowingAnchorID,
		anchor.PatternID,
		anchor.CreatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to insert anchor: %w", err)
	}

	return nil
}

// GetAnchor retrieves a semantic anchor by ID.
func (s *AnchorStorage) GetAnchor(ctx context.Context, id uuid.UUID) (*types.SemanticAnchor, error) {
	query := `
		SELECT
			id, timestamp, location, semantic_embedding, context, signals,
			duration_minutes, duration_source, duration_confidence,
			preceding_anchor_id, following_anchor_id, pattern_id, created_at
		FROM semantic_anchors
		WHERE id = $1
	`

	var anchor types.SemanticAnchor
	var contextJSON, signalsJSON []byte

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&anchor.ID,
		&anchor.Timestamp,
		&anchor.Location,
		&anchor.SemanticEmbedding,
		&contextJSON,
		&signalsJSON,
		&anchor.DurationMinutes,
		&anchor.DurationSource,
		&anchor.DurationConfidence,
		&anchor.PrecedingAnchorID,
		&anchor.FollowingAnchorID,
		&anchor.PatternID,
		&anchor.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("anchor not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query anchor: %w", err)
	}

	// Unmarshal JSONB fields
	if err := json.Unmarshal(contextJSON, &anchor.Context); err != nil {
		return nil, fmt.Errorf("failed to unmarshal context: %w", err)
	}

	if err := json.Unmarshal(signalsJSON, &anchor.Signals); err != nil {
		return nil, fmt.Errorf("failed to unmarshal signals: %w", err)
	}

	return &anchor, nil
}

// FindSimilarAnchors finds anchors similar to the given embedding using vector similarity search.
// Returns up to limit anchors ordered by similarity (most similar first).
func (s *AnchorStorage) FindSimilarAnchors(ctx context.Context, embedding pgvector.Vector, limit int) ([]*types.SemanticAnchor, error) {
	query := `
		SELECT
			id, timestamp, location, semantic_embedding, context, signals,
			duration_minutes, duration_source, duration_confidence,
			preceding_anchor_id, following_anchor_id, pattern_id, created_at,
			semantic_embedding <=> $1 AS distance
		FROM semantic_anchors
		ORDER BY semantic_embedding <=> $1
		LIMIT $2
	`

	rows, err := s.db.QueryContext(ctx, query, embedding, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query similar anchors: %w", err)
	}
	defer rows.Close()

	var anchors []*types.SemanticAnchor

	for rows.Next() {
		var anchor types.SemanticAnchor
		var contextJSON, signalsJSON []byte
		var distance float64

		err := rows.Scan(
			&anchor.ID,
			&anchor.Timestamp,
			&anchor.Location,
			&anchor.SemanticEmbedding,
			&contextJSON,
			&signalsJSON,
			&anchor.DurationMinutes,
			&anchor.DurationSource,
			&anchor.DurationConfidence,
			&anchor.PrecedingAnchorID,
			&anchor.FollowingAnchorID,
			&anchor.PatternID,
			&anchor.CreatedAt,
			&distance,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan anchor row: %w", err)
		}

		// Unmarshal JSONB fields
		if err := json.Unmarshal(contextJSON, &anchor.Context); err != nil {
			return nil, fmt.Errorf("failed to unmarshal context: %w", err)
		}

		if err := json.Unmarshal(signalsJSON, &anchor.Signals); err != nil {
			return nil, fmt.Errorf("failed to unmarshal signals: %w", err)
		}

		anchors = append(anchors, &anchor)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating anchor rows: %w", err)
	}

	return anchors, nil
}

// GetAnchorsNeedingDistances finds pairs of anchors that don't have pre-computed distances yet.
// Uses smart filtering to only consider semantically related pairs (same/adjacent locations, time windows, day types).
// This reduces the O(n²) problem to ~O(n×k) where k is average neighbors per anchor (~10-50).
func (s *AnchorStorage) GetAnchorsNeedingDistances(ctx context.Context, limit int) ([][2]uuid.UUID, error) {
	return s.GetAnchorsNeedingDistancesInWindow(ctx, limit, time.Time{}, time.Time{})
}

// GetAnchorsNeedingDistancesInWindow finds pairs of anchors within a time window that don't have pre-computed distances.
// If windowStart or windowEnd is zero, no time filtering is applied (equivalent to GetAnchorsNeedingDistances).
// For batch processing: at least one anchor must be in the batch window, the other can be in the overlap window.
func (s *AnchorStorage) GetAnchorsNeedingDistancesInWindow(
	ctx context.Context,
	limit int,
	windowStart, windowEnd time.Time,
) ([][2]uuid.UUID, error) {
	// Build query with optional time window filtering
	queryBase := `
		SELECT a1.id, a2.id
		FROM semantic_anchors a1
		CROSS JOIN semantic_anchors a2
		WHERE a1.id < a2.id
		  AND NOT EXISTS (
			SELECT 1
			FROM anchor_distances ad
			WHERE (ad.anchor1_id = a1.id AND ad.anchor2_id = a2.id)
			   OR (ad.anchor1_id = a2.id AND ad.anchor2_id = a1.id)
		  )`

	// Add time window filter if specified
	var timeFilter string
	var args []interface{}
	argIndex := 1

	if !windowStart.IsZero() && !windowEnd.IsZero() {
		// Both anchors must be within the window (windowStart to windowEnd)
		timeFilter = fmt.Sprintf(`
		  AND a1.timestamp >= $%d AND a1.timestamp < $%d
		  AND a2.timestamp >= $%d AND a2.timestamp < $%d`,
			argIndex, argIndex+1, argIndex+2, argIndex+3)
		args = append(args, windowStart, windowEnd, windowStart, windowEnd)
		argIndex += 4
	}

	query := queryBase + timeFilter + `
		  -- FILTER 1: Same or adjacent locations (reduces pairs by ~80%)
		  AND (
			a1.location = a2.location
			OR (a1.location IN ('bedroom', 'bathroom') AND a2.location IN ('bedroom', 'bathroom'))
			OR (a1.location IN ('kitchen', 'dining_room') AND a2.location IN ('kitchen', 'dining_room'))
			OR (a1.location IN ('living_room', 'dining_room') AND a2.location IN ('living_room', 'dining_room'))
			OR (a1.location IN ('living_room', 'study') AND a2.location IN ('living_room', 'study'))
		  )
		  -- FILTER 2: Within 2-hour time window (tighter temporal proximity)
		  AND ABS(EXTRACT(EPOCH FROM (a1.timestamp - a2.timestamp))) < 7200
		  -- FILTER 3: Same day type (weekend vs weekday)
		  AND (a1.context->>'day_type') = (a2.context->>'day_type')
		  -- FILTER 4: Same or adjacent time of day
		  AND (
			(a1.context->>'time_of_day') = (a2.context->>'time_of_day')
			OR ((a1.context->>'time_of_day') = 'morning' AND (a2.context->>'time_of_day') = 'afternoon')
			OR ((a1.context->>'time_of_day') = 'afternoon' AND (a2.context->>'time_of_day') = 'morning')
			OR ((a1.context->>'time_of_day') = 'afternoon' AND (a2.context->>'time_of_day') = 'evening')
			OR ((a1.context->>'time_of_day') = 'evening' AND (a2.context->>'time_of_day') = 'afternoon')
		  )
		ORDER BY a1.created_at DESC, a2.created_at DESC
		LIMIT $` + fmt.Sprintf("%d", argIndex)

	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query anchor pairs: %w", err)
	}
	defer rows.Close()

	var pairs [][2]uuid.UUID

	for rows.Next() {
		var id1, id2 uuid.UUID
		if err := rows.Scan(&id1, &id2); err != nil {
			return nil, fmt.Errorf("failed to scan anchor pair: %w", err)
		}
		pairs = append(pairs, [2]uuid.UUID{id1, id2})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating anchor pairs: %w", err)
	}

	return pairs, nil
}

// StoreDistance stores a pre-computed distance between two anchors.
// Ensures anchor1_id < anchor2_id to avoid duplicates.
func (s *AnchorStorage) StoreDistance(ctx context.Context, distance *types.AnchorDistance) error {
	// Ensure anchor1_id < anchor2_id (database constraint)
	anchor1, anchor2 := distance.Anchor1ID, distance.Anchor2ID
	if anchor1.String() > anchor2.String() {
		anchor1, anchor2 = anchor2, anchor1
	}

	// Set computed_at if not provided
	if distance.ComputedAt.IsZero() {
		distance.ComputedAt = time.Now()
	}

	query := `
		INSERT INTO anchor_distances (anchor1_id, anchor2_id, distance, source, computed_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (anchor1_id, anchor2_id)
		DO UPDATE SET
			distance = EXCLUDED.distance,
			source = EXCLUDED.source,
			computed_at = EXCLUDED.computed_at
	`

	_, err := s.db.ExecContext(ctx, query,
		anchor1,
		anchor2,
		distance.Distance,
		distance.Source,
		distance.ComputedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to store distance: %w", err)
	}

	return nil
}

// GetDistance retrieves the pre-computed distance between two anchors.
// Returns nil if no distance has been computed yet.
func (s *AnchorStorage) GetDistance(ctx context.Context, anchor1ID, anchor2ID uuid.UUID) (*types.AnchorDistance, error) {
	// Ensure anchor1_id < anchor2_id (database constraint)
	id1, id2 := anchor1ID, anchor2ID
	if id1.String() > id2.String() {
		id1, id2 = id2, id1
	}

	query := `
		SELECT anchor1_id, anchor2_id, distance, source, computed_at
		FROM anchor_distances
		WHERE anchor1_id = $1 AND anchor2_id = $2
	`

	var distance types.AnchorDistance

	err := s.db.QueryRowContext(ctx, query, id1, id2).Scan(
		&distance.Anchor1ID,
		&distance.Anchor2ID,
		&distance.Distance,
		&distance.Source,
		&distance.ComputedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil // No distance computed yet
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query distance: %w", err)
	}

	return &distance, nil
}

// CreateInterpretation stores an activity interpretation for an anchor.
func (s *AnchorStorage) CreateInterpretation(ctx context.Context, interpretation *types.ActivityInterpretation) error {
	// Generate UUID if not provided
	if interpretation.ID == uuid.Nil {
		interpretation.ID = uuid.New()
	}

	// Set created_at if not provided
	if interpretation.CreatedAt.IsZero() {
		interpretation.CreatedAt = time.Now()
	}

	// Convert evidence slice to PostgreSQL array
	evidenceJSON, err := json.Marshal(interpretation.Evidence)
	if err != nil {
		return fmt.Errorf("failed to marshal evidence: %w", err)
	}

	query := `
		INSERT INTO anchor_interpretations (
			id, anchor_id, activity_type, confidence, evidence, spawned_anchor_id, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, err = s.db.ExecContext(ctx, query,
		interpretation.ID,
		interpretation.AnchorID,
		interpretation.ActivityType,
		interpretation.Confidence,
		evidenceJSON,
		interpretation.SpawnedAnchorID,
		interpretation.CreatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to insert interpretation: %w", err)
	}

	return nil
}

// GetInterpretations retrieves all interpretations for a given anchor.
func (s *AnchorStorage) GetInterpretations(ctx context.Context, anchorID uuid.UUID) ([]*types.ActivityInterpretation, error) {
	query := `
		SELECT id, anchor_id, activity_type, confidence, evidence, spawned_anchor_id, created_at
		FROM anchor_interpretations
		WHERE anchor_id = $1
		ORDER BY confidence DESC
	`

	rows, err := s.db.QueryContext(ctx, query, anchorID)
	if err != nil {
		return nil, fmt.Errorf("failed to query interpretations: %w", err)
	}
	defer rows.Close()

	var interpretations []*types.ActivityInterpretation

	for rows.Next() {
		var interp types.ActivityInterpretation
		var evidenceJSON []byte

		err := rows.Scan(
			&interp.ID,
			&interp.AnchorID,
			&interp.ActivityType,
			&interp.Confidence,
			&evidenceJSON,
			&interp.SpawnedAnchorID,
			&interp.CreatedAt,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan interpretation row: %w", err)
		}

		// Unmarshal evidence array
		if err := json.Unmarshal(evidenceJSON, &interp.Evidence); err != nil {
			return nil, fmt.Errorf("failed to unmarshal evidence: %w", err)
		}

		interpretations = append(interpretations, &interp)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating interpretation rows: %w", err)
	}

	return interpretations, nil
}

// CreatePattern stores a new behavioral pattern.
func (s *AnchorStorage) CreatePattern(ctx context.Context, pattern *types.BehavioralPattern) error {
	// Generate UUID if not provided
	if pattern.ID == uuid.Nil {
		pattern.ID = uuid.New()
	}

	// Set timestamps if not provided
	now := time.Now()
	if pattern.CreatedAt.IsZero() {
		pattern.CreatedAt = now
	}
	if pattern.UpdatedAt.IsZero() {
		pattern.UpdatedAt = now
	}
	if pattern.FirstSeen.IsZero() {
		pattern.FirstSeen = now
	}
	if pattern.LastSeen.IsZero() {
		pattern.LastSeen = now
	}

	// Default weight to 0.1 if not set
	if pattern.Weight == 0.0 {
		pattern.Weight = 0.1
	}

	// Marshal context and dominant_context to JSONB
	// PostgreSQL JSONB columns should always have valid JSON, use {} for nil/empty
	var contextJSON []byte
	var dominantContextJSON []byte
	var err error

	if pattern.Context != nil && len(pattern.Context) > 0 {
		contextJSON, err = json.Marshal(pattern.Context)
		if err != nil {
			return fmt.Errorf("failed to marshal context: %w", err)
		}
	} else {
		contextJSON = []byte("{}")
	}

	if pattern.DominantContext != nil && len(pattern.DominantContext) > 0 {
		dominantContextJSON, err = json.Marshal(pattern.DominantContext)
		if err != nil {
			return fmt.Errorf("failed to marshal dominant_context: %w", err)
		}
	} else {
		dominantContextJSON = []byte("{}")
	}

	// Use pq.Array for TEXT[] fields
	locations := pattern.Locations
	if locations == nil {
		locations = []string{}
	}

	query := `
		INSERT INTO behavioral_patterns (
			id, name, description, pattern_type, weight, cluster_size, locations,
			observations, times_observed, predictions, acceptances, rejections,
			first_seen, last_seen, last_useful, typical_duration_minutes,
			context, dominant_context, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
	`

	_, err = s.db.ExecContext(ctx, query,
		pattern.ID,
		pattern.Name,
		pattern.Description,
		pattern.PatternType,
		pattern.Weight,
		pattern.ClusterSize,
		pq.Array(locations),
		pattern.Observations,
		pattern.TimesObserved,
		pattern.Predictions,
		pattern.Acceptances,
		pattern.Rejections,
		pattern.FirstSeen,
		pattern.LastSeen,
		pattern.LastUseful,
		pattern.TypicalDurationMinutes,
		contextJSON,
		dominantContextJSON,
		pattern.CreatedAt,
		pattern.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to insert pattern: %w", err)
	}

	return nil
}

// GetPattern retrieves a behavioral pattern by ID.
func (s *AnchorStorage) GetPattern(ctx context.Context, id uuid.UUID) (*types.BehavioralPattern, error) {
	query := `
		SELECT
			id, name, description, pattern_type, weight, cluster_size, locations,
			observations, times_observed, predictions, acceptances, rejections,
			first_seen, last_seen, last_useful, typical_duration_minutes,
			context, dominant_context, created_at, updated_at
		FROM behavioral_patterns
		WHERE id = $1
	`

	var pattern types.BehavioralPattern
	var contextJSON []byte
	var dominantContextJSON []byte

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&pattern.ID,
		&pattern.Name,
		&pattern.Description,
		&pattern.PatternType,
		&pattern.Weight,
		&pattern.ClusterSize,
		pq.Array(&pattern.Locations),
		&pattern.Observations,
		&pattern.TimesObserved,
		&pattern.Predictions,
		&pattern.Acceptances,
		&pattern.Rejections,
		&pattern.FirstSeen,
		&pattern.LastSeen,
		&pattern.LastUseful,
		&pattern.TypicalDurationMinutes,
		&contextJSON,
		&dominantContextJSON,
		&pattern.CreatedAt,
		&pattern.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("pattern not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query pattern: %w", err)
	}

	// Unmarshal context if present
	if contextJSON != nil {
		if err := json.Unmarshal(contextJSON, &pattern.Context); err != nil {
			return nil, fmt.Errorf("failed to unmarshal context: %w", err)
		}
	}

	// Unmarshal dominant_context if present
	if dominantContextJSON != nil {
		if err := json.Unmarshal(dominantContextJSON, &pattern.DominantContext); err != nil {
			return nil, fmt.Errorf("failed to unmarshal dominant_context: %w", err)
		}
	}

	return &pattern, nil
}

// UpdatePattern updates an existing behavioral pattern's statistics.
func (s *AnchorStorage) UpdatePattern(ctx context.Context, pattern *types.BehavioralPattern) error {
	pattern.UpdatedAt = time.Now()

	// Marshal context and dominant_context to JSONB
	var contextJSON []byte
	var dominantContextJSON []byte
	var err error

	if pattern.Context != nil {
		contextJSON, err = json.Marshal(pattern.Context)
		if err != nil {
			return fmt.Errorf("failed to marshal context: %w", err)
		}
	}

	if pattern.DominantContext != nil {
		dominantContextJSON, err = json.Marshal(pattern.DominantContext)
		if err != nil {
			return fmt.Errorf("failed to marshal dominant_context: %w", err)
		}
	}

	// Handle locations
	locations := pattern.Locations
	if locations == nil {
		locations = []string{}
	}

	query := `
		UPDATE behavioral_patterns
		SET
			name = $2,
			description = $3,
			pattern_type = $4,
			weight = $5,
			cluster_size = $6,
			locations = $7,
			observations = $8,
			times_observed = $9,
			predictions = $10,
			acceptances = $11,
			rejections = $12,
			last_seen = $13,
			last_useful = $14,
			typical_duration_minutes = $15,
			context = $16,
			dominant_context = $17,
			updated_at = $18
		WHERE id = $1
	`

	result, err := s.db.ExecContext(ctx, query,
		pattern.ID,
		pattern.Name,
		pattern.Description,
		pattern.PatternType,
		pattern.Weight,
		pattern.ClusterSize,
		pq.Array(locations),
		pattern.Observations,
		pattern.TimesObserved,
		pattern.Predictions,
		pattern.Acceptances,
		pattern.Rejections,
		pattern.LastSeen,
		pattern.LastUseful,
		pattern.TypicalDurationMinutes,
		contextJSON,
		dominantContextJSON,
		pattern.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to update pattern: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("pattern not found: %s", pattern.ID)
	}

	return nil
}

// GetTopPatterns retrieves the top N patterns ordered by weight.
func (s *AnchorStorage) GetTopPatterns(ctx context.Context, limit int) ([]*types.BehavioralPattern, error) {
	query := `
		SELECT
			id, name, description, pattern_type, weight, cluster_size, locations,
			observations, times_observed, predictions, acceptances, rejections,
			first_seen, last_seen, last_useful, typical_duration_minutes,
			context, dominant_context, created_at, updated_at
		FROM behavioral_patterns
		ORDER BY weight DESC
		LIMIT $1
	`

	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query patterns: %w", err)
	}
	defer rows.Close()

	var patterns []*types.BehavioralPattern

	for rows.Next() {
		var pattern types.BehavioralPattern
		var contextJSON []byte
		var dominantContextJSON []byte

		err := rows.Scan(
			&pattern.ID,
			&pattern.Name,
			&pattern.Description,
			&pattern.PatternType,
			&pattern.Weight,
			&pattern.ClusterSize,
			pq.Array(&pattern.Locations),
			&pattern.Observations,
			&pattern.TimesObserved,
			&pattern.Predictions,
			&pattern.Acceptances,
			&pattern.Rejections,
			&pattern.FirstSeen,
			&pattern.LastSeen,
			&pattern.LastUseful,
			&pattern.TypicalDurationMinutes,
			&contextJSON,
			&dominantContextJSON,
			&pattern.CreatedAt,
			&pattern.UpdatedAt,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan pattern row: %w", err)
		}

		// Unmarshal context if present
		if contextJSON != nil {
			if err := json.Unmarshal(contextJSON, &pattern.Context); err != nil {
				return nil, fmt.Errorf("failed to unmarshal context: %w", err)
			}
		}

		// Unmarshal dominant_context if present
		if dominantContextJSON != nil {
			if err := json.Unmarshal(dominantContextJSON, &pattern.DominantContext); err != nil {
				return nil, fmt.Errorf("failed to unmarshal dominant_context: %w", err)
			}
		}

		patterns = append(patterns, &pattern)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating pattern rows: %w", err)
	}

	return patterns, nil
}

// UpdateAnchorPattern updates an anchor's pattern_id reference
func (s *AnchorStorage) UpdateAnchorPattern(ctx context.Context, anchorID, patternID uuid.UUID) error {
	query := `
		UPDATE semantic_anchors
		SET pattern_id = $2
		WHERE id = $1
	`

	_, err := s.db.ExecContext(ctx, query, anchorID, patternID)
	if err != nil {
		return fmt.Errorf("failed to update anchor pattern: %w", err)
	}

	return nil
}

// GetAnchorsWithDistances retrieves anchors that have computed distances
// GetAnchorsSince retrieves all anchors since a given timestamp
// This method does NOT require pre-computed distances, making it suitable for in-memory distance computation
func (s *AnchorStorage) GetAnchorsSince(ctx context.Context, since time.Time) ([]*types.SemanticAnchor, error) {
	query := `
		SELECT id, timestamp, location, semantic_embedding,
		       context, signals, duration_minutes, duration_source,
		       duration_confidence, preceding_anchor_id, following_anchor_id,
		       pattern_id, created_at
		FROM semantic_anchors
		WHERE timestamp >= $1
		  AND pattern_id IS NULL
		ORDER BY timestamp ASC`

	rows, err := s.db.QueryContext(ctx, query, since)
	if err != nil {
		return nil, fmt.Errorf("failed to query anchors: %w", err)
	}
	defer rows.Close()

	var anchors []*types.SemanticAnchor
	for rows.Next() {
		var anchor types.SemanticAnchor
		var contextJSON, signalsJSON []byte

		err := rows.Scan(
			&anchor.ID,
			&anchor.Timestamp,
			&anchor.Location,
			&anchor.SemanticEmbedding,
			&contextJSON,
			&signalsJSON,
			&anchor.DurationMinutes,
			&anchor.DurationSource,
			&anchor.DurationConfidence,
			&anchor.PrecedingAnchorID,
			&anchor.FollowingAnchorID,
			&anchor.PatternID,
			&anchor.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan anchor: %w", err)
		}

		// Unmarshal JSONB fields
		if err := json.Unmarshal(contextJSON, &anchor.Context); err != nil {
			return nil, fmt.Errorf("failed to unmarshal context: %w", err)
		}

		if err := json.Unmarshal(signalsJSON, &anchor.Signals); err != nil {
			return nil, fmt.Errorf("failed to unmarshal signals: %w", err)
		}

		anchors = append(anchors, &anchor)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating anchors: %w", err)
	}

	return anchors, nil
}

// GetAnchorsSinceInWindow retrieves all anchors within a time window
// This method does NOT require pre-computed distances, making it suitable for in-memory distance computation
func (s *AnchorStorage) GetAnchorsSinceInWindow(ctx context.Context, windowStart, windowEnd time.Time) ([]*types.SemanticAnchor, error) {
	query := `
		SELECT id, timestamp, location, semantic_embedding,
		       context, signals, duration_minutes, duration_source,
		       duration_confidence, preceding_anchor_id, following_anchor_id,
		       pattern_id, created_at
		FROM semantic_anchors
		WHERE timestamp >= $1
		  AND timestamp < $2
		  AND pattern_id IS NULL
		ORDER BY timestamp ASC`

	rows, err := s.db.QueryContext(ctx, query, windowStart, windowEnd)
	if err != nil {
		return nil, fmt.Errorf("failed to query anchors: %w", err)
	}
	defer rows.Close()

	var anchors []*types.SemanticAnchor
	for rows.Next() {
		var anchor types.SemanticAnchor
		var contextJSON, signalsJSON []byte

		err := rows.Scan(
			&anchor.ID,
			&anchor.Timestamp,
			&anchor.Location,
			&anchor.SemanticEmbedding,
			&contextJSON,
			&signalsJSON,
			&anchor.DurationMinutes,
			&anchor.DurationSource,
			&anchor.DurationConfidence,
			&anchor.PrecedingAnchorID,
			&anchor.FollowingAnchorID,
			&anchor.PatternID,
			&anchor.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan anchor: %w", err)
		}

		// Unmarshal JSONB fields
		if err := json.Unmarshal(contextJSON, &anchor.Context); err != nil {
			return nil, fmt.Errorf("failed to unmarshal context: %w", err)
		}

		if err := json.Unmarshal(signalsJSON, &anchor.Signals); err != nil {
			return nil, fmt.Errorf("failed to unmarshal signals: %w", err)
		}

		anchors = append(anchors, &anchor)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating anchors: %w", err)
	}

	return anchors, nil
}

func (s *AnchorStorage) GetAnchorsWithDistances(ctx context.Context, since time.Time) ([]*types.SemanticAnchor, error) {
	return s.GetAnchorsWithDistancesInWindow(ctx, since, time.Time{})
}

// GetAnchorsWithDistancesInWindow retrieves anchors within a time window that have computed distances
// If windowEnd is zero, all anchors since windowStart are returned (equivalent to GetAnchorsWithDistances).
func (s *AnchorStorage) GetAnchorsWithDistancesInWindow(
	ctx context.Context,
	windowStart, windowEnd time.Time,
) ([]*types.SemanticAnchor, error) {
	queryBase := `
		SELECT DISTINCT a.id, a.timestamp, a.location, a.semantic_embedding,
		       a.context, a.signals, a.duration_minutes, a.duration_source,
		       a.duration_confidence, a.preceding_anchor_id, a.following_anchor_id,
		       a.pattern_id, a.created_at
		FROM semantic_anchors a
		WHERE a.timestamp >= $1`

	var query string
	var args []interface{}
	args = append(args, windowStart)

	if !windowEnd.IsZero() {
		query = queryBase + `
		  AND a.timestamp < $2
		  AND EXISTS (
			SELECT 1 FROM anchor_distances d
			WHERE d.anchor1_id = a.id OR d.anchor2_id = a.id
		  )
		ORDER BY a.timestamp ASC`
		args = append(args, windowEnd)
	} else {
		query = queryBase + `
		  AND EXISTS (
			SELECT 1 FROM anchor_distances d
			WHERE d.anchor1_id = a.id OR d.anchor2_id = a.id
		  )
		ORDER BY a.timestamp ASC`
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query anchors with distances: %w", err)
	}
	defer rows.Close()

	var anchors []*types.SemanticAnchor

	for rows.Next() {
		var anchor types.SemanticAnchor
		var contextJSON, signalsJSON []byte

		err := rows.Scan(
			&anchor.ID,
			&anchor.Timestamp,
			&anchor.Location,
			&anchor.SemanticEmbedding,
			&contextJSON,
			&signalsJSON,
			&anchor.DurationMinutes,
			&anchor.DurationSource,
			&anchor.DurationConfidence,
			&anchor.PrecedingAnchorID,
			&anchor.FollowingAnchorID,
			&anchor.PatternID,
			&anchor.CreatedAt,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan anchor row: %w", err)
		}

		// Unmarshal JSONB fields
		if err := json.Unmarshal(contextJSON, &anchor.Context); err != nil {
			return nil, fmt.Errorf("failed to unmarshal context: %w", err)
		}

		if err := json.Unmarshal(signalsJSON, &anchor.Signals); err != nil {
			return nil, fmt.Errorf("failed to unmarshal signals: %w", err)
		}

		anchors = append(anchors, &anchor)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating anchor rows: %w", err)
	}

	return anchors, nil
}

// GetAnchorsByIDs retrieves multiple anchors by their IDs
func (s *AnchorStorage) GetAnchorsByIDs(ctx context.Context, ids []uuid.UUID) ([]*types.SemanticAnchor, error) {
	if len(ids) == 0 {
		return []*types.SemanticAnchor{}, nil
	}

	// Convert UUIDs to strings for the query
	idStrings := make([]string, len(ids))
	for i, id := range ids {
		idStrings[i] = id.String()
	}

	query := `
		SELECT id, timestamp, location, semantic_embedding,
		       context, signals, duration_minutes, duration_source,
		       duration_confidence, preceding_anchor_id, following_anchor_id,
		       pattern_id, created_at
		FROM semantic_anchors
		WHERE id::text = ANY($1)
		ORDER BY timestamp ASC
	`

	rows, err := s.db.QueryContext(ctx, query, pq.Array(idStrings))
	if err != nil {
		return nil, fmt.Errorf("failed to query anchors by IDs: %w", err)
	}
	defer rows.Close()

	var anchors []*types.SemanticAnchor

	for rows.Next() {
		var anchor types.SemanticAnchor
		var contextJSON, signalsJSON []byte

		err := rows.Scan(
			&anchor.ID,
			&anchor.Timestamp,
			&anchor.Location,
			&anchor.SemanticEmbedding,
			&contextJSON,
			&signalsJSON,
			&anchor.DurationMinutes,
			&anchor.DurationSource,
			&anchor.DurationConfidence,
			&anchor.PrecedingAnchorID,
			&anchor.FollowingAnchorID,
			&anchor.PatternID,
			&anchor.CreatedAt,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan anchor row: %w", err)
		}

		// Unmarshal JSONB fields
		if err := json.Unmarshal(contextJSON, &anchor.Context); err != nil {
			return nil, fmt.Errorf("failed to unmarshal context: %w", err)
		}

		if err := json.Unmarshal(signalsJSON, &anchor.Signals); err != nil {
			return nil, fmt.Errorf("failed to unmarshal signals: %w", err)
		}

		anchors = append(anchors, &anchor)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating anchor rows: %w", err)
	}

	return anchors, nil
}

// UpdatePatternWeight increments a pattern's weight by delta
func (s *AnchorStorage) UpdatePatternWeight(ctx context.Context, patternID uuid.UUID, weightDelta float64) error {
	query := `
		UPDATE behavioral_patterns
		SET weight = weight + $2,
			updated_at = $3
		WHERE id = $1
	`

	_, err := s.db.ExecContext(ctx, query, patternID, weightDelta, time.Now())
	if err != nil {
		return fmt.Errorf("failed to update pattern weight: %w", err)
	}

	return nil
}

// UpdatePatternObserved increments pattern observation count
func (s *AnchorStorage) UpdatePatternObserved(ctx context.Context, patternID uuid.UUID) error {
	query := `
		UPDATE behavioral_patterns
		SET observations = observations + 1,
			last_seen = $2,
			updated_at = $2
		WHERE id = $1
	`

	now := time.Now()
	_, err := s.db.ExecContext(ctx, query, patternID, now)
	if err != nil {
		return fmt.Errorf("failed to update pattern observed: %w", err)
	}

	return nil
}

// UpdatePatternPrediction updates pattern prediction statistics
func (s *AnchorStorage) UpdatePatternPrediction(ctx context.Context, patternID uuid.UUID, accepted bool) error {
	query := `
		UPDATE behavioral_patterns
		SET predictions = predictions + 1,
			acceptances = CASE WHEN $2 THEN acceptances + 1 ELSE acceptances END,
			rejections = CASE WHEN $2 THEN rejections ELSE rejections + 1 END,
			last_useful = CASE WHEN $2 THEN $3 ELSE last_useful END,
			updated_at = $3
		WHERE id = $1
	`

	now := time.Now()
	_, err := s.db.ExecContext(ctx, query, patternID, accepted, now)
	if err != nil {
		return fmt.Errorf("failed to update pattern prediction: %w", err)
	}

	return nil
}
