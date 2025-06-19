//go:build integration || e2e

package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/hongkongkiwi/postgres-db-fork/internal/config"
	"github.com/hongkongkiwi/postgres-db-fork/internal/db"

	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/stretchr/testify/require"
)

// TestConfig holds configuration for test environments
type TestConfig struct {
	PostgreSQLVersion string
	DatabaseName      string
	Username          string
	Password          string
	Host              string
	Port              string
}

// DefaultTestConfig returns default configuration for testing
func DefaultTestConfig() *TestConfig {
	return &TestConfig{
		PostgreSQLVersion: "13-alpine",
		DatabaseName:      "testdb",
		Username:          "testuser",
		Password:          "testpass",
		Host:              "localhost",
	}
}

// TestEnvironment represents a test environment with running PostgreSQL instance
type TestEnvironment struct {
	Pool     *dockertest.Pool
	Resource *dockertest.Resource
	Config   *TestConfig
	DB       *sql.DB
}

// setupFromCI connects to a PostgreSQL instance provided by a CI environment
func setupFromCI(t *testing.T) (*TestEnvironment, func()) {
	t.Log("CI environment detected, connecting to existing PostgreSQL service.")

	testConfig := &TestConfig{
		Host:         os.Getenv("POSTGRES_HOST"),
		Port:         os.Getenv("POSTGRES_PORT"),
		Username:     os.Getenv("POSTGRES_USER"),
		Password:     os.Getenv("POSTGRES_PASSWORD"),
		DatabaseName: os.Getenv("POSTGRES_DB"),
	}

	if testConfig.Host == "" || testConfig.Port == "" || testConfig.Username == "" || testConfig.DatabaseName == "" {
		t.Fatal("Incomplete PostgreSQL connection details in CI environment. Ensure POSTGRES_HOST, POSTGRES_PORT, POSTGRES_USER, POSTGRES_PASSWORD, and POSTGRES_DB are set.")
		return nil, nil
	}

	var db *sql.DB
	var err error

	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable", // pragma: allowlist secret
		testConfig.Username,
		testConfig.Password,
		testConfig.Host,
		testConfig.Port,
		testConfig.DatabaseName,
	)

	// Retry connection
	var attempts = 10
	for i := 0; i < attempts; i++ {
		db, err = sql.Open("postgres", dsn)
		if err == nil {
			err = db.Ping()
			if err == nil {
				break
			}
		}
		t.Logf("Waiting for PostgreSQL service... (%d/%d)", i+1, attempts)
		time.Sleep(2 * time.Second)
	}

	if err != nil {
		t.Fatalf("Could not connect to PostgreSQL service in CI: %v", err)
		return nil, nil
	}

	t.Log("Successfully connected to PostgreSQL service in CI.")

	// Cleanup function does nothing in CI as the service is managed by the workflow
	cleanup := func() {
		if err := db.Close(); err != nil {
			t.Logf("Failed to close connection in CI: %v", err)
		}
	}

	return &TestEnvironment{
		Pool:     nil, // No pool in CI
		Resource: nil, // No resource in CI
		Config:   testConfig,
		DB:       db,
	}, cleanup
}

// SetupTestEnvironment creates a test PostgreSQL instance using Docker
func SetupTestEnvironment(t *testing.T) (*TestEnvironment, func()) {
	// If POSTGRES_HOST is set, assume we are in CI and connect to the service container
	if os.Getenv("POSTGRES_HOST") != "" {
		return setupFromCI(t)
	}
	// Skip if CI environment doesn't support Docker
	if os.Getenv("SKIP_INTEGRATION_TESTS") == "true" {
		t.Skip("Skipping integration tests - Docker not available")
	}

	testConfig := DefaultTestConfig()

	// Create docker pool
	pool, err := dockertest.NewPool("")
	if err != nil {
		t.Skipf("Could not connect to docker: %s - skipping integration test", err)
		return nil, nil
	}

	// Pull PostgreSQL image and run container
	resource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "postgres",
		Tag:        testConfig.PostgreSQLVersion,
		Env: []string{
			"POSTGRES_PASSWORD=" + testConfig.Password,
			"POSTGRES_USER=" + testConfig.Username,
			"POSTGRES_DB=" + testConfig.DatabaseName,
			"listen_addresses = '*'",
		},
	}, func(config *docker.HostConfig) {
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})

	if err != nil {
		t.Skipf("Could not start postgres container: %s - skipping integration test", err)
		return nil, nil
	}

	testConfig.Host = "localhost"
	testConfig.Port = resource.GetPort("5432/tcp")

	// Set cleanup function
	cleanup := func() {
		if err := pool.Purge(resource); err != nil {
			t.Logf("Could not purge resource: %s", err)
		}
	}

	// Wait for PostgreSQL to be ready
	pool.MaxWait = 120 * time.Second
	var db *sql.DB

	if err := pool.Retry(func() error {
		var err error
		db, err = sql.Open("postgres", fmt.Sprintf(
			"postgres://%s:%s@%s:%s/%s?sslmode=disable", // pragma: allowlist secret
			testConfig.Username,
			testConfig.Password,
			testConfig.Host,
			testConfig.Port,
			testConfig.DatabaseName,
		))
		if err != nil {
			return err
		}
		return db.Ping()
	}); err != nil {
		cleanup()
		t.Skipf("Could not connect to postgres container: %s - skipping integration test", err)
		return nil, nil
	}

	return &TestEnvironment{
		Pool:     pool,
		Resource: resource,
		Config:   testConfig,
		DB:       db,
	}, cleanup
}

