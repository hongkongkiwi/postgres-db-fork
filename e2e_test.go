//go:build e2e

package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hongkongkiwi/postgres-db-fork/internal/config"
	"github.com/hongkongkiwi/postgres-db-fork/internal/db"
	"github.com/hongkongkiwi/postgres-db-fork/internal/fork"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_RealWorldScenarios tests real-world database forking scenarios
func TestE2E_RealWorldScenarios(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e tests in short mode")
	}

	// Set up source environment
	sourceEnv, sourceCleanup := SetupTestEnvironment(t)
	if sourceEnv == nil {
		return // Test was skipped
	}
	defer sourceCleanup()

	// Create a realistic production-like database
	sourceDB := "production_app"
	sourceEnv.CreateTestDatabase(t, sourceDB)

	// Create tables that simulate a real application
	createRealisticSchema(t, sourceEnv, sourceDB)

	t.Run("E2E_ProductionToStaging", func(t *testing.T) {
		testProductionToStaging(t, sourceEnv, sourceDB)
	})

	t.Run("E2E_CrossServerMigration", func(t *testing.T) {
		testCrossServerMigration(t, sourceEnv, sourceDB)
	})

	t.Run("E2E_LargeDatasetFork", func(t *testing.T) {
		testLargeDatasetFork(t, sourceEnv, sourceDB)
	})

	t.Run("E2E_SchemaOnlyMigration", func(t *testing.T) {
		testSchemaOnlyMigration(t, sourceEnv, sourceDB)
	})

	t.Run("E2E_SelectiveTableFork", func(t *testing.T) {
		testSelectiveTableFork(t, sourceEnv, sourceDB)
	})
}

// createRealisticSchema creates a realistic database schema for testing
func createRealisticSchema(t *testing.T, env *TestEnvironment, dbName string) {
	conn := env.CreateConnection(t, dbName)
	defer func() {
		if err := conn.Close(); err != nil {
			t.Logf("Failed to close connection: %v", err)
		}
	}()

	// Users table
	_, err := conn.DB.Exec(`
		CREATE TABLE users (
			id SERIAL PRIMARY KEY,
			username VARCHAR(50) UNIQUE NOT NULL,
			email VARCHAR(100) UNIQUE NOT NULL,
			password_hash VARCHAR(255) NOT NULL,
			first_name VARCHAR(50),
			last_name VARCHAR(50),
			is_active BOOLEAN DEFAULT TRUE,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		);
		CREATE INDEX idx_users_email ON users(email);
		CREATE INDEX idx_users_username ON users(username);
	`)
	require.NoError(t, err)

	// Products table
	_, err = conn.DB.Exec(`
		CREATE TABLE products (
			id SERIAL PRIMARY KEY,
			name VARCHAR(200) NOT NULL,
			description TEXT,
			price DECIMAL(10,2) NOT NULL,
			stock_quantity INTEGER DEFAULT 0,
			category_id INTEGER,
			is_active BOOLEAN DEFAULT TRUE,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		);
		CREATE INDEX idx_products_category ON products(category_id);
		CREATE INDEX idx_products_active ON products(is_active);
	`)
	require.NoError(t, err)

	// Orders table
	_, err = conn.DB.Exec(`
		CREATE TABLE orders (
			id SERIAL PRIMARY KEY,
			user_id INTEGER NOT NULL,
			total_amount DECIMAL(10,2) NOT NULL,
			status VARCHAR(20) DEFAULT 'pending',
			shipping_address TEXT,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		);
		CREATE INDEX idx_orders_user_id ON orders(user_id);
		CREATE INDEX idx_orders_status ON orders(status);
		CREATE INDEX idx_orders_created_at ON orders(created_at);
	`)
	require.NoError(t, err)

	// Order items table
	_, err = conn.DB.Exec(`
		CREATE TABLE order_items (
			id SERIAL PRIMARY KEY,
			order_id INTEGER NOT NULL,
			product_id INTEGER NOT NULL,
			quantity INTEGER NOT NULL,
			unit_price DECIMAL(10,2) NOT NULL,
			created_at TIMESTAMP DEFAULT NOW()
		);
		CREATE INDEX idx_order_items_order_id ON order_items(order_id);
		CREATE INDEX idx_order_items_product_id ON order_items(product_id);
	`)
	require.NoError(t, err)

	// Audit log table (typically large in production)
	_, err = conn.DB.Exec(`
		CREATE TABLE audit_logs (
			id SERIAL PRIMARY KEY,
			table_name VARCHAR(50) NOT NULL,
			action VARCHAR(20) NOT NULL,
			user_id INTEGER,
			old_values JSONB,
			new_values JSONB,
			created_at TIMESTAMP DEFAULT NOW()
		);
		CREATE INDEX idx_audit_logs_table_name ON audit_logs(table_name);
		CREATE INDEX idx_audit_logs_created_at ON audit_logs(created_at);
	`)
	require.NoError(t, err)

	// Insert realistic test data
	insertRealisticData(t, conn)
}

