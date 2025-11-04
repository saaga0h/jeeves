package embedding

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"

	"github.com/pgvector/pgvector-go"
)

// LocationEmbeddingStorage manages location embeddings with DB caching
type LocationEmbeddingStorage struct {
	db         *sql.DB
	classifier *LocationClassifier
	logger     *slog.Logger

	// In-memory cache for fast access
	cache      map[string][]float32
	cacheMutex sync.RWMutex
}

// LocationEmbeddingData represents stored embedding data
type LocationEmbeddingData struct {
	Location                 string
	Embedding                []float32
	PrivacyLevel             string
	FunctionType             string
	MovementIntensity        string
	SocialContext            string
	ClassificationConfidence float64
	ClassifiedBy             string
	LLMReasoning             string
}

// NewLocationEmbeddingStorage creates a new storage instance
func NewLocationEmbeddingStorage(db *sql.DB, classifier *LocationClassifier, logger *slog.Logger) *LocationEmbeddingStorage {
	return &LocationEmbeddingStorage{
		db:         db,
		classifier: classifier,
		logger:     logger,
		cache:      make(map[string][]float32),
	}
}

// GetEmbedding retrieves embedding for a location (cache → DB → LLM)
func (s *LocationEmbeddingStorage) GetEmbedding(ctx context.Context, location string) ([]float32, error) {
	// Check in-memory cache first
	s.cacheMutex.RLock()
	if embedding, exists := s.cache[location]; exists {
		s.cacheMutex.RUnlock()
		s.logger.Debug("Location embedding found in cache", "location", location)
		return embedding, nil
	}
	s.cacheMutex.RUnlock()

	// Check database
	embedding, err := s.loadFromDB(ctx, location)
	if err == nil {
		// Cache it for future use
		s.cacheMutex.Lock()
		s.cache[location] = embedding
		s.cacheMutex.Unlock()

		s.logger.Debug("Location embedding loaded from DB", "location", location)

		// Update usage statistics asynchronously
		go s.updateUsageStats(location)

		return embedding, nil
	}

	if err != sql.ErrNoRows {
		s.logger.Warn("Database query failed", "location", location, "error", err)
	}

	// Not in cache or DB - classify with LLM
	s.logger.Info("Location not found, classifying with LLM", "location", location)

	result, err := s.classifier.ClassifyLocation(ctx, location)
	if err != nil {
		s.logger.Warn("LLM classification failed, using fallback",
			"location", location,
			"error", err)

		// Use fallback embedding
		fallback := GenerateFallbackEmbedding(location)

		// Store fallback in DB and cache
		data := LocationEmbeddingData{
			Location:                 location,
			Embedding:                fallback,
			PrivacyLevel:             "shared",
			FunctionType:             "utility",
			MovementIntensity:        "medium",
			SocialContext:            "family",
			ClassificationConfidence: 0.0,
			ClassifiedBy:             "fallback",
			LLMReasoning:             fmt.Sprintf("LLM classification failed: %v", err),
		}

		if err := s.storeInDB(ctx, data); err != nil {
			s.logger.Warn("Failed to store fallback embedding", "error", err)
		}

		s.cacheMutex.Lock()
		s.cache[location] = fallback
		s.cacheMutex.Unlock()

		return fallback, nil
	}

	// Store LLM result in DB and cache
	data := LocationEmbeddingData{
		Location:                 location,
		Embedding:                result.Embedding,
		PrivacyLevel:             result.Labels.PrivacyLevel,
		FunctionType:             result.Labels.FunctionType,
		MovementIntensity:        result.Labels.MovementIntensity,
		SocialContext:            result.Labels.SocialContext,
		ClassificationConfidence: result.Confidence,
		ClassifiedBy:             "llm",
		LLMReasoning:             result.Reasoning,
	}

	if err := s.storeInDB(ctx, data); err != nil {
		s.logger.Warn("Failed to store LLM embedding in DB", "error", err)
		// Continue anyway - we have the embedding in memory
	}

	s.cacheMutex.Lock()
	s.cache[location] = result.Embedding
	s.cacheMutex.Unlock()

	return result.Embedding, nil
}

