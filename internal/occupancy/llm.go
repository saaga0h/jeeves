package occupancy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"time"

	"github.com/saaga0h/jeeves-platform/pkg/config"
)

// LLMRequest represents a request to the Ollama API
type LLMRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
	Format string `json:"format"`
	Options struct {
		Temperature float64 `json:"temperature"`
	} `json:"options"`
}

// LLMResponse represents a response from the Ollama API
type LLMResponse struct {
	Model     string `json:"model"`
	CreatedAt string `json:"created_at"`
	Response  string `json:"response"`
	Done      bool   `json:"done"`
}

// LLMAnalysisResult represents the parsed JSON output from the LLM
type LLMAnalysisResult struct {
	Occupied   bool    `json:"occupied"`
	Confidence float64 `json:"confidence"`
	Reasoning  string  `json:"reasoning"`
}

// AnalyzeWithLLM performs occupancy analysis using the configured LLM
func AnalyzeWithLLM(
	ctx context.Context,
	location string,
	abstraction *TemporalAbstraction,
	stabilization StabilizationResult,
	cfg *config.Config,
	logger *slog.Logger,
) (AnalysisResult, error) {
	// Build the prompt
	prompt := buildLLMPrompt(location, abstraction, stabilization)

	// Log prompt if debug level
	logger.Debug("LLM prompt", "location", location, "prompt", prompt)

	// Create request
	req := LLMRequest{
		Model:  cfg.LLMModel,
		Prompt: prompt,
		Stream: false,
		Format: "json",
	}
	req.Options.Temperature = 0.1 // Low temperature for deterministic output

	// Serialize request
	reqBody, err := json.Marshal(req)
	if err != nil {
		return AnalysisResult{}, fmt.Errorf("failed to marshal LLM request: %w", err)
	}

	// Create HTTP request with timeout
	httpCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(httpCtx, "POST", cfg.LLMEndpoint, bytes.NewBuffer(reqBody))
	if err != nil {
		return AnalysisResult{}, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// Send request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return AnalysisResult{}, fmt.Errorf("LLM request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return AnalysisResult{}, fmt.Errorf("LLM returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var llmResp LLMResponse
	if err := json.NewDecoder(resp.Body).Decode(&llmResp); err != nil {
		return AnalysisResult{}, fmt.Errorf("failed to decode LLM response: %w", err)
	}

	// Parse the LLM's JSON output
	var llmResult LLMAnalysisResult
	if err := json.Unmarshal([]byte(llmResp.Response), &llmResult); err != nil {
		logger.Warn("Failed to parse LLM JSON output", "location", location, "response", llmResp.Response, "error", err)
		return AnalysisResult{}, fmt.Errorf("failed to parse LLM JSON output: %w", err)
	}

	// Validate and clamp confidence
	confidence := llmResult.Confidence
	if confidence < 0.1 {
		confidence = 0.1
	}
	if confidence > 0.99 {
		confidence = 0.99
	}

	// Add stabilization context to reasoning if applied
	reasoning := llmResult.Reasoning
	if stabilization.ShouldDampen {
		reasoning = fmt.Sprintf("%s (V-H stabilization: %s)", reasoning, stabilization.Recommendation)
	}

	logger.Debug("LLM analysis complete", "location", location, "occupied", llmResult.Occupied, "confidence", confidence)

	return AnalysisResult{
		Occupied:   llmResult.Occupied,
		Confidence: confidence,
		Reasoning:  reasoning,
	}, nil
}

// buildLLMPrompt constructs the prompt for the LLM
func buildLLMPrompt(location string, abstraction *TemporalAbstraction, stabilization StabilizationResult) string {
	prompt := fmt.Sprintf(`You are an occupancy detection system analyzing motion sensor data for location: %s

CURRENT DATA:
- Minutes since last motion: %.1f
- Motion in last 2 min (0-2): %d events (%s)
- Motion in 2-8 min window: %d events (%s)
- Motion in 8-20 min window: %d events (%s)
- Motion in 20-60 min window: %d events (%s)
- Time of day: %s

DECISION PATTERNS:

Pattern 1 - Active Presence:
- Motion in last 2 minutes (active_motion or recent_motion)
→ Decision: OCCUPIED (confidence: 0.8-0.9)
→ Reasoning: Someone is currently moving

Pattern 2 - Pass-Through:
- Total 1-2 motion events, quiet for 5+ minutes
- Labels: single_motion, pass_through, brief_visit
→ Decision: EMPTY (confidence: 0.7-0.8)
→ Reasoning: Single motion event, person left

Pattern 3 - Settling In:
- Multiple motions (3+) in recent windows, now quiet < 8 min
- Labels: continuous_activity, periodic_motion in 2-8min, but no_motion in 0-2min
→ Decision: OCCUPIED (confidence: 0.6-0.8)
→ Reasoning: Person entered, now sitting still (working/reading)

Pattern 4 - Extended Absence:
- No motion for 10+ minutes
- Labels: no_motion, empty, unused
→ Decision: EMPTY (confidence: 0.8-0.9)
→ Reasoning: Long time since any activity

`,
		location,
		abstraction.CurrentState.MinutesSinceLastMotion,
		abstraction.MotionDensity.Last2Min, abstraction.TemporalPatterns.Last2Min,
		abstraction.MotionDensity.Last8Min, abstraction.TemporalPatterns.Last8Min,
		abstraction.MotionDensity.Last20Min, abstraction.TemporalPatterns.Last20Min,
		abstraction.MotionDensity.Last60Min, abstraction.TemporalPatterns.Last60Min,
		abstraction.EnvironmentalSignals.TimeOfDay,
	)

	// Add stabilization guidance if needed
	if stabilization.ShouldDampen {
		prompt += fmt.Sprintf(`
STABILIZATION NOTICE:
System has detected instability (oscillation count: %d, variance: %.2f).
Recommendation: %s
Be more conservative with state changes. Only predict state changes with HIGH confidence (0.7+).
When uncertain, favor maintaining the current interpretation.

`,
			stabilization.OscillationCount,
			stabilization.VarianceFactor,
			stabilization.Recommendation,
		)
	}

	prompt += `YOUR TASK:
Analyze the motion pattern and determine if the location is currently occupied.
Consider the temporal patterns and semantic labels to understand the behavior.

Respond with ONLY valid JSON in this exact format:
{
  "occupied": true or false,
  "confidence": 0.0 to 1.0,
  "reasoning": "brief explanation"
}

JSON response:`

	return prompt
}

// AnalyzeWithFallback performs analysis with automatic fallback on LLM failure
func AnalyzeWithFallback(
	ctx context.Context,
	location string,
	abstraction *TemporalAbstraction,
	stabilization StabilizationResult,
	cfg *config.Config,
	logger *slog.Logger,
) AnalysisResult {
	// Try LLM first
	result, err := AnalyzeWithLLM(ctx, location, abstraction, stabilization, cfg, logger)
	if err != nil {
		logger.Warn("LLM analysis failed, using deterministic fallback",
			"location", location,
			"error", err)
		// Use deterministic fallback
		return FallbackAnalysis(abstraction, stabilization)
	}

	// Clamp confidence to safe range
	result.Confidence = math.Max(0.1, math.Min(0.99, result.Confidence))

	return result
}
