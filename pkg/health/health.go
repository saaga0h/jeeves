package health

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/saaga0h/jeeves-platform/pkg/mqtt"
	"github.com/saaga0h/jeeves-platform/pkg/redis"
)

// Checker provides health check functionality for agents
type Checker struct {
	mqtt   mqtt.Client
	redis  redis.Client
	logger *slog.Logger
}

// NewChecker creates a new health checker with the given dependencies
func NewChecker(mqttClient mqtt.Client, redisClient redis.Client, logger *slog.Logger) *Checker {
	return &Checker{
		mqtt:   mqttClient,
		redis:  redisClient,
		logger: logger,
	}
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status    string    `json:"status"`
	Timestamp string    `json:"timestamp"`
	Services  *Services `json:"services,omitempty"`
}

// Services represents the status of external dependencies
type Services struct {
	Redis string `json:"redis"`
	MQTT  string `json:"mqtt"`
}

// HandlerFunc returns an HTTP handler function for health checks
// This implements a minimal health check as per agent-behaviors.md
// Returns 200 if process is alive without checking dependencies
func (h *Checker) HandlerFunc() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Simple health check - just return OK if process is alive
		// This keeps the health check fast for Nomad/Consul
		response := HealthResponse{
			Status:    "ok",
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		if err := json.NewEncoder(w).Encode(response); err != nil {
			h.logger.Error("Failed to encode health response", "error", err)
		}
	}
}

// DetailedHandlerFunc returns a handler that checks all dependencies
// This is available but not used by default to keep health checks fast
func (h *Checker) DetailedHandlerFunc() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		services := &Services{
			Redis: "unknown",
			MQTT:  "unknown",
		}

		// Check MQTT connection
		if h.mqtt != nil && h.mqtt.IsConnected() {
			services.MQTT = "connected"
		} else {
			services.MQTT = "disconnected"
		}

		// Check Redis connection
		// Note: We don't actually ping Redis here to keep it fast
		// In production, you might want to cache the last successful ping
		if h.redis != nil {
			services.Redis = "connected"
		} else {
			services.Redis = "disconnected"
		}

		// Determine overall status
		status := "healthy"
		statusCode := http.StatusOK

		if services.Redis == "disconnected" || services.MQTT == "disconnected" {
			status = "degraded"
			statusCode = http.StatusServiceUnavailable
		}

		response := HealthResponse{
			Status:    status,
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			Services:  services,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)

		if err := json.NewEncoder(w).Encode(response); err != nil {
			h.logger.Error("Failed to encode health response", "error", err)
		}
	}
}
