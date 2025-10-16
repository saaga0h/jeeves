package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// Client is the interface for LLM interactions
type Client interface {
	// Generate sends a prompt and returns structured JSON response
	Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error)

	// Health checks if the LLM service is available
	Health(ctx context.Context) error
}

// GenerateRequest represents a request to the LLM
type GenerateRequest struct {
	Model     string                 `json:"model"`
	Prompt    string                 `json:"prompt"`
	System    string                 `json:"system,omitempty"`     // System prompt (optional)
	Format    string                 `json:"format,omitempty"`     // "json" for structured output
	Stream    bool                   `json:"stream"`               // Always false for now
	Options   map[string]interface{} `json:"options,omitempty"`    // Model-specific options
	KeepAlive string                 `json:"keep_alive,omitempty"` // Keep model loaded
}

// GenerateResponse represents the LLM's response
type GenerateResponse struct {
	Model              string    `json:"model"`
	CreatedAt          time.Time `json:"created_at"`
	Response           string    `json:"response"` // Raw text response
	Done               bool      `json:"done"`
	Context            []int     `json:"context,omitempty"`
	TotalDuration      int64     `json:"total_duration"` // nanoseconds
	LoadDuration       int64     `json:"load_duration"`  // nanoseconds
	PromptEvalCount    int       `json:"prompt_eval_count"`
	PromptEvalDuration int64     `json:"prompt_eval_duration"` // nanoseconds
	EvalCount          int       `json:"eval_count"`
	EvalDuration       int64     `json:"eval_duration"` // nanoseconds
}

// ollamaClient implements Client for Ollama API
type ollamaClient struct {
	baseURL    string
	httpClient *http.Client
	logger     *slog.Logger
}

// NewOllamaClient creates a new Ollama LLM client
func NewOllamaClient(baseURL string, logger *slog.Logger) Client {
	return &ollamaClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 60 * time.Second, // Generous timeout for LLM
		},
		logger: logger,
	}
}

// Generate sends a prompt to Ollama and returns the response
func (c *ollamaClient) Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
	startTime := time.Now()

	// Validate request
	if req.Model == "" {
		return nil, fmt.Errorf("model is required")
	}
	if req.Prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}

	// Force non-streaming
	req.Stream = false

	// Serialize request
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	c.logger.Debug("LLM request",
		"model", req.Model,
		"prompt_length", len(req.Prompt),
		"format", req.Format)

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		c.baseURL+"/api/generate",
		bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check status
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("LLM returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var genResp GenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&genResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	duration := time.Since(startTime)

	c.logger.Info("LLM response received",
		"model", req.Model,
		"duration_ms", duration.Milliseconds(),
		"eval_count", genResp.EvalCount,
		"response_length", len(genResp.Response))

	return &genResp, nil
}

// Health checks if Ollama is available
func (c *ollamaClient) Health(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, "GET",
		c.baseURL+"/api/tags", nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned status %d", resp.StatusCode)
	}

	return nil
}

// WithTimeout returns a new context with timeout for LLM operations
func WithTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, timeout)
}

// DefaultGenerateRequest creates a request with sensible defaults
func DefaultGenerateRequest(model, prompt string) GenerateRequest {
	return GenerateRequest{
		Model:  model,
		Prompt: prompt,
		Format: "json",
		Stream: false,
		Options: map[string]interface{}{
			"temperature": 0.1, // Low for deterministic output
			"top_p":       0.9,
			"top_k":       40,
		},
		KeepAlive: "5m", // Keep model loaded for 5 minutes
	}
}

// ===================================================================
// pkg/llm/structured.go - Helper for structured JSON responses
// ===================================================================

// ParseJSONResponse parses the LLM's JSON response into a target type
func ParseJSONResponse[T any](resp *GenerateResponse) (*T, error) {
	var result T

	if err := json.Unmarshal([]byte(resp.Response), &result); err != nil {
		return nil, fmt.Errorf("failed to parse LLM JSON: %w (response: %s)",
			err, resp.Response)
	}

	return &result, nil
}

// ValidateJSONResponse checks if response is valid JSON without parsing
func ValidateJSONResponse(resp *GenerateResponse) error {
	var js json.RawMessage
	if err := json.Unmarshal([]byte(resp.Response), &js); err != nil {
		return fmt.Errorf("response is not valid JSON: %w", err)
	}
	return nil
}

// ===================================================================
// pkg/llm/mock.go - Mock client for testing
// ===================================================================

