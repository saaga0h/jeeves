package behavior

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/saaga0h/jeeves-platform/pkg/mqtt"
)

// TimeManager manages virtual time for testing scenarios
type TimeManager struct {
	mu           sync.RWMutex
	testMode     bool
	virtualStart time.Time
	realStart    time.Time
	timeScale    int
	logger       *slog.Logger
}

// NewTimeManager creates a new time manager
func NewTimeManager(logger *slog.Logger) *TimeManager {
	return &TimeManager{
		testMode:  false,
		realStart: time.Now(),
		timeScale: 1,
		logger:    logger,
	}
}

// ConfigureFromMQTT subscribes to test mode configuration
func (tm *TimeManager) ConfigureFromMQTT(mqttClient mqtt.Client) error {
	handler := func(msg mqtt.Message) {
		tm.handleTestModeConfig(msg.Payload())
	}

	return mqttClient.Subscribe("automation/test/time_config", 1, handler)
}

// handleTestModeConfig processes test mode configuration from MQTT
func (tm *TimeManager) handleTestModeConfig(payload []byte) {
	var config struct {
		VirtualStart string `json:"virtual_start"`
		TimeScale    int    `json:"time_scale"`
		TestMode     bool   `json:"test_mode"`
	}

	if err := json.Unmarshal(payload, &config); err != nil {
		tm.logger.Error("Failed to parse test mode config", "error", err)
		return
	}

	if !config.TestMode {
		tm.logger.Info("Test mode disabled")
		tm.mu.Lock()
		tm.testMode = false
		tm.mu.Unlock()
		return
	}

	virtualStart, err := time.Parse(time.RFC3339, config.VirtualStart)
	if err != nil {
		tm.logger.Error("Invalid virtual_start time", "error", err)
		return
	}

	tm.mu.Lock()
	tm.testMode = true
	tm.virtualStart = virtualStart
	tm.realStart = time.Now()
	tm.timeScale = config.TimeScale
	tm.mu.Unlock()

	tm.logger.Info("Test mode configured",
		"virtual_start", config.VirtualStart,
		"time_scale", config.TimeScale)
}

// Now returns the current time (real or virtual)
func (tm *TimeManager) Now() time.Time {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if !tm.testMode {
		return time.Now()
	}

	// Calculate virtual time based on real elapsed time and scale
	realElapsed := time.Since(tm.realStart)
	virtualElapsed := realElapsed * time.Duration(tm.timeScale)
	return tm.virtualStart.Add(virtualElapsed)
}

// IsTestMode returns whether test mode is active
func (tm *TimeManager) IsTestMode() bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.testMode
}
