package scenario

import (
	"fmt"
	"sort"
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

	seenTimes := make(map[int]bool)
	for i, event := range events {
		if event.Time < 0 {
			return fmt.Errorf("event %d: time cannot be negative", i)
		}

		if event.Sensor == "" {
			return fmt.Errorf("event %d: sensor is required", i)
		}

		if event.Description == "" {
			return fmt.Errorf("event %d: description is required", i)
		}

		seenTimes[event.Time] = true
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

			if exp.Topic == "" {
				return fmt.Errorf("layer %s, expectation %d: topic is required", layer, i)
			}

			// Either payload or redis checks must be present
			if len(exp.Payload) == 0 && exp.RedisKey == "" {
				return fmt.Errorf("layer %s, expectation %d: either payload or redis_key must be specified", layer, i)
			}

			// If redis checks are present, validate them
			if exp.RedisKey != "" {
				if exp.RedisField == "" {
					return fmt.Errorf("layer %s, expectation %d: redis_field is required when redis_key is specified", layer, i)
				}
				if exp.Expected == "" {
					return fmt.Errorf("layer %s, expectation %d: expected is required when redis_key is specified", layer, i)
				}
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
