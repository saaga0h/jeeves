package postgres

import (
	"context"
	"database/sql"
)

// Client represents a PostgreSQL client interface for testing and abstraction
type Client interface {
	// Connect establishes a connection to the PostgreSQL database
	Connect(ctx context.Context) error

	// Disconnect closes the connection to the PostgreSQL database
	Disconnect() error

	// Exec executes a query without returning any rows
	Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error)

	// Query executes a query that returns rows
	Query(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)

	// QueryRow executes a query that is expected to return at most one row
	QueryRow(ctx context.Context, query string, args ...interface{}) *sql.Row

	// Transaction executes a function within a database transaction
	Transaction(ctx context.Context, fn func(*sql.Tx) error) error

	// HealthCheck performs a health check on the database connection
	HealthCheck(ctx context.Context) (*HealthStatus, error)
}
