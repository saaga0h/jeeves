package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

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

	// PostgreSQL configuration (for behavior agent)
	PostgresHost     string
	PostgresPort     int
	PostgresUser     string
	PostgresPassword string
	PostgresDB       string
	PostgresSSLMode  string

	// PostgreSQL connection pool settings
	PostgresMaxConnections     int
	PostgresMaxIdleConnections int
	PostgresConnMaxLifetime    time.Duration

	// Service configuration
	ServiceName string
	HealthPort  int
	LogLevel    string

	// Agent-specific configuration (can be extended by agents)
	SensorTopics          []string
	MaxSensorHistory      int
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
	LLMMinConfidence             float64
	MaxEventHistory              int

	// Consolidation settings
	ConsolidationIntervalHours int
	ConsolidationLookbackHours int
	ConsolidationMaxGapMinutes int

	// Pattern Discovery configuration
	PatternDiscoveryEnabled       bool
	PatternDistanceStrategy       string // "llm_first", "learned_first", "vector_first"
	PatternDiscoveryIntervalHours int
	PatternDiscoveryBatchSize     int
	PatternClusteringEpsilon      float64
	PatternClusteringMinPoints    int
	PatternMinAnchorsForDiscovery int
	PatternLookbackHours          int

	// Temporal Grouping configuration
	TemporalGroupingEnabled       bool
	TemporalGroupingWindowMinutes int     // Window size in minutes for temporal grouping
	TemporalGroupingOverlapRatio  float64 // Overlap threshold (0.0-1.0) for parallelism detection

	// Batch Processing configuration (sliding window)
	BatchProcessingEnabled  bool          // Enable sliding window batch processing
	BatchDuration           time.Duration // Duration of each batch window (e.g., 2 hours)
	BatchOverlap            time.Duration // Overlap duration between batches (e.g., 30 minutes)
	BatchScheduleEnabled    bool          // Enable automatic batch scheduling (vs manual MQTT trigger)
	BatchScheduleInterval   time.Duration // Interval between automatic batch runs
	BatchMetadataEnabled    bool          // Store batch metadata (batch_id, timestamps) for debugging
}

