package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/saaga0h/jeeves-platform/e2e/internal/observer"
)

func main() {
	// Parse CLI arguments
	mqttBroker := flag.String("mqtt-broker", "mqtt://mosquitto:1883", "MQTT broker URL")
	outputDir := flag.String("output-dir", "./test-output/captures", "Output directory for captures")
	snapshotInterval := flag.Int("snapshot-interval", 30, "Snapshot interval in seconds")
	flag.Parse()

	// Set up logger
	logger := log.New(os.Stdout, "", log.Ltime)

	// Create observer
	obs := observer.NewObserver(*mqttBroker, logger)

	// Start observing
	logger.Printf("Starting MQTT observer...")
	if err := obs.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start observer: %v\n", err)
		os.Exit(1)
	}
	defer obs.Stop()

	logger.Printf("Observer running. Press Ctrl+C to stop.")

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Periodic snapshots
	ticker := time.NewTicker(time.Duration(*snapshotInterval) * time.Second)
	defer ticker.Stop()

	snapshotCount := 0

	for {
		select {
		case <-ticker.C:
			// Save periodic snapshot
			snapshotCount++
			timestamp := time.Now().Format("20060102-150405")
			filename := filepath.Join(*outputDir, fmt.Sprintf("snapshot-%s-%03d.json", timestamp, snapshotCount))

			if err := obs.SaveCapture(filename); err != nil {
				logger.Printf("Warning: Failed to save snapshot: %v", err)
			} else {
				logger.Printf("Snapshot saved: %s (%d messages)", filename, obs.GetMessageCount())
			}

		case <-sigChan:
			// Save final capture
			logger.Printf("Shutting down...")
			timestamp := time.Now().Format("20060102-150405")
			filename := filepath.Join(*outputDir, fmt.Sprintf("final-%s.json", timestamp))

			if err := obs.SaveCapture(filename); err != nil {
				logger.Printf("Warning: Failed to save final capture: %v", err)
			} else {
				logger.Printf("Final capture saved: %s (%d messages)", filename, obs.GetMessageCount())
			}

			return
		}
	}
}
