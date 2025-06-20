package testutil

import (
	"database/sql"
	"fmt"
	"net"
	"os"
	"strconv"
	"testing"

	"github.com/hongkongkiwi/postgres-db-fork/internal/config"

	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/stretchr/testify/require"
)

// TestEnvironment encapsulates a Docker-based PostgreSQL test environment
type TestEnvironment struct {
	t        testing.TB
	pool     *dockertest.Pool
	resource *dockertest.Resource
	db       *sql.DB
	host     string
	port     int
}

// SetupTestEnvironment creates a temporary PostgreSQL database in Docker
func SetupTestEnvironment(t testing.TB) (*TestEnvironment, func()) {
	if os.Getenv("CI") != "" {
		t.Skip("Skipping Docker tests in CI environment")
		return nil, nil
	}

	pool, err := dockertest.NewPool("")
	require.NoError(t, err, "Could not connect to Docker")

	resource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "postgres",
		Tag:        "13",
		Env: []string{
			"POSTGRES_PASSWORD=secret", // pragma: allowlist secret
			"POSTGRES_USER=user",
			"POSTGRES_DB=testdb",
			"listen_addresses = '*'",
		},
	}, func(config *docker.HostConfig) {
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})
	require.NoError(t, err, "Could not start resource")

	hostAndPort := resource.GetHostPort("5432/tcp")
	host, portStr, _ := net.SplitHostPort(hostAndPort)
	port, _ := strconv.Atoi(portStr)

	env := &TestEnvironment{
		t:        t,
		pool:     pool,
		resource: resource,
		host:     host,
		port:     port,
	}

	// Wait for the database to be ready
	require.NoError(t, pool.Retry(func() error {
		var err error
		db, err := sql.Open("postgres", fmt.Sprintf("postgres://user:secret@%s/testdb?sslmode=disable", hostAndPort))
		if err != nil {
			return err
		}
		env.db = db
		return db.Ping()
	}), "Could not connect to Docker-based PostgreSQL")

	cleanup := func() {
		require.NoError(t, pool.Purge(resource), "Could not purge resource")
	}

	return env, cleanup
}

// GetDatabaseConfig returns a config for a database in the test environment
func (e *TestEnvironment) GetDatabaseConfig(dbName string) *config.DatabaseConfig {
	return &config.DatabaseConfig{
		Host:     e.host,
		Port:     e.port,
		Username: "user",
		Password: "secret",
		Database: dbName,
		SSLMode:  "disable",
	}
}

// CreateTestDatabase creates a new database in the test environment
func (e *TestEnvironment) CreateTestDatabase(t testing.TB, dbName string) {
	_, err := e.db.Exec(fmt.Sprintf("CREATE DATABASE %s", dbName))
	require.NoError(t, err, "Failed to create test database")
}

// CreateTestTable creates a test table with some data
func (e *TestEnvironment) CreateTestTable(t testing.TB, dbName, tableName string, numRows int) {
	conn := e.CreateConnection(t, dbName)
	defer require.NoError(t, conn.Close())

	_, err := conn.Exec(fmt.Sprintf(`
		CREATE TABLE %s (
			id SERIAL PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW()
		)
	`, tableName))
	require.NoError(t, err)

	for i := 0; i < numRows; i++ {
		_, err := conn.Exec(fmt.Sprintf("INSERT INTO %s (name) VALUES ($1)", tableName), fmt.Sprintf("test-name-%d", i))
		require.NoError(t, err)
	}
}

// AssertRowCount asserts the number of rows in a table
func (e *TestEnvironment) AssertRowCount(t testing.TB, dbName, tableName string, expectedRows int) {
	conn := e.CreateConnection(t, dbName)
	defer require.NoError(t, conn.Close())

	var count int
	err := conn.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, expectedRows, count)
}

// AssertDatabaseExists checks if a database exists
func (e *TestEnvironment) AssertDatabaseExists(t testing.TB, dbName string) {
	var exists bool
	err := e.db.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", dbName).Scan(&exists)
	require.NoError(t, err, "Failed to check if database exists")
	require.True(t, exists, "Database '%s' should exist", dbName)
}

// AssertDatabaseNotExists checks if a database does not exist
func (e *TestEnvironment) AssertDatabaseNotExists(t testing.TB, dbName string) {
	var exists bool
	err := e.db.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", dbName).Scan(&exists)
	require.NoError(t, err, "Failed to check if database exists")
	require.False(t, exists, "Database '%s' should not exist", dbName)
}

// AssertTableExists checks if a table exists in a specific database
func (e *TestEnvironment) AssertTableExists(t testing.TB, dbName, tableName string) {
	conn := e.CreateConnection(t, dbName)
	defer require.NoError(t, conn.Close())

	var exists bool
	err := conn.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = $1
		)
	`, tableName).Scan(&exists)
	require.NoError(t, err, "Failed to check if table exists")
	require.True(t, exists, "Table '%s' should exist in database '%s'", tableName, dbName)
}

// CreateConnection creates a new connection to a specific database in the test environment
func (e *TestEnvironment) CreateConnection(t testing.TB, dbName string) *sql.DB {
	connStr := fmt.Sprintf("postgres://user:secret@%s/%s?sslmode=disable", e.resource.GetHostPort("5432/tcp"), dbName)
	db, err := sql.Open("postgres", connStr)
	require.NoError(t, err, "Failed to connect to test database")
	return db
}

// Other assertion helpers (AssertDatabaseExists, AssertTableExists, etc.) would go here
// ...
