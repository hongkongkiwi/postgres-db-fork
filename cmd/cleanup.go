package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/hongkongkiwi/postgres-db-fork/internal/config"
	"github.com/hongkongkiwi/postgres-db-fork/internal/db"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// CleanupResult represents the result of a cleanup operation
type CleanupResult struct {
	Format           string   `json:"format"`
	Success          bool     `json:"success"`
	Message          string   `json:"message,omitempty"`
	Error            string   `json:"error,omitempty"`
	DeletedCount     int      `json:"deleted_count"`
	DeletedDatabases []string `json:"deleted_databases,omitempty"`
	SkippedCount     int      `json:"skipped_count"`
	SkippedDatabases []string `json:"skipped_databases,omitempty"`
	Duration         string   `json:"duration"`
}

// cleanupCmd represents the cleanup command
var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up databases created by fork operations",
	Long: `Clean up databases created by previous fork operations based on patterns and age.

This command is particularly useful for CI/CD workflows where temporary databases
are created for PR previews and need to be cleaned up when PRs are closed or after
a certain period.

Examples:
  # Delete all databases matching a pattern
  postgres-db-fork cleanup --pattern "myapp_pr_*" --host localhost

  # Delete databases older than 7 days
  postgres-db-fork cleanup --pattern "myapp_pr_*" --older-than 7d

  # Dry run to see what would be deleted
  postgres-db-fork cleanup --pattern "myapp_pr_*" --dry-run

  # Delete specific PR database
  postgres-db-fork cleanup --pattern "myapp_pr_123" --force

  # JSON output for CI/CD integration
  postgres-db-fork cleanup --pattern "myapp_pr_*" --older-than 3d --output-format json`,
	RunE: runCleanup,
}

