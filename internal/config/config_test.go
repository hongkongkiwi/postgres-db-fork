package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDatabaseConfig_ConnectionString(t *testing.T) {
	tests := []struct {
		name     string
		config   DatabaseConfig
		expected string
	}{
		{
			name: "basic connection string",
			config: DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				Username: "testuser",
				Password: "testpass",
				Database: "testdb",
				SSLMode:  "disable",
			},
			expected: "host=localhost port=5432 user=testuser password=testpass dbname=testdb sslmode=disable", // pragma: allowlist secret
		},
		{
			name: "default sslmode when empty",
			config: DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				Username: "testuser",
				Password: "testpass",
				Database: "testdb",
			},
			expected: "host=localhost port=5432 user=testuser password=testpass dbname=testdb sslmode=prefer", // pragma: allowlist secret
		},
		{
			name: "with custom sslmode",
			config: DatabaseConfig{
				Host:     "production.example.com",
				Port:     5432,
				Username: "produser",
				Password: "securepass",
				Database: "proddb",
				SSLMode:  "require",
			},
			expected: "host=production.example.com port=5432 user=produser password=securepass dbname=proddb sslmode=require", // pragma: allowlist secret
		},
		{
			name: "URI takes precedence over individual parameters",
			config: DatabaseConfig{
				URI:      "postgresql://uriuser:uripass@urihost:5555/uridb?sslmode=require", // pragma: allowlist secret
				Host:     "localhost",
				Port:     5432,
				Username: "testuser",
				Password: "testpass",
				Database: "testdb",
				SSLMode:  "disable",
			},
			expected: "postgresql://uriuser:uripass@urihost:5555/uridb?sslmode=require", // pragma: allowlist secret
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.ConnectionString()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParsePostgreSQLURI(t *testing.T) {
	tests := []struct {
		name        string
		uri         string
		expected    *DatabaseConfig
		expectError bool
	}{
		{
			name: "complete URI with all parameters",
			uri:  "postgresql://testuser:testpass@localhost:5432/testdb?sslmode=require", // pragma: allowlist secret
			expected: &DatabaseConfig{
				URI:      "postgresql://testuser:testpass@localhost:5432/testdb?sslmode=require", // pragma: allowlist secret
				Host:     "localhost",
				Port:     5432,
				Username: "testuser",
				Password: "testpass",
				Database: "testdb",
				SSLMode:  "require",
			},
		},
		{
			name: "postgres scheme variant",
			uri:  "postgres://user:pass@host:5555/db?sslmode=disable", // pragma: allowlist secret
			expected: &DatabaseConfig{
				URI:      "postgres://user:pass@host:5555/db?sslmode=disable", // pragma: allowlist secret
				Host:     "host",
				Port:     5555,
				Username: "user",
				Password: "pass",
				Database: "db",
				SSLMode:  "disable",
			},
		},
		{
			name: "minimal URI without password",
			uri:  "postgresql://user@localhost/database",
			expected: &DatabaseConfig{
				URI:      "postgresql://user@localhost/database",
				Host:     "localhost",
				Port:     5432, // Default port
				Username: "user",
				Password: "",
				Database: "database",
				SSLMode:  "",
			},
		},
		{
			name: "URI without port defaults to 5432",
			uri:  "postgresql://user:password@localhost/database", // pragma: allowlist secret
			expected: &DatabaseConfig{
				URI:      "postgresql://user:password@localhost/database", // pragma: allowlist secret
				Host:     "localhost",
				Port:     5432,
				Username: "user",
				Password: "password",
				Database: "database",
				SSLMode:  "",
			},
		},
		{
			name: "URI with IP address",
			uri:  "postgresql://user:password@192.168.1.1:3333/mydb?sslmode=prefer", // pragma: allowlist secret
			expected: &DatabaseConfig{
				URI:      "postgresql://user:password@192.168.1.1:3333/mydb?sslmode=prefer", // pragma: allowlist secret
				Host:     "192.168.1.1",
				Port:     3333,
				Username: "user",
				Password: "password",
				Database: "mydb",
				SSLMode:  "prefer",
			},
		},
		{
			name:        "invalid scheme",
			uri:         "mysql://user:password@localhost/database", // pragma: allowlist secret
			expectError: true,
		},
		{
			name:        "malformed URI",
			uri:         "postgresql://user:password@localhost:invalid_port/database", // pragma: allowlist secret
			expectError: true,                                                         // url.Parse will fail due to invalid port
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parsePostgreSQLURI(tt.uri)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestForkConfig_LoadFromEnvironment_URI(t *testing.T) {
	// Clean up environment before and after test
	originalEnv := map[string]string{
		"PGFORK_SOURCE_URI":  os.Getenv("PGFORK_SOURCE_URI"),
		"PGFORK_TARGET_URI":  os.Getenv("PGFORK_TARGET_URI"),
		"PGFORK_SOURCE_HOST": os.Getenv("PGFORK_SOURCE_HOST"),
		"PGFORK_SOURCE_USER": os.Getenv("PGFORK_SOURCE_USER"),
	}

	defer func() {
		for key, value := range originalEnv {
			if value == "" {
				require.NoError(t, os.Unsetenv(key))
			} else {
				require.NoError(t, os.Setenv(key, value))
			}
		}
	}()

	tests := []struct {
		name     string
		envVars  map[string]string
		expected ForkConfig
	}{
		{
			name: "source URI takes precedence over individual parameters",
			envVars: map[string]string{
				"PGFORK_SOURCE_URI":  "postgresql://uriuser:uripass@urihost:5555/uridb?sslmode=require", // pragma: allowlist secret
				"PGFORK_SOURCE_HOST": "localhost",
				"PGFORK_SOURCE_USER": "testuser",
			},
			expected: ForkConfig{
				Source: DatabaseConfig{
					URI:      "postgresql://uriuser:uripass@urihost:5555/uridb?sslmode=require", // pragma: allowlist secret
					Host:     "urihost",
					Port:     5555,
					Username: "uriuser",
					Password: "uripass",
					Database: "uridb",
					SSLMode:  "require",
				},
			},
		},
		{
			name: "target URI for destination configuration",
			envVars: map[string]string{
				"PGFORK_TARGET_URI": "postgresql://targetuser:targetpass@targethost:5432/targetdb", // pragma: allowlist secret
			},
			expected: ForkConfig{
				Destination: DatabaseConfig{
					URI:      "postgresql://targetuser:targetpass@targethost:5432/targetdb", // pragma: allowlist secret
					Host:     "targethost",
					Port:     5432,
					Username: "targetuser",
					Password: "targetpass",
					Database: "targetdb",
					SSLMode:  "",
				},
			},
		},
		{
			name: "fallback to individual parameters when no URI",
			envVars: map[string]string{
				"PGFORK_SOURCE_HOST": "localhost",
				"PGFORK_SOURCE_USER": "testuser",
				"PGFORK_SOURCE_PORT": "5432",
			},
			expected: ForkConfig{
				Source: DatabaseConfig{
					URI:      "",
					Host:     "localhost",
					Port:     5432,
					Username: "testuser",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear environment
			for key := range originalEnv {
				require.NoError(t, os.Unsetenv(key))
			}

			// Set test environment variables
			for key, value := range tt.envVars {
				if value == "" {
					require.NoError(t, os.Unsetenv(key))
				} else {
					require.NoError(t, os.Setenv(key, value))
				}
			}

			// Test the loading
			config := &ForkConfig{}
			config.LoadFromEnvironment()

			// Verify source configuration
			assert.Equal(t, tt.expected.Source.URI, config.Source.URI)
			assert.Equal(t, tt.expected.Source.Host, config.Source.Host)
			assert.Equal(t, tt.expected.Source.Port, config.Source.Port)
			assert.Equal(t, tt.expected.Source.Username, config.Source.Username)
			assert.Equal(t, tt.expected.Source.Password, config.Source.Password)
			assert.Equal(t, tt.expected.Source.Database, config.Source.Database)
			assert.Equal(t, tt.expected.Source.SSLMode, config.Source.SSLMode)

			// Verify destination configuration
			assert.Equal(t, tt.expected.Destination.URI, config.Destination.URI)
			assert.Equal(t, tt.expected.Destination.Host, config.Destination.Host)
			assert.Equal(t, tt.expected.Destination.Port, config.Destination.Port)
			assert.Equal(t, tt.expected.Destination.Username, config.Destination.Username)
			assert.Equal(t, tt.expected.Destination.Password, config.Destination.Password)
			assert.Equal(t, tt.expected.Destination.Database, config.Destination.Database)
			assert.Equal(t, tt.expected.Destination.SSLMode, config.Destination.SSLMode)
		})
	}
}

func TestForkConfig_LoadFromEnvironment_ExistingBehavior(t *testing.T) {
	// Clean up environment before and after test
	originalEnv := map[string]string{}
	for _, key := range []string{
		"PGFORK_SOURCE_HOST", "PGFORK_SOURCE_PORT", "PGFORK_SOURCE_USER",
		"PGFORK_SOURCE_PASSWORD", "PGFORK_SOURCE_DATABASE", "PGFORK_SOURCE_SSLMODE",
		"PGFORK_DEST_HOST", "PGFORK_DEST_PORT", "PGFORK_DEST_USER",
		"PGFORK_DEST_PASSWORD", "PGFORK_DEST_SSLMODE",
		"PGFORK_TARGET_DATABASE", "PGFORK_DROP_IF_EXISTS", "PGFORK_MAX_CONNECTIONS",
		"PGFORK_CHUNK_SIZE", "PGFORK_TIMEOUT", "PGFORK_OUTPUT_FORMAT", "PGFORK_QUIET", "PGFORK_DRY_RUN",
	} {
		originalEnv[key] = os.Getenv(key)
		require.NoError(t, os.Unsetenv(key))
	}

	defer func() {
		for key, value := range originalEnv {
			if value == "" {
				require.NoError(t, os.Unsetenv(key))
			} else {
				require.NoError(t, os.Setenv(key, value))
			}
		}
	}()

	// Set test environment variables (existing behavior)
	testEnv := map[string]string{
		"PGFORK_SOURCE_HOST":     "source.example.com",
		"PGFORK_SOURCE_PORT":     "5433",
		"PGFORK_SOURCE_USER":     "sourceuser",
		"PGFORK_SOURCE_PASSWORD": "sourcepass",
		"PGFORK_SOURCE_DATABASE": "sourcedb",
		"PGFORK_SOURCE_SSLMODE":  "require",
		"PGFORK_DEST_HOST":       "dest.example.com",
		"PGFORK_DEST_PORT":       "5434",
		"PGFORK_DEST_USER":       "destuser",
		"PGFORK_DEST_PASSWORD":   "destpass",
		"PGFORK_DEST_SSLMODE":    "prefer",
		"PGFORK_TARGET_DATABASE": "targetdb",
		"PGFORK_DROP_IF_EXISTS":  "true",
		"PGFORK_MAX_CONNECTIONS": "8",
		"PGFORK_CHUNK_SIZE":      "2000",
		"PGFORK_TIMEOUT":         "45m",
		"PGFORK_OUTPUT_FORMAT":   "json",
		"PGFORK_QUIET":           "true",
		"PGFORK_DRY_RUN":         "false",
	}

	for key, value := range testEnv {
		require.NoError(t, os.Setenv(key, value))
	}

	config := &ForkConfig{}
	config.LoadFromEnvironment()

	// Verify source configuration
	assert.Equal(t, "source.example.com", config.Source.Host)
	assert.Equal(t, 5433, config.Source.Port)
	assert.Equal(t, "sourceuser", config.Source.Username)
	assert.Equal(t, "sourcepass", config.Source.Password)
	assert.Equal(t, "sourcedb", config.Source.Database)
	assert.Equal(t, "require", config.Source.SSLMode)

	// Verify destination configuration
	assert.Equal(t, "dest.example.com", config.Destination.Host)
	assert.Equal(t, 5434, config.Destination.Port)
	assert.Equal(t, "destuser", config.Destination.Username)
	assert.Equal(t, "destpass", config.Destination.Password)
	assert.Equal(t, "prefer", config.Destination.SSLMode)

	// Verify fork configuration
	assert.Equal(t, "targetdb", config.TargetDatabase)
	assert.True(t, config.DropIfExists)
	assert.Equal(t, 8, config.MaxConnections)
	assert.Equal(t, 2000, config.ChunkSize)
	assert.Equal(t, 45*time.Minute, config.Timeout)
	assert.Equal(t, "json", config.OutputFormat)
	assert.True(t, config.Quiet)
	assert.False(t, config.DryRun)
}

func TestForkConfig_IsSameServer(t *testing.T) {
	tests := []struct {
		name     string
		config   ForkConfig
		expected bool
	}{
		{
			name: "same server - identical configs",
			config: ForkConfig{
				Source: DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					Username: "user",
				},
				Destination: DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					Username: "user",
				},
			},
			expected: true,
		},
		{
			name: "different server - different hosts",
			config: ForkConfig{
				Source: DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					Username: "user",
				},
				Destination: DatabaseConfig{
					Host:     "remote.example.com",
					Port:     5432,
					Username: "user",
				},
			},
			expected: false,
		},
		{
			name: "different server - different ports",
			config: ForkConfig{
				Source: DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					Username: "user",
				},
				Destination: DatabaseConfig{
					Host:     "localhost",
					Port:     5433,
					Username: "user",
				},
			},
			expected: false,
		},
		{
			name: "different server - different users",
			config: ForkConfig{
				Source: DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					Username: "user1",
				},
				Destination: DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					Username: "user2",
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.IsSameServer()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestForkConfig_ProcessTemplates(t *testing.T) {
	tests := []struct {
		name           string
		config         ForkConfig
		expectedTarget string
		expectedSource string
		expectError    bool
	}{
		{
			name: "simple template processing",
			config: ForkConfig{
				TargetDatabase: "test_{{.PR_NUMBER}}",
				Source: DatabaseConfig{
					Database: "source_{{.BRANCH}}",
				},
				TemplateVars: map[string]string{
					"PR_NUMBER": "123",
					"BRANCH":    "feature",
				},
			},
			expectedTarget: "test_123",
			expectedSource: "source_feature",
			expectError:    false,
		},
		{
			name: "no templates",
			config: ForkConfig{
				TargetDatabase: "simple_db",
				Source: DatabaseConfig{
					Database: "source_db",
				},
			},
			expectedTarget: "simple_db",
			expectedSource: "source_db",
			expectError:    false,
		},
		{
			name: "invalid template syntax",
			config: ForkConfig{
				TargetDatabase: "test_{{.INVALID",
				TemplateVars:   map[string]string{},
			},
			expectError: true,
		},
		{
			name: "missing template variable",
			config: ForkConfig{
				TargetDatabase: "test_{{.MISSING_VAR}}",
				TemplateVars:   map[string]string{},
			},
			expectedTarget: "test_<no value>",
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.ProcessTemplates()

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedTarget, tt.config.TargetDatabase)
			assert.Equal(t, tt.expectedSource, tt.config.Source.Database)
		})
	}
}

func TestForkConfig_Validate(t *testing.T) {
	tests := []struct {
		name        string
		config      ForkConfig
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid configuration",
			config: ForkConfig{
				Source: DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					Username: "user",
					Database: "sourcedb",
				},
				Destination: DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					Username: "user",
					Database: "destdb",
				},
				TargetDatabase: "targetdb",
				MaxConnections: 4,
				ChunkSize:      1000,
				Timeout:        30 * time.Minute,
				OutputFormat:   "text",
				LogLevel:       "info",
			},
			expectError: false,
		},
		{
			name: "missing source database",
			config: ForkConfig{
				Source: DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					Username: "user",
					// Database missing
				},
				Destination: DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					Username: "user",
					Database: "destdb",
				},
				TargetDatabase: "targetdb",
				MaxConnections: 4,
				ChunkSize:      1000,
				Timeout:        30 * time.Minute,
				OutputFormat:   "text",
				LogLevel:       "info",
			},
			expectError: true,
			errorMsg:    "Database is required",
		},
		{
			name: "missing target database",
			config: ForkConfig{
				Source: DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					Username: "user",
					Database: "sourcedb",
				},
				Destination: DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					Username: "user",
					Database: "destdb",
				},
				MaxConnections: 4,
				ChunkSize:      1000,
				Timeout:        30 * time.Minute,
				OutputFormat:   "text",
				LogLevel:       "info",
				// TargetDatabase missing
			},
			expectError: true,
			errorMsg:    "TargetDatabase is required",
		},
		{
			name: "conflicting schema and data only flags",
			config: ForkConfig{
				Source: DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					Username: "user",
					Database: "sourcedb",
				},
				Destination: DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					Username: "user",
					Database: "destdb",
				},
				TargetDatabase: "targetdb",
				MaxConnections: 4,
				ChunkSize:      1000,
				Timeout:        30 * time.Minute,
				OutputFormat:   "text",
				LogLevel:       "info",
				SchemaOnly:     true,
				DataOnly:       true,
			},
			expectError: true,
			errorMsg:    "cannot specify both schema-only and data-only options",
		},
		{
			name: "invalid max connections",
			config: ForkConfig{
				Source: DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					Username: "user",
					Database: "sourcedb",
				},
				Destination: DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					Username: "user",
					Database: "destdb",
				},
				TargetDatabase: "targetdb",
				MaxConnections: -1, // Invalid
				ChunkSize:      1000,
				Timeout:        30 * time.Minute,
				OutputFormat:   "text",
				LogLevel:       "info",
			},
			expectError: true,
			errorMsg:    "MaxConnections must be at least 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestDatabaseConfig_URI_EdgeCases tests URI parsing edge cases and error conditions
