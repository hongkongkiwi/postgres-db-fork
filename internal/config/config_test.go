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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.ConnectionString()
			assert.Equal(t, tt.expected, result)
		})
	}
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

func TestForkConfig_LoadFromEnvironment(t *testing.T) {
	// Save original environment
	originalEnv := map[string]string{}
	envVars := []string{
		"PGFORK_SOURCE_HOST", "PGFORK_SOURCE_PORT", "PGFORK_SOURCE_USER",
		"PGFORK_SOURCE_PASSWORD", "PGFORK_SOURCE_DATABASE", "PGFORK_SOURCE_SSLMODE",
		"PGFORK_DEST_HOST", "PGFORK_DEST_PORT", "PGFORK_DEST_USER",
		"PGFORK_DEST_PASSWORD", "PGFORK_DEST_SSLMODE",
		"PGFORK_TARGET_DATABASE", "PGFORK_DROP_IF_EXISTS", "PGFORK_MAX_CONNECTIONS",
		"PGFORK_CHUNK_SIZE", "PGFORK_TIMEOUT", "PGFORK_OUTPUT_FORMAT",
		"PGFORK_QUIET", "PGFORK_DRY_RUN", "PGFORK_VAR_TEST_VAR",
	}

	for _, env := range envVars {
		if val := os.Getenv(env); val != "" {
			originalEnv[env] = val
		}
		if err := os.Unsetenv(env); err != nil {
			t.Logf("Failed to unset environment variable %s: %v", env, err)
		}
	}

	// Restore environment after test
	defer func() {
		for _, env := range envVars {
			if err := os.Unsetenv(env); err != nil {
				t.Logf("Failed to unset environment variable %s: %v", env, err)
			}
		}
		for env, val := range originalEnv {
			if err := os.Setenv(env, val); err != nil {
				t.Errorf("Failed to set environment variable %s: %v", env, err)
			}
		}
	}()

	// Set test environment variables
	testEnvVars := map[string]string{
		"PGFORK_SOURCE_HOST":     "source.example.com",
		"PGFORK_SOURCE_PORT":     "5433",
		"PGFORK_SOURCE_USER":     "sourceuser",
		"PGFORK_SOURCE_PASSWORD": "sourcepass", // pragma: allowlist secret
		"PGFORK_SOURCE_DATABASE": "sourcedb",
		"PGFORK_SOURCE_SSLMODE":  "require",
		"PGFORK_DEST_HOST":       "dest.example.com",
		"PGFORK_DEST_PORT":       "5434",
		"PGFORK_DEST_USER":       "destuser",
		"PGFORK_DEST_PASSWORD":   "destpass", // pragma: allowlist secret
		"PGFORK_DEST_SSLMODE":    "prefer",
		"PGFORK_TARGET_DATABASE": "targetdb",
		"PGFORK_DROP_IF_EXISTS":  "true",
		"PGFORK_MAX_CONNECTIONS": "8",
		"PGFORK_CHUNK_SIZE":      "2000",
		"PGFORK_TIMEOUT":         "1h",
		"PGFORK_OUTPUT_FORMAT":   "json",
		"PGFORK_QUIET":           "true",
		"PGFORK_DRY_RUN":         "true",
		"PGFORK_VAR_TEST_VAR":    "test_value",
	}

	for env, val := range testEnvVars {
		if err := os.Setenv(env, val); err != nil {
			t.Errorf("Failed to set environment variable %s: %v", env, err)
		}
	}

	config := &ForkConfig{}
	config.LoadFromEnvironment()

	// Test source configuration
	assert.Equal(t, "source.example.com", config.Source.Host)
	assert.Equal(t, 5433, config.Source.Port)
	assert.Equal(t, "sourceuser", config.Source.Username)
	assert.Equal(t, "sourcepass", config.Source.Password) // pragma: allowlist secret
	assert.Equal(t, "sourcedb", config.Source.Database)
	assert.Equal(t, "require", config.Source.SSLMode)

	// Test destination configuration
	assert.Equal(t, "dest.example.com", config.Destination.Host)
	assert.Equal(t, 5434, config.Destination.Port)
	assert.Equal(t, "destuser", config.Destination.Username)
	assert.Equal(t, "destpass", config.Destination.Password) // pragma: allowlist secret
	assert.Equal(t, "prefer", config.Destination.SSLMode)

	// Test fork configuration
	assert.Equal(t, "targetdb", config.TargetDatabase)
	assert.True(t, config.DropIfExists)
	assert.Equal(t, 8, config.MaxConnections)
	assert.Equal(t, 2000, config.ChunkSize)
	assert.Equal(t, time.Hour, config.Timeout)
	assert.Equal(t, "json", config.OutputFormat)
	assert.True(t, config.Quiet)
	assert.True(t, config.DryRun)

	// Test template variables
	assert.Equal(t, "test_value", config.TemplateVars["TEST_VAR"])
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
				},
				TargetDatabase: "targetdb",
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
				},
				Destination: DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					Username: "user",
				},
				TargetDatabase: "targetdb",
			},
			expectError: true,
			errorMsg:    "source database is required",
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
				},
			},
			expectError: true,
			errorMsg:    "target database name is required",
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
				},
				TargetDatabase: "targetdb",
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
				},
				TargetDatabase: "targetdb",
				MaxConnections: -1,
			},
			expectError: true,
			errorMsg:    "max connections must be greater than 0",
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
