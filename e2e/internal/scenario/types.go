package scenario

import "time"

// Scenario represents a complete E2E test scenario
type Scenario struct {
	Name         string                    `yaml:"name"`
	Description  string                    `yaml:"description"`
	Setup        SetupConfig               `yaml:"setup"`
	Events       []SensorEvent             `yaml:"events"`
	Wait         []WaitPeriod              `yaml:"wait"`
	Expectations map[string][]Expectation  `yaml:"expectations"`
}

// SetupConfig defines the initial state for a test scenario
type SetupConfig struct {
	Location     string                 `yaml:"location"`
	InitialState map[string]interface{} `yaml:"initial_state"`
}

// SensorEvent represents a sensor event to publish during the test
type SensorEvent struct {
	Time        int         `yaml:"time"`        // Seconds from start
	Sensor      string      `yaml:"sensor"`      // e.g. "motion:hallway-sensor-1"
	Value       interface{} `yaml:"value"`
	Description string      `yaml:"description"`
}

// WaitPeriod represents a pause in the scenario
type WaitPeriod struct {
	Time        int    `yaml:"time"`        // Seconds from start
	Description string `yaml:"description"`
}

// Expectation represents an expected outcome to verify
type Expectation struct {
	Time    int                    `yaml:"time"`    // Seconds from start
	Topic   string                 `yaml:"topic"`   // MQTT topic
	Payload map[string]interface{} `yaml:"payload"` // Expected payload (supports special matchers)

	// Optional: Redis state checks
	RedisKey   string `yaml:"redis_key,omitempty"`
	RedisField string `yaml:"redis_field,omitempty"`
	Expected   string `yaml:"expected,omitempty"`
}

// TestResult represents the outcome of running a scenario
type TestResult struct {
	Scenario      *Scenario
	StartTime     time.Time
	EndTime       time.Time
	Passed        bool
	PassedCount   int
	FailedCount   int
	Expectations  []ExpectationResult
}

// ExpectationResult represents the result of checking a single expectation
type ExpectationResult struct {
	Layer       string
	Expectation Expectation
	Passed      bool
	Reason      string
	ActualTopic string
	ActualPayload interface{}
}
