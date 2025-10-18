package behavior

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// MicroEpisode represents a micro-episode from database
type MicroEpisode struct {
	ID            uuid.UUID
	TriggerType   string
	StartedAt     time.Time
	EndedAt       *time.Time
	Location      string
	ManualActions []map[string]interface{}
}

// MacroEpisode represents a macro-episode
type MacroEpisode struct {
	ID              uuid.UUID
	PatternType     string
	StartTime       time.Time
	EndTime         time.Time
	DurationMinutes int
	Locations       []string
	MicroEpisodeIDs []uuid.UUID
	Summary         string
	SemanticTags    []string
	ContextFeatures map[string]interface{}
	CreatedAt       time.Time
}

// getUnconsolidatedEpisodes retrieves episodes that haven't been consolidated
func (a *Agent) getUnconsolidatedEpisodes(ctx context.Context, sinceTime time.Time, location string) ([]*MicroEpisode, error) {
	query := `
    SELECT 
        id,
        COALESCE(jsonld->>'jeeves:triggerType', 'occupancy_transition') as trigger_type,
        started_at_text::timestamptz as started_at,
        ended_at_text::timestamptz as ended_at,
        location,
        COALESCE(jsonld->'jeeves:triggeredAdjustment', '[]'::jsonb) as manual_actions
    FROM behavioral_episodes
    WHERE started_at_text::timestamptz >= $1
        AND ended_at_text IS NOT NULL
        AND NOT EXISTS (
            SELECT 1 
            FROM macro_episodes m
            WHERE behavioral_episodes.id = ANY(m.micro_episode_ids)
        )
`

	args := []interface{}{sinceTime}

	if location != "" && location != "universe" {
		query += " AND location = $2"
		args = append(args, location)
	}

	query += " ORDER BY started_at_text ASC"

	rows, err := a.pgClient.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	var episodes []*MicroEpisode

	for rows.Next() {
		var ep MicroEpisode
		var endedAt *time.Time
		var manualActionsJSON []byte

		err := rows.Scan(
			&ep.ID,
			&ep.TriggerType,
			&ep.StartedAt,
			&endedAt,
			&ep.Location,
			&manualActionsJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}

		ep.EndedAt = endedAt

		// Parse manual actions
		if len(manualActionsJSON) > 0 {
			if err := json.Unmarshal(manualActionsJSON, &ep.ManualActions); err != nil {
				a.logger.Warn("Failed to parse manual actions", "error", err)
				ep.ManualActions = []map[string]interface{}{}
			}
		} else {
			ep.ManualActions = []map[string]interface{}{}
		}

		episodes = append(episodes, &ep)
	}

	return episodes, nil
}

// createMacroEpisode stores a macro-episode in the database
func (a *Agent) createMacroEpisode(ctx context.Context, macro *MacroEpisode) error {
	query := `
		INSERT INTO macro_episodes (
			id, pattern_type, start_time, end_time, duration_minutes,
			locations, micro_episode_ids, summary, semantic_tags,
			context_features, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`

	contextFeaturesJSON, err := json.Marshal(macro.ContextFeatures)
	if err != nil {
		return fmt.Errorf("failed to marshal context features: %w", err)
	}

	_, err = a.pgClient.Exec(ctx, query,
		macro.ID,
		macro.PatternType,
		macro.StartTime,
		macro.EndTime,
		macro.DurationMinutes,
		pq.Array(macro.Locations),
		pq.Array(macro.MicroEpisodeIDs),
		macro.Summary,
		pq.Array(macro.SemanticTags),
		contextFeaturesJSON,
		macro.CreatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to insert macro-episode: %w", err)
	}

	a.logger.Info("Macro-episode created",
		"id", macro.ID,
		"pattern", macro.PatternType,
		"duration", macro.DurationMinutes,
		"micro_episodes", len(macro.MicroEpisodeIDs))

	return nil
}

// Add these functions to storage.go

