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

-- Behavioral Vectors Table
-- Stores sequences of location transitions for pattern recognition
-- Each vector represents a potential behavioral pattern (e.g., morning routine, meal preparation)

CREATE TABLE IF NOT EXISTS behavioral_vectors (
  id UUID PRIMARY KEY,
  
  -- When this vector was detected (timestamp of first episode in sequence)
  timestamp TIMESTAMPTZ NOT NULL,
  
  -- Array of locations in sequence with metadata
  -- Example: [
  --   {"location": "bedroom", "duration_sec": 300, "gap_to_next": 0, "sensors": ["motion", "lighting"]},
  --   {"location": "bathroom", "duration_sec": 180, "gap_to_next": 0, "sensors": ["motion", "lighting"]},
  --   {"location": "kitchen", "duration_sec": 720, "gap_to_next": 0, "sensors": ["motion", "lighting"]}
  -- ]
  sequence JSONB NOT NULL,
  
  -- Contextual metadata about when this vector occurred
  -- Example: {
  --   "time_of_day": "morning",
  --   "day_of_week": "Friday",
  --   "total_duration_sec": 1200,
  --   "location_count": 3,
  --   "transition_count": 2,
  --   "virtual_time": "2025-10-17T07:00:00Z"  -- for test scenarios
  -- }
  context JSONB NOT NULL,
  
  -- Statistical information about edges (transitions between locations)
  -- Example: {
  --   "bedroom->bathroom": {
  --     "gap_seconds": 0,
  --     "frequency": 0.85,  -- how often this transition occurs historically
  --     "typical_gap": 5.2,  -- typical gap in seconds
  --     "temporal_proximity_score": 0.95
  --   },
  --   "bathroom->kitchen": {...}
  -- }
  edge_stats JSONB,
  
  -- References to the micro-episodes that form this vector
  micro_episode_ids TEXT[] NOT NULL,
  
  -- Test scenario name (null for production data)
  scenario_name TEXT,
  
  -- LLM-assigned semantic label (populated after LLM analysis)
  -- Examples: "morning_routine", "meal_preparation", "work_transition"
  semantic_label TEXT,
  
  -- Has this vector been consolidated into a macro-episode?
  consolidated BOOLEAN DEFAULT FALSE,
  
  -- Reference to macro-episode if consolidated
  macro_episode_id UUID,
  
  -- Vector quality score (0-1) based on:
  -- - Transition tightness (smaller gaps = higher score)
  -- - Historical frequency (common patterns = higher score)
  -- - Semantic coherence (related activities = higher score)
  quality_score DECIMAL(3,2),
  
  -- Metadata
  created_at TIMESTAMPTZ DEFAULT NOW(),
  updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Indexes for efficient querying
CREATE INDEX idx_behavioral_vectors_timestamp ON behavioral_vectors(timestamp);
CREATE INDEX idx_behavioral_vectors_scenario ON behavioral_vectors(scenario_name) WHERE scenario_name IS NOT NULL;
CREATE INDEX idx_behavioral_vectors_consolidated ON behavioral_vectors(consolidated);
CREATE INDEX idx_behavioral_vectors_macro_episode ON behavioral_vectors(macro_episode_id) WHERE macro_episode_id IS NOT NULL;

-- GIN index for JSONB queries on sequence and context
CREATE INDEX idx_behavioral_vectors_sequence ON behavioral_vectors USING GIN (sequence);
CREATE INDEX idx_behavioral_vectors_context ON behavioral_vectors USING GIN (context);

-- Partial index for unconsolidated vectors (active pattern matching)
CREATE INDEX idx_behavioral_vectors_unconsolidated ON behavioral_vectors(timestamp DESC) 
  WHERE consolidated = FALSE;

-- Index for time-of-day pattern queries
CREATE INDEX idx_behavioral_vectors_time_of_day ON behavioral_vectors((context->>'time_of_day'));

-- Index for finding similar vectors (by location sequence)
-- This enables queries like "find all vectors that start with bedroom->bathroom"
CREATE INDEX idx_behavioral_vectors_first_locations ON behavioral_vectors(
  (sequence->0->>'location'),
  (sequence->1->>'location')
);


-- Vector Edges Table (optional - for detailed edge statistics)
-- This is a normalized version of edge_stats for faster edge-specific queries
CREATE TABLE IF NOT EXISTS behavioral_vector_edges (
  id SERIAL PRIMARY KEY,
  
  -- Location pair
  from_location TEXT NOT NULL,
  to_location TEXT NOT NULL,
  
  -- Statistics for this edge across all observations
  observation_count INTEGER DEFAULT 1,
  
  -- Gap statistics (in seconds)
  min_gap_sec INTEGER,
  max_gap_sec INTEGER,
  avg_gap_sec DECIMAL(8,2),
  median_gap_sec INTEGER,
  
  -- Temporal context patterns
  -- Example: {
  --   "morning": {"count": 45, "avg_gap": 2.1},
  --   "evening": {"count": 12, "avg_gap": 8.5}
  -- }
  time_of_day_stats JSONB,
  
  -- Frequency score (0-1): how often does this edge occur in the data?
  frequency_score DECIMAL(3,2),
  
  -- Most recent observation
  last_seen TIMESTAMPTZ,
  
  -- Metadata
  created_at TIMESTAMPTZ DEFAULT NOW(),
  updated_at TIMESTAMPTZ DEFAULT NOW(),
  
  -- Unique constraint on edge pair
  UNIQUE(from_location, to_location)
);

-- Indexes for edge queries
CREATE INDEX idx_vector_edges_from ON behavioral_vector_edges(from_location);
CREATE INDEX idx_vector_edges_to ON behavioral_vector_edges(to_location);
CREATE INDEX idx_vector_edges_frequency ON behavioral_vector_edges(frequency_score DESC);
CREATE INDEX idx_vector_edges_last_seen ON behavioral_vector_edges(last_seen DESC);


-- View: Recent Unconsolidated Vectors
-- Useful for finding vectors that need LLM analysis
CREATE OR REPLACE VIEW unconsolidated_vectors AS
SELECT 
  id,
  timestamp,
  sequence,
  context,
  edge_stats,
  micro_episode_ids,
  quality_score,
  -- Extract first and last locations for quick pattern identification
  sequence->0->>'location' as start_location,
  sequence->(jsonb_array_length(sequence)-1)->>'location' as end_location,
  jsonb_array_length(sequence) as location_count,
  (context->>'total_duration_sec')::int as total_duration_sec,
  context->>'time_of_day' as time_of_day
FROM behavioral_vectors
WHERE consolidated = FALSE
ORDER BY timestamp DESC;


-- View: Vector Pattern Summary
-- Groups similar vectors for pattern discovery
CREATE OR REPLACE VIEW vector_patterns AS
SELECT 
  -- Create a sequence signature (just the location names in order)
  jsonb_agg(elem->>'location' ORDER BY ord) as location_sequence,
  context->>'time_of_day' as time_of_day,
  COUNT(*) as occurrence_count,
  AVG((context->>'total_duration_sec')::int) as avg_duration_sec,
  MIN(timestamp) as first_seen,
  MAX(timestamp) as last_seen,
  -- Example vector IDs for this pattern
  array_agg(id ORDER BY timestamp DESC) as vector_ids
FROM behavioral_vectors,
     jsonb_array_elements(sequence) WITH ORDINALITY arr(elem, ord)
GROUP BY 
  -- Group by the sequence signature
  (SELECT jsonb_agg(e->>'location' ORDER BY o) 
   FROM jsonb_array_elements(sequence) WITH ORDINALITY x(e, o)),
  context->>'time_of_day'
HAVING COUNT(*) >= 2  -- Only show patterns that occur at least twice
ORDER BY occurrence_count DESC;


-- Function: Calculate edge frequency score
-- Updates the frequency score based on observation count
CREATE OR REPLACE FUNCTION update_edge_frequency_scores()
RETURNS void AS $$
DECLARE
  max_count INTEGER;
BEGIN
  -- Get max observation count for normalization
  SELECT MAX(observation_count) INTO max_count FROM behavioral_vector_edges;
  
  IF max_count > 0 THEN
    UPDATE behavioral_vector_edges
    SET frequency_score = LEAST(1.0, observation_count::decimal / (max_count / 2));
  END IF;
END;
$$ LANGUAGE plpgsql;


-- Function: Record a new behavioral vector
-- Convenience function for inserting vectors with automatic edge updates
CREATE OR REPLACE FUNCTION record_behavioral_vector(
  p_timestamp TIMESTAMPTZ,
  p_sequence JSONB,
  p_context JSONB,
  p_edge_stats JSONB,
  p_micro_episode_ids INTEGER[],
  p_scenario_name TEXT DEFAULT NULL
)
RETURNS INTEGER AS $$
DECLARE
  v_id INTEGER;
  edge JSONB;
  from_loc TEXT;
  to_loc TEXT;
  gap_sec INTEGER;
BEGIN
  -- Insert the vector
  INSERT INTO behavioral_vectors (
    timestamp, sequence, context, edge_stats, 
    micro_episode_ids, scenario_name
  ) VALUES (
    p_timestamp, p_sequence, p_context, p_edge_stats,
    p_micro_episode_ids, p_scenario_name
  ) RETURNING id INTO v_id;
  
  -- Update edge statistics
  FOR i IN 0..(jsonb_array_length(p_sequence) - 2) LOOP
    from_loc := p_sequence->i->>'location';
    to_loc := p_sequence->(i+1)->>'location';
    gap_sec := (p_sequence->i->>'gap_to_next')::int;
    
    -- Insert or update edge
    INSERT INTO behavioral_vector_edges (
      from_location, to_location, 
      min_gap_sec, max_gap_sec, avg_gap_sec, median_gap_sec,
      observation_count, last_seen
    ) VALUES (
      from_loc, to_loc,
      gap_sec, gap_sec, gap_sec, gap_sec,
      1, p_timestamp
    )
    ON CONFLICT (from_location, to_location) DO UPDATE SET
      observation_count = behavioral_vector_edges.observation_count + 1,
      min_gap_sec = LEAST(behavioral_vector_edges.min_gap_sec, gap_sec),
      max_gap_sec = GREATEST(behavioral_vector_edges.max_gap_sec, gap_sec),
      avg_gap_sec = (behavioral_vector_edges.avg_gap_sec * behavioral_vector_edges.observation_count + gap_sec) 
                    / (behavioral_vector_edges.observation_count + 1),
      last_seen = p_timestamp,
      updated_at = NOW();
  END LOOP;
  
  -- Update frequency scores
  PERFORM update_edge_frequency_scores();
  
  RETURN v_id;
END;
$$ LANGUAGE plpgsql;


-- Trigger: Update updated_at timestamp
CREATE OR REPLACE FUNCTION update_behavioral_vector_timestamp()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = NOW();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_behavioral_vectors_updated_at
BEFORE UPDATE ON behavioral_vectors
FOR EACH ROW
EXECUTE FUNCTION update_behavioral_vector_timestamp();

CREATE TRIGGER trg_vector_edges_updated_at
BEFORE UPDATE ON behavioral_vector_edges
FOR EACH ROW
EXECUTE FUNCTION update_behavioral_vector_timestamp();


-- Sample query comments for documentation

-- Find morning routine patterns:
-- SELECT * FROM vector_patterns WHERE time_of_day = 'morning' ORDER BY occurrence_count DESC;

-- Find all vectors starting with bedroom->bathroom:
-- SELECT * FROM behavioral_vectors 
-- WHERE sequence->0->>'location' = 'bedroom' 
--   AND sequence->1->>'location' = 'bathroom';

-- Find vectors needing consolidation:
-- SELECT * FROM unconsolidated_vectors WHERE quality_score > 0.7;

-- Get edge statistics for kitchen->dining_room:
-- SELECT * FROM behavioral_vector_edges 
-- WHERE from_location = 'kitchen' AND to_location = 'dining_room';

-- Find similar vectors to a given sequence:
-- WITH target_sequence AS (
--   SELECT sequence FROM behavioral_vectors WHERE id = 123
-- )
-- SELECT bv.*, similarity(bv.sequence::text, ts.sequence::text) as sim
-- FROM behavioral_vectors bv, target_sequence ts
-- WHERE bv.id != 123
-- ORDER BY sim DESC LIMIT 10;

-- Success message
DO $$
BEGIN
    RAISE NOTICE 'J.E.E.V.E.S. behavior database schema initialized successfully';
END $$;