// insertRealisticData inserts realistic test data
func insertRealisticData(t *testing.T, conn *db.Connection) {
	// Insert users
	for i := 1; i <= 100; i++ {
		_, err := conn.DB.Exec(`
			INSERT INTO users (username, email, password_hash, first_name, last_name)
			VALUES ($1, $2, $3, $4, $5)
		`,
			fmt.Sprintf("user%d", i),
			fmt.Sprintf("user%d@example.com", i),
			"$2a$10$hash_placeholder",
			fmt.Sprintf("User%d", i),
			fmt.Sprintf("LastName%d", i),
		)
		require.NoError(t, err)
	}

	// Insert products
	categories := []string{"Electronics", "Books", "Clothing", "Home", "Sports"}
	for i := 1; i <= 50; i++ {
		_, err := conn.DB.Exec(`
			INSERT INTO products (name, description, price, stock_quantity, category_id)
			VALUES ($1, $2, $3, $4, $5)
		`,
			fmt.Sprintf("Product %d", i),
			fmt.Sprintf("Description for product %d", i),
			fmt.Sprintf("%.2f", float64(i)*10.99),
			i*5,
			(i%len(categories))+1,
		)
		require.NoError(t, err)
	}

	// Insert orders
	for i := 1; i <= 200; i++ {
		userID := (i % 100) + 1
		_, err := conn.DB.Exec(`
			INSERT INTO orders (user_id, total_amount, status, shipping_address)
			VALUES ($1, $2, $3, $4)
		`,
			userID,
			fmt.Sprintf("%.2f", float64(i)*15.50),
			[]string{"pending", "shipped", "delivered", "cancelled"}[i%4],
			fmt.Sprintf("%d Main St, City %d", i, userID),
		)
		require.NoError(t, err)
	}

	// Insert order items
	for i := 1; i <= 500; i++ {
		orderID := (i % 200) + 1
		productID := (i % 50) + 1
		_, err := conn.DB.Exec(`
			INSERT INTO order_items (order_id, product_id, quantity, unit_price)
			VALUES ($1, $2, $3, $4)
		`,
			orderID,
			productID,
			(i%5)+1,
			fmt.Sprintf("%.2f", float64(productID)*10.99),
		)
		require.NoError(t, err)
	}

	// Insert audit logs (simulate a busy system)
	tables := []string{"users", "products", "orders", "order_items"}
	actions := []string{"INSERT", "UPDATE", "DELETE"}
	for i := 1; i <= 1000; i++ {
		_, err := conn.DB.Exec(`
			INSERT INTO audit_logs (table_name, action, user_id, old_values, new_values)
			VALUES ($1, $2, $3, $4, $5)
		`,
			tables[i%len(tables)],
			actions[i%len(actions)],
			(i%100)+1,
			`{"old": "value"}`,
			`{"new": "value"}`,
		)
		require.NoError(t, err)
	}
}

