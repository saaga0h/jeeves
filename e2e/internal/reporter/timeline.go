package reporter

import (
	"fmt"
	"strings"
	"time"

	"github.com/saaga0h/jeeves-platform/e2e/internal/scenario"
)

// TimelineEvent represents a single event in the timeline
type TimelineEvent struct {
	Elapsed     float64
	Layer       string
	Description string
	Success     bool // true = success, false = failure, ignored for regular events
	IsCheck     bool // true if this is an expectation check
}

// GenerateTimeline creates a human-readable timeline of test execution
func GenerateTimeline(result *scenario.TestResult, events []TimelineEvent) string {
	var sb strings.Builder

	duration := result.EndTime.Sub(result.StartTime)

	// Header
	sb.WriteString("╔══════════════════════════════════════════════════════════╗\n")
	sb.WriteString(fmt.Sprintf("║  Scenario: %-46s║\n", truncate(result.Scenario.Name, 46)))
	sb.WriteString(fmt.Sprintf("║  Duration: %-46s║\n", formatDuration(duration)))
	sb.WriteString("╚══════════════════════════════════════════════════════════╝\n\n")

	// Events timeline
	for _, event := range events {
		icon := "→"
		if event.IsCheck {
			if event.Success {
				icon = "✓"
			} else {
				icon = "✗"
			}
		}

		sb.WriteString(fmt.Sprintf("[%7.2fs] %s %-13s: %s\n",
			event.Elapsed,
			icon,
			event.Layer,
			event.Description,
		))
	}

	// Expectations summary
	sb.WriteString("\n=== Expectations ===\n")

	// Group expectations by layer
	layerResults := make(map[string][]scenario.ExpectationResult)
	for _, expResult := range result.Expectations {
		layerResults[expResult.Layer] = append(layerResults[expResult.Layer], expResult)
	}

	for layer, results := range layerResults {
		sb.WriteString(fmt.Sprintf("Layer: %s\n", layer))
		for _, expResult := range results {
			icon := "✓"
			if !expResult.Passed {
				icon = "✗"
			}

			sb.WriteString(fmt.Sprintf("  %s %s", icon, expResult.Expectation.Topic))

			// Add payload details for failed checks
			if !expResult.Passed {
				sb.WriteString(fmt.Sprintf(": %s", expResult.Reason))
			} else {
				// Show matched conditions for passed checks
				if len(expResult.Expectation.Payload) > 0 {
					var conditions []string
					for key, val := range expResult.Expectation.Payload {
						conditions = append(conditions, fmt.Sprintf("%s=%v", key, val))
					}
					if len(conditions) > 0 {
						sb.WriteString(fmt.Sprintf(": %s", strings.Join(conditions, ", ")))
					}
				}
			}

			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	// Summary footer
	status := "✓ ALL TESTS PASSED"
	if result.FailedCount > 0 {
		status = fmt.Sprintf("✗ %d TEST(S) FAILED", result.FailedCount)
	}

	sb.WriteString("╔══════════════════════════════════════════════════════════╗\n")
	sb.WriteString("║  SUMMARY                                                 ║\n")
	sb.WriteString(fmt.Sprintf("║  Passed: %-48d║\n", result.PassedCount))
	sb.WriteString(fmt.Sprintf("║  Failed: %-48d║\n", result.FailedCount))
	sb.WriteString(fmt.Sprintf("║  Status: %-48s║\n", status))
	sb.WriteString("╚══════════════════════════════════════════════════════════╝\n")

	return sb.String()
}

// formatDuration formats a duration as human-readable string
func formatDuration(d time.Duration) string {
	seconds := d.Seconds()
	if seconds < 60 {
		return fmt.Sprintf("%.1fs", seconds)
	}

	minutes := int(seconds / 60)
	remainingSeconds := seconds - float64(minutes*60)
	return fmt.Sprintf("%dm %.1fs", minutes, remainingSeconds)
}

// truncate truncates a string to maxLen characters
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
