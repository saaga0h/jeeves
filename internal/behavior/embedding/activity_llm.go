package embedding

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/saaga0h/jeeves-platform/pkg/llm"
)

// ActivityLLMEmbeddingGenerator generates activity embeddings using LLM
type ActivityLLMEmbeddingGenerator struct {
	llm    llm.Client
	model  string
	logger *slog.Logger
}

// NewActivityLLMEmbeddingGenerator creates a new LLM-based embedding generator
func NewActivityLLMEmbeddingGenerator(
	llmClient llm.Client,
	model string,
	logger *slog.Logger,
) *ActivityLLMEmbeddingGenerator {
	return &ActivityLLMEmbeddingGenerator{
		llm:    llmClient,
		model:  model,
		logger: logger,
	}
}

// GenerateActivityEmbedding generates a 20-dimensional activity embedding via LLM
func (g *ActivityLLMEmbeddingGenerator) GenerateActivityEmbedding(
	ctx context.Context,
	fingerprint ActivityFingerprint,
) ([]float32, error) {
	prompt := g.buildPrompt(fingerprint)

	g.logger.Info("Generating activity embedding via LLM",
		"fingerprint", fingerprint.Hash(),
		"description", fingerprint.Describe())

	// Create LLM request
	req := llm.GenerateRequest{
		Model:  g.model,
		Prompt: prompt,
		Format: "json",
		Stream: false,
	}

	// Call LLM
	response, err := g.llm.Generate(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("LLM generation failed: %w", err)
	}

	// Parse response
	var result struct {
		Embedding []float32 `json:"embedding"`
		Reasoning string    `json:"reasoning"`
	}

	if err := json.Unmarshal([]byte(response.Response), &result); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response: %w", err)
	}

	if len(result.Embedding) != 20 {
		return nil, fmt.Errorf("invalid embedding size: got %d, expected 20", len(result.Embedding))
	}

	g.logger.Info("Activity embedding generated",
		"fingerprint", fingerprint.Hash(),
		"reasoning", result.Reasoning)

	return result.Embedding, nil
}

// buildPrompt constructs the LLM prompt for activity embedding generation
func (g *ActivityLLMEmbeddingGenerator) buildPrompt(fp ActivityFingerprint) string {
	return fmt.Sprintf(`You are an expert in behavioral pattern recognition and semantic embeddings.

Your task is to generate a 20-dimensional activity embedding vector for the following activity pattern:

Activity Description: %s

The embedding should capture the semantic meaning of this activity across these dimensions:
- [0-2]: Physical activity level (sedentary to highly active)
- [3-5]: Social engagement level (solitary to highly social)
- [6-8]: Cognitive load (passive to intensive focus)
- [9-11]: Creative vs. routine (routine to creative)
- [12-14]: Entertainment vs. productivity (pure entertainment to pure productivity)
- [15-17]: Technology engagement (no tech to high tech)
- [18-19]: Energy level (low energy to high energy)

Requirements:
1. Each dimension should be in the range [0, 1]
2. The embedding should be semantically meaningful and consistent across similar activities
3. Different activity types (e.g., watching TV vs. working) should have distinctly different embeddings
4. Return a 20-element array of float32 values

Example embeddings for reference:
- Watching TV (evening, weekday): high entertainment [12-14]~0.9, low productivity [12-14]~0.1, medium tech [15-17]~0.6
- Working (evening, weekday): low entertainment [12-14]~0.2, high productivity [12-14]~0.8, high cognitive load [6-8]~0.8
- Reading (evening, weekend): medium cognitive load [6-8]~0.5, low social [3-5]~0.2, low tech [15-17]~0.2

Respond ONLY with valid JSON in this format:
{
  "embedding": [0.1, 0.2, ..., 0.9],
  "reasoning": "Brief explanation of key dimension values"
}`, fp.Describe())
}
