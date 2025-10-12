package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/pflag"
)

// Config holds the configuration for a J.E.E.V.E.S. agent
type Config struct {
	// MQTT configuration
	MQTTBroker   string
	MQTTPort     int
	MQTTUser     string
	MQTTPassword string
	MQTTClientID string

	// Redis configuration
	RedisHost     string
	RedisPort     int
	RedisPassword string
	RedisDB       int

	// Service configuration
	ServiceName string
	HealthPort  int
	LogLevel    string

	// Agent-specific configuration (can be extended by agents)
	SensorTopics      []string
	MaxSensorHistory  int
	EnableVictoriaMetrics bool
	VictoriaMetricsURL    string

	// Illuminance agent configuration
	Latitude            float64
	Longitude           float64
	AnalysisIntervalSec int
	MaxDataAgeHours     float64
	MinReadingsRequired int

	// Light agent configuration
	DecisionIntervalSec   int
	ManualOverrideMinutes int
	MinDecisionIntervalMs int
	APIPort               int

	// Occupancy agent configuration
	OccupancyAnalysisIntervalSec int
	LLMEndpoint                  string
	LLMModel                     string
	MaxEventHistory              int
}

// NewConfig creates a new Config with default values
func NewConfig() *Config {
	return &Config{
		MQTTBroker:   "localhost",
		MQTTPort:     1883,
		MQTTUser:     "",
		MQTTPassword: "",
		MQTTClientID: "",
		RedisHost:    "localhost",
		RedisPort:    6379,
		RedisPassword: "",
		RedisDB:      0,
		ServiceName:  "jeeves-agent",
		HealthPort:   8080,
		LogLevel:     "info",
		SensorTopics: []string{"automation/raw/+/+"},
		MaxSensorHistory: 1000,
		EnableVictoriaMetrics: false,
		VictoriaMetricsURL: "",
		// Illuminance agent defaults (Helsinki coordinates)
		Latitude:            60.1695,
		Longitude:           24.9354,
		AnalysisIntervalSec: 30,
		MaxDataAgeHours:     1.0,
		MinReadingsRequired: 3,
		// Light agent defaults
		DecisionIntervalSec:   30,
		ManualOverrideMinutes: 30,
		MinDecisionIntervalMs: 10000,
		APIPort:               3002,
		// Occupancy agent defaults
		OccupancyAnalysisIntervalSec: 30,
		LLMEndpoint:                  "http://localhost:11434/api/generate",
		LLMModel:                     "llama3.2:3b",
		MaxEventHistory:              100,
	}
}

