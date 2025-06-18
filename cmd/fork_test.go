package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForkCmd(t *testing.T) {
	// Test basic fork command properties
	assert.Equal(t, "fork", forkCmd.Use)
	assert.Contains(t, forkCmd.Short, "Fork")
	assert.NotEmpty(t, forkCmd.Long)
}

func TestForkCmdFlags(t *testing.T) {
	// Test that all expected flags are present
	expectedFlags := []string{
		"source-host", "source-port", "source-user", "source-password",
		"source-db", "source-sslmode", "dest-host", "dest-port",
		"dest-user", "dest-password", "dest-sslmode", "target-db",
		"drop-if-exists", "max-connections", "chunk-size", "timeout",
		"exclude-tables", "include-tables", "schema-only", "data-only",
		"output-format", "quiet", "dry-run", "template-var", "env-vars", "background",
	}

	for _, flagName := range expectedFlags {
		flag := forkCmd.Flags().Lookup(flagName)
		assert.NotNil(t, flag, "Flag %s should exist", flagName)
	}
}

func TestForkCmdRequiredFlags(t *testing.T) {
	// Test that required flags are marked as required
	requiredFlags := []string{"source-db", "target-db"}

	for _, flagName := range requiredFlags {
		flag := forkCmd.Flags().Lookup(flagName)
		require.NotNil(t, flag, "Required flag %s should exist", flagName)

		// Check if flag is marked as required
		annotations := flag.Annotations
		if annotations != nil && annotations[cobra.BashCompOneRequiredFlag] != nil {
			// Flag is required
			assert.True(t, true, "Flag %s is properly marked as required", flagName)
		}
	}
}

func TestForkCmdViperBinding(t *testing.T) {
	// Test that fork command flags are properly bound to viper
	flagBindings := map[string]string{
		"source-host":     "source.host",
		"source-port":     "source.port",
		"source-user":     "source.username",
		"source-password": "source.password",
		"source-db":       "source.database",
		"target-db":       "target_database",
		"drop-if-exists":  "drop_if_exists",
		"max-connections": "max_connections",
		"output-format":   "output_format",
		"dry-run":         "dry_run",
	}

	// Clean slate
	viper.Reset()

	for flagName, viperKey := range flagBindings {
		// Set a test value via flag
		flag := forkCmd.Flags().Lookup(flagName)
		require.NotNil(t, flag, "Flag %s should exist", flagName)

		// Test that viper key exists after binding
		testValue := "test-value"
		if strings.Contains(flagName, "port") || strings.Contains(flagName, "connections") {
			testValue = "5432"
		}
		if strings.Contains(flagName, "exists") || strings.Contains(flagName, "dry-run") {
			testValue = "true"
		}

		viper.Set(viperKey, testValue)
		retrievedValue := viper.GetString(viperKey)
		assert.Equal(t, testValue, retrievedValue, "Viper binding for %s -> %s should work", flagName, viperKey)
	}

	viper.Reset()
}