// PreloadCache loads all embeddings from DB into memory cache
func (s *LocationEmbeddingStorage) PreloadCache(ctx context.Context) error {
	query := `SELECT location, embedding FROM location_embeddings`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query embeddings: %w", err)
	}
	defer rows.Close()

	count := 0
	s.cacheMutex.Lock()
	defer s.cacheMutex.Unlock()

	for rows.Next() {
		var location string
		var embeddingBytes []byte

		if err := rows.Scan(&location, &embeddingBytes); err != nil {
			s.logger.Warn("Failed to scan embedding row", "error", err)
			continue
		}

		// Parse pgvector bytes to float32 slice
		var embedding pgvector.Vector
		if err := embedding.Scan(embeddingBytes); err != nil {
			s.logger.Warn("Failed to parse embedding vector",
				"location", location,
				"error", err)
			continue
		}

		s.cache[location] = embedding.Slice()
		count++
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating embeddings: %w", err)
	}

	s.logger.Info("Location embeddings cache preloaded",
		"count", count)

	return nil
}

// loadFromDB retrieves embedding from database
func (s *LocationEmbeddingStorage) loadFromDB(ctx context.Context, location string) ([]float32, error) {
	query := `SELECT embedding FROM location_embeddings WHERE location = $1`

	var embeddingBytes []byte
	err := s.db.QueryRowContext(ctx, query, location).Scan(&embeddingBytes)
	if err != nil {
		return nil, err
	}

	// Parse pgvector bytes to float32 slice
	var embedding pgvector.Vector
	if err := embedding.Scan(embeddingBytes); err != nil {
		return nil, fmt.Errorf("failed to parse embedding vector: %w", err)
	}

	return embedding.Slice(), nil
}

// storeInDB stores embedding in database
func (s *LocationEmbeddingStorage) storeInDB(ctx context.Context, data LocationEmbeddingData) error {
	query := `
		INSERT INTO location_embeddings (
			location, embedding, privacy_level, function_type,
			movement_intensity, social_context, classification_confidence,
			classified_by, llm_reasoning
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (location) DO UPDATE SET
			embedding = EXCLUDED.embedding,
			privacy_level = EXCLUDED.privacy_level,
			function_type = EXCLUDED.function_type,
			movement_intensity = EXCLUDED.movement_intensity,
			social_context = EXCLUDED.social_context,
			classification_confidence = EXCLUDED.classification_confidence,
			classified_by = EXCLUDED.classified_by,
			llm_reasoning = EXCLUDED.llm_reasoning,
			updated_at = NOW()
	`

	// Convert float32 slice to pgvector
	embedding := pgvector.NewVector(data.Embedding)

	_, err := s.db.ExecContext(ctx, query,
		data.Location,
		embedding,
		data.PrivacyLevel,
		data.FunctionType,
		data.MovementIntensity,
		data.SocialContext,
		data.ClassificationConfidence,
		data.ClassifiedBy,
		data.LLMReasoning,
	)

	if err != nil {
		return fmt.Errorf("failed to insert/update embedding: %w", err)
	}

	s.logger.Debug("Location embedding stored in DB",
		"location", data.Location,
		"classified_by", data.ClassifiedBy)

	return nil
}

// updateUsageStats increments usage counter for a location
func (s *LocationEmbeddingStorage) updateUsageStats(location string) {
	ctx := context.Background()
	query := `SELECT update_location_embedding_usage($1)`

	if _, err := s.db.ExecContext(ctx, query, location); err != nil {
		s.logger.Debug("Failed to update usage stats",
			"location", location,
			"error", err)
	}
}

// GetCacheSize returns the number of cached embeddings
func (s *LocationEmbeddingStorage) GetCacheSize() int {
	s.cacheMutex.RLock()
	defer s.cacheMutex.RUnlock()
	return len(s.cache)
}

// ClearCache clears the in-memory cache
func (s *LocationEmbeddingStorage) ClearCache() {
	s.cacheMutex.Lock()
	defer s.cacheMutex.Unlock()
	s.cache = make(map[string][]float32)
	s.logger.Info("Location embeddings cache cleared")
}
