package patterns

import (
	"log/slog"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/saaga0h/jeeves-platform/internal/behavior/types"
)

// LocationSession represents a continuous activity session in a single location
type LocationSession struct {
	Location  string
	Anchors   []*types.SemanticAnchor
	StartTime time.Time
	EndTime   time.Time
}

// ActivitySequence represents a sequence of anchors that form a coherent activity
// This could be single-location (e.g., working in study for 3 hours)
// or cross-location (e.g., morning routine: bedroom→bathroom→kitchen)
type ActivitySequence struct {
	ID              string
	Anchors         []*types.SemanticAnchor
	Locations       []string // Unique locations in sequence
	StartTime       time.Time
	EndTime         time.Time
	IsCrossLocation bool // true if spans multiple locations
}

// LocationTemporalClusterer groups anchors using location-aware temporal density
type LocationTemporalClusterer struct {
	// Configuration
	TemporalGapThreshold time.Duration // Gap indicating separate activity sessions (default: 30 min)
	SequenceMaxGap       time.Duration // Max gap for cross-location sequences (default: 20 min)
	MinSequenceLength    int           // Minimum anchors in a sequence (default: 2)
	logger               *slog.Logger
}

// NewLocationTemporalClusterer creates a new clusterer with default settings
func NewLocationTemporalClusterer(logger *slog.Logger) *LocationTemporalClusterer {
	return &LocationTemporalClusterer{
		TemporalGapThreshold: 30 * time.Minute,
		SequenceMaxGap:       20 * time.Minute,
		MinSequenceLength:    2,
		logger:               logger,
	}
}

// ClusterByLocationTemporal performs location-aware temporal clustering
// Returns activity sequences ready for semantic validation
func (c *LocationTemporalClusterer) ClusterByLocationTemporal(
	anchors []*types.SemanticAnchor,
) []*ActivitySequence {
	if len(anchors) == 0 {
		return nil
	}

	// STEP 1: Group by location and find temporal sessions
	locationSessions := c.findLocationSessions(anchors)

	// STEP 2: Try to connect sessions across locations (sequences)
	sequences := c.detectSequences(locationSessions)

	// STEP 3: Add standalone sessions that weren't part of sequences
	sequences = c.addStandaloneSessions(sequences, locationSessions)

	return sequences
}

// findLocationSessions groups anchors by location and finds continuous temporal sessions
func (c *LocationTemporalClusterer) findLocationSessions(
	anchors []*types.SemanticAnchor,
) []*LocationSession {
	// Group by location
	byLocation := make(map[string][]*types.SemanticAnchor)
	for _, anchor := range anchors {
		byLocation[anchor.Location] = append(byLocation[anchor.Location], anchor)
	}

	var sessions []*LocationSession

	// For each location, find temporal sessions
	for location, locationAnchors := range byLocation {
		// Sort by timestamp
		sort.Slice(locationAnchors, func(i, j int) bool {
			return locationAnchors[i].Timestamp.Before(locationAnchors[j].Timestamp)
		})

		// Find temporal gaps to split into sessions
		var currentSession *LocationSession

		for _, anchor := range locationAnchors {
			if currentSession == nil {
				// Start new session
				currentSession = &LocationSession{
					Location:  location,
					Anchors:   []*types.SemanticAnchor{anchor},
					StartTime: anchor.Timestamp,
					EndTime:   anchor.Timestamp,
				}
			} else {
				// Check temporal gap from last anchor in session
				lastAnchor := currentSession.Anchors[len(currentSession.Anchors)-1]
				gap := anchor.Timestamp.Sub(lastAnchor.Timestamp)

				if gap > c.TemporalGapThreshold {
					// Gap too large - finish current session and start new one
					sessions = append(sessions, currentSession)
					currentSession = &LocationSession{
						Location:  location,
						Anchors:   []*types.SemanticAnchor{anchor},
						StartTime: anchor.Timestamp,
						EndTime:   anchor.Timestamp,
					}
				} else {
					// Add to current session
					currentSession.Anchors = append(currentSession.Anchors, anchor)
					currentSession.EndTime = anchor.Timestamp
				}
			}
		}

		// Don't forget last session
		if currentSession != nil {
			sessions = append(sessions, currentSession)
		}
	}

	// Sort sessions by start time
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].StartTime.Before(sessions[j].StartTime)
	})

	return sessions
}