// disconnectAllUsers terminates all connections to a database
func disconnectAllUsers(t *testing.T, env *TestEnvironment, dbName string) {
	// Connect to a maintenance database (like 'postgres') to terminate connections
	conn := env.CreateConnection(t, "postgres")
	defer func() {
		if err := conn.Close(); err != nil {
			t.Logf("Failed to close maintenance connection: %v", err)
		}
	}()

	err := conn.TerminateAllConnections(dbName)
	require.NoError(t, err, "Failed to terminate connections for database %s", dbName)
}

// testProductionToStaging simulates forking a production database to staging
func testProductionToStaging(t *testing.T, sourceEnv *TestEnvironment, sourceDB string) {
	stagingDB := "staging_app"

	cfg := &config.ForkConfig{
		Source:         *sourceEnv.GetDatabaseConfig(sourceDB),
		Destination:    *sourceEnv.GetDatabaseConfig("postgres"),
		TargetDatabase: stagingDB,
		DropIfExists:   true,
		MaxConnections: 4,
		ChunkSize:      100,
		Timeout:        10 * time.Minute,
	}

	// Execute fork
	forker := fork.NewForker(cfg)
	err := forker.Fork(context.Background())
	require.NoError(t, err)

	// Verify all tables were copied
	sourceEnv.AssertDatabaseExists(t, stagingDB)
	sourceEnv.AssertTableExists(t, stagingDB, "users")
	sourceEnv.AssertTableExists(t, stagingDB, "products")
	sourceEnv.AssertTableExists(t, stagingDB, "orders")
	sourceEnv.AssertTableExists(t, stagingDB, "order_items")
	sourceEnv.AssertTableExists(t, stagingDB, "audit_logs")

	// Verify data counts match
	sourceEnv.AssertRowCount(t, stagingDB, "users", 100)
	sourceEnv.AssertRowCount(t, stagingDB, "products", 50)
	sourceEnv.AssertRowCount(t, stagingDB, "orders", 200)
	sourceEnv.AssertRowCount(t, stagingDB, "order_items", 500)
	sourceEnv.AssertRowCount(t, stagingDB, "audit_logs", 1000)

	// Verify indexes were created
	conn := sourceEnv.CreateConnection(t, stagingDB)
	defer func() {
		if err := conn.Close(); err != nil {
			t.Logf("Failed to close connection: %v", err)
		}
	}()

	var indexCount int
	err = conn.DB.QueryRow(`
		SELECT COUNT(*) FROM pg_indexes
		WHERE schemaname = 'public' AND tablename IN ('users', 'products', 'orders', 'order_items', 'audit_logs')
	`).Scan(&indexCount)
	require.NoError(t, err)
	assert.Greater(t, indexCount, 5, "Indexes should be copied")
}

// testCrossServerMigration tests forking between different PostgreSQL instances
func testCrossServerMigration(t *testing.T, sourceEnv *TestEnvironment, sourceDB string) {
	// Set up destination environment (different PostgreSQL instance)
	destEnv, destCleanup := SetupTestEnvironment(t)
	if destEnv == nil {
		return
	}
	defer destCleanup()

	targetDB := "migrated_app"
	destEnv.CreateTestDatabase(t, targetDB)

	cfg := &config.ForkConfig{
		Source:         *sourceEnv.GetDatabaseConfig(sourceDB),
		Destination:    *destEnv.GetDatabaseConfig(targetDB),
		TargetDatabase: targetDB,
		DropIfExists:   true,
		MaxConnections: 2,
		ChunkSize:      50,
		Timeout:        15 * time.Minute,
	}

	// Execute cross-server fork
	forker := fork.NewForker(cfg)
	err := forker.Fork(context.Background())
	require.NoError(t, err)

	// Verify migration to different server
	destEnv.AssertTableExists(t, targetDB, "users")
	destEnv.AssertTableExists(t, targetDB, "products")
	destEnv.AssertRowCount(t, targetDB, "users", 100)
	destEnv.AssertRowCount(t, targetDB, "products", 50)
}

