-- e2e/init-scripts/03_learned_patterns_temporal_decay.sql
-- Enhanced learned patterns with temporal decay and observation tracking

-- Drop existing learned_distances table and recreate with temporal decay support
DROP TABLE IF EXISTS learned_distances CASCADE;

-- Learned patterns with temporal decay support
CREATE TABLE learned_patterns (
    pattern_key VARCHAR(255) PRIMARY KEY,

    -- Current weighted average (computed from observations with decay)
    weighted_distance FLOAT NOT NULL CHECK (weighted_distance >= 0 AND weighted_distance <= 1),
    confidence_score FLOAT NOT NULL CHECK (confidence_score >= 0 AND confidence_score <= 1),

    -- Metadata
    observation_count INT NOT NULL DEFAULT 0,
    first_seen TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_updated TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_computed TIMESTAMPTZ NOT NULL DEFAULT NOW(),  -- When weighted avg last calculated

    -- Decay parameters
    decay_half_life_hours INT NOT NULL DEFAULT 720,  -- 30 days default

    -- Sample anchors for debugging/reference
    sample_anchor1_id UUID,
    sample_anchor2_id UUID,

    -- Statistics (for monitoring pattern stability)
    min_distance FLOAT,
    max_distance FLOAT,
    std_deviation FLOAT,

    -- Pattern characteristics (extracted from key for querying)
    location1 TEXT,
    location2 TEXT,
    time_of_day1 TEXT,
    time_of_day2 TEXT,
    day_type1 TEXT,
    day_type2 TEXT
);

CREATE INDEX idx_learned_patterns_confidence ON learned_patterns(confidence_score DESC);
CREATE INDEX idx_learned_patterns_updated ON learned_patterns(last_updated DESC);
CREATE INDEX idx_learned_patterns_computed ON learned_patterns(last_computed DESC);
CREATE INDEX idx_learned_patterns_locations ON learned_patterns(location1, location2);
CREATE INDEX idx_learned_patterns_context ON learned_patterns(time_of_day1, day_type1);

-- Pattern observations: Individual observations with weights and decay
CREATE TABLE pattern_observations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pattern_key VARCHAR(255) NOT NULL REFERENCES learned_patterns(pattern_key) ON DELETE CASCADE,

    -- Observation data
    distance FLOAT NOT NULL CHECK (distance >= 0 AND distance <= 1),
    source VARCHAR(50) NOT NULL,  -- 'llm', 'llm_verify', 'llm_seed', 'similarity_cached', 'vector'
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    weight FLOAT NOT NULL DEFAULT 1.0 CHECK (weight > 0),

    -- Context at observation time (for contextual decay)
    season VARCHAR(20),
    day_type VARCHAR(20),
    time_of_day VARCHAR(20),

    -- Reference anchors for debugging
    anchor1_id UUID REFERENCES semantic_anchors(id) ON DELETE SET NULL,
    anchor2_id UUID REFERENCES semantic_anchors(id) ON DELETE SET NULL,

    -- Vector distance at time of observation (for agreement tracking)
    vector_distance FLOAT CHECK (vector_distance IS NULL OR (vector_distance >= 0 AND vector_distance <= 1))
);

CREATE INDEX idx_observations_pattern ON pattern_observations(pattern_key, timestamp DESC);
CREATE INDEX idx_observations_timestamp ON pattern_observations(timestamp DESC);
CREATE INDEX idx_observations_source ON pattern_observations(source);
CREATE INDEX idx_observations_season ON pattern_observations(season);

-- Pattern relearning queue: Patterns that need LLM verification
CREATE TABLE pattern_relearning_queue (
    pattern_key VARCHAR(255) PRIMARY KEY REFERENCES learned_patterns(pattern_key) ON DELETE CASCADE,

    reason TEXT NOT NULL,  -- 'confidence_drop', 'variance_increase', 'seasonal_refresh', 'manual'
    priority INT NOT NULL DEFAULT 5 CHECK (priority >= 1 AND priority <= 10),

    queued_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_attempt TIMESTAMPTZ,
    attempt_count INT NOT NULL DEFAULT 0,

    -- Original values when queued (for comparison)
    original_confidence FLOAT,
    original_distance FLOAT
);

CREATE INDEX idx_relearning_priority ON pattern_relearning_queue(priority DESC, queued_at ASC);
CREATE INDEX idx_relearning_queued ON pattern_relearning_queue(queued_at DESC);

-- Similar pair cache: For similarity-based cache lookups
-- This helps find "similar enough" pairs that have been computed before
CREATE INDEX idx_distances_similarity_lookup ON anchor_distances(distance, source)
    WHERE source IN ('llm', 'llm_verify', 'llm_seed');

-- View: Recent high-quality LLM computations for similarity matching
CREATE VIEW recent_llm_distances AS
SELECT
    ad.anchor1_id,
    ad.anchor2_id,
    ad.distance,
    ad.source,
    ad.computed_at,
    a1.location as location1,
    a2.location as location2,
    a1.timestamp as timestamp1,
    a2.timestamp as timestamp2,
    a1.context as context1,
    a2.context as context2,
    a1.semantic_embedding as embedding1,
    a2.semantic_embedding as embedding2,
    -- Compute vector distance for comparison
    1 - (a1.semantic_embedding <=> a2.semantic_embedding) as vector_similarity
