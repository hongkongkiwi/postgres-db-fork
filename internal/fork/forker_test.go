package fork

import (
	"context"
	"testing"

	"github.com/hongkongkiwi/postgres-db-fork/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewForker(t *testing.T) {
	cfg := &config.ForkConfig{
		Source: config.DatabaseConfig{
			Host:     "localhost",
			Port:     5432,
			Database: "sourcedb",
		},
		Destination: config.DatabaseConfig{
			Host: "localhost",
			Port: 5432,
		},
		TargetDatabase: "targetdb",
	}

	forker := NewForker(cfg)

	assert.NotNil(t, forker)
	assert.Equal(t, cfg, forker.config)
}

func TestForker_Fork_ValidationError(t *testing.T) {
	// Test with invalid configuration
	cfg := &config.ForkConfig{
		// Missing required fields
	}

	forker := NewForker(cfg)
	err := forker.Fork(context.Background())

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid configuration")
}

func TestForker_IsSameServerDetection(t *testing.T) {
	tests := []struct {
		name         string
		config       *config.ForkConfig
		expectedSame bool
	}{
		{
			name: "same server configuration",
			config: &config.ForkConfig{
				Source: config.DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					Username: "user",
					Database: "sourcedb",
				},
				Destination: config.DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					Username: "user",
				},
				TargetDatabase: "targetdb",
			},
			expectedSame: true,
		},
		{
			name: "different server configuration",
			config: &config.ForkConfig{
				Source: config.DatabaseConfig{
					Host:     "source.example.com",
					Port:     5432,
					Username: "user",
					Database: "sourcedb",
				},
				Destination: config.DatabaseConfig{
					Host:     "dest.example.com",
					Port:     5432,
					Username: "user",
				},
				TargetDatabase: "targetdb",
			},
			expectedSame: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.IsSameServer()
			assert.Equal(t, tt.expectedSame, result)
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name     string
		bytes    int64
		expected string
	}{
		{
			name:     "bytes",
			bytes:    512,
			expected: "512 B",
		},
		{
			name:     "kilobytes",
			bytes:    1536, // 1.5 KB
			expected: "1.5 KB",
		},
		{
			name:     "megabytes",
			bytes:    1572864, // 1.5 MB
			expected: "1.5 MB",
		},
		{
			name:     "gigabytes",
			bytes:    1610612736, // 1.5 GB
			expected: "1.5 GB",
		},
		{
			name:     "zero bytes",
			bytes:    0,
			expected: "0 B",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatBytes(tt.bytes)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Note: Testing the actual Fork method with database connections would require
// integration tests with real or mocked database connections. For unit tests,
// we focus on the configuration validation and logic flow.
func TestForker_ConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      *config.ForkConfig
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid same-server config",
			config: &config.ForkConfig{
				Source: config.DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					Username: "user",
					Database: "sourcedb",
				},
				Destination: config.DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					Username: "user",
				},
				TargetDatabase: "targetdb",
			},
			expectError: false,
		},
		{
			name: "valid cross-server config",
			config: &config.ForkConfig{
				Source: config.DatabaseConfig{
					Host:     "source.example.com",
					Port:     5432,
					Username: "user",
					Database: "sourcedb",
				},
				Destination: config.DatabaseConfig{
					Host:     "dest.example.com",
					Port:     5432,
					Username: "user",
				},
				TargetDatabase: "targetdb",
			},
			expectError: false,
		},
		{
			name: "missing source database",
			config: &config.ForkConfig{
				Source: config.DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					Username: "user",
				},
				Destination: config.DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					Username: "user",
				},
				TargetDatabase: "targetdb",
			},
			expectError: true,
		},
		{
			name: "missing target database",
			config: &config.ForkConfig{
				Source: config.DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					Username: "user",
					Database: "sourcedb",
				},
				Destination: config.DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					Username: "user",
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			forker := NewForker(tt.config)
			err := forker.Fork(context.Background())

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "invalid configuration")
			} else {
				// For valid configs, we expect connection errors since we don't have actual DBs
				// The important thing is that validation passes
				if err != nil {
					// Should be a connection error, not a validation error
					assert.NotContains(t, err.Error(), "invalid configuration")
				}
			}
		})
	}
}
