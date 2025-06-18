package db

import (
	"database/sql"
	"testing"

	"github.com/hongkongkiwi/postgres-db-fork/internal/config"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewConnection(t *testing.T) {
	tests := []struct {
		name        string
		config      *config.DatabaseConfig
		expectError bool
	}{
		{
			name: "valid configuration",
			config: &config.DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				Username: "testuser",
				Password: "testpass",
				Database: "testdb",
				SSLMode:  "disable",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Since we can't easily test actual connections without a running PostgreSQL instance,
			// we'll focus on testing the configuration validation and mocked operations
			if tt.expectError {
				// Test would expect error (implementation depends on actual DB availability)
				t.Skip("Skipping actual connection test - would require running PostgreSQL")
			} else {
				// Test would succeed (implementation depends on actual DB availability)
				t.Skip("Skipping actual connection test - would require running PostgreSQL")
			}
		})
	}
}

func TestConnection_DatabaseExists(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Failed to close database connection: %v", err)
		}
	}()

	conn := &Connection{
		DB: db,
		Config: &config.DatabaseConfig{
			Host:     "localhost",
			Port:     5432,
			Username: "testuser",
			Database: "testdb",
		},
	}

	tests := []struct {
		name      string
		dbName    string
		mockSetup func()
		expected  bool
		expectErr bool
	}{
		{
			name:   "database exists",
			dbName: "existing_db",
			mockSetup: func() {
				mock.ExpectQuery("SELECT 1 FROM pg_database WHERE datname = \\$1").
					WithArgs("existing_db").
					WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
			},
			expected:  true,
			expectErr: false,
		},
		{
			name:   "database does not exist",
			dbName: "nonexistent_db",
			mockSetup: func() {
				mock.ExpectQuery("SELECT 1 FROM pg_database WHERE datname = \\$1").
					WithArgs("nonexistent_db").
					WillReturnError(sql.ErrNoRows)
			},
			expected:  false,
			expectErr: false,
		},
		{
			name:   "database query error",
			dbName: "error_db",
			mockSetup: func() {
				mock.ExpectQuery("SELECT 1 FROM pg_database WHERE datname = \\$1").
					WithArgs("error_db").
					WillReturnError(assert.AnError)
			},
			expected:  false,
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup()

			exists, err := conn.DatabaseExists(tt.dbName)

			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, exists)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestConnection_CreateDatabase(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Failed to close database connection: %v", err)
		}
	}()

	conn := &Connection{
		DB: db,
		Config: &config.DatabaseConfig{
			Host:     "localhost",
			Port:     5432,
			Username: "testuser",
			Database: "testdb",
		},
	}

	tests := []struct {
		name         string
		targetDB     string
		sourceDB     string
		dropIfExists bool
		mockSetup    func()
		expectErr    bool
	}{
		{
			name:         "create database successfully",
			targetDB:     "new_db",
			sourceDB:     "template_db",
			dropIfExists: false,
			mockSetup: func() {
				mock.ExpectExec(`CREATE DATABASE "new_db" WITH TEMPLATE "template_db"`).
					WillReturnResult(sqlmock.NewResult(0, 0))
			},
			expectErr: false,
		},
		{
			name:         "create database with drop if exists",
			targetDB:     "new_db",
			sourceDB:     "template_db",
			dropIfExists: true,
			mockSetup: func() {
				// Expect terminate connections query
				mock.ExpectExec("SELECT pg_terminate_backend\\(pid\\)").
					WillReturnResult(sqlmock.NewResult(0, 0))
				// Expect drop database query
				mock.ExpectExec(`DROP DATABASE IF EXISTS "new_db"`).
					WillReturnResult(sqlmock.NewResult(0, 0))
				// Expect create database query
				mock.ExpectExec(`CREATE DATABASE "new_db" WITH TEMPLATE "template_db"`).
					WillReturnResult(sqlmock.NewResult(0, 0))
			},
			expectErr: false,
		},
		{
			name:         "create database with error",
			targetDB:     "error_db",
			sourceDB:     "template_db",
			dropIfExists: false,
			mockSetup: func() {
				mock.ExpectExec(`CREATE DATABASE "error_db" WITH TEMPLATE "template_db"`).
					WillReturnError(assert.AnError)
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup()

			err := conn.CreateDatabase(tt.targetDB, tt.sourceDB, tt.dropIfExists)

			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestConnection_DropDatabase(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Failed to close database connection: %v", err)
		}
	}()

	conn := &Connection{
		DB: db,
		Config: &config.DatabaseConfig{
			Host:     "localhost",
			Port:     5432,
			Username: "testuser",
			Database: "testdb",
		},
	}

	tests := []struct {
		name      string
		dbName    string
		mockSetup func()
		expectErr bool
	}{
		{
			name:   "drop database successfully",
			dbName: "drop_me",
			mockSetup: func() {
				// Expect terminate connections query
				mock.ExpectExec("SELECT pg_terminate_backend\\(pid\\)").
					WithArgs("drop_me").
					WillReturnResult(sqlmock.NewResult(0, 0))
				// Expect drop database query
				mock.ExpectExec(`DROP DATABASE IF EXISTS "drop_me"`).
					WillReturnResult(sqlmock.NewResult(0, 0))
			},
			expectErr: false,
		},
		{
			name:   "drop database with error",
			dbName: "error_db",
			mockSetup: func() {
				// Expect terminate connections query (may fail)
				mock.ExpectExec("SELECT pg_terminate_backend\\(pid\\)").
					WithArgs("error_db").
					WillReturnError(assert.AnError)
				// Expect drop database query to also fail
				mock.ExpectExec(`DROP DATABASE IF EXISTS "error_db"`).
					WillReturnError(assert.AnError)
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup()

			err := conn.DropDatabase(tt.dbName)

			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestConnection_GetDatabaseSize(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Failed to close database connection: %v", err)
		}
	}()

	conn := &Connection{
		DB: db,
		Config: &config.DatabaseConfig{
			Host:     "localhost",
			Port:     5432,
			Username: "testuser",
			Database: "testdb",
		},
	}

	tests := []struct {
		name         string
		dbName       string
		mockSetup    func()
		expectedSize int64
		expectErr    bool
	}{
		{
			name:   "get database size successfully",
			dbName: "test_db",
			mockSetup: func() {
				mock.ExpectQuery("SELECT pg_database_size\\(\\$1\\)").
					WithArgs("test_db").
					WillReturnRows(sqlmock.NewRows([]string{"size"}).AddRow(1024000))
			},
			expectedSize: 1024000,
			expectErr:    false,
		},
		{
			name:   "get database size with error",
			dbName: "error_db",
			mockSetup: func() {
				mock.ExpectQuery("SELECT pg_database_size\\(\\$1\\)").
					WithArgs("error_db").
					WillReturnError(assert.AnError)
			},
			expectedSize: 0,
			expectErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup()

			size, err := conn.GetDatabaseSize(tt.dbName)

			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedSize, size)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestConnection_GetTableList(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() {
		if err := db.Close(); err != nil {
			t.Logf("Failed to close database connection: %v", err)
		}
	}()

	conn := &Connection{
		DB: db,
		Config: &config.DatabaseConfig{
			Host:     "localhost",
			Port:     5432,
			Username: "testuser",
			Database: "testdb",
		},
	}

	tests := []struct {
		name           string
		schemaName     string
		mockSetup      func()
		expectedTables []string
		expectErr      bool
	}{
		{
			name:       "get table list successfully",
			schemaName: "public",
			mockSetup: func() {
				rows := sqlmock.NewRows([]string{"tablename"}).
					AddRow("users").
					AddRow("orders").
					AddRow("products")
				mock.ExpectQuery("SELECT tablename FROM pg_tables WHERE schemaname = \\$1 ORDER BY tablename").
					WithArgs("public").
					WillReturnRows(rows)
			},
			expectedTables: []string{"users", "orders", "products"},
			expectErr:      false,
		},
		{
			name:       "get table list with default schema",
			schemaName: "",
			mockSetup: func() {
				rows := sqlmock.NewRows([]string{"tablename"}).
					AddRow("table1").
					AddRow("table2")
				mock.ExpectQuery("SELECT tablename FROM pg_tables WHERE schemaname = \\$1 ORDER BY tablename").
					WithArgs("public").
					WillReturnRows(rows)
			},
			expectedTables: []string{"table1", "table2"},
			expectErr:      false,
		},
		{
			name:       "get table list with error",
			schemaName: "public",
			mockSetup: func() {
				mock.ExpectQuery("SELECT tablename FROM pg_tables WHERE schemaname = \\$1 ORDER BY tablename").
					WithArgs("public").
					WillReturnError(assert.AnError)
			},
			expectedTables: nil,
			expectErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup()

			tables, err := conn.GetTableList(tt.schemaName)

			if tt.expectErr {
				assert.Error(t, err)
				assert.Nil(t, tables)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedTables, tables)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestConnection_Close(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)

	conn := &Connection{
		DB: db,
		Config: &config.DatabaseConfig{
			Host: "localhost",
			Port: 5432,
		},
	}

	// Expect the close call
	mock.ExpectClose()

	// Test closing connection
	err = conn.Close()
	assert.NoError(t, err)

	// Verify expectations were met
	assert.NoError(t, mock.ExpectationsWereMet())

	// Test closing nil connection
	conn.DB = nil
	err = conn.Close()
	assert.NoError(t, err)
}
