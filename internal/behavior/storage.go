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
			(jsonld->>'jeeves:startedAt')::timestamptz as started_at,
			CASE 
				WHEN jsonld->>'jeeves:endedAt' IS NOT NULL 
				THEN (jsonld->>'jeeves:endedAt')::timestamptz 
				ELSE NULL 
			END as ended_at,
			location,
			COALESCE(jsonld->'jeeves:triggeredAdjustment', '[]'::jsonb) as manual_actions
		FROM behavioral_episodes
		WHERE (jsonld->>'jeeves:startedAt')::timestamptz >= $1
			AND jsonld->>'jeeves:endedAt' IS NOT NULL
			AND id NOT IN (
				SELECT UNNEST(micro_episode_ids)
				FROM macro_episodes
			)
	`

	args := []interface{}{sinceTime}

	if location != "" {
		query += " AND location = $2"
		args = append(args, location)
	}

	query += " ORDER BY (jsonld->>'jeeves:startedAt')::timestamptz ASC"

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
