-- Location Embeddings Table
-- Stores semantic embeddings for household locations
-- LLM-based classification cached for reuse

CREATE TABLE IF NOT EXISTS location_embeddings (
    location VARCHAR(255) PRIMARY KEY,
    embedding VECTOR(16) NOT NULL,

    -- Semantic classification (for human readability and queries)
    privacy_level VARCHAR(20),      -- 'private', 'shared', 'public'
    function_type VARCHAR(20),      -- 'rest', 'work', 'leisure', 'utility'
    movement_intensity VARCHAR(20), -- 'low', 'medium', 'high'
    social_context VARCHAR(20),     -- 'solitary', 'family', 'social'

    -- Metadata
    classification_confidence FLOAT CHECK (classification_confidence >= 0 AND classification_confidence <= 1),
    classified_by VARCHAR(50) NOT NULL DEFAULT 'llm',  -- 'llm', 'manual', 'seed'
    classified_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Raw LLM response for debugging
    llm_reasoning TEXT,

    -- Usage statistics
    usage_count INT NOT NULL DEFAULT 0,
    last_used_at TIMESTAMPTZ
);

-- Indexes
CREATE INDEX idx_location_embeddings_type ON location_embeddings(function_type);
CREATE INDEX idx_location_embeddings_privacy ON location_embeddings(privacy_level);
CREATE INDEX idx_location_embeddings_usage ON location_embeddings(usage_count DESC);
CREATE INDEX idx_location_embeddings_updated ON location_embeddings(updated_at DESC);

-- Vector similarity search index
CREATE INDEX idx_location_embeddings_vector ON location_embeddings
USING ivfflat (embedding vector_cosine_ops) WITH (lists = 10);

-- Seed data: Common household locations with predefined embeddings
-- These serve as fallbacks and examples for the LLM to learn from

INSERT INTO location_embeddings (location, embedding, privacy_level, function_type, movement_intensity, social_context, classification_confidence, classified_by, llm_reasoning) VALUES
-- Bedrooms (Private, Rest-focused, Low movement, Solitary)
('bedroom', '[0.9, 1.0, 0.0, 0.0, 0.1, 0.9, 0.1, 0.0, 0.9, 0.1, 0.0, 0.8, 0.2, 0.1, 0.0, 0.0]', 'private', 'rest', 'low', 'solitary', 0.95, 'seed', 'Primary sleeping area, private rest space'),
('master_bedroom', '[0.9, 1.0, 0.0, 0.0, 0.1, 0.9, 0.1, 0.0, 0.9, 0.2, 0.0, 0.8, 0.3, 0.1, 0.0, 0.0]', 'private', 'rest', 'low', 'solitary', 0.95, 'seed', 'Primary bedroom with ensuite, private rest space'),
('guest_bedroom', '[0.8, 1.0, 0.0, 0.2, 0.1, 0.8, 0.2, 0.0, 0.5, 0.3, 0.4, 0.7, 0.2, 0.1, 0.0, 0.0]', 'private', 'rest', 'low', 'social', 0.90, 'seed', 'Guest sleeping area, semi-private'),

-- Bathrooms (Private, Utility, Medium movement, Solitary)
('bathroom', '[0.9, 0.0, 0.0, 0.0, 1.0, 0.1, 0.6, 0.3, 0.9, 0.1, 0.0, 0.5, 0.7, 0.6, 0.4, 0.0]', 'private', 'utility', 'medium', 'solitary', 0.95, 'seed', 'Personal hygiene space, functional and private'),
('master_bathroom', '[0.9, 0.0, 0.0, 0.0, 1.0, 0.1, 0.5, 0.4, 0.9, 0.2, 0.0, 0.6, 0.7, 0.5, 0.4, 0.0]', 'private', 'utility', 'medium', 'solitary', 0.95, 'seed', 'Ensuite bathroom, private utility space'),

-- Kitchen (Shared, Work, High movement, Family)
('kitchen', '[0.1, 0.0, 0.8, 0.2, 0.3, 0.0, 0.2, 0.8, 0.2, 0.8, 0.3, 0.9, 0.8, 0.5, 0.6, 0.0]', 'shared', 'work', 'high', 'family', 0.95, 'seed', 'Food preparation area, high activity and shared space'),

-- Dining Areas (Shared, Leisure, Low movement, Family)
('dining_room', '[0.1, 0.0, 0.0, 0.9, 0.2, 0.9, 0.1, 0.0, 0.1, 0.9, 0.5, 0.8, 0.4, 0.7, 0.3, 0.0]', 'shared', 'leisure', 'low', 'family', 0.95, 'seed', 'Eating and gathering space, shared and social'),
('dining_area', '[0.2, 0.0, 0.0, 0.9, 0.2, 0.9, 0.1, 0.0, 0.2, 0.8, 0.4, 0.7, 0.4, 0.6, 0.3, 0.0]', 'shared', 'leisure', 'low', 'family', 0.90, 'seed', 'Informal dining space within another room'),

