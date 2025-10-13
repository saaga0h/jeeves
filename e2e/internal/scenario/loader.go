package scenario

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadScenario loads a scenario from a YAML file
func LoadScenario(filepath string) (*Scenario, error) {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to read scenario file: %w", err)
	}

	var scenario Scenario
	if err := yaml.Unmarshal(data, &scenario); err != nil {
		return nil, fmt.Errorf("failed to parse scenario YAML: %w", err)
	}

	// Validate the loaded scenario
	if err := ValidateScenario(&scenario); err != nil {
		return nil, fmt.Errorf("scenario validation failed: %w", err)
	}

	return &scenario, nil
}

// LoadScenarioFromBytes loads a scenario from byte data (useful for testing)
func LoadScenarioFromBytes(data []byte) (*Scenario, error) {
	var scenario Scenario
	if err := yaml.Unmarshal(data, &scenario); err != nil {
		return nil, fmt.Errorf("failed to parse scenario YAML: %w", err)
	}

	if err := ValidateScenario(&scenario); err != nil {
		return nil, fmt.Errorf("scenario validation failed: %w", err)
	}

	return &scenario, nil
}
