package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/saaga0h/jeeves-platform/e2e/internal/checker"
	"github.com/saaga0h/jeeves-platform/e2e/internal/observer"
	"github.com/saaga0h/jeeves-platform/e2e/internal/reporter"
	"github.com/saaga0h/jeeves-platform/e2e/internal/scenario"
	"github.com/saaga0h/jeeves-platform/pkg/postgres"
)

// Runner orchestrates test scenario execution
type Runner struct {
	mqttBroker      string
	redisHost       string
	pgClient        postgres.Client
	logger          *log.Logger
	observer        *observer.Observer
	player          *MQTTPlayer
	redisClient     *redis.Client
	postgresChecker *checker.PostgresChecker
}

// NewRunner creates a new test runner
func NewRunner(mqttBroker, redisHost string, pgClient postgres.Client, logger *log.Logger) *Runner {
	if logger == nil {
		logger = log.Default()
	}

	return &Runner{
		mqttBroker: mqttBroker,
		redisHost:  redisHost,
		pgClient:   pgClient,
		logger:     logger,
	}
}

// Run executes a test scenario
func (r *Runner) Run(ctx context.Context, s *scenario.Scenario) (*scenario.TestResult, []reporter.TimelineEvent, error) {
	r.logger.Printf("Starting scenario: %s", s.Name)
	r.logger.Printf("Description: %s", s.Description)

	// Log test mode if configured
	if s.TestMode != nil {
		r.logger.Printf("Test mode enabled: virtual_start=%s, time_scale=%dx",
			s.TestMode.VirtualStart, s.TestMode.TimeScale)
	}

	// Initialize connections
	if err := r.initialize(); err != nil {
		return nil, nil, fmt.Errorf("initialization failed: %w", err)
	}
	defer r.cleanup()

	// Publish test mode configuration to MQTT for agents BEFORE waiting for startup
	if s.TestMode != nil {
		if err := r.publishTestMode(s.TestMode); err != nil {
			return nil, nil, fmt.Errorf("failed to publish test mode: %w", err)
		}
	}

	// Wait for agents to start up
	r.logger.Printf("Waiting 5 seconds for agents to start up...")
	time.Sleep(5 * time.Second)

	// Start observer
	if err := r.observer.Start(); err != nil {
		return nil, nil, fmt.Errorf("failed to start observer: %w", err)
	}

	startTime := time.Now()
	var timelineEvents []reporter.TimelineEvent

	// Determine time scale for event timing
	timeScale := 1
	if s.TestMode != nil {
		timeScale = s.TestMode.TimeScale
	}

	// Execute events
	for _, event := range s.Events {
		WaitUntil(startTime, event.Time, timeScale)
		elapsed := GetElapsed(startTime)

		// Determine event description
		var eventDesc string
		if event.Sensor != "" {
			eventDesc = fmt.Sprintf("%s = %v (%s)", event.Sensor, event.Value, event.Description)
		} else {
			eventDesc = fmt.Sprintf("%s/%s (%s)", event.Type, event.Location, event.Description)
		}

		r.logger.Printf("[%.2fs] Publishing event: %s", elapsed, eventDesc)

		// Route event based on category
		var err error
		switch event.Category() {
		case "sensor":
			err = r.player.PublishEvent(event)
		case "context":
			err = r.player.PublishContextEvent(event.Type, event.Location, event.Data)
		case "media":
			err = r.player.PublishMediaEvent(event.Location, event.Data)
		default:
			err = fmt.Errorf("unknown event category")
		}

		if err != nil {
			return nil, nil, fmt.Errorf("failed to publish event: %w", err)
		}

		timelineEvents = append(timelineEvents, reporter.TimelineEvent{
			Elapsed:     elapsed,
			Layer:       "sensor",
			Description: eventDesc,
			IsCheck:     false,
		})
	}

	// Execute wait periods
	for _, wait := range s.Wait {
		WaitUntil(startTime, wait.Time, timeScale)
		elapsed := GetElapsed(startTime)

		r.logger.Printf("[%.2fs] Wait: %s", elapsed, wait.Description)

		timelineEvents = append(timelineEvents, reporter.TimelineEvent{
			Elapsed:     elapsed,
			Layer:       "wait",
			Description: fmt.Sprintf("%s (%.1fs)", wait.Description, float64(wait.Time)),
			IsCheck:     false,
		})
	}

	// Check expectations
	var expectationResults []scenario.ExpectationResult

	// Sort expectations by layer and time
	type layerExp struct {
		layer string
		exp   scenario.Expectation
	}
	var allExpectations []layerExp
	for layer, exps := range s.Expectations {
		for _, exp := range exps {
			allExpectations = append(allExpectations, layerExp{layer, exp})
		}
	}
	sort.Slice(allExpectations, func(i, j int) bool {
		return allExpectations[i].exp.Time < allExpectations[j].exp.Time
	})

	for _, le := range allExpectations {
		WaitUntil(startTime, le.exp.Time, timeScale)
		elapsed := GetElapsed(startTime)

		var checkDesc string
		if le.exp.Topic != "" {
			checkDesc = le.exp.Topic
		} else if le.exp.PostgresQuery != "" {
			checkDesc = "postgres query"
		}

		r.logger.Printf("[%.2fs] Checking expectation: %s - %s",
			elapsed, le.layer, checkDesc)

		var passed bool
		var reason string
		var actualPayload interface{}

		// Route to appropriate checker
		if le.exp.PostgresQuery != "" {
			// Postgres expectation
			passed, reason, actualPayload = r.checkPostgresExpectation(ctx, le.exp)
		} else if le.exp.RedisKey != "" {
			// Redis expectation
			passed, reason, actualPayload = checker.CheckRedisExpectation(ctx, r.redisClient, le.exp)
		} else if len(le.exp.Payload) > 0 {
			// MQTT expectation
			messages := r.observer.GetAllMessages()
			passed, reason, actualPayload = checker.CheckExpectation(le.exp, messages)
		}

		result := scenario.ExpectationResult{
			Layer:         le.layer,
			Expectation:   le.exp,
			Passed:        passed,
			Reason:        reason,
			ActualTopic:   le.exp.Topic,
			ActualPayload: actualPayload,
		}

		expectationResults = append(expectationResults, result)

		status := "PASS"
		if !passed {
			status = "FAIL"
			r.logger.Printf("[%.2fs] ✗ %s: %s", elapsed, status, reason)
		} else {
			r.logger.Printf("[%.2fs] ✓ %s", elapsed, status)
		}

		timelineEvents = append(timelineEvents, reporter.TimelineEvent{
			Elapsed:     elapsed,
			Layer:       le.layer,
			Description: checkDesc,
			Success:     passed,
			IsCheck:     true,
		})
	}

	endTime := time.Now()

	// Calculate results
	passedCount := 0
	failedCount := 0
	for _, result := range expectationResults {
		if result.Passed {
			passedCount++
		} else {
			failedCount++
		}
	}

	testResult := &scenario.TestResult{
		Scenario:     s,
		StartTime:    startTime,
		EndTime:      endTime,
		Passed:       failedCount == 0,
		PassedCount:  passedCount,
		FailedCount:  failedCount,
		Expectations: expectationResults,
	}

	return testResult, timelineEvents, nil
}

