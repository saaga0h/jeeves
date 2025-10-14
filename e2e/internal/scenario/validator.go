package scenario

import (
	"fmt"
	"sort"
	"time"
)

// ValidateScenario performs validation checks on a loaded scenario
func ValidateScenario(s *Scenario) error {
	if s.Name == "" {
		return fmt.Errorf("scenario name is required")
	}

	if s.Description == "" {
		return fmt.Errorf("scenario description is required")
	}

	if s.Setup.Location == "" {
		return fmt.Errorf("setup.location is required")
	}

	// Validate events
	if err := validateEvents(s.Events); err != nil {
		return fmt.Errorf("events validation failed: %w", err)
	}

	// Validate wait periods
	if err := validateWaitPeriods(s.Wait); err != nil {
		return fmt.Errorf("wait periods validation failed: %w", err)
	}

	// Validate expectations
	if err := validateExpectations(s.Expectations); err != nil {
		return fmt.Errorf("expectations validation failed: %w", err)
	}

	// Validate test mode configuration
	if err := validateTestMode(s.TestMode); err != nil {
		return fmt.Errorf("test_mode validation failed: %w", err)
	}

	// Validate timing consistency
	if err := validateTimingConsistency(s); err != nil {
		return fmt.Errorf("timing validation failed: %w", err)
	}

	return nil
}

func validateEvents(events []SensorEvent) error {
	if len(events) == 0 {
		return fmt.Errorf("at least one event is required")
	}

	for i, event := range events {
		if event.Time < 0 {
			return fmt.Errorf("event %d: time cannot be negative", i)
		}

		// Must have either Sensor OR (Type + Location)
		if event.Sensor == "" && (event.Type == "" || event.Location == "") {
			return fmt.Errorf("event %d: must have either 'sensor' or both 'type' and 'location'", i)
		}

		if event.Sensor != "" && event.Type != "" {
			return fmt.Errorf("event %d: cannot specify both 'sensor' and 'type'", i)
		}

		if event.Description == "" {
			return fmt.Errorf("event %d: description is required", i)
		}

		// Validate value/data based on category
		category := event.Category()
		if category == "sensor" && event.Value == nil {
			return fmt.Errorf("event %d: raw sensor events require 'value'", i)
		}
		if (category == "context" || category == "media") && len(event.Data) == 0 {
			return fmt.Errorf("event %d: context/media events require 'data'", i)
		}
	}

	return nil
}

func validateWaitPeriods(waits []WaitPeriod) error {
	for i, wait := range waits {
		if wait.Time < 0 {
			return fmt.Errorf("wait period %d: time cannot be negative", i)
		}

		if wait.Description == "" {
			return fmt.Errorf("wait period %d: description is required", i)
		}
	}

	return nil
}

func validateExpectations(expectations map[string][]Expectation) error {
	if len(expectations) == 0 {
		return fmt.Errorf("at least one expectation is required")
	}

	for layer, exps := range expectations {
		if layer == "" {
			return fmt.Errorf("expectation layer name cannot be empty")
		}

		for i, exp := range exps {
			if exp.Time < 0 {
				return fmt.Errorf("layer %s, expectation %d: time cannot be negative", layer, i)
			}

			if exp.Topic == "" && exp.PostgresQuery == "" {
				return fmt.Errorf("layer %s, expectation %d: either topic or postgres_query is required", layer, i)
			}

			// MQTT expectations: payload or redis checks
			if exp.Topic != "" {
				hasPayload := len(exp.Payload) > 0
				hasRedis := exp.RedisKey != ""

				if !hasPayload && !hasRedis {
					return fmt.Errorf("layer %s, expectation %d: MQTT expectations require either payload or redis_key", layer, i)
				}

				if hasRedis {
					if exp.RedisField == "" {
						return fmt.Errorf("layer %s, expectation %d: redis_field is required when redis_key is specified", layer, i)
					}
					if exp.Expected == "" {
						return fmt.Errorf("layer %s, expectation %d: expected is required when redis_key is specified", layer, i)
					}
				}
			}

			// Postgres expectations
			if exp.PostgresQuery != "" && exp.PostgresExpected == nil {
				return fmt.Errorf("layer %s, expectation %d: postgres_expected is required when postgres_query is specified", layer, i)
			}
		}
	}

	return nil
}

func validateTimingConsistency(s *Scenario) error {
	// Collect all timestamps
	var allTimes []int

	for _, event := range s.Events {
		allTimes = append(allTimes, event.Time)
	}

	for _, wait := range s.Wait {
		allTimes = append(allTimes, wait.Time)
	}

	for _, exps := range s.Expectations {
		for _, exp := range exps {
			allTimes = append(allTimes, exp.Time)
		}
	}

	// Sort times
	sort.Ints(allTimes)

	// Check for reasonable timing (expectations should come after events)
	maxEventTime := 0
	for _, event := range s.Events {
		if event.Time > maxEventTime {
			maxEventTime = event.Time
		}
	}

	minExpectationTime := int(^uint(0) >> 1) // Max int
	for _, exps := range s.Expectations {
		for _, exp := range exps {
			if exp.Time < minExpectationTime {
				minExpectationTime = exp.Time
			}
		}
	}

	// Warn if expectations come before events (but don't fail - might be intentional)
	if minExpectationTime < maxEventTime {
		// This is actually okay - expectations can be checked at any time
	}

	return nil
}

func validateTestMode(tm *TestModeConfig) error {
	if tm == nil {
		return nil // test_mode is optional
	}

	if tm.VirtualStart != "" {
		// Validate ISO 8601 format
		if _, err := time.Parse(time.RFC3339, tm.VirtualStart); err != nil {
			return fmt.Errorf("virtual_start must be valid ISO 8601 timestamp: %w", err)
		}
	}

	if tm.TimeScale < 1 {
		return fmt.Errorf("time_scale must be >= 1 (got %d)", tm.TimeScale)
	}

	// If time_scale is set, virtual_start should also be set
	if tm.TimeScale > 1 && tm.VirtualStart == "" {
		return fmt.Errorf("virtual_start is required when time_scale is set")
	}

	return nil
}
