-- e2e/init-scripts/01_init_schema.sql

-- Enable extensions
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Behavioral episodes table
CREATE TABLE behavioral_episodes (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    
    -- Full JSON-LD document
    jsonld JSONB NOT NULL,
    
    -- Generated columns for querying (simple text extraction - immutable)
    activity_type TEXT GENERATED ALWAYS AS (
        jsonld->'adl:activity'->>'@type'
    ) STORED,
    
    started_at_text TEXT GENERATED ALWAYS AS (
        jsonld->>'jeeves:startedAt'
    ) STORED,
    
    ended_at_text TEXT GENERATED ALWAYS AS (
        jsonld->>'jeeves:endedAt'
    ) STORED,
    
    location TEXT GENERATED ALWAYS AS (
        jsonld->'adl:activity'->'adl:location'->>'name'
    ) STORED,
    
    -- Metadata
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Indexes for common queries
CREATE INDEX idx_episodes_activity ON behavioral_episodes(activity_type);
CREATE INDEX idx_episodes_location ON behavioral_episodes(location);
CREATE INDEX idx_episodes_started ON behavioral_episodes(started_at_text DESC);

-- Full JSONB GIN index for arbitrary queries
CREATE INDEX idx_episodes_jsonb ON behavioral_episodes USING GIN (jsonld);

-- Success message
DO $$
BEGIN
    RAISE NOTICE 'J.E.E.V.E.S. behavior database schema initialized successfully';
END $$;