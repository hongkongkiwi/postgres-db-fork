package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// configCmd represents the config command
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage configuration settings",
	Long: `Manage postgres-db-fork configuration settings.

This command provides basic configuration management functionality to view and
modify configuration values. Configuration can come from:
1. Command line flags (highest priority)
2. Environment variables (PGFORK_* prefix)
3. Configuration file (~/.postgres-db-fork.yaml)
4. Default values (lowest priority)

Examples:
  # List all current configuration
  postgres-db-fork config list

  # Get a specific value
  postgres-db-fork config get source.host

  # Set a value in config file
  postgres-db-fork config set source.host localhost

  # Show configuration file location
  postgres-db-fork config path`,
}

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configuration settings",
	Long: `List all current configuration settings from all sources.

Shows the effective configuration that would be used, with values from
flags, environment variables, config file, and defaults merged.`,
	RunE: runConfigList,
}

var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Get a configuration value",
	Long: `Get the value of a specific configuration key.

Key names use dot notation (e.g., source.host, destination.port).`,
	Args: cobra.ExactArgs(1),
	RunE: runConfigGet,
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a configuration value",
	Long: `Set a configuration value in the user's config file.

Creates the config file if it doesn't exist. Key names use dot notation.`,
	Args: cobra.ExactArgs(2),
	RunE: runConfigSet,
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Show configuration file path",
	Long:  `Show the path to the configuration file (whether it exists or not).`,
	RunE:  runConfigPath,
}

func init() {
	rootCmd.AddCommand(configCmd)

	// Add subcommands
	configCmd.AddCommand(configListCmd)
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configPathCmd)

	// Flags
	configListCmd.Flags().Bool("show-source", false, "Show source of each value")
}

func runConfigList(cmd *cobra.Command, args []string) error {
	showSource, _ := cmd.Flags().GetBool("show-source")

	settings := viper.AllSettings()
	if len(settings) == 0 {
		fmt.Println("No configuration settings found.")
		return nil
	}

	fmt.Println("Current configuration:")
	fmt.Println()

	// Print settings in a organized way
	printConfigMap(settings, "", showSource)

	if showSource {
		fmt.Println()
		fmt.Println("Configuration sources (in order of precedence):")
		fmt.Println("1. Command line flags")
		fmt.Println("2. Environment variables (PGFORK_* prefix)")
		if viper.ConfigFileUsed() != "" {
			fmt.Printf("3. Config file: %s\n", viper.ConfigFileUsed())
		} else {
			fmt.Println("3. Config file: (not found)")
		}
		fmt.Println("4. Default values")
	}

	return nil
}

func runConfigGet(cmd *cobra.Command, args []string) error {
	key := args[0]
	value := viper.Get(key)

	if value == nil {
		fmt.Printf("Configuration key '%s' not found\n", key)
		return nil
	}

	fmt.Printf("%s = %v\n", key, value)
	return nil
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	key := args[0]
	value := args[1]

	// Get the config file path
	configFile := viper.ConfigFileUsed()
	if configFile == "" {
		// Create default config file
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("cannot determine home directory: %w", err)
		}
		configFile = filepath.Join(home, ".postgres-db-fork.yaml")
	}

	// Set the value
	viper.Set(key, value)

	// Write to config file
	if err := viper.WriteConfigAs(configFile); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	fmt.Printf("Set %s = %s\n", key, value)
	fmt.Printf("Saved to: %s\n", configFile)

	return nil
}

func runConfigPath(cmd *cobra.Command, args []string) error {
	configFile := viper.ConfigFileUsed()

	if configFile != "" {
		fmt.Printf("Configuration file: %s (exists)\n", configFile)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("cannot determine home directory: %w", err)
		}
		defaultPath := filepath.Join(home, ".postgres-db-fork.yaml")
		fmt.Printf("Configuration file: %s (not found)\n", defaultPath)
		fmt.Println("Note: Config file will be created when you use 'config set' command")
	}

	return nil
}

func printConfigMap(m map[string]interface{}, prefix string, showSource bool) {
	for key, value := range m {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}

		switch v := value.(type) {
		case map[string]interface{}:
			// Nested map - print header and recurse
			fmt.Printf("[%s]\n", fullKey)
			printConfigMap(v, fullKey, showSource)
			fmt.Println()
		default:
			// Simple value
			fmt.Printf("  %s = %v", fullKey, v)

			if showSource {
				// Try to determine the source (this is simplified)
				if viper.IsSet(fullKey) {
					// Check if it's an environment variable
					envKey := "PGFORK_" + strings.ToUpper(strings.ReplaceAll(fullKey, ".", "_"))
					if os.Getenv(envKey) != "" {
						fmt.Printf(" (env: %s)", envKey)
					} else if viper.ConfigFileUsed() != "" {
						fmt.Printf(" (config file)")
					} else {
						fmt.Printf(" (default)")
					}
				}
			}

			fmt.Println()
		}
	}
}