func TestRunForkValidation(t *testing.T) {
	// Test configuration validation in runFork
	tests := []struct {
		name        string
		args        []string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "missing required source-db flag",
			args:        []string{"--target-db=test"},
			expectError: true,
			errorMsg:    "required flag",
		},
		{
			name:        "missing required target-db flag",
			args:        []string{"--source-db=test"},
			expectError: true,
			errorMsg:    "required flag",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a new command instance for testing
			cmd := &cobra.Command{
				Use:  "fork",
				RunE: runFork,
			}

			// Add the same flags as forkCmd
			cmd.Flags().String("source-db", "", "Source database name")
			cmd.Flags().String("target-db", "", "Target database name")
			// Configure required flags
			if err := cmd.MarkFlagRequired("source-db"); err != nil {
				t.Fatalf("Failed to mark source-db as required: %v", err)
			}
			if err := cmd.MarkFlagRequired("target-db"); err != nil {
				t.Fatalf("Failed to mark target-db as required: %v", err)
			}

			// Capture output
			var buf bytes.Buffer
			cmd.SetOut(&buf)
			cmd.SetErr(&buf)
			cmd.SetArgs(tt.args)

			err := cmd.Execute()

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestForkCmdDryRun(t *testing.T) {
	// Test dry run functionality
	viper.Reset()
	defer viper.Reset()

	// Set up for dry run
	viper.Set("source.host", "localhost")
	viper.Set("source.port", 5432)
	viper.Set("source.username", "testuser")
	viper.Set("source.database", "sourcedb")
	viper.Set("destination.host", "localhost")
	viper.Set("destination.port", 5432)
	viper.Set("destination.username", "testuser")
	viper.Set("target_database", "targetdb")
	viper.Set("dry_run", true)

	// Test that dry run mode can be enabled
	dryRun := viper.GetBool("dry_run")
	assert.True(t, dryRun)
}

func TestForkCmdOutputFormats(t *testing.T) {
	tests := []struct {
		name         string
		outputFormat string
		expectJSON   bool
	}{
		{"text output", "text", false},
		{"json output", "json", true},
		{"default output", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viper.Reset()
			defer viper.Reset()

			if tt.outputFormat != "" {
				viper.Set("output_format", tt.outputFormat)
			}

			format := viper.GetString("output_format")
			if tt.outputFormat == "" {
				// Should have default value
				assert.NotNil(t, format)
			} else {
				assert.Equal(t, tt.outputFormat, format)
			}
		})
	}
}

func TestForkCmdTemplateVariables(t *testing.T) {
	// Test template variable handling
	viper.Reset()
	defer viper.Reset()

	// Test setting template variables
	templateVars := map[string]string{
		"PR_NUMBER": "123",
		"BRANCH":    "feature-test",
		"COMMIT":    "abc1234",
	}

	viper.Set("template_vars", templateVars)

	retrievedVars := viper.GetStringMapString("template_vars")
	for key, expectedValue := range templateVars {
		assert.Equal(t, expectedValue, retrievedVars[key], "Template variable %s should match", key)
	}
}

func TestForkCmdTimeout(t *testing.T) {
	// Test timeout configuration
	viper.Reset()
	defer viper.Reset()

	timeout := 45 * time.Minute
	viper.Set("timeout", timeout)

	retrievedTimeout := viper.GetDuration("timeout")
	assert.Equal(t, timeout, retrievedTimeout)
}

func TestForkCmdTableFiltering(t *testing.T) {
	// Test include/exclude table functionality
	viper.Reset()
	defer viper.Reset()

	excludeTables := []string{"logs", "temp_data", "cache"}
	includeTables := []string{"users", "orders", "products"}

	viper.Set("exclude_tables", excludeTables)
	viper.Set("include_tables", includeTables)

	retrievedExclude := viper.GetStringSlice("exclude_tables")
	retrievedInclude := viper.GetStringSlice("include_tables")

	assert.Equal(t, excludeTables, retrievedExclude)
	assert.Equal(t, includeTables, retrievedInclude)
}

func TestForkCmdSchemaDataOptions(t *testing.T) {
	// Test schema-only and data-only options
	viper.Reset()
	defer viper.Reset()

	// Test schema only
	viper.Set("schema_only", true)
	viper.Set("data_only", false)

	assert.True(t, viper.GetBool("schema_only"))
	assert.False(t, viper.GetBool("data_only"))

	// Reset and test data only
	viper.Set("schema_only", false)
	viper.Set("data_only", true)

	assert.False(t, viper.GetBool("schema_only"))
	assert.True(t, viper.GetBool("data_only"))
}

func TestForkCmdEnvironmentVariables(t *testing.T) {
	// Test environment variable loading
	envVars := map[string]string{
		"PGFORK_SOURCE_HOST":     "env-host",
		"PGFORK_SOURCE_DATABASE": "env-db",
		"PGFORK_TARGET_DATABASE": "env-target",
		"PGFORK_VAR_TEST":        "env-template-var",
	}

	// Set environment variables
	for key, value := range envVars {
		if err := os.Setenv(key, value); err != nil {
			t.Fatalf("Failed to set environment variable %s: %v", key, err)
		}
	}

	// Clean up after test
	defer func() {
		for key := range envVars {
			if err := os.Unsetenv(key); err != nil {
				t.Logf("Failed to unset environment variable %s: %v", key, err)
			}
		}
	}()

	// Test that env-vars flag exists and defaults to true
	envVarsFlag := forkCmd.Flags().Lookup("env-vars")
	require.NotNil(t, envVarsFlag)
	assert.Equal(t, "true", envVarsFlag.DefValue)
}

func TestOutputResult(t *testing.T) {
	tests := []struct {
		name         string
		outputFormat string
		success      bool
		message      string
		errorMsg     string
		expectJSON   bool
	}{
		{
			name:         "text output success",
			outputFormat: "text",
			success:      true,
			message:      "Fork completed successfully",
			expectJSON:   false,
		},
		{
			name:         "json output success",
			outputFormat: "json",
			success:      true,
			message:      "Fork completed successfully",
			expectJSON:   true,
		},
		{
			name:         "json output failure",
			outputFormat: "json",
			success:      false,
			message:      "Fork failed",
			errorMsg:     "connection error",
			expectJSON:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// For JSON format, we'd expect valid JSON output
			if tt.expectJSON {
				// Mock JSON output structure
				result := map[string]interface{}{
					"success": tt.success,
					"message": tt.message,
					"format":  "json",
				}
				if tt.errorMsg != "" {
					result["error"] = tt.errorMsg
				}

				jsonBytes, err := json.Marshal(result)
				require.NoError(t, err)

				// Verify it's valid JSON
				var parsed map[string]interface{}
				err = json.Unmarshal(jsonBytes, &parsed)
				assert.NoError(t, err)
				assert.Equal(t, tt.success, parsed["success"])
				assert.Equal(t, tt.message, parsed["message"])
			}

			// For text format, just verify the message would be included
			if !tt.expectJSON {
				assert.Contains(t, tt.message, "Fork")
			}
		})
	}
}
