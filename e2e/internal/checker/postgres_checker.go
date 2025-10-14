package checker

import (
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"

	_ "github.com/lib/pq"
)

// PostgresChecker validates database state
type PostgresChecker struct {
	db     *sql.DB
	logger *log.Logger
}

// NewPostgresChecker creates a new Postgres checker
func NewPostgresChecker(connStr string, logger *log.Logger) (*PostgresChecker, error) {
	if logger == nil {
		logger = log.Default()
	}

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	logger.Printf("Connected to Postgres")

	return &PostgresChecker{db: db, logger: logger}, nil
}

// CheckQuery executes a query and compares result
func (p *PostgresChecker) CheckQuery(query string, expected interface{}) error {
	p.logger.Printf("Executing query: %s", query)

	var result interface{}
	err := p.db.QueryRow(query).Scan(&result)
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}

	p.logger.Printf("Query result: %v (expected: %v)", result, expected)

	return p.compareResults(result, expected)
}

func (p *PostgresChecker) compareResults(actual, expected interface{}) error {
	// Handle approximate matches: "~10" means 8-12
	if expectedStr, ok := expected.(string); ok {
		if strings.HasPrefix(expectedStr, "~") {
			return p.compareApproximate(actual, expectedStr)
		}
	}

	// Exact match
	actualStr := fmt.Sprintf("%v", actual)
	expectedStr := fmt.Sprintf("%v", expected)

	if actualStr == expectedStr {
		return nil
	}

	return fmt.Errorf("mismatch: expected %v, got %v", expected, actual)
}

func (p *PostgresChecker) compareApproximate(actual interface{}, expectedStr string) error {
	// Parse "~10" as target 10 with ±20% tolerance
	targetStr := strings.TrimPrefix(expectedStr, "~")
	target, err := strconv.ParseFloat(targetStr, 64)
	if err != nil {
		return fmt.Errorf("invalid approximate value: %s", expectedStr)
	}

	// Convert actual to float
	var actualFloat float64
	switch v := actual.(type) {
	case int64:
		actualFloat = float64(v)
	case float64:
		actualFloat = v
	case string:
		actualFloat, err = strconv.ParseFloat(v, 64)
		if err != nil {
			return fmt.Errorf("cannot convert actual value to number: %v", actual)
		}
	default:
		return fmt.Errorf("unsupported type for approximate comparison: %T", actual)
	}

	// 20% tolerance
	tolerance := target * 0.2
	if actualFloat >= (target-tolerance) && actualFloat <= (target+tolerance) {
		return nil
	}

	return fmt.Errorf("value %.2f not within ±20%% of %.0f", actualFloat, target)
}

// Close closes the database connection
func (p *PostgresChecker) Close() error {
	if p.db != nil {
		return p.db.Close()
	}
	return nil
}
