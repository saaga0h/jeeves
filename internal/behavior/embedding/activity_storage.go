package embedding

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/lib/pq"
)

// ActivityEmbeddingStorageImpl implements ActivityEmbeddingStorage using PostgreSQL
type ActivityEmbeddingStorageImpl struct {
	db *sql.DB
}

// NewActivityEmbeddingStorage creates a new activity embedding storage
func NewActivityEmbeddingStorage(db *sql.DB) *ActivityEmbeddingStorageImpl {
	return &ActivityEmbeddingStorageImpl{db: db}
}

// GetActivityEmbedding retrieves a cached activity embedding by fingerprint hash
func (s *ActivityEmbeddingStorageImpl) GetActivityEmbedding(
	ctx context.Context,
	fingerprintHash string,
) ([]float32, error) {
	query := `
		SELECT embedding
		FROM activity_embeddings
		WHERE fingerprint_hash = $1
	`

	var embeddingArray pq.Float32Array
	err := s.db.QueryRowContext(ctx, query, fingerprintHash).Scan(&embeddingArray)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("activity embedding not found")
		}
		return nil, fmt.Errorf("failed to query activity embedding: %w", err)
	}

	return []float32(embeddingArray), nil
}

// StoreActivityEmbedding stores a new activity embedding in the cache
func (s *ActivityEmbeddingStorageImpl) StoreActivityEmbedding(
	ctx context.Context,
	fingerprintHash string,
	fingerprint ActivityFingerprint,
	embedding []float32,
) error {
	fingerprintJSON, err := json.Marshal(fingerprint)
	if err != nil {
		return fmt.Errorf("failed to marshal fingerprint: %w", err)
	}

	query := `
		INSERT INTO activity_embeddings (
			fingerprint_hash,
			fingerprint,
			embedding,
			usage_count,
			created_at,
			last_used_at
		) VALUES ($1, $2, $3, 1, NOW(), NOW())
		ON CONFLICT (fingerprint_hash)
		DO UPDATE SET
			usage_count = activity_embeddings.usage_count + 1,
			last_used_at = NOW()
	`

	_, err = s.db.ExecContext(
		ctx,
		query,
		fingerprintHash,
		fingerprintJSON,
		pq.Array(embedding),
	)
	if err != nil {
		return fmt.Errorf("failed to store activity embedding: %w", err)
	}

	return nil
}

// GetActivityEmbeddingStats retrieves statistics about cached embeddings
func (s *ActivityEmbeddingStorageImpl) GetActivityEmbeddingStats(ctx context.Context) (map[string]interface{}, error) {
	query := `
		SELECT
			COUNT(*) as total_embeddings,
			SUM(usage_count) as total_uses,
			AVG(usage_count) as avg_uses_per_embedding,
			MAX(usage_count) as max_uses
		FROM activity_embeddings
	`

	var stats struct {
		TotalEmbeddings      int
		TotalUses            int
		AvgUsesPerEmbedding  float64
		MaxUses              int
	}

	err := s.db.QueryRowContext(ctx, query).Scan(
		&stats.TotalEmbeddings,
		&stats.TotalUses,
		&stats.AvgUsesPerEmbedding,
		&stats.MaxUses,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query stats: %w", err)
	}

	return map[string]interface{}{
		"total_embeddings":      stats.TotalEmbeddings,
		"total_uses":            stats.TotalUses,
		"avg_uses_per_embedding": stats.AvgUsesPerEmbedding,
		"max_uses":              stats.MaxUses,
		"cache_hit_potential":   float64(stats.TotalUses-stats.TotalEmbeddings) / float64(stats.TotalUses) * 100,
	}, nil
}
