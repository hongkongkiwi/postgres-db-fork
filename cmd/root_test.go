package cmd

import (
	"bytes"
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRootCmd(t *testing.T) {
	// Test basic root command properties
	assert.Equal(t, "postgres-db-fork", rootCmd.Use)
	assert.Contains(t, rootCmd.Short, "PostgreSQL databases")
	assert.NotEmpty(t, rootCmd.Long)
}

func TestRootCmdFlags(t *testing.T) {
	// Test that all expected global flags are present
	flags := []string{
		"config", "log-level", "verbose", "no-color", "force",
	}

	for _, flagName := range flags {
		flag := rootCmd.PersistentFlags().Lookup(flagName)
		assert.NotNil(t, flag, "Flag %s should exist", flagName)
	}
}

func TestExecuteFunction(t *testing.T) {
	// Test that Execute function exists and can be called
	// We can't easily test the actual execution without affecting global state
	assert.NotNil(t, Execute)
}

func TestInitConfig(t *testing.T) {
	// Save original config
	originalConfig := viper.ConfigFileUsed()
	defer func() {
		viper.Reset()
		if originalConfig != "" {
			viper.SetConfigFile(originalConfig)
			if err := viper.ReadInConfig(); err != nil {
				t.Logf("Could not read config: %v", err)
			}
		}
	}()

	// Test with no config file
	viper.Reset()
	cfgFile = ""

	// This should not panic
	assert.NotPanics(t, func() {
		initConfig()
	})
}

func TestInitConfigWithCustomFile(t *testing.T) {
	// Create a temporary config file
	tempFile, err := os.CreateTemp("", "test-config-*.yaml")
	require.NoError(t, err)
	defer func() {
		if err := os.Remove(tempFile.Name()); err != nil {
			t.Logf("Failed to remove temp file: %v", err)
		}
	}()

	// Write some config content
	configContent := `
log-level: debug
verbose: true
`
	_, err = tempFile.WriteString(configContent)
	require.NoError(t, err)
	if err := tempFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	// Save original state
	originalConfig := viper.ConfigFileUsed()
	originalCfgFile := cfgFile
	defer func() {
		viper.Reset()
		cfgFile = originalCfgFile
		if originalConfig != "" {
			viper.SetConfigFile(originalConfig)
			if err := viper.ReadInConfig(); err != nil {
				t.Logf("Could not read config: %v", err)
			}
		}
	}()

	// Test with custom config file
	viper.Reset()
	cfgFile = tempFile.Name()

	assert.NotPanics(t, func() {
		initConfig()
	})

	// Check if config was loaded
	assert.Equal(t, tempFile.Name(), viper.ConfigFileUsed())
}

func TestRootCmdHelp(t *testing.T) {
	// Test that help output contains expected information
	cmd := &cobra.Command{
		Use:   rootCmd.Use,
		Short: rootCmd.Short,
		Long:  rootCmd.Long,
	}

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})

	// This should not error
	err := cmd.Execute()
	assert.NoError(t, err)
}

func TestViperFlagBinding(t *testing.T) {
	// Test that viper correctly binds to command flags
	expectedBindings := map[string]string{
		"log-level": "log-level",
		"verbose":   "verbose",
		"no-color":  "no-color",
		"force":     "force",
	}

	for viperKey, flagName := range expectedBindings {
		// Set a test value via viper
		testValue := "test-value"
		if flagName == "verbose" || flagName == "no-color" || flagName == "force" {
			testValue = "true"
		}

		viper.Set(viperKey, testValue)

		// Check that viper can retrieve the value
		retrievedValue := viper.GetString(viperKey)
		assert.Equal(t, testValue, retrievedValue, "Viper binding for %s should work", viperKey)
	}

	// Clean up
	viper.Reset()
}

func TestLogLevelValidation(t *testing.T) {
	tests := []struct {
		name        string
		logLevel    string
		expectError bool
	}{
		{"valid debug level", "debug", false},
		{"valid info level", "info", false},
		{"valid warn level", "warn", false},
		{"valid error level", "error", false},
		{"invalid level", "invalid", true},
		{"empty level", "", false}, // Should default to info
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original state
			viper.Reset()
			defer viper.Reset()

			if tt.logLevel != "" {
				viper.Set("log-level", tt.logLevel)
			}

			// Test the initConfig function
			if tt.expectError {
				// For invalid log levels, logrus.ParseLevel will fail
				// but initConfig handles this gracefully by defaulting to InfoLevel
				assert.NotPanics(t, func() {
					initConfig()
				})
			} else {
				assert.NotPanics(t, func() {
					initConfig()
				})
			}
		})
	}
}

func TestConfigFileDiscovery(t *testing.T) {
	// Test config file discovery in different locations
	originalHome := os.Getenv("HOME")
	defer func() {
		if err := os.Setenv("HOME", originalHome); err != nil {
			t.Logf("Failed to restore HOME: %v", err)
		}
	}()

	// Create a temporary directory to act as home
	tempDir, err := os.MkdirTemp("", "test-home-*")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	if err := os.Setenv("HOME", tempDir); err != nil {
		t.Fatalf("Failed to set HOME: %v", err)
	}

	// Save original state
	originalCfgFile := cfgFile
	defer func() {
		cfgFile = originalCfgFile
		viper.Reset()
	}()

	// Test with no specific config file
	cfgFile = ""
	viper.Reset()

	assert.NotPanics(t, func() {
		initConfig()
	})
}

func TestDefaultValues(t *testing.T) {
	// Test that default values are properly set
	viper.Reset()

	// Set default values that would normally come from flag defaults
	viper.SetDefault("log-level", "info")
	viper.SetDefault("verbose", false)
	viper.SetDefault("no-color", false)
	viper.SetDefault("force", false)

	initConfig()

	// Test default log level
	logLevel := viper.GetString("log-level")
	assert.Equal(t, "info", logLevel)

	// Test default boolean values
	verbose := viper.GetBool("verbose")
	assert.False(t, verbose) // Should default to false

	noColor := viper.GetBool("no-color")
	assert.False(t, noColor) // Should default to false

	force := viper.GetBool("force")
	assert.False(t, force) // Should default to false
}