FROM anchor_distances ad
JOIN semantic_anchors a1 ON a1.id = ad.anchor1_id
JOIN semantic_anchors a2 ON a2.id = ad.anchor2_id
WHERE ad.source IN ('llm', 'llm_verify', 'llm_seed')
  AND ad.computed_at > NOW() - INTERVAL '90 days';

CREATE INDEX idx_recent_llm_distances_computed ON anchor_distances(computed_at DESC)
    WHERE source IN ('llm', 'llm_verify', 'llm_seed');

-- View: Pattern health monitoring
CREATE VIEW pattern_health AS
SELECT
    lp.pattern_key,
    lp.weighted_distance,
    lp.confidence_score,
    lp.observation_count,
    lp.std_deviation,
    lp.last_updated,
    lp.last_computed,
    EXTRACT(EPOCH FROM (NOW() - lp.last_updated))/3600 as hours_since_update,
    EXTRACT(EPOCH FROM (NOW() - lp.last_computed))/3600 as hours_since_computation,
    -- Count recent observations (last 30 days)
    COUNT(po.id) FILTER (WHERE po.timestamp > NOW() - INTERVAL '30 days') as recent_observations,
    -- Average recent distance (for trend detection)
    AVG(po.distance) FILTER (WHERE po.timestamp > NOW() - INTERVAL '30 days') as recent_avg_distance,
    -- Check if pattern is stale
    CASE
        WHEN lp.last_updated < NOW() - INTERVAL '90 days' THEN 'stale'
        WHEN lp.confidence_score < 0.5 THEN 'low_confidence'
        WHEN lp.std_deviation > 0.2 THEN 'high_variance'
        WHEN lp.confidence_score >= 0.8 AND lp.observation_count >= 3 THEN 'healthy'
        ELSE 'moderate'
    END as health_status
FROM learned_patterns lp
LEFT JOIN pattern_observations po ON po.pattern_key = lp.pattern_key
GROUP BY lp.pattern_key, lp.weighted_distance, lp.confidence_score,
         lp.observation_count, lp.std_deviation, lp.last_updated, lp.last_computed;

-- Function: Calculate current season from timestamp
CREATE OR REPLACE FUNCTION get_season(ts TIMESTAMPTZ) RETURNS TEXT AS $$
BEGIN
    CASE EXTRACT(MONTH FROM ts)
        WHEN 12, 1, 2 THEN RETURN 'winter';
        WHEN 3, 4, 5 THEN RETURN 'spring';
        WHEN 6, 7, 8 THEN RETURN 'summer';
        WHEN 9, 10, 11 THEN RETURN 'fall';
    END CASE;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

-- Function: Check if locations are adjacent (for routine flows)
CREATE OR REPLACE FUNCTION is_adjacent(loc1 TEXT, loc2 TEXT) RETURNS BOOLEAN AS $$
BEGIN
    -- Define adjacent location pairs
    RETURN (
        (loc1 = 'bedroom' AND loc2 IN ('bathroom', 'kitchen')) OR
        (loc1 = 'bathroom' AND loc2 IN ('bedroom', 'kitchen')) OR
        (loc1 = 'kitchen' AND loc2 IN ('dining_room', 'bedroom', 'bathroom')) OR
        (loc1 = 'dining_room' AND loc2 IN ('kitchen', 'living_room')) OR
        (loc1 = 'living_room' AND loc2 IN ('dining_room', 'study')) OR
        (loc1 = 'study' AND loc2 IN ('living_room')) OR
        -- Reverse pairs
        (loc2 = 'bedroom' AND loc1 IN ('bathroom', 'kitchen')) OR
        (loc2 = 'bathroom' AND loc1 IN ('bedroom', 'kitchen')) OR
        (loc2 = 'kitchen' AND loc1 IN ('dining_room', 'bedroom', 'bathroom')) OR
        (loc2 = 'dining_room' AND loc1 IN ('kitchen', 'living_room')) OR
        (loc2 = 'living_room' AND loc1 IN ('dining_room', 'study')) OR
        (loc2 = 'study' AND loc1 IN ('living_room'))
    );
END;
$$ LANGUAGE plpgsql IMMUTABLE;

-- Trigger: Update last_updated timestamp on learned_patterns
CREATE OR REPLACE FUNCTION update_learned_pattern_timestamp()
RETURNS TRIGGER AS $$
BEGIN
    NEW.last_updated = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_update_learned_pattern_timestamp
    BEFORE UPDATE ON learned_patterns
    FOR EACH ROW
    EXECUTE FUNCTION update_learned_pattern_timestamp();

-- Comments for documentation
COMMENT ON TABLE learned_patterns IS 'Learned distance patterns with temporal decay support. Stores weighted averages of observations.';
COMMENT ON TABLE pattern_observations IS 'Individual distance observations with weights for temporal decay calculation.';
COMMENT ON TABLE pattern_relearning_queue IS 'Queue of patterns that need LLM re-verification due to confidence drops or variance increases.';
COMMENT ON COLUMN learned_patterns.decay_half_life_hours IS 'Number of hours until observation weight decays to 50%. Default 720 hours (30 days).';
COMMENT ON COLUMN learned_patterns.confidence_score IS 'Confidence in the learned distance (0-1). Based on observation count, recency, and consistency.';
COMMENT ON COLUMN pattern_observations.weight IS 'Base weight of observation. Higher for LLM (1.0), lower for inferred (0.5).';
COMMENT ON COLUMN pattern_observations.season IS 'Season when observation was made. Used for contextual decay.';
COMMENT ON VIEW pattern_health IS 'Monitoring view for pattern quality: shows staleness, confidence, and recent activity.';
