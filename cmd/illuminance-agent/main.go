package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/saaga0h/jeeves-platform/internal/illuminance"
	"github.com/saaga0h/jeeves-platform/pkg/config"
	"github.com/saaga0h/jeeves-platform/pkg/health"
	"github.com/saaga0h/jeeves-platform/pkg/mqtt"
	"github.com/saaga0h/jeeves-platform/pkg/redis"
)

func main() {
	// Load configuration with hierarchy: defaults → env → flags
	cfg := config.NewConfig()
	cfg.ServiceName = "illuminance-agent"
	cfg.LoadFromEnv()
	cfg.LoadFromFlags()

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		os.Exit(1)
	}

	// Set up structured logging
	logLevel := parseLogLevel(cfg.LogLevel)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	logger.Info("Starting J.E.E.V.E.S. Illuminance Agent",
		"version", "2.0",
		"service_name", cfg.ServiceName,
		"mqtt_broker", cfg.MQTTAddress(),
		"redis_host", cfg.RedisAddress(),
		"log_level", cfg.LogLevel)

	// Set up context with cancellation for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Initialize MQTT client
	mqttClient := mqtt.NewClient(cfg, logger)

	// Initialize Redis client
	redisClient := redis.NewClient(cfg, logger)

	// Create illuminance agent
	agent := illuminance.NewAgent(mqttClient, redisClient, cfg, logger)

	// Start health check server
	healthChecker := health.NewChecker(mqttClient, redisClient, logger)
	httpServer := startHealthServer(cfg.HealthPort, healthChecker, logger)

	// Start agent in a goroutine
	agentErr := make(chan error, 1)
	go func() {
		if err := agent.Start(ctx); err != nil {
			logger.Error("Agent error", "error", err)
			agentErr <- err
		}
	}()

	// Wait for shutdown signal or agent error
	select {
	case <-sigChan:
		logger.Info("Shutdown signal received (SIGTERM/SIGINT)")
	case err := <-agentErr:
		logger.Error("Agent failed", "error", err)
	}

	// Graceful shutdown
	logger.Info("Initiating graceful shutdown")
	cancel()

	if err := agent.Stop(); err != nil {
		logger.Error("Error stopping agent", "error", err)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("Error shutting down health server", "error", err)
	}

	logger.Info("Illuminance agent shutdown complete")
}

func startHealthServer(port int, checker *health.Checker, logger *slog.Logger) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", checker.HandlerFunc())

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go func() {
		logger.Info("Starting health check server", "port", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Health server error", "error", err)
		}
	}()

	return server
}

func parseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