// testLargeDatasetFork simulates forking a database with a large amount of data
func testLargeDatasetFork(t *testing.T, sourceEnv *TestEnvironment, sourceDB string) {
	largeDatasetDB := "large_dataset_copy"

	// Create additional large dataset in the source DB for this specific test
	conn := sourceEnv.CreateConnection(t, sourceDB)
	// Create a large events table
	_, err := conn.DB.Exec(`
		CREATE TABLE events (
			id SERIAL PRIMARY KEY,
			event_type VARCHAR(50),
			user_id INTEGER,
			data JSONB,
			created_at TIMESTAMP DEFAULT NOW()
		);
		CREATE INDEX idx_events_type ON events(event_type);
		CREATE INDEX idx_events_created_at ON events(created_at);
	`)
	require.NoError(t, err)

	// Insert large dataset (simulate production volume)
	for i := 1; i <= 2000; i++ {
		_, err := conn.DB.Exec(`
			INSERT INTO events (event_type, user_id, data)
			VALUES ($1, $2, $3)
		`,
			[]string{"login", "logout", "purchase", "view", "click"}[i%5],
			(i%100)+1,
			fmt.Sprintf(`{"timestamp": "%d", "session_id": "sess_%d"}`, i, i),
		)
		require.NoError(t, err)
	}
	if err := conn.Close(); err != nil {
		t.Logf("Failed to close connection after data insertion: %v", err)
	}

	// Disconnect any active connections to the source database before using it as a template
	disconnectAllUsers(t, sourceEnv, sourceDB)

	cfg := &config.ForkConfig{
		Source:         *sourceEnv.GetDatabaseConfig(sourceDB),
		Destination:    *sourceEnv.GetDatabaseConfig("postgres"),
		TargetDatabase: largeDatasetDB,
		DropIfExists:   true,
		MaxConnections: 3,
		ChunkSize:      200, // Smaller chunks to test chunking logic
		Timeout:        20 * time.Minute,
	}

	forker := fork.NewForker(cfg)
	err = forker.Fork(context.Background())
	require.NoError(t, err)

	// Verify large dataset was copied correctly
	sourceEnv.AssertRowCount(t, largeDatasetDB, "events", 2000)
	sourceEnv.AssertTableExists(t, largeDatasetDB, "users")
	sourceEnv.AssertRowCount(t, largeDatasetDB, "users", 100)
}

// testSchemaOnlyMigration tests schema-only forking
func testSchemaOnlyMigration(t *testing.T, sourceEnv *TestEnvironment, sourceDB string) {
	targetDB := "schema_only_copy"
	cfg := &config.ForkConfig{
		Source:         *sourceEnv.GetDatabaseConfig(sourceDB),
		Destination:    *sourceEnv.GetDatabaseConfig("postgres"),
		TargetDatabase: targetDB,
		DropIfExists:   true,
		SchemaOnly:     true,
		MaxConnections: 2,
		Timeout:        5 * time.Minute,
	}

	forker := fork.NewForker(cfg)
	err := forker.Fork(context.Background())
	require.NoError(t, err)

	// Verify schema exists but no data
	sourceEnv.AssertDatabaseExists(t, targetDB)
	sourceEnv.AssertTableExists(t, targetDB, "users")
	sourceEnv.AssertTableExists(t, targetDB, "products")
	sourceEnv.AssertTableExists(t, targetDB, "orders")

	// Verify no data was copied
	sourceEnv.AssertRowCount(t, targetDB, "users", 0)
	sourceEnv.AssertRowCount(t, targetDB, "products", 0)
	sourceEnv.AssertRowCount(t, targetDB, "orders", 0)

	// Verify table structure is correct
	conn := sourceEnv.CreateConnection(t, targetDB)
	defer func() {
		if err := conn.Close(); err != nil {
			t.Logf("Failed to close connection: %v", err)
		}
	}()

	var columnCount int
	err = conn.DB.QueryRow(`
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'users'
	`).Scan(&columnCount)
	require.NoError(t, err)
	assert.Equal(t, 9, columnCount, "Users table should have correct column count")
}

