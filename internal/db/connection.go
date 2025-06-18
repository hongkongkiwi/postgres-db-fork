package db

import (
	"database/sql"
	"fmt"
	"time"

	"postgres-db-fork/internal/config"

	_ "github.com/lib/pq"
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
		db.Close()
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
	var exists bool
	query := "SELECT EXISTS(SELECT datname FROM pg_catalog.pg_database WHERE datname = $1)"
	err := c.DB.QueryRow(query, dbName).Scan(&exists)
	return exists, err
}

// CreateDatabase creates a new database
func (c *Connection) CreateDatabase(dbName string, templateDB string) error {
	query := fmt.Sprintf("CREATE DATABASE %s", dbName)
	if templateDB != "" {
		query += fmt.Sprintf(" WITH TEMPLATE %s", templateDB)
	}

	logrus.Infof("Creating database: %s", dbName)
	_, err := c.DB.Exec(query)
	return err
}

// DropDatabase drops a database
func (c *Connection) DropDatabase(dbName string) error {
	// First, terminate all connections to the database
	terminateQuery := `
		SELECT pg_terminate_backend(pid)
		FROM pg_stat_activity 
		WHERE datname = $1 AND pid <> pg_backend_pid()`

	logrus.Infof("Terminating connections to database: %s", dbName)
	_, err := c.DB.Exec(terminateQuery, dbName)
	if err != nil {
		logrus.Warnf("Failed to terminate connections: %v", err)
	}

	// Wait a moment for connections to close
	time.Sleep(100 * time.Millisecond)

	// Drop the database
	dropQuery := fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName)
	logrus.Infof("Dropping database: %s", dbName)
	_, err = c.DB.Exec(dropQuery)
	return err
}

// GetDatabaseSize returns the size of a database in bytes
func (c *Connection) GetDatabaseSize(dbName string) (int64, error) {
	var size int64
	query := "SELECT pg_database_size($1)"
	err := c.DB.QueryRow(query, dbName).Scan(&size)
	return size, err
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
	defer rows.Close()

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
