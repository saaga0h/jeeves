-- e2e/init-scripts/02_semantic_anchors_schema.sql
-- Semantic Anchor System: Flexible behavioral pattern representation using high-dimensional embeddings

-- Semantic anchors: Points in behavioral space (replacing fixed episodes)
CREATE TABLE semantic_anchors (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Physical coordinates (low-dimensional projection)
    timestamp TIMESTAMPTZ NOT NULL,
    location TEXT NOT NULL,

    -- Semantic tensor (native 128-dimensional vector)
    semantic_embedding vector(128) NOT NULL,

    -- Human-readable context (for debugging/querying)
    context JSONB NOT NULL,
    -- Example: {
    --   "time_of_day": "morning",
    --   "day_type": "weekday",
    --   "season": "winter",
    --   "weather": "dark_rainy",
    --   "household_mode": "waking"
    -- }

    -- Activity signals (what we observed)
    signals JSONB NOT NULL,
    -- Example: [
    --   {"type": "motion", "value": "detected", "confidence": 0.8},
    --   {"type": "lighting", "brightness": 60, "confidence": 0.7}
    -- ]

    -- Optional duration (three-tier: measured > estimated > null)
    duration_minutes INT,  -- nullable
    duration_source TEXT,  -- 'measured', 'estimated', 'inferred', or null
    duration_confidence FLOAT,  -- 0.0-1.0

    -- Relationships (graph structure)
    preceding_anchor_id UUID REFERENCES semantic_anchors(id),
    following_anchor_id UUID REFERENCES semantic_anchors(id),
    pattern_id UUID,  -- which discovered pattern (not FK yet, will add later)

    -- Metadata
    created_at TIMESTAMPTZ DEFAULT NOW(),

    -- Constraints
    CONSTRAINT valid_duration_source
        CHECK (duration_source IN ('measured', 'estimated', 'inferred') OR duration_source IS NULL),
    CONSTRAINT valid_duration_confidence
        CHECK (duration_confidence IS NULL OR (duration_confidence >= 0 AND duration_confidence <= 1))
);

-- Vector similarity index (using IVFFlat for approximate nearest neighbor)
CREATE INDEX idx_semantic_similarity
ON semantic_anchors
USING ivfflat (semantic_embedding vector_cosine_ops)
WITH (lists = 100);

-- Standard indexes
CREATE INDEX idx_anchors_time ON semantic_anchors(timestamp);
CREATE INDEX idx_anchors_location ON semantic_anchors(location);
CREATE INDEX idx_anchors_pattern ON semantic_anchors(pattern_id) WHERE pattern_id IS NOT NULL;
CREATE INDEX idx_anchors_preceding ON semantic_anchors(preceding_anchor_id) WHERE preceding_anchor_id IS NOT NULL;
CREATE INDEX idx_anchors_context ON semantic_anchors USING GIN (context);
CREATE INDEX idx_anchors_signals ON semantic_anchors USING GIN (signals);

-- Anchor interpretations: Support for parallel activities in same space
CREATE TABLE anchor_interpretations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    anchor_id UUID NOT NULL REFERENCES semantic_anchors(id) ON DELETE CASCADE,

    activity_type TEXT NOT NULL,
    confidence FLOAT NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
    evidence TEXT[] NOT NULL,

    -- If this interpretation spawned separate anchor
    spawned_anchor_id UUID REFERENCES semantic_anchors(id),

    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_interpretations_anchor ON anchor_interpretations(anchor_id);
CREATE INDEX idx_interpretations_activity ON anchor_interpretations(activity_type);

-- Anchor distances: Pre-computed semantic distances between anchors
CREATE TABLE anchor_distances (
    anchor1_id UUID NOT NULL REFERENCES semantic_anchors(id) ON DELETE CASCADE,
    anchor2_id UUID NOT NULL REFERENCES semantic_anchors(id) ON DELETE CASCADE,

    distance FLOAT NOT NULL CHECK (distance >= 0 AND distance <= 1),
    source TEXT NOT NULL,  -- 'llm', 'learned', 'vector'

    computed_at TIMESTAMPTZ DEFAULT NOW(),

    PRIMARY KEY (anchor1_id, anchor2_id),

    -- Ensure anchor1_id < anchor2_id (avoid duplicates)
    CONSTRAINT ordered_anchors CHECK (anchor1_id < anchor2_id)
);

CREATE INDEX idx_distances_anchor1 ON anchor_distances(anchor1_id);
CREATE INDEX idx_distances_anchor2 ON anchor_distances(anchor2_id);
CREATE INDEX idx_distances_source ON anchor_distances(source);

-- Learned distances: Pattern-based distance library built from LLM computations
CREATE TABLE learned_distances (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    pattern_key TEXT NOT NULL UNIQUE,  -- Generated from anchor characteristics
    -- Example: "morning_bedroom_motion_weekday"

    distance FLOAT NOT NULL CHECK (distance >= 0 AND distance <= 1),
    interpretation TEXT,  -- LLM's explanation of why they're similar/different

    -- Usage tracking
    times_used INT NOT NULL DEFAULT 0,
    last_used TIMESTAMPTZ,

    -- Metadata
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_learned_pattern_key ON learned_distances(pattern_key);
CREATE INDEX idx_learned_last_used ON learned_distances(last_used) WHERE last_used IS NOT NULL;

-- Behavioral patterns: Discovered patterns with weight-based ranking
CREATE TABLE behavioral_patterns (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    name TEXT NOT NULL,
    description TEXT,  -- LLM-generated description of the pattern
    pattern_type TEXT,  -- 'morning_routine', 'meal_cycle', etc.

    -- Weight (decimal scale: starts at 0.1, only increases)
    weight FLOAT NOT NULL DEFAULT 0.1 CHECK (weight >= 0.1),

    -- Cluster metadata
    cluster_size INT NOT NULL DEFAULT 0,  -- number of anchors in cluster
    locations TEXT[] NOT NULL DEFAULT '{}',  -- locations involved in pattern

    -- Usage tracking
    observations INT NOT NULL DEFAULT 0,      -- times pattern observed (alias for times_observed)
    times_observed INT NOT NULL DEFAULT 0,    -- times pattern observed
    predictions INT NOT NULL DEFAULT 0,       -- times used for prediction
    acceptances INT NOT NULL DEFAULT 0,       -- predictions accepted
    rejections INT NOT NULL DEFAULT 0,        -- predictions rejected

    -- Temporal tracking
    first_seen TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_useful TIMESTAMPTZ,  -- last successful prediction

    -- Pattern metadata
    typical_duration_minutes INT,
    context JSONB,  -- typical context for this pattern (deprecated, use dominant_context)
    dominant_context JSONB,  -- dominant context from cluster (time_of_day, day_type, etc.)

    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_patterns_weight ON behavioral_patterns(weight DESC);
CREATE INDEX idx_patterns_last_seen ON behavioral_patterns(last_seen DESC);
CREATE INDEX idx_patterns_type ON behavioral_patterns(pattern_type) WHERE pattern_type IS NOT NULL;
CREATE INDEX idx_patterns_context ON behavioral_patterns USING GIN (context);

-- Now add the foreign key from semantic_anchors to behavioral_patterns
ALTER TABLE semantic_anchors
ADD CONSTRAINT fk_anchor_pattern
FOREIGN KEY (pattern_id) REFERENCES behavioral_patterns(id);
