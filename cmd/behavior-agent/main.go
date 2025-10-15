package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/saaga0h/jeeves-platform/internal/behavior"
	"github.com/saaga0h/jeeves-platform/pkg/config"
	"github.com/saaga0h/jeeves-platform/pkg/mqtt"
	"github.com/saaga0h/jeeves-platform/pkg/postgres"
	"github.com/saaga0h/jeeves-platform/pkg/redis"
)

func main() {
	// Standard bootstrap (consistent with other agents)
	cfg := config.NewConfig()
	cfg.ServiceName = "behavior-agent"
	cfg.LoadFromEnv()
	cfg.LoadFromFlags()

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Config error: %v\n", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	slog.SetDefault(logger)

	logger.Info("Starting Behavior Agent",
		"mqtt", cfg.MQTTAddress(),
		"redis", cfg.RedisAddress(),
		"postgres", fmt.Sprintf("%s:%d/%s", cfg.PostgresHost, cfg.PostgresPort, cfg.PostgresDB))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Initialize clients
	mqttClient := mqtt.NewClient(cfg, logger)
	redisClient := redis.NewClient(cfg, logger)

	// Initialize and connect postgres client
	pgClient := postgres.NewClient(cfg, logger)
	if err := pgClient.Connect(ctx); err != nil {
		logger.Error("Failed to connect to postgres", "error", err)
		os.Exit(1)
	} // Create behavior agent
	agent, err := behavior.NewAgent(mqttClient, redisClient, pgClient, cfg, logger)
	if err != nil {
		logger.Error("Failed to create agent", "error", err)
		os.Exit(1)
	}

	// Start agent
	agentErr := make(chan error, 1)
	go func() {
		if err := agent.Start(ctx); err != nil {
			agentErr <- err
		}
	}()

	// Wait for shutdown
	select {
	case <-sigChan:
		logger.Info("Shutdown signal received")
	case err := <-agentErr:
		logger.Error("Agent failed", "error", err)
	}

	cancel()
	agent.Stop()
	logger.Info("Behavior agent stopped")
}