// MockClient is a mock LLM client for testing
type MockClient struct {
	GenerateFunc func(ctx context.Context, req GenerateRequest) (*GenerateResponse, error)
	HealthFunc   func(ctx context.Context) error
}

func (m *MockClient) Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
	if m.GenerateFunc != nil {
		return m.GenerateFunc(ctx, req)
	}
	// Default mock response
	return &GenerateResponse{
		Model:     req.Model,
		Response:  `{"result": "mock"}`,
		Done:      true,
		CreatedAt: time.Now(),
	}, nil
}

func (m *MockClient) Health(ctx context.Context) error {
	if m.HealthFunc != nil {
		return m.HealthFunc(ctx)
	}
	return nil
}

// NewMockClient creates a mock client with default behavior
func NewMockClient() *MockClient {
	return &MockClient{}
}

// ===================================================================
// pkg/llm/analyzer.go - Generic analyzer pattern
// ===================================================================

// Analyzer is a generic interface for domain-specific LLM analysis
type Analyzer[TInput any, TOutput any] interface {
	// BuildPrompt creates the LLM prompt from input data
	BuildPrompt(input TInput) string

	// ParseResponse extracts structured output from LLM response
	ParseResponse(response string) (TOutput, error)

	// Validate checks if the output meets domain constraints
	Validate(output TOutput) error
}

// Analyze is a generic helper that combines prompt building, LLM call, and parsing
func Analyze[TInput any, TOutput any](
	ctx context.Context,
	client Client,
	analyzer Analyzer[TInput, TOutput],
	model string,
	input TInput,
	logger *slog.Logger,
) (TOutput, error) {
	var zero TOutput

	// Build prompt
	prompt := analyzer.BuildPrompt(input)

	logger.Debug("Building LLM prompt", "prompt_length", len(prompt))

	// Create request
	req := DefaultGenerateRequest(model, prompt)

	// Call LLM
	resp, err := client.Generate(ctx, req)
	if err != nil {
		return zero, fmt.Errorf("LLM generate failed: %w", err)
	}

	// Parse response
	output, err := analyzer.ParseResponse(resp.Response)
	if err != nil {
		logger.Error("Failed to parse LLM response",
			"response", resp.Response,
			"error", err)
		return zero, fmt.Errorf("parse response failed: %w", err)
	}

	// Validate
	if err := analyzer.Validate(output); err != nil {
		return zero, fmt.Errorf("validation failed: %w", err)
	}

	logger.Debug("LLM analysis complete",
		"eval_count", resp.EvalCount,
		"duration_ms", resp.TotalDuration/1_000_000)

	return output, nil
}

// ===================================================================
// pkg/llm/metrics.go - Observability helpers
// ===================================================================

// Metrics tracks LLM usage statistics
type Metrics struct {
	TotalRequests    int64
	TotalTokens      int64
	TotalDurationMs  int64
	AverageLatencyMs float64
	ErrorCount       int64
}

// MetricsCollector collects LLM usage metrics
type MetricsCollector struct {
	metrics Metrics
	logger  *slog.Logger
}

// NewMetricsCollector creates a new metrics collector
func NewMetricsCollector(logger *slog.Logger) *MetricsCollector {
	return &MetricsCollector{
		logger: logger,
	}
}

// Record records metrics from a response
func (mc *MetricsCollector) Record(resp *GenerateResponse) {
	mc.metrics.TotalRequests++
	mc.metrics.TotalTokens += int64(resp.EvalCount + resp.PromptEvalCount)
	mc.metrics.TotalDurationMs += resp.TotalDuration / 1_000_000

	if mc.metrics.TotalRequests > 0 {
		mc.metrics.AverageLatencyMs = float64(mc.metrics.TotalDurationMs) / float64(mc.metrics.TotalRequests)
	}
}

// RecordError records an error
func (mc *MetricsCollector) RecordError() {
	mc.metrics.ErrorCount++
}

// GetMetrics returns current metrics
func (mc *MetricsCollector) GetMetrics() Metrics {
	return mc.metrics
}

// LogMetrics logs current metrics
func (mc *MetricsCollector) LogMetrics() {
	mc.logger.Info("LLM metrics",
		"total_requests", mc.metrics.TotalRequests,
		"total_tokens", mc.metrics.TotalTokens,
		"avg_latency_ms", mc.metrics.AverageLatencyMs,
		"error_count", mc.metrics.ErrorCount)
}
