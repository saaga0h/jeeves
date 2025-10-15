package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	_ "github.com/lib/pq"
	"github.com/saaga0h/jeeves-platform/pkg/config"
)

// PostgresClient wraps a Postgres connection pool
type PostgresClient struct {
	db     *sql.DB
	config *config.Config
	logger *slog.Logger
}

// NewClient creates a new Postgres client
func NewClient(cfg *config.Config, logger *slog.Logger) Client {
	if logger == nil {
		logger = slog.Default()
	}

	return &PostgresClient{
		config: cfg,
		logger: logger,
	}
}

// Connect establishes connection to the database
func (c *PostgresClient) Connect(ctx context.Context) error {
	c.logger.Info("Connecting to Postgres",
		"host", c.config.PostgresHost,
		"port", c.config.PostgresPort,
		"database", c.config.PostgresDB)

	db, err := sql.Open("postgres", c.config.PostgresConnectionString())
	if err != nil {
		return fmt.Errorf("failed to open postgres connection: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(c.config.PostgresMaxConnections)
	db.SetMaxIdleConns(c.config.PostgresMaxIdleConnections)
	db.SetConnMaxLifetime(c.config.PostgresConnMaxLifetime)

	// Test connection
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping postgres: %w", err)
	}

	c.db = db
	c.logger.Info("Connected to Postgres successfully")

	return nil
}

// Disconnect closes the Postgres connection
func (c *PostgresClient) Disconnect() error {
	if c.db == nil {
		return nil
	}

	c.logger.Info("Disconnecting from Postgres")

	if err := c.db.Close(); err != nil {
		return fmt.Errorf("failed to close postgres connection: %w", err)
	}

	c.db = nil
	c.logger.Info("Disconnected from Postgres")

	return nil
}

// DB returns the underlying database connection pool
func (c *PostgresClient) DB() *sql.DB {
	return c.db
}

// IsConnected returns whether the client is connected
func (c *PostgresClient) IsConnected() bool {
	return c.db != nil
}

// Exec executes a query without returning rows
func (c *PostgresClient) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	if c.db == nil {
		return nil, fmt.Errorf("postgres client not connected")
	}
	return c.db.ExecContext(ctx, query, args...)
}

// Query executes a query that returns rows
func (c *PostgresClient) Query(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	if c.db == nil {
		return nil, fmt.Errorf("postgres client not connected")
	}
	return c.db.QueryContext(ctx, query, args...)
}

// QueryRow executes a query that returns a single row
func (c *PostgresClient) QueryRow(ctx context.Context, query string, args ...interface{}) *sql.Row {
	if c.db == nil {
		// Return a row that will return an error when scanned
		return &sql.Row{}
	}
	return c.db.QueryRowContext(ctx, query, args...)
}

// Transaction executes a function within a database transaction
func (c *PostgresClient) Transaction(ctx context.Context, fn func(*sql.Tx) error) error {
	if c.db == nil {
		return fmt.Errorf("postgres client not connected")
	}

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("failed to rollback transaction: %v (original error: %w)", rbErr, err)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// Ping tests the database connection
func (c *PostgresClient) Ping(ctx context.Context) error {
	if c.db == nil {
		return fmt.Errorf("postgres client not connected")
	}
	return c.db.PingContext(ctx)
}
