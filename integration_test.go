//go:build integration

package main_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/hongkongkiwi/postgres-db-fork/internal/config"
	"github.com/hongkongkiwi/postgres-db-fork/internal/fork"
	"github.com/hongkongkiwi/postgres-db-fork/internal/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSameServerFork_Integration(t *testing.T) {
	// Set up test environment
	env, cleanup := testutil.SetupTestEnvironment(t)
	if env == nil {
		t.Skip("Skipping integration test")
		return
	}
	defer cleanup()

	// Create source database with test data
	sourceDB := "source_test_db"
	targetDB := "target_test_db"

	env.CreateTestDatabase(t, sourceDB)
	env.CreateTestTable(t, sourceDB, "users", 100)
	env.CreateTestTable(t, sourceDB, "orders", 50)

	// Configure fork operation
	cfg := &config.ForkConfig{
		Source:         *env.GetDatabaseConfig(sourceDB),
		Destination:    *env.GetDatabaseConfig("postgres"), // Use postgres DB for admin operations
		TargetDatabase: targetDB,
		DropIfExists:   true,
		MaxConnections: 2,
		ChunkSize:      50,
		Timeout:        5 * time.Minute,
	}

	// Execute fork
	forker := fork.NewForker(cfg)
	err := forker.Fork(context.Background())
	require.NoError(t, err)

	// Verify target database was created
	env.AssertDatabaseExists(t, targetDB)

	// Verify data was copied
	env.AssertTableExists(t, targetDB, "users")
	env.AssertTableExists(t, targetDB, "orders")
	env.AssertRowCount(t, targetDB, "users", 100)
	env.AssertRowCount(t, targetDB, "orders", 50)
}

func TestCrossServerFork_Integration(t *testing.T) {
	// Set up two test environments to simulate cross-server fork
	sourceEnv, sourceCleanup := testutil.SetupTestEnvironment(t)
	if sourceEnv == nil {
		return // Test was skipped
	}
	defer sourceCleanup()

	destEnv, destCleanup := testutil.SetupTestEnvironment(t)
	if destEnv == nil {
		return // Test was skipped
	}
	defer destCleanup()

	// Create source database with test data
	sourceDB := "source_db"
	targetDB := "target_db"

	sourceEnv.CreateTestDatabase(t, sourceDB)
	sourceEnv.CreateTestTable(t, sourceDB, "customers", 75)
	sourceEnv.CreateTestTable(t, sourceDB, "products", 25)

	// Create target database
	destEnv.CreateTestDatabase(t, targetDB)

	// Configure cross-server fork
	cfg := &config.ForkConfig{
		Source:         *sourceEnv.GetDatabaseConfig(sourceDB),
		Destination:    *destEnv.GetDatabaseConfig(targetDB),
		TargetDatabase: targetDB,
		DropIfExists:   true,
		MaxConnections: 2,
		ChunkSize:      25,
		Timeout:        10 * time.Minute,
	}

	// Execute fork
	forker := fork.NewForker(cfg)
	err := forker.Fork(context.Background())
	require.NoError(t, err)

	// Verify data was copied to destination
	destEnv.AssertTableExists(t, targetDB, "customers")
	destEnv.AssertTableExists(t, targetDB, "products")
	destEnv.AssertRowCount(t, targetDB, "customers", 75)
	destEnv.AssertRowCount(t, targetDB, "products", 25)
}