// LoadFromEnv loads configuration from environment variables with JEEVES_ prefix
func (c *Config) LoadFromEnv() {
	// MQTT configuration
	if v := os.Getenv("JEEVES_MQTT_BROKER"); v != "" {
		c.MQTTBroker = v
	}
	if v := os.Getenv("JEEVES_MQTT_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			c.MQTTPort = port
		}
	}
	if v := os.Getenv("JEEVES_MQTT_USER"); v != "" {
		c.MQTTUser = v
	}
	if v := os.Getenv("JEEVES_MQTT_PASSWORD"); v != "" {
		c.MQTTPassword = v
	}
	if v := os.Getenv("JEEVES_MQTT_CLIENT_ID"); v != "" {
		c.MQTTClientID = v
	}

	// Redis configuration
	if v := os.Getenv("JEEVES_REDIS_HOST"); v != "" {
		c.RedisHost = v
	}
	if v := os.Getenv("JEEVES_REDIS_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			c.RedisPort = port
		}
	}
	if v := os.Getenv("JEEVES_REDIS_PASSWORD"); v != "" {
		c.RedisPassword = v
	}
	if v := os.Getenv("JEEVES_REDIS_DB"); v != "" {
		if db, err := strconv.Atoi(v); err == nil {
			c.RedisDB = db
		}
	}

	// Service configuration
	if v := os.Getenv("JEEVES_SERVICE_NAME"); v != "" {
		c.ServiceName = v
	}
	if v := os.Getenv("JEEVES_HEALTH_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			c.HealthPort = port
		}
	}
	if v := os.Getenv("JEEVES_LOG_LEVEL"); v != "" {
		c.LogLevel = v
	}

	// Agent-specific configuration
	if v := os.Getenv("JEEVES_MAX_SENSOR_HISTORY"); v != "" {
		if max, err := strconv.Atoi(v); err == nil {
			c.MaxSensorHistory = max
		}
	}
	if v := os.Getenv("JEEVES_ENABLE_VICTORIA_METRICS"); v != "" {
		if enable, err := strconv.ParseBool(v); err == nil {
			c.EnableVictoriaMetrics = enable
		}
	}
	if v := os.Getenv("JEEVES_VICTORIA_METRICS_URL"); v != "" {
		c.VictoriaMetricsURL = v
	}

	// Illuminance agent configuration
	if v := os.Getenv("JEEVES_LATITUDE"); v != "" {
		if lat, err := strconv.ParseFloat(v, 64); err == nil {
			c.Latitude = lat
		}
	}
	if v := os.Getenv("JEEVES_LONGITUDE"); v != "" {
		if lon, err := strconv.ParseFloat(v, 64); err == nil {
			c.Longitude = lon
		}
	}
	if v := os.Getenv("JEEVES_ANALYSIS_INTERVAL_SEC"); v != "" {
		if interval, err := strconv.Atoi(v); err == nil {
			c.AnalysisIntervalSec = interval
		}
	}
	if v := os.Getenv("JEEVES_MAX_DATA_AGE_HOURS"); v != "" {
		if hours, err := strconv.ParseFloat(v, 64); err == nil {
			c.MaxDataAgeHours = hours
		}
	}
	if v := os.Getenv("JEEVES_MIN_READINGS_REQUIRED"); v != "" {
		if minReadings, err := strconv.Atoi(v); err == nil {
			c.MinReadingsRequired = minReadings
		}
	}

	// Light agent configuration
	if v := os.Getenv("JEEVES_DECISION_INTERVAL_SEC"); v != "" {
		if interval, err := strconv.Atoi(v); err == nil {
			c.DecisionIntervalSec = interval
		}
	}
	if v := os.Getenv("JEEVES_MANUAL_OVERRIDE_MINUTES"); v != "" {
		if minutes, err := strconv.Atoi(v); err == nil {
			c.ManualOverrideMinutes = minutes
		}
	}
	if v := os.Getenv("JEEVES_MIN_DECISION_INTERVAL_MS"); v != "" {
		if ms, err := strconv.Atoi(v); err == nil {
			c.MinDecisionIntervalMs = ms
		}
	}
	if v := os.Getenv("JEEVES_API_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			c.APIPort = port
		}
	}

	// Occupancy agent configuration
	if v := os.Getenv("JEEVES_OCCUPANCY_ANALYSIS_INTERVAL_SEC"); v != "" {
		if interval, err := strconv.Atoi(v); err == nil {
			c.OccupancyAnalysisIntervalSec = interval
		}
	}
	if v := os.Getenv("JEEVES_LLM_ENDPOINT"); v != "" {
		c.LLMEndpoint = v
	}
	if v := os.Getenv("JEEVES_LLM_MODEL"); v != "" {
		c.LLMModel = v
	}
	if v := os.Getenv("JEEVES_MAX_EVENT_HISTORY"); v != "" {
		if max, err := strconv.Atoi(v); err == nil {
			c.MaxEventHistory = max
		}
	}
}