func init() {
	rootCmd.AddCommand(cleanupCmd)

	// Database connection flags
	cleanupCmd.Flags().String("host", "localhost", "Database server host")
	cleanupCmd.Flags().Int("port", 5432, "Database server port")
	cleanupCmd.Flags().String("user", "", "Database username (required)")
	cleanupCmd.Flags().String("password", "", "Database password")
	cleanupCmd.Flags().String("sslmode", "prefer", "SSL mode")

	// Cleanup criteria
	cleanupCmd.Flags().String("pattern", "", "Database name pattern (supports wildcards like 'myapp_pr_*') (required)")
	cleanupCmd.Flags().Duration("older-than", 0, "Delete databases older than this duration (e.g., 7d, 24h)")
	cleanupCmd.Flags().StringSlice("exclude", []string{}, "Database names to exclude from deletion")
	cleanupCmd.Flags().Bool("force", false, "Force deletion without age requirement")

	// Output options
	cleanupCmd.Flags().String("output-format", "text", "Output format: text or json")
	cleanupCmd.Flags().Bool("quiet", false, "Suppress output except errors")
	cleanupCmd.Flags().Bool("dry-run", false, "Show what would be deleted without actually deleting")

	// Mark required flags
	if err := cleanupCmd.MarkFlagRequired("pattern"); err != nil {
		panic(fmt.Sprintf("Failed to mark flag as required: %v", err))
	}
	if err := cleanupCmd.MarkFlagRequired("user"); err != nil {
		panic(fmt.Sprintf("Failed to mark flag as required: %v", err))
	}

	// Bind to viper
	if err := viper.BindPFlag("cleanup.host", cleanupCmd.Flags().Lookup("host")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
	if err := viper.BindPFlag("cleanup.port", cleanupCmd.Flags().Lookup("port")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
	if err := viper.BindPFlag("cleanup.user", cleanupCmd.Flags().Lookup("user")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
	if err := viper.BindPFlag("cleanup.password", cleanupCmd.Flags().Lookup("password")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
	if err := viper.BindPFlag("cleanup.sslmode", cleanupCmd.Flags().Lookup("sslmode")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
	if err := viper.BindPFlag("cleanup.pattern", cleanupCmd.Flags().Lookup("pattern")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
	if err := viper.BindPFlag("cleanup.older_than", cleanupCmd.Flags().Lookup("older-than")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
	if err := viper.BindPFlag("cleanup.exclude", cleanupCmd.Flags().Lookup("exclude")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
	if err := viper.BindPFlag("cleanup.force", cleanupCmd.Flags().Lookup("force")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
	if err := viper.BindPFlag("cleanup.output_format", cleanupCmd.Flags().Lookup("output-format")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
	if err := viper.BindPFlag("cleanup.quiet", cleanupCmd.Flags().Lookup("quiet")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
	if err := viper.BindPFlag("cleanup.dry_run", cleanupCmd.Flags().Lookup("dry-run")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
}

func runCleanup(cmd *cobra.Command, args []string) error {
	start := time.Now()

	// Load configuration from environment variables
	loadCleanupFromEnvironment()

	// Build database configuration
	dbConfig := &config.DatabaseConfig{
		Host:     viper.GetString("cleanup.host"),
		Port:     viper.GetInt("cleanup.port"),
		Username: viper.GetString("cleanup.user"),
		Password: viper.GetString("cleanup.password"),
		Database: "postgres", // Connect to postgres database for admin operations
		SSLMode:  viper.GetString("cleanup.sslmode"),
	}

	// Get cleanup parameters
	pattern := viper.GetString("cleanup.pattern")
	olderThan := viper.GetDuration("cleanup.older_than")
	exclude := viper.GetStringSlice("cleanup.exclude")
	force := viper.GetBool("cleanup.force")
	outputFormat := viper.GetString("cleanup.output_format")
	quiet := viper.GetBool("cleanup.quiet")
	dryRun := viper.GetBool("cleanup.dry_run")

	// Validate parameters
	if !force && olderThan == 0 {
		return outputCleanupResult(&CleanupResult{
			Format:  outputFormat,
			Success: false,
			Error:   "Must specify either --older-than or --force",
		}, quiet)
	}

	// Connect to database
	conn, err := db.NewConnection(dbConfig)
	if err != nil {
		return outputCleanupResult(&CleanupResult{
			Format:  outputFormat,
			Success: false,
			Error:   fmt.Sprintf("Failed to connect to database: %v", err),
		}, quiet)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			fmt.Printf("Warning: Failed to close connection: %v\n", err)
		}
	}()

	// Find matching databases
	databases, err := findMatchingDatabases(conn, pattern, exclude)
	if err != nil {
		return outputCleanupResult(&CleanupResult{
			Format:  outputFormat,
			Success: false,
			Error:   fmt.Sprintf("Failed to find databases: %v", err),
		}, quiet)
	}

	if len(databases) == 0 {
		return outputCleanupResult(&CleanupResult{
			Format:   outputFormat,
			Success:  true,
			Message:  fmt.Sprintf("No databases found matching pattern '%s'", pattern),
			Duration: time.Since(start).String(),
		}, quiet)
	}

	// Filter by age if specified
	var toDelete []string
	var skipped []string

	for _, dbName := range databases {
		if !force && olderThan > 0 {
			age, err := getDatabaseAge(conn, dbName)
			if err != nil {
				if !quiet {
					fmt.Printf("Warning: Could not determine age of database %s: %v\n", dbName, err)
				}
				skipped = append(skipped, dbName)
				continue
			}

			if age < olderThan {
				skipped = append(skipped, dbName)
				continue
			}
		}

		toDelete = append(toDelete, dbName)
	}

	result := &CleanupResult{
		Format:           outputFormat,
		Success:          true,
		DeletedCount:     len(toDelete),
		DeletedDatabases: toDelete,
		SkippedCount:     len(skipped),
		SkippedDatabases: skipped,
		Duration:         time.Since(start).String(),
	}

	if dryRun {
		result.Message = fmt.Sprintf("DRY RUN: Would delete %d databases", len(toDelete))
		return outputCleanupResult(result, quiet)
	}

	// Delete databases
	var deleted []string
	var failed []string

	for _, dbName := range toDelete {
		if err := conn.DropDatabase(dbName); err != nil {
			if !quiet {
				fmt.Printf("Failed to delete database %s: %v\n", dbName, err)
			}
			failed = append(failed, dbName)
		} else {
			deleted = append(deleted, dbName)
			if !quiet && outputFormat != "json" {
				fmt.Printf("Deleted database: %s\n", dbName)
			}
		}
	}

	result.DeletedCount = len(deleted)
	result.DeletedDatabases = deleted

	if len(failed) > 0 {
		result.Success = false
		result.Error = fmt.Sprintf("Failed to delete %d databases: %v", len(failed), failed)
	} else {
		result.Message = fmt.Sprintf("Successfully deleted %d databases", len(deleted))
	}

	return outputCleanupResult(result, quiet)
}

// loadCleanupFromEnvironment loads cleanup configuration from environment variables
func loadCleanupFromEnvironment() {
	if host := os.Getenv("PGFORK_CLEANUP_HOST"); host != "" {
		viper.Set("cleanup.host", host)
	}
	if port := os.Getenv("PGFORK_CLEANUP_PORT"); port != "" {
		viper.Set("cleanup.port", port)
	}
	if user := os.Getenv("PGFORK_CLEANUP_USER"); user != "" {
		viper.Set("cleanup.user", user)
	}
	if password := os.Getenv("PGFORK_CLEANUP_PASSWORD"); password != "" {
		viper.Set("cleanup.password", password)
	}
	if sslmode := os.Getenv("PGFORK_CLEANUP_SSLMODE"); sslmode != "" {
		viper.Set("cleanup.sslmode", sslmode)
	}
	if pattern := os.Getenv("PGFORK_CLEANUP_PATTERN"); pattern != "" {
		viper.Set("cleanup.pattern", pattern)
	}
	if olderThan := os.Getenv("PGFORK_CLEANUP_OLDER_THAN"); olderThan != "" {
		if duration, err := time.ParseDuration(olderThan); err == nil {
			viper.Set("cleanup.older_than", duration)
		}
	}
}

// findMatchingDatabases finds databases matching the given pattern
func findMatchingDatabases(conn *db.Connection, pattern string, exclude []string) ([]string, error) {
	// Convert wildcard pattern to regex
	regexPattern := strings.ReplaceAll(pattern, "*", ".*")
	regexPattern = strings.ReplaceAll(regexPattern, "?", ".")
	regex, err := regexp.Compile("^" + regexPattern + "$")
	if err != nil {
		return nil, fmt.Errorf("invalid pattern: %w", err)
	}

	// Create exclude map for faster lookup
	excludeMap := make(map[string]bool)
	for _, name := range exclude {
		excludeMap[name] = true
	}

	// Query all databases
	query := `
		SELECT datname
		FROM pg_database
		WHERE datistemplate = false
		  AND datname NOT IN ('postgres', 'template0', 'template1')
		ORDER BY datname`

	rows, err := conn.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			fmt.Printf("Warning: Failed to close rows: %v\n", err)
		}
	}()

	var matches []string
	for rows.Next() {
		var dbName string
		if err := rows.Scan(&dbName); err != nil {
			return nil, err
		}

		// Check if name matches pattern and is not excluded
		if regex.MatchString(dbName) && !excludeMap[dbName] {
			matches = append(matches, dbName)
		}
	}

	return matches, rows.Err()
}

// getDatabaseAge returns the age of a database
func getDatabaseAge(conn *db.Connection, dbName string) (time.Duration, error) {
	query := `
		SELECT EXTRACT(EPOCH FROM (NOW() - pg_stat_file('base/'||oid||'/PG_VERSION').modification))::int
		FROM pg_database
		WHERE datname = $1`

	var ageSeconds int64
	err := conn.DB.QueryRow(query, dbName).Scan(&ageSeconds)
	if err != nil {
		// Fallback: try to get creation time from pg_stat_database
		fallbackQuery := `
			SELECT EXTRACT(EPOCH FROM (NOW() - stats_reset))::int
			FROM pg_stat_database
			WHERE datname = $1 AND stats_reset IS NOT NULL`

		err = conn.DB.QueryRow(fallbackQuery, dbName).Scan(&ageSeconds)
		if err != nil {
			return 0, fmt.Errorf("could not determine database age: %w", err)
		}
	}

	return time.Duration(ageSeconds) * time.Second, nil
}

// outputCleanupResult outputs the cleanup result in the specified format
func outputCleanupResult(result *CleanupResult, quiet bool) error {
	if result.Format == "json" {
		jsonOutput, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON output: %w", err)
		}
		fmt.Println(string(jsonOutput))
	} else {
		// Text output
		if !quiet {
			if result.Success {
				if result.Message != "" {
					fmt.Printf("✅ %s\n", result.Message)
				}
				if result.DeletedCount > 0 {
					fmt.Printf("Deleted databases (%d):\n", result.DeletedCount)
					for _, db := range result.DeletedDatabases {
						fmt.Printf("  - %s\n", db)
					}
				}
				if result.SkippedCount > 0 {
					fmt.Printf("Skipped databases (%d):\n", result.SkippedCount)
					for _, db := range result.SkippedDatabases {
						fmt.Printf("  - %s\n", db)
					}
				}
				fmt.Printf("Duration: %s\n", result.Duration)
			} else {
				fmt.Printf("❌ %s\n", result.Error)
			}
		} else {
			// Quiet mode - only essential output
			if result.Success {
				fmt.Printf("%d\n", result.DeletedCount)
			} else {
				fmt.Fprintf(os.Stderr, "Error: %s\n", result.Error)
			}
		}
	}

	// Set exit code
	if !result.Success {
		os.Exit(1)
	}

	return nil
}
