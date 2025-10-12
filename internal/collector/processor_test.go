package collector

import (
	"encoding/json"
	"log/slog"
	"os"
	"testing"
)

func TestParseMessage(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	processor := NewProcessor(logger)

	tests := []struct {
		name        string
		topic       string
		payload     string
		wantType    string
		wantLoc     string
		wantErr     bool
		description string
	}{
		{
			name:        "valid motion message",
			topic:       "automation/raw/motion/study",
			payload:     `{"data":{"state":"on","entity_id":"binary_sensor.motion_study","sequence":42}}`,
			wantType:    "motion",
			wantLoc:     "study",
			wantErr:     false,
			description: "Should parse valid motion sensor message",
		},
		{
			name:        "valid temperature message",
			topic:       "automation/raw/temperature/living_room",
			payload:     `{"data":{"value":22.5,"unit":"°C"}}`,
			wantType:    "temperature",
			wantLoc:     "living_room",
			wantErr:     false,
			description: "Should parse valid temperature sensor message",
		},
		{
			name:        "valid illuminance message",
			topic:       "automation/raw/illuminance/bedroom",
			payload:     `{"data":{"value":450.0,"unit":"lux"}}`,
			wantType:    "illuminance",
			wantLoc:     "bedroom",
			wantErr:     false,
			description: "Should parse valid illuminance sensor message",
		},
		{
			name:        "invalid topic format",
			topic:       "invalid/topic",
			payload:     `{"data":{}}`,
			wantType:    "",
			wantLoc:     "",
			wantErr:     true,
			description: "Should fail on invalid topic format",
		},
		{
			name:        "invalid JSON payload",
			topic:       "automation/raw/motion/study",
			payload:     `{invalid json}`,
			wantType:    "",
			wantLoc:     "",
			wantErr:     true,
			description: "Should fail on invalid JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := processor.ParseMessage(tt.topic, []byte(tt.payload))

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseMessage() expected error but got none: %s", tt.description)
				}
				return
			}

			if err != nil {
				t.Errorf("ParseMessage() unexpected error: %v (%s)", err, tt.description)
				return
			}

			if msg.SensorType != tt.wantType {
				t.Errorf("ParseMessage() sensorType = %v, want %v", msg.SensorType, tt.wantType)
			}

			if msg.Location != tt.wantLoc {
				t.Errorf("ParseMessage() location = %v, want %v", msg.Location, tt.wantLoc)
			}

			if msg.OriginalTopic != tt.topic {
				t.Errorf("ParseMessage() originalTopic = %v, want %v", msg.OriginalTopic, tt.topic)
			}
		})
	}
}

func TestBuildMotionData(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	processor := NewProcessor(logger)

	tests := []struct {
		name        string
		payload     string
		wantState   string
		wantSeq     int
		description string
	}{
		{
			name:        "motion on with all fields",
			payload:     `{"data":{"state":"on","entity_id":"binary_sensor.motion_study","sequence":42}}`,
			wantState:   "on",
			wantSeq:     42,
			description: "Should parse motion on with all fields",
		},
		{
			name:        "motion off",
			payload:     `{"data":{"state":"off","entity_id":"binary_sensor.motion_study","sequence":43}}`,
			wantState:   "off",
			wantSeq:     43,
			description: "Should parse motion off",
		},
		{
			name:        "motion with defaults",
			payload:     `{"data":{}}`,
			wantState:   "unknown",
			wantSeq:     0,
			description: "Should use defaults for missing fields",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := processor.ParseMessage("automation/raw/motion/study", []byte(tt.payload))
			if err != nil {
				t.Fatalf("ParseMessage() failed: %v", err)
			}

			motionData := processor.BuildMotionData(msg)

			if motionData.State != tt.wantState {
				t.Errorf("BuildMotionData() state = %v, want %v", motionData.State, tt.wantState)
			}

			if motionData.Sequence != tt.wantSeq {
				t.Errorf("BuildMotionData() sequence = %v, want %v", motionData.Sequence, tt.wantSeq)
			}

			if motionData.Timestamp == "" {
				t.Error("BuildMotionData() timestamp should not be empty")
			}

			if motionData.CollectedAt == 0 {
				t.Error("BuildMotionData() collectedAt should not be zero")
			}
		})
	}
}

func TestBuildEnvironmentalData(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	processor := NewProcessor(logger)

	tests := []struct {
		name        string
		topic       string
		payload     string
		wantTemp    *float64
		wantIllum   *float64
		description string
	}{
		{
			name:        "temperature reading",
			topic:       "automation/raw/temperature/living_room",
			payload:     `{"data":{"value":22.5,"unit":"°C"}}`,
			wantTemp:    floatPtr(22.5),
			wantIllum:   nil,
			description: "Should parse temperature reading",
		},
		{
			name:        "illuminance reading",
			topic:       "automation/raw/illuminance/bedroom",
			payload:     `{"data":{"value":450.0,"unit":"lux"}}`,
			wantTemp:    nil,
			wantIllum:   floatPtr(450.0),
			description: "Should parse illuminance reading",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := processor.ParseMessage(tt.topic, []byte(tt.payload))
			if err != nil {
				t.Fatalf("ParseMessage() failed: %v", err)
			}

			envData := processor.BuildEnvironmentalData(msg)

			if tt.wantTemp != nil {
				if envData.Temperature == nil {
					t.Error("BuildEnvironmentalData() temperature should not be nil")
				} else if *envData.Temperature != *tt.wantTemp {
					t.Errorf("BuildEnvironmentalData() temperature = %v, want %v", *envData.Temperature, *tt.wantTemp)
				}
			}

			if tt.wantIllum != nil {
				if envData.Illuminance == nil {
					t.Error("BuildEnvironmentalData() illuminance should not be nil")
				} else if *envData.Illuminance != *tt.wantIllum {
					t.Errorf("BuildEnvironmentalData() illuminance = %v, want %v", *envData.Illuminance, *tt.wantIllum)
				}
			}
		})
	}
}

func TestBuildTriggerPayload(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	processor := NewProcessor(logger)

	payload := `{"data":{"state":"on"}}`
	msg, err := processor.ParseMessage("automation/raw/motion/study", []byte(payload))
	if err != nil {
		t.Fatalf("ParseMessage() failed: %v", err)
	}

	triggerPayload, err := processor.BuildTriggerPayload(msg)
	if err != nil {
		t.Fatalf("BuildTriggerPayload() failed: %v", err)
	}

	if len(triggerPayload) == 0 {
		t.Error("BuildTriggerPayload() should return non-empty payload")
	}

	// Verify it's valid JSON
	var result map[string]interface{}
	if err := json.Unmarshal(triggerPayload, &result); err != nil {
		t.Errorf("BuildTriggerPayload() produced invalid JSON: %v", err)
	}

	// Verify required fields
	if _, ok := result["data"]; !ok {
		t.Error("BuildTriggerPayload() missing 'data' field")
	}
	if _, ok := result["original_topic"]; !ok {
		t.Error("BuildTriggerPayload() missing 'original_topic' field")
	}
	if _, ok := result["stored_at"]; !ok {
		t.Error("BuildTriggerPayload() missing 'stored_at' field")
	}
}

// Helper function to create float pointers
func floatPtr(f float64) *float64 {
	return &f
}
