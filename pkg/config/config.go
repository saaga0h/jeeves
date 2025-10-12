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
