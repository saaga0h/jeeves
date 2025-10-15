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

-- Macro-episodes table (consolidated from micro-episodes)
CREATE TABLE macro_episodes (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    
    -- Pattern information
    pattern_type TEXT NOT NULL,  -- e.g., "WatchingMovie", "WorkSession"
    
    -- Time range (virtual time aware)
    start_time TIMESTAMPTZ NOT NULL,
    end_time TIMESTAMPTZ NOT NULL,
    duration_minutes INT NOT NULL,
    
    -- Locations involved
    locations TEXT[] NOT NULL,  -- Array of location names
    
    -- References to micro-episodes
    micro_episode_ids UUID[] NOT NULL,  -- Array of micro-episode IDs
    
    -- Semantic summary
    summary TEXT,
    semantic_tags TEXT[],  -- e.g., ["WatchingMovie", "living_room", "evening"]
    
    -- Context features (for future pattern detection)
    context_features JSONB,  -- { manual_action_count: 3, ... }
    
    -- Vector embedding for RAG (future)
    embedding vector(1536),
    
    -- Metadata
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Indexes
CREATE INDEX idx_macro_pattern ON macro_episodes(pattern_type);
CREATE INDEX idx_macro_start ON macro_episodes(start_time DESC);
CREATE INDEX idx_macro_locations ON macro_episodes USING GIN(locations);
CREATE INDEX idx_macro_tags ON macro_episodes USING GIN(semantic_tags);
CREATE INDEX idx_macro_micro_ids ON macro_episodes USING GIN(micro_episode_ids);

-- Success message
DO $$
BEGIN
    RAISE NOTICE 'J.E.E.V.E.S. behavior database schema initialized successfully';
END $$;