// LoadFromFlags parses command-line flags and overrides config values
func (c *Config) LoadFromFlags() {
	// MQTT flags
	pflag.StringVar(&c.MQTTBroker, "mqtt-broker", c.MQTTBroker, "MQTT broker hostname")
	pflag.IntVar(&c.MQTTPort, "mqtt-port", c.MQTTPort, "MQTT broker port")
	pflag.StringVar(&c.MQTTUser, "mqtt-user", c.MQTTUser, "MQTT username")
	pflag.StringVar(&c.MQTTPassword, "mqtt-password", c.MQTTPassword, "MQTT password")
	pflag.StringVar(&c.MQTTClientID, "mqtt-client-id", c.MQTTClientID, "MQTT client ID")

	// Redis flags
	pflag.StringVar(&c.RedisHost, "redis-host", c.RedisHost, "Redis hostname")
	pflag.IntVar(&c.RedisPort, "redis-port", c.RedisPort, "Redis port")
	pflag.StringVar(&c.RedisPassword, "redis-password", c.RedisPassword, "Redis password")
	pflag.IntVar(&c.RedisDB, "redis-db", c.RedisDB, "Redis database number")

	// Service flags
	pflag.StringVar(&c.ServiceName, "service-name", c.ServiceName, "Service name")
	pflag.IntVar(&c.HealthPort, "health-port", c.HealthPort, "Health check HTTP port")
	pflag.StringVar(&c.LogLevel, "log-level", c.LogLevel, "Log level (debug, info, warn, error)")

	// Agent-specific flags
	pflag.IntVar(&c.MaxSensorHistory, "max-sensor-history", c.MaxSensorHistory, "Maximum sensor history entries")
	pflag.BoolVar(&c.EnableVictoriaMetrics, "enable-victoria-metrics", c.EnableVictoriaMetrics, "Enable VictoriaMetrics forwarding")
	pflag.StringVar(&c.VictoriaMetricsURL, "victoria-metrics-url", c.VictoriaMetricsURL, "VictoriaMetrics URL")

	// Illuminance agent flags
	pflag.Float64Var(&c.Latitude, "latitude", c.Latitude, "Geographic latitude for daylight calculation")
	pflag.Float64Var(&c.Longitude, "longitude", c.Longitude, "Geographic longitude for daylight calculation")
	pflag.IntVar(&c.AnalysisIntervalSec, "analysis-interval", c.AnalysisIntervalSec, "Analysis interval in seconds")
	pflag.Float64Var(&c.MaxDataAgeHours, "max-data-age-hours", c.MaxDataAgeHours, "Maximum age of data to consider (hours)")
	pflag.IntVar(&c.MinReadingsRequired, "min-readings-required", c.MinReadingsRequired, "Minimum readings required for sufficient data")

	// Light agent flags
	pflag.IntVar(&c.DecisionIntervalSec, "decision-interval", c.DecisionIntervalSec, "Decision loop interval in seconds")
	pflag.IntVar(&c.ManualOverrideMinutes, "manual-override-minutes", c.ManualOverrideMinutes, "Manual override duration in minutes")
	pflag.IntVar(&c.MinDecisionIntervalMs, "min-decision-interval-ms", c.MinDecisionIntervalMs, "Minimum time between decisions per location (ms)")
	pflag.IntVar(&c.APIPort, "api-port", c.APIPort, "HTTP API port")

	// Occupancy agent flags
	pflag.IntVar(&c.OccupancyAnalysisIntervalSec, "occupancy-analysis-interval", c.OccupancyAnalysisIntervalSec, "Occupancy analysis interval in seconds")
	pflag.StringVar(&c.LLMEndpoint, "llm-endpoint", c.LLMEndpoint, "LLM API endpoint URL")
	pflag.StringVar(&c.LLMModel, "llm-model", c.LLMModel, "LLM model name")
	pflag.IntVar(&c.MaxEventHistory, "max-event-history", c.MaxEventHistory, "Maximum motion event history to keep")

	pflag.Parse()
}

// Validate checks that required configuration values are set
func (c *Config) Validate() error {
	if c.MQTTBroker == "" {
		return fmt.Errorf("MQTT broker is required")
	}
	if c.MQTTPort <= 0 || c.MQTTPort > 65535 {
		return fmt.Errorf("MQTT port must be between 1 and 65535")
	}
	if c.RedisHost == "" {
		return fmt.Errorf("Redis host is required")
	}
	if c.RedisPort <= 0 || c.RedisPort > 65535 {
		return fmt.Errorf("Redis port must be between 1 and 65535")
	}
	if c.HealthPort <= 0 || c.HealthPort > 65535 {
		return fmt.Errorf("Health port must be between 1 and 65535")
	}
	if c.ServiceName == "" {
		return fmt.Errorf("Service name is required")
	}

	// Validate log level
	validLogLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}
	if !validLogLevels[c.LogLevel] {
		return fmt.Errorf("invalid log level: %s (must be debug, info, warn, or error)", c.LogLevel)
	}

	return nil
}

// MQTTAddress returns the full MQTT broker address
func (c *Config) MQTTAddress() string {
	return fmt.Sprintf("tcp://%s:%d", c.MQTTBroker, c.MQTTPort)
}

// RedisAddress returns the full Redis address
func (c *Config) RedisAddress() string {
	return fmt.Sprintf("%s:%d", c.RedisHost, c.RedisPort)
}