// detectSequences tries to connect location sessions into cross-location sequences
func (c *LocationTemporalClusterer) detectSequences(
	sessions []*LocationSession,
) []*ActivitySequence {
	if len(sessions) == 0 {
		return nil
	}

	var sequences []*ActivitySequence
	usedSessions := make(map[int]bool)

	// Try to build sequences starting from each session
	for i, startSession := range sessions {
		if usedSessions[i] {
			continue
		}

		// Try to extend sequence forward
		sequence := &ActivitySequence{
			ID:              uuid.New().String(),
			Anchors:         append([]*types.SemanticAnchor{}, startSession.Anchors...),
			Locations:       []string{startSession.Location},
			StartTime:       startSession.StartTime,
			EndTime:         startSession.EndTime,
			IsCrossLocation: false,
		}

		// Track which session indices are part of this sequence
		sessionIndices := []int{i}
		usedSessions[i] = true
		currentSession := startSession

		// Look for adjacent sessions that could be part of sequence
		for j := i + 1; j < len(sessions); j++ {
			if usedSessions[j] {
				continue
			}

			nextSession := sessions[j]

			// Check temporal proximity
			gap := nextSession.StartTime.Sub(currentSession.EndTime)
			if gap > c.SequenceMaxGap {
				// Too far apart, stop extending this sequence
				break
			}

			// Check if different location (we're looking for transitions)
			if nextSession.Location == currentSession.Location {
				// Same location - skip for now, might be separate activity
				continue
			}

			// Potential sequence transition - add to sequence
			sequence.Anchors = append(sequence.Anchors, nextSession.Anchors...)
			if !contains(sequence.Locations, nextSession.Location) {
				sequence.Locations = append(sequence.Locations, nextSession.Location)
			}
			sequence.EndTime = nextSession.EndTime
			sequence.IsCrossLocation = true
			usedSessions[j] = true
			sessionIndices = append(sessionIndices, j)
			currentSession = nextSession
		}

		// Check for back-and-forth patterns (A→B→A→B)
		// These indicate parallel/interleaved activities, not natural sequences
		if sequence.IsCrossLocation && c.isBackAndForthPattern(sequence) {
			// Don't add as sequence - mark all sessions in this sequence as unused
			for _, idx := range sessionIndices {
				usedSessions[idx] = false
			}
			continue
		}

		// Only keep sequences with minimum length
		if len(sequence.Anchors) >= c.MinSequenceLength {
			sequences = append(sequences, sequence)
		} else {
			// Too short, mark all sessions in this sequence as unused
			for _, idx := range sessionIndices {
				usedSessions[idx] = false
			}
		}
	}

	return sequences
}

// isBackAndForthPattern detects if a sequence alternates between locations (A→B→A→B)
// This indicates parallel/interleaved activities rather than a natural routine flow
func (c *LocationTemporalClusterer) isBackAndForthPattern(sequence *ActivitySequence) bool {
	if !sequence.IsCrossLocation || len(sequence.Anchors) < 3 {
		return false
	}

	// Build location timeline from anchors (unique consecutive locations)
	var locationTimeline []string
	lastLocation := ""
	for _, anchor := range sequence.Anchors {
		if anchor.Location != lastLocation {
			locationTimeline = append(locationTimeline, anchor.Location)
			lastLocation = anchor.Location
		}
	}

	// Count location transitions
	transitions := len(locationTimeline) - 1

	// If we have multiple transitions (2+), check if it's alternating
	if transitions >= 2 {
		// Count how many times each location appears in the timeline
		locationCount := make(map[string]int)
		for _, loc := range locationTimeline {
			locationCount[loc]++
		}

		// Check if any location appears multiple times (indicating back-and-forth)
		for loc, count := range locationCount {
			if count >= 2 {
				// This location was visited, left, and returned to
				// This indicates back-and-forth pattern
				c.logger.Debug("Detected back-and-forth pattern",
					"sequence_id", sequence.ID,
					"location", loc,
					"visits", count,
					"timeline", locationTimeline,
					"transitions", transitions)
				return true
			}
		}
	}

	return false
}

// addStandaloneSessions adds sessions that weren't part of any sequence
func (c *LocationTemporalClusterer) addStandaloneSessions(
	sequences []*ActivitySequence,
	sessions []*LocationSession,
) []*ActivitySequence {
	// Track which sessions are already in sequences
	usedSessionAnchors := make(map[uuid.UUID]bool)
	for _, seq := range sequences {
		for _, anchor := range seq.Anchors {
			usedSessionAnchors[anchor.ID] = true
		}
	}

	// Add unused sessions as standalone sequences
	for _, session := range sessions {
		// Check if any anchor from this session is used
		isUsed := false
		for _, anchor := range session.Anchors {
			if usedSessionAnchors[anchor.ID] {
				isUsed = true
				break
			}
		}

		if !isUsed && len(session.Anchors) >= c.MinSequenceLength {
			// Create standalone sequence from session
			sequences = append(sequences, &ActivitySequence{
				ID:              uuid.New().String(),
				Anchors:         session.Anchors,
				Locations:       []string{session.Location},
				StartTime:       session.StartTime,
				EndTime:         session.EndTime,
				IsCrossLocation: false,
			})
		}
	}

	return sequences
}

// Helper functions

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