// checkPostgresExpectation checks a Postgres query expectation
func (r *Runner) checkPostgresExpectation(ctx context.Context, exp scenario.Expectation) (bool, string, interface{}) {
	if r.postgresChecker == nil {
		return false, "postgres checker not initialized", nil
	}

	err := r.postgresChecker.CheckQuery(exp.PostgresQuery, exp.PostgresExpected)
	if err != nil {
		return false, fmt.Sprintf("postgres check failed: %v", err), nil
	}

	return true, "postgres check passed", exp.PostgresExpected
}

// initialize sets up connections
func (r *Runner) initialize() error {
	// Create observer
	r.observer = observer.NewObserver(r.mqttBroker, r.logger)

	// Create MQTT player
	player, err := NewMQTTPlayer(r.mqttBroker, r.logger)
	if err != nil {
		return fmt.Errorf("failed to create MQTT player: %w", err)
	}
	r.player = player

	// Create Redis client
	r.redisClient = redis.NewClient(&redis.Options{
		Addr: r.redisHost,
	})

	// Test Redis connection
	ctx := context.Background()
	if err := r.redisClient.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}

	r.logger.Printf("Connected to Redis at %s", r.redisHost)

	// Create Postgres checker (if client provided)
	if r.pgClient != nil {
		postgresChecker, err := checker.NewPostgresChecker(r.pgClient, r.logger)
		if err != nil {
			return fmt.Errorf("failed to create Postgres checker: %w", err)
		}
		r.postgresChecker = postgresChecker
		r.logger.Printf("Connected to Postgres")
	}

	return nil
}

// cleanup closes all connections
func (r *Runner) cleanup() {
	if r.observer != nil {
		r.observer.Stop()
	}
	if r.player != nil {
		r.player.Close()
	}
	if r.redisClient != nil {
		r.redisClient.Close()
	}
	if r.postgresChecker != nil {
		r.postgresChecker.Close()
	}
}

// SaveCapture saves the MQTT capture to a file
func (r *Runner) SaveCapture(filename string) error {
	if r.observer == nil {
		return fmt.Errorf("observer not initialized")
	}
	return r.observer.SaveCapture(filename)
}

// publishTestMode publishes test mode configuration to MQTT for agents
func (r *Runner) publishTestMode(tm *scenario.TestModeConfig) error {
	topic := "automation/test/time_config"

	payload := map[string]interface{}{
		"virtual_start": tm.VirtualStart,
		"time_scale":    tm.TimeScale,
		"test_mode":     true,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal test mode config: %w", err)
	}

	if err := r.player.Publish(topic, 1, true, payloadBytes); err != nil {
		return fmt.Errorf("failed to publish test mode config: %w", err)
	}

	r.logger.Printf("Published test mode configuration to %s", topic)

	// Give agents time to receive and process configuration
	time.Sleep(1 * time.Second)

	return nil
}