func TestDatabaseConfig_URI_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		uri         string
		expectError bool
		expected    *DatabaseConfig
	}{
		{
			name: "URI with special characters in password",
			uri:  "postgresql://user:!%40%23%24%5E%26*()_%2B%60-%3D%7B%7D%5B%5D%7C%5C%3A%22%3B'%3C%3E%3F%2C.%2F@localhost/database", // pragma: allowlist secret
			expected: &DatabaseConfig{
				URI:      "postgresql://user:!%40%23%24%5E%26*()_%2B%60-%3D%7B%7D%5B%5D%7C%5C%3A%22%3B'%3C%3E%3F%2C.%2F@localhost/database", // pragma: allowlist secret
				Host:     "localhost",
				Port:     5432,
				Username: "user",
				Password: "!@#$^&*()_+`-={}[]|\\:\";'<>?,./", // pragma: allowlist secret
				Database: "database",
			},
		},
		{
			name: "URI with query parameters",
			uri:  "postgresql://user:pass@localhost:5432/db?sslmode=require&connect_timeout=30", // pragma: allowlist secret
			expected: &DatabaseConfig{
				URI:      "postgresql://user:pass@localhost:5432/db?sslmode=require&connect_timeout=30", // pragma: allowlist secret
				Host:     "localhost",
				Port:     5432,
				Username: "user",
				Password: "pass", // pragma: allowlist secret
				Database: "db",
				SSLMode:  "require",
			},
		},
		{
			name: "URI with IPv6 address",
			uri:  "postgresql://user:pass@[::1]:5432/db", // pragma: allowlist secret
			expected: &DatabaseConfig{
				URI:      "postgresql://user:pass@[::1]:5432/db", // pragma: allowlist secret
				Host:     "::1",
				Port:     5432,
				Username: "user",
				Password: "pass", // pragma: allowlist secret
				Database: "db",
			},
		},
		{
			name: "URI with default port",
			uri:  "postgresql://user:pass@localhost/db", // pragma: allowlist secret
			expected: &DatabaseConfig{
				URI:      "postgresql://user:pass@localhost/db", // pragma: allowlist secret
				Host:     "localhost",
				Port:     5432, // Default port
				Username: "user",
				Password: "pass", // pragma: allowlist secret
				Database: "db",
			},
		},
		{
			name: "URI with encoded database name",
			uri:  "postgresql://user:pass@localhost:5432/my%2Ddb", // pragma: allowlist secret
			expected: &DatabaseConfig{
				URI:      "postgresql://user:pass@localhost:5432/my%2Ddb", // pragma: allowlist secret
				Host:     "localhost",
				Port:     5432,
				Username: "user",
				Password: "pass", // pragma: allowlist secret
				Database: "my-db",
			},
		},
		{
			name:        "malformed URI",
			uri:         "postgresql://user:password@localhost:invalid_port/database", // pragma: allowlist secret
			expectError: true,
		},
		{
			name:        "invalid scheme",
			uri:         "mysql://user:pass@localhost:3306/db", // pragma: allowlist secret
			expectError: true,
		},
		{
			name:        "completely invalid URI",
			uri:         "not-a-valid-uri",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parsePostgreSQLURI(tt.uri)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)

				assert.Equal(t, tt.expected.URI, result.URI)
				assert.Equal(t, tt.expected.Host, result.Host)
				assert.Equal(t, tt.expected.Port, result.Port)
				assert.Equal(t, tt.expected.Username, result.Username)
				assert.Equal(t, tt.expected.Password, result.Password)
				assert.Equal(t, tt.expected.Database, result.Database)
				assert.Equal(t, tt.expected.SSLMode, result.SSLMode)
			}
		})
	}
}

