package checker

import (
	"fmt"

	"github.com/saaga0h/jeeves-platform/e2e/internal/observer"
	"github.com/saaga0h/jeeves-platform/e2e/internal/scenario"
)

// CheckExpectation validates an expectation against captured MQTT messages
func CheckExpectation(exp scenario.Expectation, messages []observer.CapturedMessage) (bool, string, interface{}) {
	// Find messages matching the topic
	var matchingMessages []observer.CapturedMessage
	for _, msg := range messages {
		if msg.Topic == exp.Topic {
			matchingMessages = append(matchingMessages, msg)
		}
	}

	if len(matchingMessages) == 0 {
		return false, fmt.Sprintf("no messages found for topic %q", exp.Topic), nil
	}

	// Use the most recent message
	latestMsg := matchingMessages[len(matchingMessages)-1]

	// Check payload expectations
	if len(exp.Payload) > 0 {
		// Ensure payload is a map
		payloadMap, ok := latestMsg.Payload.(map[string]interface{})
		if !ok {
			return false, fmt.Sprintf("payload is not a JSON object, got %T", latestMsg.Payload), latestMsg.Payload
		}

		// Match each expected field
		matches, reason := MatchesExpectation(payloadMap, exp.Payload)
		if !matches {
			return false, reason, latestMsg.Payload
		}
	}

	return true, "", latestMsg.Payload
}
