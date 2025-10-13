package executor

import (
	"context"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/saaga0h/jeeves-platform/e2e/internal/checker"
	"github.com/saaga0h/jeeves-platform/e2e/internal/observer"
	"github.com/saaga0h/jeeves-platform/e2e/internal/reporter"
	"github.com/saaga0h/jeeves-platform/e2e/internal/scenario"
)

// Runner orchestrates test scenario execution
type Runner struct {
	mqttBroker string
	redisHost  string
	logger     *log.Logger
	observer   *observer.Observer
	player     *MQTTPlayer
	redisClient *redis.Client
}

// NewRunner creates a new test runner
func NewRunner(mqttBroker, redisHost string, logger *log.Logger) *Runner {
	if logger == nil {
		logger = log.Default()
	}

	return &Runner{
		mqttBroker: mqttBroker,
		redisHost:  redisHost,
		logger:     logger,
	}
}

// Run executes a test scenario
func (r *Runner) Run(ctx context.Context, s *scenario.Scenario) (*scenario.TestResult, []reporter.TimelineEvent, error) {
	r.logger.Printf("Starting scenario: %s", s.Name)
	r.logger.Printf("Description: %s", s.Description)

	// Initialize connections
	if err := r.initialize(); err != nil {
		return nil, nil, fmt.Errorf("initialization failed: %w", err)
	}
	defer r.cleanup()

	// Wait for agents to start up
	r.logger.Printf("Waiting 5 seconds for agents to start up...")
	time.Sleep(5 * time.Second)

	// Start observer
	if err := r.observer.Start(); err != nil {
		return nil, nil, fmt.Errorf("failed to start observer: %w", err)
	}

	startTime := time.Now()
	var timelineEvents []reporter.TimelineEvent

	// Execute events
	for _, event := range s.Events {
		WaitUntil(startTime, event.Time)
		elapsed := GetElapsed(startTime)

		r.logger.Printf("[%.2fs] Publishing event: %s = %v (%s)",
			elapsed, event.Sensor, event.Value, event.Description)

		if err := r.player.PublishEvent(event); err != nil {
			return nil, nil, fmt.Errorf("failed to publish event: %w", err)
		}

		timelineEvents = append(timelineEvents, reporter.TimelineEvent{
			Elapsed:     elapsed,
			Layer:       "sensor",
			Description: fmt.Sprintf("%s = %v (%s)", event.Sensor, event.Value, event.Description),
			IsCheck:     false,
		})
	}

	// Execute wait periods
	for _, wait := range s.Wait {
		WaitUntil(startTime, wait.Time)
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
		WaitUntil(startTime, le.exp.Time)
		elapsed := GetElapsed(startTime)

		r.logger.Printf("[%.2fs] Checking expectation: %s - %s",
			elapsed, le.layer, le.exp.Topic)

		// Get all messages captured so far
		messages := r.observer.GetAllMessages()

		var passed bool
		var reason string
		var actualPayload interface{}

		// Check MQTT expectations
		if len(le.exp.Payload) > 0 {
			passed, reason, actualPayload = checker.CheckExpectation(le.exp, messages)
		} else if le.exp.RedisKey != "" {
			// Check Redis expectations
			passed, reason, actualPayload = checker.CheckRedisExpectation(ctx, r.redisClient, le.exp)
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
			Description: le.exp.Topic,
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
		Scenario:      s,
		StartTime:     startTime,
		EndTime:       endTime,
		Passed:        failedCount == 0,
		PassedCount:   passedCount,
		FailedCount:   failedCount,
		Expectations:  expectationResults,
	}

	return testResult, timelineEvents, nil
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
}

// SaveCapture saves the MQTT capture to a file
func (r *Runner) SaveCapture(filename string) error {
	if r.observer == nil {
		return fmt.Errorf("observer not initialized")
	}
	return r.observer.SaveCapture(filename)
}