// storeVector persists a behavioral vector to the database
func (a *Agent) storeVector(ctx context.Context, vector *BehavioralVector) error {
	// Marshal sequence to JSONB
	sequenceJSON, err := json.Marshal(vector.Sequence)
	if err != nil {
		return fmt.Errorf("failed to marshal sequence: %w", err)
	}

	// Marshal context to JSONB
	contextJSON, err := json.Marshal(vector.Context)
	if err != nil {
		return fmt.Errorf("failed to marshal context: %w", err)
	}

	// Marshal edge stats to JSONB
	edgeStatsJSON, err := json.Marshal(vector.EdgeStats)
	if err != nil {
		return fmt.Errorf("failed to marshal edge stats: %w", err)
	}

	// Convert UUID slice to PostgreSQL array
	episodeIDs := make([]string, len(vector.MicroEpisodeIDs))
	for i, id := range vector.MicroEpisodeIDs {
		episodeIDs[i] = id.String()
	}

	query := `
		INSERT INTO behavioral_vectors (
			id, timestamp, sequence, context, edge_stats,
			micro_episode_ids, scenario_name, quality_score, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	_, err = a.pgClient.Exec(ctx, query,
		vector.ID.String(),
		vector.Timestamp,
		sequenceJSON,
		contextJSON,
		edgeStatsJSON,
		pq.Array(episodeIDs),
		vector.ScenarioName,
		vector.QualityScore,
		time.Now(),
	)

	if err != nil {
		a.logger.Error("Failed to insert behavioral vector into database",
			"vector_id", vector.ID,
			"error", err,
			"episode_ids", episodeIDs)
		return fmt.Errorf("failed to insert vector: %w", err)
	}

	a.logger.Info("Vector stored in database",
		"vector_id", vector.ID,
		"locations", len(vector.Sequence),
		"quality_score", vector.QualityScore)

	return nil
}

// getRecentVectors retrieves vectors from a time window
func (a *Agent) getRecentVectors(ctx context.Context, since time.Time, limit int) ([]*BehavioralVector, error) {
	query := `
		SELECT 
			id, timestamp, sequence, context, edge_stats,
			micro_episode_ids, scenario_name, quality_score, created_at
		FROM behavioral_vectors
		WHERE timestamp >= $1
		ORDER BY timestamp DESC
		LIMIT $2
	`

	rows, err := a.pgClient.Query(ctx, query, since, limit)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	var vectors []*BehavioralVector

	for rows.Next() {
		var v BehavioralVector
		var sequenceJSON, contextJSON, edgeStatsJSON []byte
		var episodeIDStrings []string
		var scenarioName *string

		err := rows.Scan(
			&v.ID,
			&v.Timestamp,
			&sequenceJSON,
			&contextJSON,
			&edgeStatsJSON,
			&episodeIDStrings,
			&scenarioName,
			&v.QualityScore,
			&v.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}

		// Unmarshal JSON fields
		if err := json.Unmarshal(sequenceJSON, &v.Sequence); err != nil {
			return nil, fmt.Errorf("failed to unmarshal sequence: %w", err)
		}

		if err := json.Unmarshal(contextJSON, &v.Context); err != nil {
			return nil, fmt.Errorf("failed to unmarshal context: %w", err)
		}

		if err := json.Unmarshal(edgeStatsJSON, &v.EdgeStats); err != nil {
			return nil, fmt.Errorf("failed to unmarshal edge stats: %w", err)
		}

		// Parse episode IDs
		v.MicroEpisodeIDs = make([]uuid.UUID, len(episodeIDStrings))
		for i, idStr := range episodeIDStrings {
			id, err := uuid.Parse(idStr)
			if err != nil {
				return nil, fmt.Errorf("failed to parse episode ID: %w", err)
			}
			v.MicroEpisodeIDs[i] = id
		}

		if scenarioName != nil {
			v.ScenarioName = *scenarioName
		}

		vectors = append(vectors, &v)
	}

	return vectors, nil
}

// getVectorsByPattern finds vectors matching a location sequence pattern
func (a *Agent) getVectorsByPattern(ctx context.Context, startLocation, secondLocation string, limit int) ([]*BehavioralVector, error) {
	// Query vectors where first two locations match the pattern
	query := `
		SELECT 
			id, timestamp, sequence, context, edge_stats,
			micro_episode_ids, scenario_name, quality_score, created_at
		FROM behavioral_vectors
		WHERE sequence->0->>'location' = $1
		  AND sequence->1->>'location' = $2
		ORDER BY timestamp DESC
		LIMIT $3
	`

	rows, err := a.pgClient.Query(ctx, query, startLocation, secondLocation, limit)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	var vectors []*BehavioralVector

	for rows.Next() {
		var v BehavioralVector
		var sequenceJSON, contextJSON, edgeStatsJSON []byte
		var episodeIDStrings []string
		var scenarioName *string

		err := rows.Scan(
			&v.ID,
			&v.Timestamp,
			&sequenceJSON,
			&contextJSON,
			&edgeStatsJSON,
			&episodeIDStrings,
			&scenarioName,
			&v.QualityScore,
			&v.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}

		// Unmarshal JSON fields
		if err := json.Unmarshal(sequenceJSON, &v.Sequence); err != nil {
			return nil, fmt.Errorf("failed to unmarshal sequence: %w", err)
		}

		if err := json.Unmarshal(contextJSON, &v.Context); err != nil {
			return nil, fmt.Errorf("failed to unmarshal context: %w", err)
		}

		if err := json.Unmarshal(edgeStatsJSON, &v.EdgeStats); err != nil {
			return nil, fmt.Errorf("failed to unmarshal edge stats: %w", err)
		}

		// Parse episode IDs
		v.MicroEpisodeIDs = make([]uuid.UUID, len(episodeIDStrings))
		for i, idStr := range episodeIDStrings {
			id, err := uuid.Parse(idStr)
			if err != nil {
				return nil, fmt.Errorf("failed to parse episode ID: %w", err)
			}
			v.MicroEpisodeIDs[i] = id
		}

		if scenarioName != nil {
			v.ScenarioName = *scenarioName
		}

		vectors = append(vectors, &v)
	}

	return vectors, nil
}
