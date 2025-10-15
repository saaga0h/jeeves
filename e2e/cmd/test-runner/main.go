package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/saaga0h/jeeves-platform/e2e/internal/executor"
	"github.com/saaga0h/jeeves-platform/e2e/internal/reporter"
	"github.com/saaga0h/jeeves-platform/e2e/internal/scenario"
	"github.com/saaga0h/jeeves-platform/pkg/config"
	"github.com/saaga0h/jeeves-platform/pkg/postgres"
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

	// Create main config and set postgres values
	cfg := config.NewConfig()

	// Split host:port for postgres config
	parts := strings.Split(postgresHostPort, ":")
	cfg.PostgresHost = parts[0]
	if len(parts) > 1 {
		if port, err := strconv.Atoi(parts[1]); err == nil {
			cfg.PostgresPort = port
		}
	} else {
		cfg.PostgresPort = 5432 // default
	}

	cfg.PostgresUser = "jeeves"
	cfg.PostgresPassword = "jeeves_test"
	cfg.PostgresDB = "jeeves_behavior"
	cfg.PostgresSSLMode = "disable"

	// Create slog logger from log logger
	slogger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	pgClient := postgres.NewClient(cfg, slogger)
	if err := pgClient.Connect(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to postgres: %v\n", err)
		os.Exit(1)
	}

	runner := executor.NewRunner(*mqttBroker, *redisHost, pgClient, logger)

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
