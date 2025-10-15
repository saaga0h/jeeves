package postgres

import (
	"context"
	"fmt"
	"time"
)

// HealthStatus represents the health of the Postgres connection
type HealthStatus struct {
	Connected     bool      `json:"connected"`
	ServerVersion string    `json:"server_version,omitempty"`
	Database      string    `json:"database"`
	Error         string    `json:"error,omitempty"`
	Timestamp     time.Time `json:"timestamp"`
}

// HealthCheck performs a health check on the PostgreSQL connection
func (c *PostgresClient) HealthCheck(ctx context.Context) (*HealthStatus, error) {
	status := HealthStatus{
		Database:  c.config.PostgresDB,
		Timestamp: time.Now(),
	}

	if c.db == nil {
		status.Connected = false
		status.Error = "not connected"
		return &status, nil
	}

	// Test connection with ping
	if err := c.db.PingContext(ctx); err != nil {
		status.Connected = false
		status.Error = fmt.Sprintf("ping failed: %v", err)
		return &status, nil
	}

	// Get server version
	var version string
	err := c.db.QueryRowContext(ctx, "SELECT version()").Scan(&version)
	if err != nil {
		status.Connected = true // Ping worked
		status.Error = fmt.Sprintf("failed to get version: %v", err)
		return &status, nil
	}

	status.Connected = true
	status.ServerVersion = version

	return &status, nil
}
