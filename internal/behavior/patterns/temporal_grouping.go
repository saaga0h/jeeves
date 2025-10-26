package patterns

import (
	"sort"
	"time"

	"github.com/saaga0h/jeeves-platform/internal/behavior/types"
)

// TemporalGroup represents a group of anchors within a time window
type TemporalGroup struct {
	Anchors   []*types.SemanticAnchor
	StartTime time.Time
	EndTime   time.Time
}

// GroupByTimeWindow groups anchors into temporal windows
// Anchors within windowSize of each other are grouped together
func GroupByTimeWindow(anchors []*types.SemanticAnchor, windowSize time.Duration) []TemporalGroup {
	if len(anchors) == 0 {
		return []TemporalGroup{}
	}

	// Sort anchors by timestamp (chronological order)
	sortedAnchors := make([]*types.SemanticAnchor, len(anchors))
	copy(sortedAnchors, anchors)
	sort.Slice(sortedAnchors, func(i, j int) bool {
		return sortedAnchors[i].Timestamp.Before(sortedAnchors[j].Timestamp)
	})

	var groups []TemporalGroup
	currentGroup := TemporalGroup{
		Anchors:   []*types.SemanticAnchor{sortedAnchors[0]},
		StartTime: sortedAnchors[0].Timestamp,
		EndTime:   sortedAnchors[0].Timestamp,
	}

	for i := 1; i < len(sortedAnchors); i++ {
		anchor := sortedAnchors[i]
		timeSinceLastAnchor := anchor.Timestamp.Sub(sortedAnchors[i-1].Timestamp)

		if timeSinceLastAnchor <= windowSize {
			// Add to current group
			currentGroup.Anchors = append(currentGroup.Anchors, anchor)
			currentGroup.EndTime = anchor.Timestamp
		} else {
			// Start new group
			groups = append(groups, currentGroup)
			currentGroup = TemporalGroup{
				Anchors:   []*types.SemanticAnchor{anchor},
				StartTime: anchor.Timestamp,
				EndTime:   anchor.Timestamp,
			}
		}
	}

	// Add final group
	groups = append(groups, currentGroup)

	return groups
}

// LocationTimeRange represents the time span of activity in a location
type LocationTimeRange struct {
	Location  string
	StartTime time.Time
	EndTime   time.Time
}

// DetectParallelism determines if anchors in a group represent parallel activities
// Returns true if activities in different locations overlap temporally
func DetectParallelism(group TemporalGroup, overlapThreshold time.Duration) bool {
	// Get unique locations
	locationMap := make(map[string][]time.Time)
	for _, anchor := range group.Anchors {
		locationMap[anchor.Location] = append(locationMap[anchor.Location], anchor.Timestamp)
	}

	// If only one location, it's sequential
	if len(locationMap) <= 1 {
		return false
	}

	// Build time ranges for each location
	var locationRanges []LocationTimeRange
	for location, timestamps := range locationMap {
		if len(timestamps) == 0 {
			continue
		}

		// Sort timestamps for this location
		sort.Slice(timestamps, func(i, j int) bool {
			return timestamps[i].Before(timestamps[j])
		})

		locationRanges = append(locationRanges, LocationTimeRange{
			Location:  location,
			StartTime: timestamps[0],
			EndTime:   timestamps[len(timestamps)-1],
		})
	}

	// Check for temporal overlap between any pair of locations
	for i := 0; i < len(locationRanges); i++ {
		for j := i + 1; j < len(locationRanges); j++ {
			loc1 := locationRanges[i]
			loc2 := locationRanges[j]

			// Check if time ranges overlap
			// Overlap exists if: start1 <= end2 AND start2 <= end1
			if !loc1.StartTime.After(loc2.EndTime.Add(overlapThreshold)) &&
				!loc2.StartTime.After(loc1.EndTime.Add(overlapThreshold)) {
				// Overlapping time ranges in different locations = parallel activities
				return true
			}
		}
	}

	// No overlap found = sequential activities
	return false
}

// GetUniqueLocations returns list of unique locations in a group
func GetUniqueLocations(group TemporalGroup) []string {
	locationSet := make(map[string]bool)
	for _, anchor := range group.Anchors {
		locationSet[anchor.Location] = true
	}

	locations := make([]string, 0, len(locationSet))
	for location := range locationSet {
		locations = append(locations, location)
	}
	return locations
}

// FilterByLocation returns anchors from group that match the specified location
func FilterByLocation(group TemporalGroup, location string) []*types.SemanticAnchor {
	var filtered []*types.SemanticAnchor
	for _, anchor := range group.Anchors {
		if anchor.Location == location {
			filtered = append(filtered, anchor)
		}
	}
	return filtered
}
