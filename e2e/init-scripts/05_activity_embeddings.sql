-- Activity Embeddings Cache
-- Stores LLM-generated activity embeddings for progressive learning
-- Each fingerprint represents a unique activity type (location + time_period + day_type + signals)

CREATE TABLE IF NOT EXISTS activity_embeddings (
    fingerprint_hash TEXT PRIMARY KEY,
    fingerprint JSONB NOT NULL,
    embedding REAL[] NOT NULL,
    usage_count INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_activity_embeddings_fingerprint ON activity_embeddings USING gin(fingerprint);
CREATE INDEX IF NOT EXISTS idx_activity_embeddings_usage ON activity_embeddings(usage_count DESC);
CREATE INDEX IF NOT EXISTS idx_activity_embeddings_last_used ON activity_embeddings(last_used_at DESC);

COMMENT ON TABLE activity_embeddings IS 'Cached LLM-generated embeddings for activity patterns';
COMMENT ON COLUMN activity_embeddings.fingerprint_hash IS 'SHA-256 hash of normalized activity fingerprint';
COMMENT ON COLUMN activity_embeddings.fingerprint IS 'Normalized activity pattern (location, time_period, day_type, signals)';
COMMENT ON COLUMN activity_embeddings.embedding IS '20-dimensional activity embedding vector';
COMMENT ON COLUMN activity_embeddings.usage_count IS 'Number of times this embedding has been reused';
COMMENT ON COLUMN activity_embeddings.last_used_at IS 'Last time this embedding was accessed';
