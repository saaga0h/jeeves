// cmd/observer/main.go
package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/saaga0h/jeeves-platform/pkg/config"
	"github.com/saaga0h/jeeves-platform/pkg/postgres"
)

//go:embed web/*
var webFiles embed.FS

type EpisodeData struct {
	ID              string                 `json:"id"`
	Type            string                 `json:"type"` // "macro" or "micro"
	PatternType     string                 `json:"pattern_type,omitempty"`
	StartTime       time.Time              `json:"start_time"`
	EndTime         time.Time              `json:"end_time"`
	DurationMinutes float64                `json:"duration_minutes"`
	Locations       []string               `json:"locations"`
	Summary         string                 `json:"summary,omitempty"`
	SemanticTags    []string               `json:"semantic_tags,omitempty"`
	Children        []EpisodeData          `json:"children,omitempty"` // Micro episodes if macro
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
}

func main() {
	cfg := config.NewConfig()
	cfg.ServiceName = "observer-agent"
	cfg.LoadFromEnv()
	cfg.LoadFromFlags()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	slog.SetDefault(logger)

	logger.Info("Starting Observer Agent",
		"postgres", fmt.Sprintf("%s:%d/%s", cfg.PostgresHost, cfg.PostgresPort, cfg.PostgresDB))

	pgClient := postgres.NewClient(cfg, logger)
	if err := pgClient.Connect(ctx); err != nil {
		logger.Error("Failed to connect to postgres", "error", err)
		os.Exit(1)
	}

	// Get local timezone (EEST or whatever system is set to)
	localTZ := time.Local

	// API endpoint
	http.HandleFunc("/api/episodes", func(w http.ResponseWriter, r *http.Request) {
		fromStr := r.URL.Query().Get("from") // ddmmyyyy
		toStr := r.URL.Query().Get("to")     // ddmmyyyy

		if fromStr == "" || toStr == "" {
			http.Error(w, "Missing from or to parameter (format: ddmmyyyy)", http.StatusBadRequest)
			return
		}

		// Parse dates
		from, err := parseDateToMidnight(fromStr, localTZ)
		if err != nil {
			http.Error(w, fmt.Sprintf("Invalid from date: %v", err), http.StatusBadRequest)
			return
		}

		to, err := parseDateToMidnight(toStr, localTZ)
		if err != nil {
			http.Error(w, fmt.Sprintf("Invalid to date: %v", err), http.StatusBadRequest)
			return
		}

		// Add 24 hours to 'to' to include the entire end day
		toEndOfDay := to.Add(24 * time.Hour)

		episodes, err := getEpisodesWithChildren(pgClient, from, toEndOfDay)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(episodes)
	})

	// Serve static files
	http.Handle("/", http.FileServer(http.FS(webFiles)))

	http.ListenAndServe(":8080", nil)
}

// parseDateToMidnight parses ddmmyyyy and returns midnight in local timezone
func parseDateToMidnight(dateStr string, tz *time.Location) (time.Time, error) {
	if len(dateStr) != 8 {
		return time.Time{}, fmt.Errorf("date must be 8 characters (ddmmyyyy), got %d", len(dateStr))
	}

	day := dateStr[0:2]
	month := dateStr[2:4]
	year := dateStr[4:8]

	// Parse as "02-01-2006" in local timezone
	t, err := time.ParseInLocation("02-01-2006", fmt.Sprintf("%s-%s-%s", day, month, year), tz)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date format: %w", err)
	}

	return t, nil
}

