package embedding

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/saaga0h/jeeves-platform/pkg/llm"
)

// LocationClassifier uses LLM to classify locations into semantic embeddings
type LocationClassifier struct {
	llmClient llm.Client
	model     string
	logger    *slog.Logger
}

// LocationClassificationResult contains the LLM classification output
type LocationClassificationResult struct {
	Embedding  []float32          `json:"embedding"`
	Labels     LocationLabels     `json:"labels"`
	Confidence float64            `json:"confidence"`
	Reasoning  string             `json:"reasoning"`
}

// LocationLabels contains human-readable classification labels
type LocationLabels struct {
	PrivacyLevel       string `json:"privacy_level"`       // private, shared, public
	FunctionType       string `json:"function_type"`       // rest, work, leisure, utility
	MovementIntensity  string `json:"movement_intensity"`  // low, medium, high
	SocialContext      string `json:"social_context"`      // solitary, family, social
}

// NewLocationClassifier creates a new location classifier
func NewLocationClassifier(llmClient llm.Client, model string, logger *slog.Logger) *LocationClassifier {
	return &LocationClassifier{
		llmClient: llmClient,
		model:     model,
		logger:    logger,
	}
}

// ClassifyLocation uses LLM to generate semantic embedding for a location
func (c *LocationClassifier) ClassifyLocation(ctx context.Context, location string) (*LocationClassificationResult, error) {
	prompt := c.buildClassificationPrompt(location)

	c.logger.Debug("Classifying location with LLM",
		"location", location,
		"model", c.model)

	req := llm.GenerateRequest{
		Model:  c.model,
		Prompt: prompt,
		Format: "json",
	}

	response, err := c.llmClient.Generate(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("LLM generation failed: %w", err)
	}

	// Parse JSON response
	result, err := c.parseResponse(response.Response)
	if err != nil {
		c.logger.Warn("Failed to parse LLM response, trying to extract JSON",
			"location", location,
			"error", err,
			"response_preview", truncate(response.Response, 200))

		// Try to extract JSON from markdown code blocks or other formatting
		result, err = c.extractAndParseJSON(response.Response)
		if err != nil {
			return nil, fmt.Errorf("failed to parse LLM response: %w", err)
		}
	}

	// Validate embedding dimensions
	if len(result.Embedding) != 16 {
		return nil, fmt.Errorf("invalid embedding dimension: got %d, expected 16", len(result.Embedding))
	}

	// Normalize embedding values to [0, 1] range
	for i := range result.Embedding {
		if result.Embedding[i] < 0.0 {
			result.Embedding[i] = 0.0
		}
		if result.Embedding[i] > 1.0 {
			result.Embedding[i] = 1.0
		}
	}

	c.logger.Info("Location classified successfully",
		"location", location,
		"privacy", result.Labels.PrivacyLevel,
		"function", result.Labels.FunctionType,
		"confidence", result.Confidence)

	return result, nil
}

// buildClassificationPrompt creates the LLM prompt for location classification
func (c *LocationClassifier) buildClassificationPrompt(location string) string {
	return fmt.Sprintf(`You are a smart home location classifier. Classify this location: "%s"

Analyze the location name and provide semantic dimensions as a 16-dimensional embedding vector:

**Dimension Guidelines:**

Dimensions 0 (Privacy Level, single value 0.0-1.0):
- 0.0-0.3: Public spaces (entryway, garage, patio, porch)
- 0.4-0.6: Shared spaces (kitchen, living room, dining room, family room)
- 0.7-1.0: Private spaces (bedroom, bathroom, closet, office)

Dimensions 1-4 (Function Type, 0.0-1.0 each):
- [1] Rest: Sleeping, relaxation (bedroom, reading nook, quiet areas)
- [2] Work: Productive activities (office, kitchen prep, workshop, study)
- [3] Leisure: Entertainment, hobbies (living room, game room, patio, media room)
- [4] Utility: Functional tasks (bathroom, laundry, pantry, garage)

Dimensions 5-7 (Movement Intensity, 0.0-1.0 each):
- [5] Low: Minimal movement (bedroom during sleep, reading area, meditation)
- [6] Medium: Moderate activity (bathroom routines, casual cooking)
- [7] High: Intense activity (kitchen during meal prep, gym, workshop)

Dimensions 8-10 (Social Context, 0.0-1.0 each):
- [8] Solitary: Typically used alone (bedroom, bathroom, personal office)
- [9] Family: Shared with household (kitchen, dining, family room)
- [10] Social: Guest-friendly (living room, patio, entryway, guest areas)

Dimensions 11-15 (Location-specific features, 0.0-1.0 each):
- [11] Comfort level (soft furnishings, cozy atmosphere)
- [12] Activity level (frequency of use, energy)
- [13] Cleanliness requirements (hygiene importance)
- [14] Technology presence (electronics, smart devices)

**Important:** Return ONLY valid JSON in this exact format, no markdown, no code blocks:

{
  "embedding": [privacy, rest, work, leisure, utility, move_low, move_med, move_high, sol, fam, soc, comfort, activity, clean, tech, reserved],
  "labels": {
    "privacy_level": "private|shared|public",
    "function_type": "rest|work|leisure|utility",
    "movement_intensity": "low|medium|high",
    "social_context": "solitary|family|social"
  },
  "confidence": 0.85,
  "reasoning": "Brief explanation of classification"
}`, location)
}

// parseResponse parses the LLM JSON response
func (c *LocationClassifier) parseResponse(response string) (*LocationClassificationResult, error) {
	var result LocationClassificationResult
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// extractAndParseJSON attempts to extract JSON from formatted text (markdown, code blocks, etc.)
func (c *LocationClassifier) extractAndParseJSON(response string) (*LocationClassificationResult, error) {
	// Try to find JSON in markdown code blocks
	if strings.Contains(response, "```json") {
		start := strings.Index(response, "```json") + 7
		end := strings.Index(response[start:], "```")
		if end > 0 {
			jsonStr := response[start : start+end]
			return c.parseResponse(strings.TrimSpace(jsonStr))
		}
	}

	// Try to find JSON in generic code blocks
	if strings.Contains(response, "```") {
		start := strings.Index(response, "```") + 3
		end := strings.Index(response[start:], "```")
		if end > 0 {
			jsonStr := response[start : start+end]
			return c.parseResponse(strings.TrimSpace(jsonStr))
		}
	}

	// Try to find JSON object by looking for { and }
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")
	if start >= 0 && end > start {
		jsonStr := response[start : end+1]
		return c.parseResponse(jsonStr)
	}

	return nil, fmt.Errorf("could not extract JSON from response")
}

// GenerateFallbackEmbedding creates a neutral embedding for unknown locations
func GenerateFallbackEmbedding(location string) []float32 {
	// Neutral embedding: moderate values for all dimensions
	embedding := make([]float32, 16)
	for i := range embedding {
		embedding[i] = 0.5
	}
	return embedding
}

// truncate helper for logging
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