// CreateTestDatabase creates a test database in the test environment
func (te *TestEnvironment) CreateTestDatabase(t *testing.T, dbName string) {
	_, err := te.DB.Exec(fmt.Sprintf("CREATE DATABASE %s", dbName))
	require.NoError(t, err)
}

// CreateTestTable creates a test table with sample data
func (te *TestEnvironment) CreateTestTable(t *testing.T, dbName, tableName string, rowCount int) {
	// Connect to the specific database
	db, err := sql.Open("postgres", fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable", // pragma: allowlist secret
		te.Config.Username,
		te.Config.Password,
		te.Config.Host,
		te.Config.Port,
		dbName,
	))
	require.NoError(t, err)
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Failed to close database connection: %v", err)
		}
	}()

	// Create table
	_, err = db.Exec(fmt.Sprintf(`
		CREATE TABLE %s (
			id SERIAL PRIMARY KEY,
			name VARCHAR(100),
			email VARCHAR(100),
			created_at TIMESTAMP DEFAULT NOW()
		)
	`, tableName))
	require.NoError(t, err)

	// Insert sample data
	for i := 1; i <= rowCount; i++ {
		_, err = db.Exec(fmt.Sprintf(`
			INSERT INTO %s (name, email) VALUES ($1, $2)
		`, tableName), fmt.Sprintf("User %d", i), fmt.Sprintf("user%d@example.com", i))
		require.NoError(t, err)
	}
}

// GetDatabaseConfig returns a database config for the test environment
func (te *TestEnvironment) GetDatabaseConfig(dbName string) *config.DatabaseConfig {
	return &config.DatabaseConfig{
		Host:     te.Config.Host,
		Port:     parsePort(te.Config.Port),
		Username: te.Config.Username,
		Password: te.Config.Password,
		Database: dbName,
		SSLMode:  "disable",
	}
}

// CreateConnection creates a database connection for testing
func (te *TestEnvironment) CreateConnection(t *testing.T, dbName string) *db.Connection {
	cfg := te.GetDatabaseConfig(dbName)
	conn, err := db.NewConnection(cfg)
	require.NoError(t, err)
	return conn
}

// WaitForCondition waits for a condition to be true with timeout
func WaitForCondition(t *testing.T, condition func() bool, timeout time.Duration, message string) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("Timeout waiting for condition: %s", message)
		case <-ticker.C:
			if condition() {
				return
			}
		}
	}
}

// AssertDatabaseExists checks if a database exists
func (te *TestEnvironment) AssertDatabaseExists(t *testing.T, dbName string) {
	var exists bool
	err := te.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", dbName).Scan(&exists)
	require.NoError(t, err)
	require.True(t, exists, "Database %s should exist", dbName)
}

// AssertDatabaseNotExists checks if a database does not exist
func (te *TestEnvironment) AssertDatabaseNotExists(t *testing.T, dbName string) {
	var exists bool
	err := te.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", dbName).Scan(&exists)
	require.NoError(t, err)
	require.False(t, exists, "Database %s should not exist", dbName)
}

// AssertTableExists checks if a table exists in a database
func (te *TestEnvironment) AssertTableExists(t *testing.T, dbName, tableName string) {
	conn := te.CreateConnection(t, dbName)
	defer func() {
		if err := conn.Close(); err != nil {
			t.Logf("Failed to close connection: %v", err)
		}
	}()

	var exists bool
	err := conn.DB.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = $1
		)
	`, tableName).Scan(&exists)
	require.NoError(t, err)
	require.True(t, exists, "Table %s should exist in database %s", tableName, dbName)
}

// AssertRowCount checks the number of rows in a table
func (te *TestEnvironment) AssertRowCount(t *testing.T, dbName, tableName string, expectedCount int) {
	conn := te.CreateConnection(t, dbName)
	defer func() {
		if err := conn.Close(); err != nil {
			t.Logf("Failed to close connection: %v", err)
		}
	}()

	var count int
	err := conn.DB.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, expectedCount, count, "Table %s should have %d rows", tableName, expectedCount)
}

// parsePort converts string port to int
func parsePort(portStr string) int {
	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		// Default to 5432 if parsing fails
		return 5432
	}
	return port
}

// MockConfig creates a mock configuration for testing
func MockConfig() *config.ForkConfig {
	return &config.ForkConfig{
		Source: config.DatabaseConfig{
			Host:     "localhost",
			Port:     5432,
			Username: "testuser",
			Password: "testpass",
			Database: "sourcedb",
			SSLMode:  "disable",
		},
		Destination: config.DatabaseConfig{
			Host:     "localhost",
			Port:     5432,
			Username: "testuser",
			Password: "testpass",
			SSLMode:  "disable",
		},
		TargetDatabase: "targetdb",
		MaxConnections: 4,
		ChunkSize:      1000,
		Timeout:        30 * time.Minute,
		DropIfExists:   true,
	}
}

// CreateTestConfigFile creates a temporary config file for testing
func CreateTestConfigFile(t *testing.T, content string) (string, func()) {
	tmpFile, err := os.CreateTemp("", "test-config-*.yaml")
	require.NoError(t, err)

	_, err = tmpFile.WriteString(content)
	require.NoError(t, err)

	if err := tmpFile.Close(); err != nil {
		t.Logf("Failed to close temp file: %v", err)
	}

	cleanup := func() {
		if err := os.Remove(tmpFile.Name()); err != nil {
			t.Logf("Failed to remove temp file: %v", err)
		}
	}

	return tmpFile.Name(), cleanup
}
