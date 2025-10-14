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
	// Parse CLI arguments - environment variables will override flag defaults
	scenarioPath := flag.String("scenario", "", "Path to YAML scenario file (required)")
	mqttBroker := flag.String("mqtt-broker", "mqtt://mosquitto:1883", "MQTT broker URL")
	redisHost := flag.String("redis-host", "redis:6379", "Redis host")
	postgresHost := flag.String("postgres-host", "postgres:5432", "PostgreSQL host:port")
	outputDir := flag.String("output-dir", "./test-output", "Output directory for test artifacts")
	verbose := flag.Bool("verbose", false, "Enable verbose logging")
	flag.Parse()

	// Override with environment variables if set
	if broker := os.Getenv("MQTT_BROKER"); broker != "" {
		*mqttBroker = broker
	}
	if redis := os.Getenv("REDIS_HOST"); redis != "" {
		*redisHost = redis
	}
	if postgres := os.Getenv("POSTGRES_HOST"); postgres != "" {
		*postgresHost = postgres
	}

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

	// Parse PostgreSQL host:port from flag (which gets env override) or use default
	postgresHostPort := *postgresHost

	// Split host:port
	parts := strings.Split(postgresHostPort, ":")
	pgHost := parts[0]
	pgPort := "5432" // default
	if len(parts) > 1 {
		pgPort = parts[1]
	}

	// Construct connection string
	postgresConn := fmt.Sprintf(
		"host=%s port=%s user=jeeves password=jeeves_test dbname=jeeves_behavior sslmode=disable",
		pgHost, pgPort,
	)

	runner := executor.NewRunner(*mqttBroker, *redisHost, postgresConn, logger)

	// postgresConn := fmt.Sprintf("host=%s port=5432 user=jeeves password=jeeves_test dbname=jeeves_behavior sslmode=disable",
	// 	os.Getenv("POSTGRES_HOST"))
	// // Create runner
	// runner := executor.NewRunner(*mqttBroker, *redisHost, postgresConn, logger)

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
