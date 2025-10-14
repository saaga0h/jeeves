package behavior

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/saaga0h/jeeves-platform/pkg/config"
	"github.com/saaga0h/jeeves-platform/pkg/mqtt"
	"github.com/saaga0h/jeeves-platform/pkg/ontology"
	"github.com/saaga0h/jeeves-platform/pkg/redis"
)

type Agent struct {
	mqtt   mqtt.Client
	redis  redis.Client
	db     *sql.DB
	cfg    *config.Config
	logger *slog.Logger

	activeEpisodes     map[string]string // location → episode ID
	lastOccupancyState map[string]string // location → "occupied" | "empty"
	stateMux           sync.RWMutex
}

func NewAgent(mqttClient mqtt.Client, redisClient redis.Client, cfg *config.Config, logger *slog.Logger) (*Agent, error) {
	// Connect to Postgres using config
	connStr := cfg.PostgresConnectionString()

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	return &Agent{
		mqtt:               mqttClient,
		redis:              redisClient,
		db:                 db,
		cfg:                cfg,
		logger:             logger,
		activeEpisodes:     make(map[string]string),
		lastOccupancyState: make(map[string]string),
	}, nil
}

func (a *Agent) Start(ctx context.Context) error {
	a.logger.Info("Starting behavior agent")

	// Connect to MQTT
	if err := a.mqtt.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to MQTT: %w", err)
	}

	// Subscribe to context topics
	topics := []string{
		"automation/context/occupancy/+",
		"automation/context/lighting/+",
		"automation/media/+/+", // New: media events
	}

	for _, topic := range topics {
		if err := a.mqtt.Subscribe(topic, 0, a.handleMessage); err != nil {
			return fmt.Errorf("failed to subscribe to %s: %w", topic, err)
		}
	}

	a.logger.Info("Subscribed to topics", "topics", topics)

	// Block until context cancelled
	<-ctx.Done()
	return nil
}

func (a *Agent) Stop() error {
	a.logger.Info("Stopping behavior agent")
	a.mqtt.Disconnect()
	a.db.Close()
	return nil
}

func (a *Agent) handleMessage(msg mqtt.Message) {
	topic := msg.Topic()

	if strings.Contains(topic, "/occupancy/") {
		a.handleOccupancyMessage(msg)
	} else if strings.Contains(topic, "/lighting/") {
		a.handleLightingMessage(msg)
	} else if strings.Contains(topic, "/media/") {
		a.handleMediaMessage(msg)
	}
}

func (a *Agent) handleOccupancyMessage(msg mqtt.Message) {
	var data struct {
		Location   string  `json:"location"`
		State      string  `json:"state"`
		Confidence float64 `json:"confidence"`
	}

	if err := json.Unmarshal(msg.Payload(), &data); err != nil {
		a.logger.Error("Failed to parse occupancy", "error", err)
		return
	}

	a.stateMux.Lock()
	defer a.stateMux.Unlock()

	previousState := a.lastOccupancyState[data.Location]
	currentState := data.State

	a.lastOccupancyState[data.Location] = currentState

	// Detect transitions
	if previousState != "occupied" && currentState == "occupied" {
		a.startEpisode(data.Location)
	}

	if previousState == "occupied" && currentState == "empty" {
		a.endEpisode(data.Location, "occupancy_empty")
	}
}

func (a *Agent) startEpisode(location string) {
	// Create minimal episode
	episode := ontology.NewEpisode(
		ontology.Activity{
			Type: "adl:Present",
			Name: "Present",
		},
		ontology.Location{
			Type: "saref:Room",
			ID:   fmt.Sprintf("urn:room:%s", location),
			Name: location,
		},
	)

	// Store in Postgres
	jsonld, _ := json.Marshal(episode)

	var id string
	err := a.db.QueryRow(
		"INSERT INTO behavioral_episodes (jsonld) VALUES ($1) RETURNING id",
		jsonld,
	).Scan(&id)

	if err != nil {
		a.logger.Error("Failed to create episode", "error", err)
		return
	}

	a.activeEpisodes[location] = id
	a.logger.Info("Episode started", "location", location, "id", id)

	// Publish event
	a.publishEpisodeEvent("started", map[string]interface{}{
		"location":     location,
		"trigger_type": "occupancy_transition",
	})
}

func (a *Agent) endEpisode(location string, reason string) {
	id, exists := a.activeEpisodes[location]
	if !exists {
		return
	}

	now := time.Now()
	_, err := a.db.Exec(
		"UPDATE behavioral_episodes SET jsonld = jsonb_set(jsonld, '{jeeves:endedAt}', to_jsonb($1::text)) WHERE id = $2",
		now.Format(time.RFC3339),
		id,
	)

	if err != nil {
		a.logger.Error("Failed to end episode", "error", err)
		return
	}

	delete(a.activeEpisodes, location)
	a.logger.Info("Episode ended", "location", location, "id", id)

	// Publish event
	a.publishEpisodeEvent("closed", map[string]interface{}{
		"location":   location,
		"end_reason": reason,
	})
}

func (a *Agent) publishEpisodeEvent(eventType string, data map[string]interface{}) {
	topic := fmt.Sprintf("automation/behavior/episode/%s", eventType)
	payload, _ := json.Marshal(data)
	a.mqtt.Publish(topic, 0, false, payload)
}

// Stubs for other handlers (implement in later iterations)
func (a *Agent) handleLightingMessage(msg mqtt.Message) {
	// TODO: Track manual adjustments
}

func (a *Agent) handleMediaMessage(msg mqtt.Message) {
	// TODO: Detect media activity for inference
}