-- Living Areas (Shared, Leisure, Low movement, Family/Social)
('living_room', '[0.2, 0.2, 0.0, 0.9, 0.1, 0.8, 0.2, 0.0, 0.1, 0.7, 0.7, 0.6, 0.5, 0.8, 0.4, 0.0]', 'shared', 'leisure', 'low', 'social', 0.95, 'seed', 'Main gathering and entertainment space'),
('family_room', '[0.3, 0.3, 0.0, 0.9, 0.1, 0.8, 0.2, 0.0, 0.2, 0.9, 0.4, 0.7, 0.5, 0.7, 0.3, 0.0]', 'shared', 'leisure', 'low', 'family', 0.90, 'seed', 'Casual family gathering space'),

-- Office/Study (Private, Work, Low movement, Solitary)
('office', '[0.7, 0.0, 1.0, 0.0, 0.1, 0.8, 0.2, 0.0, 0.9, 0.1, 0.0, 0.6, 0.3, 0.2, 0.8, 0.0]', 'private', 'work', 'low', 'solitary', 0.95, 'seed', 'Work and productivity space, focus-oriented'),
('study', '[0.6, 0.2, 0.9, 0.0, 0.1, 0.8, 0.2, 0.0, 0.7, 0.3, 0.0, 0.6, 0.3, 0.2, 0.7, 0.0]', 'private', 'work', 'low', 'solitary', 0.90, 'seed', 'Reading and study area'),

-- Utility Spaces (Shared, Utility, Medium movement, Solitary)
('laundry_room', '[0.5, 0.0, 0.3, 0.0, 0.9, 0.1, 0.5, 0.4, 0.8, 0.2, 0.0, 0.4, 0.6, 0.7, 0.5, 0.0]', 'shared', 'utility', 'medium', 'solitary', 0.90, 'seed', 'Laundry and cleaning tasks'),
('garage', '[0.4, 0.0, 0.4, 0.1, 0.8, 0.1, 0.4, 0.5, 0.7, 0.3, 0.0, 0.3, 0.5, 0.6, 0.7, 0.0]', 'shared', 'utility', 'medium', 'solitary', 0.90, 'seed', 'Vehicle storage and workshop'),
('pantry', '[0.5, 0.0, 0.2, 0.0, 0.9, 0.7, 0.3, 0.0, 0.8, 0.2, 0.0, 0.5, 0.4, 0.3, 0.2, 0.0]', 'shared', 'utility', 'low', 'solitary', 0.90, 'seed', 'Food storage area'),

-- Transitional Spaces (Shared, Utility, Medium movement, Mixed)
('hallway', '[0.4, 0.0, 0.0, 0.0, 0.9, 0.1, 0.5, 0.4, 0.6, 0.4, 0.2, 0.2, 0.3, 0.5, 0.6, 0.0]', 'shared', 'utility', 'medium', 'family', 0.90, 'seed', 'Transitional pathway between rooms'),
('entryway', '[0.3, 0.0, 0.0, 0.2, 0.7, 0.2, 0.4, 0.4, 0.3, 0.5, 0.6, 0.3, 0.4, 0.6, 0.7, 0.0]', 'public', 'utility', 'medium', 'social', 0.90, 'seed', 'Entry and greeting area'),

-- Outdoor Spaces (Public/Shared, Leisure, Medium movement, Social)
('patio', '[0.2, 0.2, 0.0, 0.8, 0.1, 0.3, 0.5, 0.2, 0.1, 0.6, 0.8, 0.4, 0.6, 0.7, 0.3, 0.0]', 'public', 'leisure', 'medium', 'social', 0.90, 'seed', 'Outdoor relaxation and entertainment'),
('backyard', '[0.2, 0.2, 0.1, 0.8, 0.1, 0.3, 0.4, 0.3, 0.2, 0.7, 0.7, 0.4, 0.5, 0.6, 0.4, 0.0]', 'public', 'leisure', 'medium', 'social', 0.90, 'seed', 'Outdoor activity and leisure space')

ON CONFLICT (location) DO NOTHING;

-- Function to update usage statistics when embedding is accessed
CREATE OR REPLACE FUNCTION update_location_embedding_usage(p_location VARCHAR)
RETURNS void AS $$
BEGIN
    UPDATE location_embeddings
    SET usage_count = usage_count + 1,
        last_used_at = NOW()
    WHERE location = p_location;
END;
$$ LANGUAGE plpgsql;

-- View for debugging: show embeddings with human-readable labels
CREATE OR REPLACE VIEW location_embeddings_summary AS
SELECT
    location,
    privacy_level,
    function_type,
    movement_intensity,
    social_context,
    classification_confidence,
    classified_by,
    usage_count,
    classified_at,
    last_used_at
FROM location_embeddings
ORDER BY usage_count DESC, location;