// NewConfig creates a new Config with default values
func NewConfig() *Config {
	return &Config{
		MQTTBroker:                 "localhost",
		MQTTPort:                   1883,
		MQTTUser:                   "",
		MQTTPassword:               "",
		MQTTClientID:               "",
		RedisHost:                  "localhost",
		RedisPort:                  6379,
		RedisPassword:              "",
		RedisDB:                    0,
		PostgresHost:               "localhost",
		PostgresPort:               5432,
		PostgresUser:               "postgres",
		PostgresPassword:           "",
		PostgresDB:                 "postgres",
		PostgresSSLMode:            "disable",
		PostgresMaxConnections:     10,
		PostgresMaxIdleConnections: 5,
		PostgresConnMaxLifetime:    5 * time.Minute,
		ServiceName:                "jeeves-agent",
		HealthPort:                 8080,
		LogLevel:                   "info",
		SensorTopics:               []string{"automation/raw/+/+"},
		MaxSensorHistory:           1000,
		EnableVictoriaMetrics:      false,
		VictoriaMetricsURL:         "",
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
		LLMEndpoint:                  "http://localhost:11434",
		LLMModel:                     "mixtral:8x7b",
		LLMMinConfidence:             0.7,
		MaxEventHistory:              100,
		// Consolidation defaults
		ConsolidationIntervalHours: 24,
		ConsolidationLookbackHours: 48,
		ConsolidationMaxGapMinutes: 120,
		// Pattern Discovery defaults
		PatternDiscoveryEnabled:       false,
		PatternDistanceStrategy:       "vector_first",
		PatternDiscoveryIntervalHours: 6,
		PatternDiscoveryBatchSize:     100,
		PatternClusteringEpsilon:      0.3,
		PatternClusteringMinPoints:    3,
		PatternMinAnchorsForDiscovery: 10,
		PatternLookbackHours:          168, // 7 days
		// Temporal Grouping defaults
		TemporalGroupingEnabled:       true,
		TemporalGroupingWindowMinutes: 60,  // 60 minute window (better for longer activities)
		TemporalGroupingOverlapRatio:  0.5, // 50% overlap = parallel
		// Batch Processing defaults
		BatchProcessingEnabled:  false,          // Disabled by default, use traditional approach
		BatchDuration:           2 * time.Hour,  // 2 hour batch windows
		BatchOverlap:            30 * time.Minute, // 30 minute overlap
		BatchScheduleEnabled:    false,          // Manual MQTT trigger by default
		BatchScheduleInterval:   2 * time.Hour,  // Run every 2 hours if enabled
		BatchMetadataEnabled:    true,           // Store metadata for debugging
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

	// PostgreSQL configuration
	if v := os.Getenv("JEEVES_POSTGRES_HOST"); v != "" {
		c.PostgresHost = v
	}
	if v := os.Getenv("JEEVES_POSTGRES_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			c.PostgresPort = port
		}
	}
	if v := os.Getenv("JEEVES_POSTGRES_USER"); v != "" {
		c.PostgresUser = v
	}
	if v := os.Getenv("JEEVES_POSTGRES_PASSWORD"); v != "" {
		c.PostgresPassword = v
	}
	if v := os.Getenv("JEEVES_POSTGRES_DB"); v != "" {
		c.PostgresDB = v
	}
	if v := os.Getenv("JEEVES_POSTGRES_SSLMODE"); v != "" {
		c.PostgresSSLMode = v
	}
	if v := os.Getenv("JEEVES_POSTGRES_MAX_OPEN_CONNS"); v != "" {
		if maxConns, err := strconv.Atoi(v); err == nil {
			c.PostgresMaxConnections = maxConns
		}
	}
	if v := os.Getenv("JEEVES_POSTGRES_MAX_IDLE_CONNS"); v != "" {
		if maxIdle, err := strconv.Atoi(v); err == nil {
			c.PostgresMaxIdleConnections = maxIdle
		}
	}
	if v := os.Getenv("JEEVES_POSTGRES_CONN_MAX_LIFE"); v != "" {
		if duration, err := time.ParseDuration(v); err == nil {
			c.PostgresConnMaxLifetime = duration
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
	if v := os.Getenv("JEEVES_LLM_MIN_CONFIDENCE"); v != "" {
		if conf, err := strconv.ParseFloat(v, 64); err == nil {
			c.LLMMinConfidence = conf
		}
	}
	if v := os.Getenv("JEEVES_MAX_EVENT_HISTORY"); v != "" {
		if max, err := strconv.Atoi(v); err == nil {
			c.MaxEventHistory = max
		}
	}

	// Consolidation configuration
	if v := os.Getenv("JEEVES_CONSOLIDATION_INTERVAL_HOURS"); v != "" {
		if hours, err := strconv.Atoi(v); err == nil {
			c.ConsolidationIntervalHours = hours
		}
	}
	if v := os.Getenv("JEEVES_CONSOLIDATION_LOOKBACK_HOURS"); v != "" {
		if hours, err := strconv.Atoi(v); err == nil {
			c.ConsolidationLookbackHours = hours
		}
	}
	if v := os.Getenv("JEEVES_CONSOLIDATION_MAX_GAP_MINUTES"); v != "" {
		if minutes, err := strconv.Atoi(v); err == nil {
			c.ConsolidationMaxGapMinutes = minutes
		}
	}

	// Pattern Discovery configuration
	if v := os.Getenv("JEEVES_PATTERN_DISCOVERY_ENABLED"); v != "" {
		if enabled, err := strconv.ParseBool(v); err == nil {
			c.PatternDiscoveryEnabled = enabled
		}
	}
	if v := os.Getenv("JEEVES_PATTERN_DISTANCE_STRATEGY"); v != "" {
		c.PatternDistanceStrategy = v
	}
	if v := os.Getenv("JEEVES_PATTERN_DISCOVERY_INTERVAL_HOURS"); v != "" {
		if hours, err := strconv.Atoi(v); err == nil {
			c.PatternDiscoveryIntervalHours = hours
		}
	}
	if v := os.Getenv("JEEVES_PATTERN_DISCOVERY_BATCH_SIZE"); v != "" {
		if batchSize, err := strconv.Atoi(v); err == nil {
			c.PatternDiscoveryBatchSize = batchSize
		}
	}
	if v := os.Getenv("JEEVES_PATTERN_CLUSTERING_EPSILON"); v != "" {
		if epsilon, err := strconv.ParseFloat(v, 64); err == nil {
			c.PatternClusteringEpsilon = epsilon
		}
	}
	if v := os.Getenv("JEEVES_PATTERN_CLUSTERING_MIN_POINTS"); v != "" {
		if minPoints, err := strconv.Atoi(v); err == nil {
			c.PatternClusteringMinPoints = minPoints
		}
	}
	if v := os.Getenv("JEEVES_PATTERN_MIN_ANCHORS_FOR_DISCOVERY"); v != "" {
		if minAnchors, err := strconv.Atoi(v); err == nil {
			c.PatternMinAnchorsForDiscovery = minAnchors
		}
	}
	if v := os.Getenv("JEEVES_PATTERN_LOOKBACK_HOURS"); v != "" {
		if hours, err := strconv.Atoi(v); err == nil {
			c.PatternLookbackHours = hours
		}
	}

	// Temporal Grouping configuration
	if v := os.Getenv("JEEVES_TEMPORAL_GROUPING_ENABLED"); v != "" {
		if enabled, err := strconv.ParseBool(v); err == nil {
			c.TemporalGroupingEnabled = enabled
		}
	}
	if v := os.Getenv("JEEVES_TEMPORAL_GROUPING_WINDOW_MINUTES"); v != "" {
		if minutes, err := strconv.Atoi(v); err == nil {
			c.TemporalGroupingWindowMinutes = minutes
		}
	}
	if v := os.Getenv("JEEVES_TEMPORAL_GROUPING_OVERLAP_RATIO"); v != "" {
		if ratio, err := strconv.ParseFloat(v, 64); err == nil {
			c.TemporalGroupingOverlapRatio = ratio
		}
	}

	// Batch Processing configuration
	if v := os.Getenv("JEEVES_BATCH_PROCESSING_ENABLED"); v != "" {
		if enabled, err := strconv.ParseBool(v); err == nil {
			c.BatchProcessingEnabled = enabled
		}
	}
	if v := os.Getenv("JEEVES_BATCH_DURATION"); v != "" {
		if duration, err := time.ParseDuration(v); err == nil {
			c.BatchDuration = duration
		}
	}
	if v := os.Getenv("JEEVES_BATCH_OVERLAP"); v != "" {
		if overlap, err := time.ParseDuration(v); err == nil {
			c.BatchOverlap = overlap
		}
	}
	if v := os.Getenv("JEEVES_BATCH_SCHEDULE_ENABLED"); v != "" {
		if enabled, err := strconv.ParseBool(v); err == nil {
			c.BatchScheduleEnabled = enabled
		}
	}
	if v := os.Getenv("JEEVES_BATCH_SCHEDULE_INTERVAL"); v != "" {
		if interval, err := time.ParseDuration(v); err == nil {
			c.BatchScheduleInterval = interval
		}
	}
	if v := os.Getenv("JEEVES_BATCH_METADATA_ENABLED"); v != "" {
		if enabled, err := strconv.ParseBool(v); err == nil {
			c.BatchMetadataEnabled = enabled
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

	// PostgreSQL flags
	pflag.StringVar(&c.PostgresHost, "postgres-host", c.PostgresHost, "PostgreSQL hostname")
	pflag.IntVar(&c.PostgresPort, "postgres-port", c.PostgresPort, "PostgreSQL port")
	pflag.StringVar(&c.PostgresUser, "postgres-user", c.PostgresUser, "PostgreSQL username")
	pflag.StringVar(&c.PostgresPassword, "postgres-password", c.PostgresPassword, "PostgreSQL password")
	pflag.StringVar(&c.PostgresDB, "postgres-db", c.PostgresDB, "PostgreSQL database name")
	pflag.StringVar(&c.PostgresSSLMode, "postgres-sslmode", c.PostgresSSLMode, "PostgreSQL SSL mode")
	pflag.IntVar(&c.PostgresMaxConnections, "postgres-max-conns", c.PostgresMaxConnections, "PostgreSQL max connections")
	pflag.IntVar(&c.PostgresMaxIdleConnections, "postgres-max-idle-conns", c.PostgresMaxIdleConnections, "PostgreSQL max idle connections")
	pflag.DurationVar(&c.PostgresConnMaxLifetime, "postgres-conn-max-life", c.PostgresConnMaxLifetime, "PostgreSQL connection max lifetime")

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
	pflag.Float64Var(&c.LLMMinConfidence, "llm-min-confidence", c.LLMMinConfidence, "Minimum LLM confidence threshold")
	pflag.IntVar(&c.MaxEventHistory, "max-event-history", c.MaxEventHistory, "Maximum motion event history to keep")

	// Consolidation flags
	pflag.IntVar(&c.ConsolidationIntervalHours, "consolidation-interval-hours", c.ConsolidationIntervalHours, "Episode consolidation interval in hours")
	pflag.IntVar(&c.ConsolidationLookbackHours, "consolidation-lookback-hours", c.ConsolidationLookbackHours, "Episode consolidation lookback period in hours")
	pflag.IntVar(&c.ConsolidationMaxGapMinutes, "consolidation-max-gap-minutes", c.ConsolidationMaxGapMinutes, "Maximum gap between episodes for consolidation in minutes")

	// Pattern Discovery flags
	pflag.BoolVar(&c.PatternDiscoveryEnabled, "pattern-discovery-enabled", c.PatternDiscoveryEnabled, "Enable pattern discovery")
	pflag.StringVar(&c.PatternDistanceStrategy, "pattern-distance-strategy", c.PatternDistanceStrategy, "Distance computation strategy (llm_first, learned_first, vector_first)")
	pflag.IntVar(&c.PatternDiscoveryIntervalHours, "pattern-discovery-interval-hours", c.PatternDiscoveryIntervalHours, "Pattern discovery interval in hours")
	pflag.IntVar(&c.PatternDiscoveryBatchSize, "pattern-discovery-batch-size", c.PatternDiscoveryBatchSize, "Pattern discovery batch size")
	pflag.Float64Var(&c.PatternClusteringEpsilon, "pattern-clustering-epsilon", c.PatternClusteringEpsilon, "DBSCAN epsilon (maximum distance for neighborhood)")
	pflag.IntVar(&c.PatternClusteringMinPoints, "pattern-clustering-min-points", c.PatternClusteringMinPoints, "DBSCAN minimum points to form cluster")
	pflag.IntVar(&c.PatternMinAnchorsForDiscovery, "pattern-min-anchors-for-discovery", c.PatternMinAnchorsForDiscovery, "Minimum anchors required for pattern discovery")
	pflag.IntVar(&c.PatternLookbackHours, "pattern-lookback-hours", c.PatternLookbackHours, "Pattern discovery lookback period in hours")

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

// PostgresConnectionString returns a PostgreSQL connection string
func (c *Config) PostgresConnectionString() string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.PostgresHost, c.PostgresPort, c.PostgresUser, c.PostgresPassword, c.PostgresDB, c.PostgresSSLMode)
}
