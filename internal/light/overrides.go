package light

import (
	"sync"
	"time"
)

// Override represents a manual override for a location
type Override struct {
	Location  string
	ExpiresAt time.Time
}

// OverrideManager manages manual overrides
type OverrideManager struct {
	mu        sync.RWMutex
	overrides map[string]time.Time
}

// NewOverrideManager creates a new override manager
func NewOverrideManager() *OverrideManager {
	return &OverrideManager{
		overrides: make(map[string]time.Time),
	}
}

// SetManualOverride sets a manual override for a location
func (om *OverrideManager) SetManualOverride(location string, durationMinutes int) time.Time {
	om.mu.Lock()
	defer om.mu.Unlock()

	expiresAt := time.Now().Add(time.Duration(durationMinutes) * time.Minute)
	om.overrides[location] = expiresAt

	return expiresAt
}

// CheckManualOverride checks if a manual override is active for a location
// Returns true if active, false if not active or expired
func (om *OverrideManager) CheckManualOverride(location string) bool {
	om.mu.Lock()
	defer om.mu.Unlock()

	expiresAt, exists := om.overrides[location]
	if !exists {
		return false
	}

	// Check if expired
	if time.Now().After(expiresAt) {
		// Clean up expired override
		delete(om.overrides, location)
		return false
	}

	return true
}

// ClearManualOverride removes a manual override for a location
func (om *OverrideManager) ClearManualOverride(location string) bool {
	om.mu.Lock()
	defer om.mu.Unlock()

	_, exists := om.overrides[location]
	if exists {
		delete(om.overrides, location)
		return true
	}

	return false
}

// GetManualOverrides returns all active overrides
func (om *OverrideManager) GetManualOverrides() []string {
	om.mu.RLock()
	defer om.mu.RUnlock()

	locations := make([]string, 0, len(om.overrides))
	now := time.Now()

	for location, expiresAt := range om.overrides {
		if now.Before(expiresAt) {
			locations = append(locations, location)
		}
	}

	return locations
}

// CleanupExpiredOverrides removes all expired overrides
func (om *OverrideManager) CleanupExpiredOverrides() int {
	om.mu.Lock()
	defer om.mu.Unlock()

	now := time.Now()
	cleaned := 0

	for location, expiresAt := range om.overrides {
		if now.After(expiresAt) {
			delete(om.overrides, location)
			cleaned++
		}
	}

	return cleaned
}