func TestForkWithTableFiltering_Integration(t *testing.T) {
	env, cleanup := testutil.SetupTestEnvironment(t)
	if env == nil {
		return
	}
	defer cleanup()

	sourceDB := "source_filtered"
	targetDB := "target_filtered"

	env.CreateTestDatabase(t, sourceDB)
	env.CreateTestTable(t, sourceDB, "important_data", 50)
	env.CreateTestTable(t, sourceDB, "temp_logs", 100)
	env.CreateTestTable(t, sourceDB, "user_sessions", 25)

	// Configure fork with table filtering
	cfg := &config.ForkConfig{
		Source:         *env.GetDatabaseConfig(sourceDB),
		Destination:    *env.GetDatabaseConfig("postgres"),
		TargetDatabase: targetDB,
		DropIfExists:   true,
		ExcludeTables:  []string{"temp_logs"},                       // Exclude temporary logs
		IncludeTables:  []string{"important_data", "user_sessions"}, // Only include specific tables
		MaxConnections: 2,
		ChunkSize:      25,
		Timeout:        5 * time.Minute,
	}

	forker := fork.NewForker(cfg)
	err := forker.Fork(context.Background())
	require.NoError(t, err)

	// Verify only included tables were copied
	env.AssertDatabaseExists(t, targetDB)
	env.AssertTableExists(t, targetDB, "important_data")
	env.AssertTableExists(t, targetDB, "user_sessions")
	env.AssertRowCount(t, targetDB, "important_data", 50)
	env.AssertRowCount(t, targetDB, "user_sessions", 25)

	// Verify excluded table was not copied
	conn := env.CreateConnection(t, targetDB)
	defer func() {
		if err := conn.Close(); err != nil {
			t.Logf("Failed to close connection: %v", err)
		}
	}()

	var exists bool
	err = conn.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = 'temp_logs'
		)
	`).Scan(&exists)
	require.NoError(t, err)
	assert.False(t, exists, "Excluded table should not exist")
}

func TestForkSchemaOnly_Integration(t *testing.T) {
	env, cleanup := testutil.SetupTestEnvironment(t)
	if env == nil {
		return
	}
	defer cleanup()

	sourceDB := "source_schema"
	targetDB := "target_schema"

	env.CreateTestDatabase(t, sourceDB)
	env.CreateTestTable(t, sourceDB, "test_table", 100)

	// Configure schema-only fork
	cfg := &config.ForkConfig{
		Source:         *env.GetDatabaseConfig(sourceDB),
		Destination:    *env.GetDatabaseConfig("postgres"),
		TargetDatabase: targetDB,
		DropIfExists:   true,
		SchemaOnly:     true,
		MaxConnections: 2,
		Timeout:        5 * time.Minute,
	}

	forker := fork.NewForker(cfg)
	err := forker.Fork(context.Background())
	require.NoError(t, err)

	// Verify table structure exists but no data
	env.AssertDatabaseExists(t, targetDB)
	env.AssertTableExists(t, targetDB, "test_table")
	env.AssertRowCount(t, targetDB, "test_table", 0) // Should have no data
}

func TestForkErrorHandling_Integration(t *testing.T) {
	env, cleanup := testutil.SetupTestEnvironment(t)
	if env == nil {
		return
	}
	defer cleanup()

	// Test fork with non-existent source database
	cfg := &config.ForkConfig{
		Source:         *env.GetDatabaseConfig("nonexistent_db"),
		Destination:    *env.GetDatabaseConfig("postgres"),
		TargetDatabase: "should_not_be_created",
		DropIfExists:   false,
		MaxConnections: 2,
		Timeout:        1 * time.Minute,
	}

	forker := fork.NewForker(cfg)
	err := forker.Fork(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")

	// Verify target database was not created
	env.AssertDatabaseNotExists(t, "should_not_be_created")
}

func TestForkWithExistingTarget_Integration(t *testing.T) {
	env, cleanup := testutil.SetupTestEnvironment(t)
	if env == nil {
		return
	}
	defer cleanup()

	sourceDB := "source_existing"
	targetDB := "existing_target"

	// Create both source and target databases
	env.CreateTestDatabase(t, sourceDB)
	env.CreateTestDatabase(t, targetDB)
	env.CreateTestTable(t, sourceDB, "new_data", 50)

	// Test fork without drop-if-exists (should fail)
	cfg := &config.ForkConfig{
		Source:         *env.GetDatabaseConfig(sourceDB),
		Destination:    *env.GetDatabaseConfig("postgres"),
		TargetDatabase: targetDB,
		DropIfExists:   false,
		MaxConnections: 2,
		Timeout:        5 * time.Minute,
	}

	forker := fork.NewForker(cfg)
	err := forker.Fork(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")

	// Test fork with drop-if-exists (should succeed)
	cfg.DropIfExists = true
	err = forker.Fork(context.Background())
	require.NoError(t, err)

	// Verify target was recreated with source data
	env.AssertTableExists(t, targetDB, "new_data")
	env.AssertRowCount(t, targetDB, "new_data", 50)
}

func TestForkProgress_Integration(t *testing.T) {
	env, cleanup := testutil.SetupTestEnvironment(t)
	if env == nil {
		return
	}
	defer cleanup()

	sourceDB := "source_progress"
	targetDB := "target_progress"

	env.CreateTestDatabase(t, sourceDB)

	// Create larger tables to test progress reporting
	env.CreateTestTable(t, sourceDB, "large_table1", 500)
	env.CreateTestTable(t, sourceDB, "large_table2", 300)

	cfg := &config.ForkConfig{
		Source:         *env.GetDatabaseConfig(sourceDB),
		Destination:    *env.GetDatabaseConfig("postgres"),
		TargetDatabase: targetDB,
		DropIfExists:   true,
		MaxConnections: 2,
		ChunkSize:      100, // Smaller chunks to test chunking
		Timeout:        10 * time.Minute,
	}

	forker := fork.NewForker(cfg)
	err := forker.Fork(context.Background())
	require.NoError(t, err)

	// Verify all data was transferred
	env.AssertRowCount(t, targetDB, "large_table1", 500)
	env.AssertRowCount(t, targetDB, "large_table2", 300)
}

func TestForkTimeout_Integration(t *testing.T) {
	env, cleanup := testutil.SetupTestEnvironment(t)
	if env == nil {
		return
	}
	defer cleanup()

	sourceDB := "source_timeout"
	targetDB := "target_timeout"

	env.CreateTestDatabase(t, sourceDB)
	env.CreateTestTable(t, sourceDB, "test_data", 10)

	// Configure with very short timeout
	cfg := &config.ForkConfig{
		Source:         *env.GetDatabaseConfig(sourceDB),
		Destination:    *env.GetDatabaseConfig("postgres"),
		TargetDatabase: targetDB,
		DropIfExists:   true,
		MaxConnections: 1,
		ChunkSize:      1,
		Timeout:        1 * time.Nanosecond, // Extremely short timeout
	}

	forker := fork.NewForker(cfg)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	err := forker.Fork(ctx)
	// Should either timeout or complete quickly
	if err != nil {
		assert.Contains(t, err.Error(), "context")
	}
}

// TestURI_Integration tests URI functionality in integration scenarios
func TestURI_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	// This test requires a running PostgreSQL instance
	// Skip if we can't connect to a test database
	testConfig := &config.DatabaseConfig{
		Host:     "localhost",
		Port:     5432,
		Username: "postgres",
		Password: "postgres",
		Database: "postgres",
		SSLMode:  "disable",
	}

	// Try to connect to verify PostgreSQL is available
	db, err := sql.Open("postgres", testConfig.ConnectionString())
	if err != nil {
		t.Skipf("Skipping integration test: PostgreSQL not available: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Skipf("Skipping integration test: PostgreSQL not accessible: %v", err)
	}

	t.Run("URI connection equivalence", func(t *testing.T) {
		// Test that URI and individual parameters produce equivalent connections
		individualConfig := &config.DatabaseConfig{
			Host:     "localhost",
			Port:     5432,
			Username: "postgres",
			Password: "postgres",
			Database: "postgres",
			SSLMode:  "disable",
		}

		uriConfig := &config.DatabaseConfig{
			URI: "postgresql://postgres:postgres@localhost:5432/postgres?sslmode=disable", // pragma: allowlist secret
		}

		// Both should connect successfully
		db1, err := sql.Open("postgres", individualConfig.ConnectionString())
		require.NoError(t, err)
		defer db1.Close()

		db2, err := sql.Open("postgres", uriConfig.ConnectionString())
		require.NoError(t, err)
		defer db2.Close()

		// Both should be able to ping
		require.NoError(t, db1.Ping())
		require.NoError(t, db2.Ping())

		// Both should be able to execute queries
		var result1, result2 int
		err = db1.QueryRow("SELECT 1").Scan(&result1)
		require.NoError(t, err)
		assert.Equal(t, 1, result1)

		err = db2.QueryRow("SELECT 1").Scan(&result2)
		require.NoError(t, err)
		assert.Equal(t, 1, result2)
	})

	t.Run("environment variable URI support", func(t *testing.T) {
		// Save original environment
		originalEnv := map[string]string{
			"PGFORK_SOURCE_URI":  os.Getenv("PGFORK_SOURCE_URI"),
			"PGFORK_SOURCE_HOST": os.Getenv("PGFORK_SOURCE_HOST"),
		}

		defer func() {
			for key, value := range originalEnv {
				if value == "" {
					os.Unsetenv(key)
				} else {
					os.Setenv(key, value)
				}
			}
		}()

		// Set URI environment variable
		testURI := "postgresql://postgres:postgres@localhost:5432/postgres?sslmode=disable" // pragma: allowlist secret
		os.Setenv("PGFORK_SOURCE_URI", testURI)
		os.Unsetenv("PGFORK_SOURCE_HOST") // Ensure individual params don't interfere

		// Load configuration from environment
		config := &config.ForkConfig{}
		config.LoadFromEnvironment()

		// Verify URI was loaded correctly
		assert.Equal(t, testURI, config.Source.URI)
		assert.Equal(t, "localhost", config.Source.Host)
		assert.Equal(t, 5432, config.Source.Port)
		assert.Equal(t, "postgres", config.Source.Username)
		assert.Equal(t, "postgres", config.Source.Password)
		assert.Equal(t, "postgres", config.Source.Database)
		assert.Equal(t, "disable", config.Source.SSLMode)

		// Verify connection works
		db, err := sql.Open("postgres", config.Source.ConnectionString())
		require.NoError(t, err)
		defer db.Close()

		require.NoError(t, db.Ping())

		var result int
		err = db.QueryRow("SELECT 1").Scan(&result)
		require.NoError(t, err)
		assert.Equal(t, 1, result)
	})
}
