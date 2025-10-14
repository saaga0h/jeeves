package ontology

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// BehavioralEpisode is the root JSON-LD document
type BehavioralEpisode struct {
	Context    map[string]interface{} `json:"@context"`
	Type       string                 `json:"@type"`
	ID         string                 `json:"@id"`
	StartedAt  time.Time              `json:"jeeves:startedAt"`
	EndedAt    *time.Time             `json:"jeeves:endedAt,omitempty"`
	DayOfWeek  string                 `json:"jeeves:dayOfWeek"`
	TimeOfDay  string                 `json:"jeeves:timeOfDay"`
	Duration   string                 `json:"jeeves:duration,omitempty"`
	Activity   Activity               `json:"adl:activity"`
	EnvContext EnvironmentalContext   `json:"jeeves:hadEnvironmentalContext"`
}

type Activity struct {
	Type     string   `json:"@type"`
	Name     string   `json:"name"`
	Location Location `json:"adl:location"`
}

type Location struct {
	Type string `json:"@type"`
	ID   string `json:"@id"`
	Name string `json:"name"`
}

type EnvironmentalContext struct {
	Type string `json:"@type"`
	ID   string `json:"@id"`
	// Add more fields as needed
}

// NewEpisode creates a new behavioral episode
func NewEpisode(activity Activity, location Location) *BehavioralEpisode {
	now := time.Now()

	return &BehavioralEpisode{
		Context:   GetDefaultContext(),
		Type:      "jeeves:BehavioralEpisode",
		ID:        fmt.Sprintf("urn:uuid:%s", uuid.New().String()),
		StartedAt: now,
		DayOfWeek: now.Weekday().String(),
		TimeOfDay: getTimeOfDay(now),
		Activity: Activity{
			Type:     activity.Type,
			Name:     activity.Name,
			Location: location,
		},
		EnvContext: EnvironmentalContext{
			Type: "jeeves:EnvironmentalContext",
			ID:   fmt.Sprintf("urn:uuid:%s", uuid.New().String()),
		},
	}
}

func getTimeOfDay(t time.Time) string {
	hour := t.Hour()
	switch {
	case hour < 6:
		return "night"
	case hour < 12:
		return "morning"
	case hour < 17:
		return "afternoon"
	case hour < 21:
		return "evening"
	default:
		return "night"
	}
}