// TestDatabaseConfig_URI_ValidationTests tests URI validation in various contexts
func TestDatabaseConfig_URI_ValidationTests(t *testing.T) {
	tests := []struct {
		name        string
		config      DatabaseConfig
		expectValid bool
		errorMsg    string
	}{
		{
			name: "valid URI configuration",
			config: DatabaseConfig{
				URI: "postgresql://user:pass@localhost:5432/db", // pragma: allowlist secret
			},
			expectValid: true,
		},
		{
			name: "invalid URI format",
			config: DatabaseConfig{
				URI: "not-a-valid-uri",
			},
			expectValid: false,
			errorMsg:    "invalid PostgreSQL URI",
		},
		{
			name: "unsupported URI scheme",
			config: DatabaseConfig{
				URI: "mysql://user:pass@localhost:3306/db", // pragma: allowlist secret
			},
			expectValid: false,
			errorMsg:    "invalid PostgreSQL URI scheme",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.config.URI != "" {
				_, err := parsePostgreSQLURI(tt.config.URI)
				if tt.expectValid {
					assert.NoError(t, err)
				} else {
					assert.Error(t, err)
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			}
		})
	}
}

// TestForkConfig_URI_EnvironmentPrecedence tests environment variable precedence with URIs
func TestForkConfig_URI_EnvironmentPrecedence(t *testing.T) {
	// Clean environment
	envVars := []string{
		"PGFORK_SOURCE_URI", "PGFORK_TARGET_URI",
		"PGFORK_SOURCE_HOST", "PGFORK_SOURCE_PORT", "PGFORK_SOURCE_USER",
		"PGFORK_SOURCE_PASSWORD", "PGFORK_SOURCE_DATABASE", "PGFORK_SOURCE_SSLMODE",
		"PGFORK_DEST_HOST", "PGFORK_DEST_PORT", "PGFORK_DEST_USER",
		"PGFORK_DEST_PASSWORD", "PGFORK_DEST_SSLMODE", "PGFORK_TARGET_DATABASE",
	}
	originalEnv := make(map[string]string)
	for _, env := range envVars {
		originalEnv[env] = os.Getenv(env)
		require.NoError(t, os.Unsetenv(env))
	}

	defer func() {
		for key, value := range originalEnv {
			if value == "" {
				require.NoError(t, os.Unsetenv(key))
			} else {
				require.NoError(t, os.Setenv(key, value))
			}
		}
	}()

	t.Run("URI overrides individual parameters", func(t *testing.T) {
		// Set both URI and individual parameters
		require.NoError(t, os.Setenv("PGFORK_SOURCE_URI", "postgresql://uriuser:uripass@urihost:5555/uridb?sslmode=require")) // pragma: allowlist secret
		require.NoError(t, os.Setenv("PGFORK_SOURCE_HOST", "localhost"))
		require.NoError(t, os.Setenv("PGFORK_SOURCE_PORT", "5432"))
		require.NoError(t, os.Setenv("PGFORK_SOURCE_USER", "testuser"))
		require.NoError(t, os.Setenv("PGFORK_SOURCE_PASSWORD", "testpass")) // pragma: allowlist secret
		require.NoError(t, os.Setenv("PGFORK_SOURCE_DATABASE", "testdb"))
		require.NoError(t, os.Setenv("PGFORK_SOURCE_SSLMODE", "disable"))

		config := &ForkConfig{}
		config.LoadFromEnvironment()

		// URI values should take precedence
		assert.Equal(t, "postgresql://uriuser:uripass@urihost:5555/uridb?sslmode=require", config.Source.URI)
		assert.Equal(t, "urihost", config.Source.Host)
		assert.Equal(t, 5555, config.Source.Port)
		assert.Equal(t, "uriuser", config.Source.Username)
		assert.Equal(t, "uripass", config.Source.Password)
		assert.Equal(t, "uridb", config.Source.Database)
		assert.Equal(t, "require", config.Source.SSLMode)

		// Connection string should use URI
		connStr := config.Source.ConnectionString()
		assert.Equal(t, "postgresql://uriuser:uripass@urihost:5555/uridb?sslmode=require", connStr)
	})

	t.Run("fallback to individual parameters when URI is empty", func(t *testing.T) {
		// Clear environment first
		for _, env := range envVars {
			require.NoError(t, os.Unsetenv(env))
		}

		// Set only individual parameters
		require.NoError(t, os.Setenv("PGFORK_SOURCE_HOST", "localhost"))
		require.NoError(t, os.Setenv("PGFORK_SOURCE_PORT", "5432"))
		require.NoError(t, os.Setenv("PGFORK_SOURCE_USER", "testuser"))
		require.NoError(t, os.Setenv("PGFORK_SOURCE_PASSWORD", "testpass")) // pragma: allowlist secret
		require.NoError(t, os.Setenv("PGFORK_SOURCE_DATABASE", "testdb"))
		require.NoError(t, os.Setenv("PGFORK_SOURCE_SSLMODE", "disable"))

		config := &ForkConfig{}
		config.LoadFromEnvironment()

		// Individual parameter values should be used
		assert.Empty(t, config.Source.URI)
		assert.Equal(t, "localhost", config.Source.Host)
		assert.Equal(t, 5432, config.Source.Port)
		assert.Equal(t, "testuser", config.Source.Username)
		assert.Equal(t, "testpass", config.Source.Password) // pragma: allowlist secret
		assert.Equal(t, "testdb", config.Source.Database)
		assert.Equal(t, "disable", config.Source.SSLMode)

		// Connection string should use individual parameters
		connStr := config.Source.ConnectionString()
		expected := "host=localhost port=5432 user=testuser password=testpass dbname=testdb sslmode=disable" // pragma: allowlist secret
		assert.Equal(t, expected, connStr)
	})
}

