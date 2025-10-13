package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/saaga0h/jeeves-platform/e2e/internal/executor"
	"github.com/saaga0h/jeeves-platform/e2e/internal/reporter"
	"github.com/saaga0h/jeeves-platform/e2e/internal/scenario"
)

func main() {
	// Parse CLI arguments
	scenarioPath := flag.String("scenario", "", "Path to YAML scenario file (required)")
	mqttBroker := flag.String("mqtt-broker", "mqtt://mosquitto:1883", "MQTT broker URL")
	redisHost := flag.String("redis-host", "redis:6379", "Redis host")
	outputDir := flag.String("output-dir", "./test-output", "Output directory for test artifacts")
	verbose := flag.Bool("verbose", false, "Enable verbose logging")
	flag.Parse()

	if *scenarioPath == "" {
		fmt.Fprintf(os.Stderr, "Error: --scenario is required\n")
		flag.Usage()
		os.Exit(1)
	}

	// Set up logger
	logger := log.New(os.Stdout, "", log.Ltime)
	if !*verbose {
		logger.SetOutput(os.Stderr)
	}

	// Load scenario
	logger.Printf("Loading scenario from %s", *scenarioPath)
	scen, err := scenario.LoadScenario(*scenarioPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load scenario: %v\n", err)
		os.Exit(1)
	}

	// Create runner
	runner := executor.NewRunner(*mqttBroker, *redisHost, logger)

	// Run scenario
	ctx := context.Background()
	result, timelineEvents, err := runner.Run(ctx, scen)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Test execution failed: %v\n", err)
		os.Exit(1)
	}

	// Extract scenario name for filenames
	scenarioName := strings.TrimSuffix(filepath.Base(*scenarioPath), ".yaml")

	// Generate timeline report
	timeline := reporter.GenerateTimeline(result, timelineEvents)
	fmt.Println(timeline)

	// Save timeline to file
	timelinePath := filepath.Join(*outputDir, "timelines", scenarioName+".txt")
	if err := reporter.SaveTimeline(timeline, timelinePath); err != nil {
		logger.Printf("Warning: Failed to save timeline: %v", err)
	} else {
		logger.Printf("Timeline saved to %s", timelinePath)
	}

	// Save MQTT capture
	capturePath := filepath.Join(*outputDir, "captures", scenarioName+".json")
	if err := runner.SaveCapture(capturePath); err != nil {
		logger.Printf("Warning: Failed to save capture: %v", err)
	} else {
		logger.Printf("MQTT capture saved to %s", capturePath)
	}

	// Save summary
	summaryPath := filepath.Join(*outputDir, "summaries", scenarioName+".json")
	if err := reporter.SaveSummary(result, summaryPath); err != nil {
		logger.Printf("Warning: Failed to save summary: %v", err)
	} else {
		logger.Printf("Summary saved to %s", summaryPath)
	}

	// Exit with appropriate status code
	if result.Passed {
		os.Exit(0)
	} else {
		os.Exit(1)
	}
}
