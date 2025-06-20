package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/hongkongkiwi/postgres-db-fork/internal/config"

	"github.com/lib/pq"
	"github.com/sirupsen/logrus"
)

// Connection represents a PostgreSQL database connection
type Connection struct {
	DB     *sql.DB
	Config *config.DatabaseConfig
}

// NewConnection creates a new database connection
func NewConnection(cfg *config.DatabaseConfig) (*Connection, error) {
	db, err := sql.Open("postgres", cfg.ConnectionString())
	if err != nil {
		return nil, fmt.Errorf("failed to open database connection: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Test the connection
	if err := db.Ping(); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			logrus.WithError(closeErr).Error("Failed to close database connection after ping failure")
		}
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	logrus.Debugf("Connected to PostgreSQL at %s:%d", cfg.Host, cfg.Port)

	return &Connection{
		DB:     db,
		Config: cfg,
	}, nil
}

// Close closes the database connection
func (c *Connection) Close() error {
	if c.DB != nil {
		return c.DB.Close()
	}
	return nil
}

// DatabaseExists checks if a database exists
func (c *Connection) DatabaseExists(dbName string) (bool, error) {
	query := "SELECT 1 FROM pg_database WHERE datname = $1"
	var exists int
	err := c.DB.QueryRow(query, dbName).Scan(&exists)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// CreateDatabase creates a new database using template-based cloning
func (c *Connection) CreateDatabase(targetDB, sourceDB string, dropIfExists bool) error {
	if dropIfExists {
		if err := c.DropDatabase(targetDB); err != nil {
			logrus.WithError(err).Warnf("Could not drop existing database %s (may not exist)", targetDB)
		}
	}

	// Retry logic for database creation (handle concurrent access to source database)
	maxRetries := 5
	retryDelay := time.Millisecond * 500

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			logrus.Debugf("Retry attempt %d/%d for creating database %s", attempt, maxRetries, targetDB)
			time.Sleep(retryDelay)
			retryDelay *= 2 // exponential backoff
		}

		query := fmt.Sprintf(
			"CREATE DATABASE %s WITH TEMPLATE %s",
			pq.QuoteIdentifier(targetDB),
			pq.QuoteIdentifier(sourceDB),
		)

		_, err := c.DB.Exec(query)
		if err != nil {
			// Check if it's a "being accessed by other users" error
			if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "55006" {
				if attempt < maxRetries {
					logrus.Debugf("Source database %s is being accessed by other users, retrying... (attempt %d/%d)", sourceDB, attempt, maxRetries)
					continue
				}
			}
			return fmt.Errorf("failed to create database %s after %d attempts: %w", targetDB, maxRetries, err)
		}

		logrus.Infof("Created database %s using template %s", targetDB, sourceDB)
		return nil
	}

	return fmt.Errorf("failed to create database %s after %d attempts: max retries exceeded", targetDB, maxRetries)
}

// DropDatabase drops a database if it exists
func (c *Connection) DropDatabase(dbName string) error {
	// Retry logic for database drops (handle concurrent connections)
	maxRetries := 5
	retryDelay := time.Millisecond * 500

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			logrus.Debugf("Retry attempt %d/%d for dropping database %s", attempt, maxRetries, dbName)
			time.Sleep(retryDelay)
			retryDelay *= 2 // exponential backoff
		}

		// First, terminate all connections to the database
		terminateQuery := `
			SELECT pg_terminate_backend(pid)
			FROM pg_stat_activity
			WHERE datname = $1 AND pid <> pg_backend_pid() AND state = 'active'`

		result, err := c.DB.Exec(terminateQuery, dbName)
		if err != nil {
			logrus.WithError(err).Debugf("Could not terminate connections to database %s (attempt %d)", dbName, attempt)
		} else {
			if rowsAffected, _ := result.RowsAffected(); rowsAffected > 0 {
				logrus.Debugf("Terminated %d connections to database %s", rowsAffected, dbName)
				// Give a moment for connections to actually terminate
				time.Sleep(time.Millisecond * 100)
			}
		}

		// Try to drop the database
		query := fmt.Sprintf("DROP DATABASE IF EXISTS %s", pq.QuoteIdentifier(dbName))
		_, err = c.DB.Exec(query)
		if err != nil {
			// Check if it's a "being accessed by other users" error
			if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "55006" {
				if attempt < maxRetries {
					logrus.Debugf("Database %s is being accessed by other users, retrying... (attempt %d/%d)", dbName, attempt, maxRetries)
					continue
				}
			}
			return fmt.Errorf("failed to drop database %s after %d attempts: %w", dbName, maxRetries, err)
		}

		logrus.Infof("Dropped database %s", dbName)
		return nil
	}

	return fmt.Errorf("failed to drop database %s after %d attempts: max retries exceeded", dbName, maxRetries)
}

// GetDatabaseSize returns the size of a database in bytes
func (c *Connection) GetDatabaseSize(dbName string) (int64, error) {
	var size int64
	query := "SELECT pg_database_size($1)"
	err := c.DB.QueryRow(query, dbName).Scan(&size)
	return size, err
}

// GetVersion returns the PostgreSQL server version string
func (c *Connection) GetVersion() (string, error) {
	var version string
	err := c.DB.QueryRow("SHOW server_version").Scan(&version)
	if err != nil {
		return "", fmt.Errorf("failed to get server version: %w", err)
	}
	return version, nil
}

// GetTableList returns a list of tables in the database
func (c *Connection) GetTableList(schemaName string) ([]string, error) {
	if schemaName == "" {
		schemaName = "public"
	}

	query := `
		SELECT tablename
		FROM pg_tables
		WHERE schemaname = $1
		ORDER BY tablename`

	rows, err := c.DB.Query(query, schemaName)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logrus.WithError(err).Error("Failed to close rows")
		}
	}()

	var tables []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return nil, err
		}
		tables = append(tables, tableName)
	}

	return tables, rows.Err()
}

// TerminateAllConnections terminates all connections to the specified database except for the current one
func (c *Connection) TerminateAllConnections(dbName string) error {
	terminateSQL := `
		SELECT pg_terminate_backend(pg_stat_activity.pid)
		FROM pg_stat_activity
		WHERE pg_stat_activity.datname = $1
		  AND pid <> pg_backend_pid();
	`
	_, err := c.DB.Exec(terminateSQL, dbName)
	if err != nil {
		return fmt.Errorf("failed to terminate connections for database %s: %w", dbName, err)
	}
	logrus.Infof("Terminated all connections to database %s", dbName)
	return nil
}