// TestDatabaseConfig_URI_SameServerDetection tests same-server detection with URIs
func TestDatabaseConfig_URI_SameServerDetection(t *testing.T) {
	tests := []struct {
		name       string
		config     ForkConfig
		expectSame bool
	}{
		{
			name: "same server with URIs - same host and port",
			config: ForkConfig{
				Source: DatabaseConfig{
					URI: "postgresql://user1:pass1@localhost:5432/db1", // pragma: allowlist secret
				},
				Destination: DatabaseConfig{
					URI: "postgresql://user1:pass1@localhost:5432/db2", // Same user for same server // pragma: allowlist secret
				},
			},
			expectSame: true,
		},
		{
			name: "different servers with URIs - different hosts",
			config: ForkConfig{
				Source: DatabaseConfig{
					URI: "postgresql://user1:pass1@host1:5432/db1", // pragma: allowlist secret
				},
				Destination: DatabaseConfig{
					URI: "postgresql://user2:pass2@host2:5432/db2", // pragma: allowlist secret
				},
			},
			expectSame: false,
		},
		{
			name: "different servers with URIs - different ports",
			config: ForkConfig{
				Source: DatabaseConfig{
					URI: "postgresql://user1:pass1@localhost:5432/db1", // pragma: allowlist secret
				},
				Destination: DatabaseConfig{
					URI: "postgresql://user2:pass2@localhost:5433/db2", // pragma: allowlist secret
				},
			},
			expectSame: false,
		},
		{
			name: "mixed configuration - URI and individual params, same server",
			config: ForkConfig{
				Source: DatabaseConfig{
					URI: "postgresql://user1:pass1@localhost:5432/db1", // pragma: allowlist secret
				},
				Destination: DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					Username: "user1", // Same user for same server
					Password: "pass1", // pragma: allowlist secret
					Database: "db2",
				},
			},
			expectSame: true,
		},
		{
			name: "mixed configuration - URI and individual params, different servers",
			config: ForkConfig{
				Source: DatabaseConfig{
					URI: "postgresql://user1:pass1@host1:5432/db1", // pragma: allowlist secret
				},
				Destination: DatabaseConfig{
					Host:     "host2",
					Port:     5432,
					Username: "user2",
					Password: "pass2", // pragma: allowlist secret
					Database: "db2",
				},
			},
			expectSame: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make a copy of the config to avoid modifying the test data
			config := tt.config

			// Parse URIs to populate individual fields for same-server detection
			if config.Source.URI != "" {
				parsed, err := parsePostgreSQLURI(config.Source.URI)
				require.NoError(t, err)
				config.Source.Host = parsed.Host
				config.Source.Port = parsed.Port
				config.Source.Username = parsed.Username
			}
			if config.Destination.URI != "" {
				parsed, err := parsePostgreSQLURI(config.Destination.URI)
				require.NoError(t, err)
				config.Destination.Host = parsed.Host
				config.Destination.Port = parsed.Port
				config.Destination.Username = parsed.Username
			}

			result := config.IsSameServer()
			assert.Equal(t, tt.expectSame, result, "Same server detection result mismatch")
		})
	}
}