func getEpisodesWithChildren(pg postgres.Client, from, to time.Time) ([]EpisodeData, error) {
	query := `
        WITH macro_eps AS (
            SELECT 
                id,
                'macro' as type,
                pattern_type,
                start_time,
                end_time,
                duration_minutes,
                locations,
                summary,
                semantic_tags,
                micro_episode_ids,
                context_features
            FROM macro_episodes
            WHERE start_time >= $1
              AND start_time < $2
            ORDER BY start_time
        ),
        micro_eps AS (
            SELECT
                id,
                'micro' as type,
                COALESCE(jsonld->>'jeeves:triggerType', 'occupancy_transition') as pattern_type,
                started_at_text::timestamptz as start_time,
                COALESCE(ended_at_text::timestamptz, NOW()) as end_time,
                COALESCE(
                    EXTRACT(EPOCH FROM (ended_at_text::timestamptz - started_at_text::timestamptz))/60,
                    EXTRACT(EPOCH FROM (NOW() - started_at_text::timestamptz))/60
                ) as duration_minutes,
                ARRAY[location] as locations,
                '' as summary,
                ARRAY[]::text[] as semantic_tags,
                jsonld as metadata
            FROM behavioral_episodes
            WHERE started_at_text::timestamptz >= $1
              AND started_at_text::timestamptz < $2
        )
        -- Return macros with their children
        SELECT 
            m.id::text,
            m.type,
            m.pattern_type,
            m.start_time,
            m.end_time,
            m.duration_minutes,
            array_to_json(m.locations)::text as locations_json,
            m.summary,
            array_to_json(m.semantic_tags)::text as tags_json,
            array_to_json(m.micro_episode_ids)::text as micro_ids_json,
            m.context_features::text as context_json,
            COALESCE(
                json_agg(
                    json_build_object(
                        'id', me.id::text,
                        'type', me.type,
                        'pattern_type', me.pattern_type,
                        'start_time', me.start_time,
                        'end_time', me.end_time,
                        'duration_minutes', me.duration_minutes,
                        'locations', array_to_json(me.locations),
                        'metadata', me.metadata
                    ) ORDER BY me.start_time
                ) FILTER (WHERE me.id IS NOT NULL),
                '[]'
            )::text as children
        FROM macro_eps m
        LEFT JOIN micro_eps me ON me.id = ANY(m.micro_episode_ids)
        GROUP BY m.id, m.type, m.pattern_type, m.start_time, m.end_time, 
                 m.duration_minutes, m.locations, m.summary, m.semantic_tags,
                 m.micro_episode_ids, m.context_features
        
        UNION ALL
        
        -- Return standalone micro episodes (not in any macro)
        SELECT
            me.id::text,
            me.type,
            me.pattern_type,
            me.start_time,
            me.end_time,
            me.duration_minutes,
            array_to_json(me.locations)::text as locations_json,
            me.summary,
            '[]'::text as tags_json,
            '[]'::text as micro_ids_json,
            me.metadata::text as context_json,
            '[]'::text as children
        FROM micro_eps me
        WHERE NOT EXISTS (
            SELECT 1 FROM macro_episodes m
            WHERE me.id = ANY(m.micro_episode_ids)
        )
        
        ORDER BY start_time
    `

	rows, err := pg.Query(context.Background(), query, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var episodes []EpisodeData
	for rows.Next() {
		var ep EpisodeData
		var locationsJSON, tagsJSON, microIDsJSON, contextJSON, childrenJSON string

		err := rows.Scan(
			&ep.ID,
			&ep.Type,
			&ep.PatternType,
			&ep.StartTime,
			&ep.EndTime,
			&ep.DurationMinutes,
			&locationsJSON,
			&ep.Summary,
			&tagsJSON,
			&microIDsJSON,
			&contextJSON,
			&childrenJSON,
		)
		if err != nil {
			return nil, err
		}

		// Parse JSON strings back to arrays/objects
		if locationsJSON != "" && locationsJSON != "null" {
			json.Unmarshal([]byte(locationsJSON), &ep.Locations)
		}

		if tagsJSON != "" && tagsJSON != "null" {
			json.Unmarshal([]byte(tagsJSON), &ep.SemanticTags)
		}

		if contextJSON != "" && contextJSON != "null" {
			json.Unmarshal([]byte(contextJSON), &ep.Metadata)
		}

		// Parse children
		if childrenJSON != "" && childrenJSON != "[]" {
			json.Unmarshal([]byte(childrenJSON), &ep.Children)
		}

		episodes = append(episodes, ep)
	}

	return episodes, nil
}