// testSelectiveTableFork tests forking only specific tables
func testSelectiveTableFork(t *testing.T, sourceEnv *TestEnvironment, sourceDB string) {
	targetDB := "selective_copy"
	cfg := &config.ForkConfig{
		Source:         *sourceEnv.GetDatabaseConfig(sourceDB),
		Destination:    *sourceEnv.GetDatabaseConfig("postgres"),
		TargetDatabase: targetDB,
		DropIfExists:   true,
		IncludeTables:  []string{"users", "products"}, // Only copy these tables
		ExcludeTables:  []string{"audit_logs"},        // Exclude audit logs
		MaxConnections: 2,
		ChunkSize:      50,
		Timeout:        10 * time.Minute,
	}

	forker := fork.NewForker(cfg)
	err := forker.Fork(context.Background())
	require.NoError(t, err)

	// Verify only selected tables were copied
	sourceEnv.AssertDatabaseExists(t, targetDB)
	sourceEnv.AssertTableExists(t, targetDB, "users")
	sourceEnv.AssertTableExists(t, targetDB, "products")
	sourceEnv.AssertRowCount(t, targetDB, "users", 100)
	sourceEnv.AssertRowCount(t, targetDB, "products", 50)

	// Verify excluded tables don't exist
	conn := sourceEnv.CreateConnection(t, targetDB)
	defer func() {
		if err := conn.Close(); err != nil {
			t.Logf("Failed to close connection: %v", err)
		}
	}()

	var auditExists, ordersExist bool
	err = conn.DB.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = 'audit_logs'
		)
	`).Scan(&auditExists)
	require.NoError(t, err)
	assert.False(t, auditExists, "Audit logs table should not exist")

	err = conn.DB.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = 'orders'
		)
	`).Scan(&ordersExist)
	require.NoError(t, err)
	assert.False(t, ordersExist, "Orders table should not exist (not in include list)")
}

// TestE2E_CLIIntegration tests the CLI integration with real Docker containers
func TestE2E_CLIIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e CLI tests in short mode")
	}

	env, cleanup := SetupTestEnvironment(t)
	if env == nil {
		return
	}
	defer cleanup()

	// Create source database and data
	sourceDB := "cli_source"
	env.CreateTestDatabase(t, sourceDB)
	env.CreateTestTable(t, sourceDB, "test_table", 25)

	// Create config file
	configContent := fmt.Sprintf(`
source:
  host: %s
  port: %s
  username: %s
  password: %s
  database: %s
  sslmode: disable

destination:
  host: %s
  port: %s
  username: %s
  password: %s
  database: postgres
  sslmode: disable

settings:
  target_database: cli_target
  drop_if_exists: true
  max_connections: 2
  chunk_size: 10
  timeout: 5m
`,
		env.Config.Host, env.Config.Port, env.Config.Username, env.Config.Password, sourceDB,
		env.Config.Host, env.Config.Port, env.Config.Username, env.Config.Password,
	)

	configFile, configCleanup := CreateTestConfigFile(t, configContent)
	defer configCleanup()

	// Test CLI execution would go here - this would require building the binary
	// For now, we'll test the core functionality directly
	cfg := &config.ForkConfig{
		Source:         *env.GetDatabaseConfig(sourceDB),
		Destination:    *env.GetDatabaseConfig("postgres"),
		TargetDatabase: "cli_target",
		DropIfExists:   true,
		MaxConnections: 2,
		ChunkSize:      10,
		Timeout:        5 * time.Minute,
	}

	forker := fork.NewForker(cfg)
	err := forker.Fork(context.Background())
	require.NoError(t, err)

	env.AssertDatabaseExists(t, "cli_target")
	env.AssertRowCount(t, "cli_target", "test_table", 25)

	t.Logf("Config file created at: %s", configFile)
}
