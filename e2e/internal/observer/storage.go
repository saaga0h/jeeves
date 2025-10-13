package observer

import (
	"fmt"
	"os"
	"path/filepath"
)

// saveToFile writes data to a file, creating directories if needed
func saveToFile(filename string, data []byte) error {
	// Ensure directory exists
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Write file
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}
