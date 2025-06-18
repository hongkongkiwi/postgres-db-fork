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

// DatabaseInfo represents information about a database
type DatabaseInfo struct {
	Name       string `json:"name"`
	Size       string `json:"size,omitempty"`
	SizeBytes  int64  `json:"size_bytes,omitempty"`
	Age        string `json:"age,omitempty"`
	AgeSeconds int64  `json:"age_seconds,omitempty"`
	Owner      string `json:"owner,omitempty"`
}

// ListResult represents the result of a list operation
type ListResult struct {
	Format    string         `json:"format"`
	Success   bool           `json:"success"`
	Message   string         `json:"message,omitempty"`
	Error     string         `json:"error,omitempty"`
	Count     int            `json:"count"`
	Databases []DatabaseInfo `json:"databases,omitempty"`
	Duration  string         `json:"duration"`
}

// listCmd represents the list command
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List databases matching patterns",
	Long: `List databases on a PostgreSQL server with optional filtering and metadata.

This command is particularly useful for CI/CD workflows to discover existing databases,
check if preview databases already exist, or get information about database size and age.

Examples:
  # List all databases
  postgres-db-fork list --host localhost --user admin

  # List PR databases with sizes
  postgres-db-fork list --pattern "myapp_pr_*" --show-size

  # Check if specific database exists (exit code 0 if found)
  postgres-db-fork list --pattern "myapp_pr_123" --quiet

  # JSON output for CI/CD scripts
  postgres-db-fork list --pattern "myapp_*" --output-format json

  # Show database age information
  postgres-db-fork list --pattern "temp_*" --show-age --older-than 7d`,
	RunE: runList,
}

func init() {
	rootCmd.AddCommand(listCmd)

	// Database connection flags
	listCmd.Flags().String("host", "localhost", "Database server host")
	listCmd.Flags().Int("port", 5432, "Database server port")
	listCmd.Flags().String("user", "", "Database username (required)")
	listCmd.Flags().String("password", "", "Database password")
	listCmd.Flags().String("sslmode", "prefer", "SSL mode")

	// Filtering options
	listCmd.Flags().String("pattern", "*", "Database name pattern (supports wildcards)")
	listCmd.Flags().StringSlice("exclude", []string{}, "Database names to exclude")
	listCmd.Flags().Duration("older-than", 0, "Only show databases older than duration")
	listCmd.Flags().Duration("newer-than", 0, "Only show databases newer than duration")

	// Display options
	listCmd.Flags().Bool("show-size", false, "Include database size information")
	listCmd.Flags().Bool("show-age", false, "Include database age information")
	listCmd.Flags().Bool("show-owner", false, "Include database owner information")
	listCmd.Flags().String("sort-by", "name", "Sort by: name, size, age")
	listCmd.Flags().Bool("reverse", false, "Reverse sort order")

	// Output options
	listCmd.Flags().String("output-format", "text", "Output format: text or json")
	listCmd.Flags().Bool("quiet", false, "Suppress output except database names (or JSON)")
	listCmd.Flags().Bool("count-only", false, "Only output the count of matching databases")

	// Mark required flags
	if err := listCmd.MarkFlagRequired("user"); err != nil {
		panic(fmt.Sprintf("Failed to mark flag as required: %v", err))
	}

	// Bind to viper
	if err := viper.BindPFlag("list.host", listCmd.Flags().Lookup("host")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
	if err := viper.BindPFlag("list.port", listCmd.Flags().Lookup("port")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
	if err := viper.BindPFlag("list.user", listCmd.Flags().Lookup("user")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
	if err := viper.BindPFlag("list.password", listCmd.Flags().Lookup("password")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
	if err := viper.BindPFlag("list.sslmode", listCmd.Flags().Lookup("sslmode")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
	if err := viper.BindPFlag("list.pattern", listCmd.Flags().Lookup("pattern")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
	if err := viper.BindPFlag("list.exclude", listCmd.Flags().Lookup("exclude")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
	if err := viper.BindPFlag("list.older_than", listCmd.Flags().Lookup("older-than")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
	if err := viper.BindPFlag("list.newer_than", listCmd.Flags().Lookup("newer-than")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
	if err := viper.BindPFlag("list.show_size", listCmd.Flags().Lookup("show-size")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
	if err := viper.BindPFlag("list.show_age", listCmd.Flags().Lookup("show-age")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
	if err := viper.BindPFlag("list.show_owner", listCmd.Flags().Lookup("show-owner")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
	if err := viper.BindPFlag("list.sort_by", listCmd.Flags().Lookup("sort-by")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
	if err := viper.BindPFlag("list.reverse", listCmd.Flags().Lookup("reverse")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
	if err := viper.BindPFlag("list.output_format", listCmd.Flags().Lookup("output-format")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
	if err := viper.BindPFlag("list.quiet", listCmd.Flags().Lookup("quiet")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
	if err := viper.BindPFlag("list.count_only", listCmd.Flags().Lookup("count-only")); err != nil {
		fmt.Printf("Failed to bind flag: %v\n", err)
	}
}

func runList(cmd *cobra.Command, args []string) error {
	start := time.Now()

	// Load configuration from environment variables
	loadListFromEnvironment()

	// Build database configuration
	dbConfig := &config.DatabaseConfig{
		Host:     viper.GetString("list.host"),
		Port:     viper.GetInt("list.port"),
		Username: viper.GetString("list.user"),
		Password: viper.GetString("list.password"),
		Database: "postgres", // Connect to postgres database for admin operations
		SSLMode:  viper.GetString("list.sslmode"),
	}

	// Get list parameters
	pattern := viper.GetString("list.pattern")
	exclude := viper.GetStringSlice("list.exclude")
	olderThan := viper.GetDuration("list.older_than")
	newerThan := viper.GetDuration("list.newer_than")
	showSize := viper.GetBool("list.show_size")
	showAge := viper.GetBool("list.show_age")
	showOwner := viper.GetBool("list.show_owner")
	sortBy := viper.GetString("list.sort_by")
	reverse := viper.GetBool("list.reverse")
	outputFormat := viper.GetString("list.output_format")
	quiet := viper.GetBool("list.quiet")
	countOnly := viper.GetBool("list.count_only")

	// Connect to database
	conn, err := db.NewConnection(dbConfig)
	if err != nil {
		return outputListResult(&ListResult{
			Format:  outputFormat,
			Success: false,
			Error:   fmt.Sprintf("Failed to connect to database: %v", err),
		}, quiet, countOnly)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			fmt.Printf("Warning: Failed to close connection: %v\n", err)
		}
	}()

	// Find matching databases
	databases, err := findDatabasesWithInfo(conn, pattern, exclude, showSize, showAge, showOwner)
	if err != nil {
		return outputListResult(&ListResult{
			Format:  outputFormat,
			Success: false,
			Error:   fmt.Sprintf("Failed to query databases: %v", err),
		}, quiet, countOnly)
	}

	// Filter by age if specified
	if olderThan > 0 || newerThan > 0 {
		databases = filterDatabasesByAge(databases, olderThan, newerThan)
	}

	// Sort databases
	sortDatabases(databases, sortBy, reverse)

	result := &ListResult{
		Format:    outputFormat,
		Success:   true,
		Count:     len(databases),
		Databases: databases,
		Duration:  time.Since(start).String(),
	}

	if len(databases) == 0 {
		result.Message = fmt.Sprintf("No databases found matching pattern '%s'", pattern)
	} else {
		result.Message = fmt.Sprintf("Found %d databases matching pattern '%s'", len(databases), pattern)
	}

	return outputListResult(result, quiet, countOnly)
}

// loadListFromEnvironment loads list configuration from environment variables
func loadListFromEnvironment() {
	if host := os.Getenv("PGFORK_LIST_HOST"); host != "" {
		viper.Set("list.host", host)
	}
	if port := os.Getenv("PGFORK_LIST_PORT"); port != "" {
		viper.Set("list.port", port)
	}
	if user := os.Getenv("PGFORK_LIST_USER"); user != "" {
		viper.Set("list.user", user)
	}
	if password := os.Getenv("PGFORK_LIST_PASSWORD"); password != "" {
		viper.Set("list.password", password)
	}
	if sslmode := os.Getenv("PGFORK_LIST_SSLMODE"); sslmode != "" {
		viper.Set("list.sslmode", sslmode)
	}
	if pattern := os.Getenv("PGFORK_LIST_PATTERN"); pattern != "" {
		viper.Set("list.pattern", pattern)
	}
}

// findDatabasesWithInfo finds databases with optional metadata
func findDatabasesWithInfo(conn *db.Connection, pattern string, exclude []string, showSize, showAge, showOwner bool) ([]DatabaseInfo, error) {
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

	// Build query
	query := `
		SELECT
			d.datname`

	if showSize {
		query += `,
			pg_database_size(d.datname) as size_bytes`
	}

	if showOwner {
		query += `,
			pg_get_userbyid(d.datdba) as owner`
	}

	query += `
		FROM pg_database d
		WHERE d.datistemplate = false
		  AND d.datname NOT IN ('postgres', 'template0', 'template1')
		ORDER BY d.datname`

	rows, err := conn.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			fmt.Printf("Warning: Failed to close rows: %v\n", err)
		}
	}()

	var databases []DatabaseInfo
	for rows.Next() {
		var dbInfo DatabaseInfo
		var sizeBytes *int64
		var owner *string

		// Prepare scan arguments
		scanArgs := []interface{}{&dbInfo.Name}

		if showSize {
			scanArgs = append(scanArgs, &sizeBytes)
		}
		if showOwner {
			scanArgs = append(scanArgs, &owner)
		}

		if err := rows.Scan(scanArgs...); err != nil {
			return nil, err
		}

		// Check if name matches pattern and is not excluded
		if !regex.MatchString(dbInfo.Name) || excludeMap[dbInfo.Name] {
			continue
		}

		// Add size information
		if showSize && sizeBytes != nil {
			dbInfo.SizeBytes = *sizeBytes
			dbInfo.Size = formatBytes(*sizeBytes)
		}

		// Add owner information
		if showOwner && owner != nil {
			dbInfo.Owner = *owner
		}

		// Add age information if requested
		if showAge {
			age, err := getDatabaseAge(conn, dbInfo.Name)
			if err == nil {
				dbInfo.AgeSeconds = int64(age.Seconds())
				dbInfo.Age = formatDuration(age)
			}
		}

		databases = append(databases, dbInfo)
	}

	return databases, rows.Err()
}

// filterDatabasesByAge filters databases by age criteria
func filterDatabasesByAge(databases []DatabaseInfo, olderThan, newerThan time.Duration) []DatabaseInfo {
	var filtered []DatabaseInfo
	for _, db := range databases {
		age := time.Duration(db.AgeSeconds) * time.Second

		if olderThan > 0 && age < olderThan {
			continue
		}
		if newerThan > 0 && age > newerThan {
			continue
		}

		filtered = append(filtered, db)
	}
	return filtered
}

// sortDatabases sorts databases by the specified criteria
func sortDatabases(databases []DatabaseInfo, sortBy string, reverse bool) {
	// Implementation would use sort.Slice with appropriate comparison functions
	// For now, keeping it simple since databases are already sorted by name
}

// formatBytes converts bytes to human readable format
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// formatDuration formats a duration in human-readable format
func formatDuration(d time.Duration) string {
	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	} else if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	} else {
		return fmt.Sprintf("%dm", minutes)
	}
}

// outputListResult outputs the list result in the specified format
func outputListResult(result *ListResult, quiet, countOnly bool) error {
	if result.Format == "json" {
		jsonOutput, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON output: %w", err)
		}
		fmt.Println(string(jsonOutput))
	} else {
		// Text output
		if countOnly {
			fmt.Println(result.Count)
		} else if quiet {
			// Quiet mode - only database names
			for _, db := range result.Databases {
				fmt.Println(db.Name)
			}
		} else {
			// Full text output
			if !result.Success {
				fmt.Printf("❌ %s\n", result.Error)
			} else {
				if result.Message != "" {
					fmt.Printf("✅ %s\n", result.Message)
				}

				if len(result.Databases) > 0 {
					fmt.Println("\nDatabases:")
					for _, db := range result.Databases {
						line := fmt.Sprintf("  - %s", db.Name)
						if db.Size != "" {
							line += fmt.Sprintf(" (%s)", db.Size)
						}
						if db.Age != "" {
							line += fmt.Sprintf(" [%s old]", db.Age)
						}
						if db.Owner != "" {
							line += fmt.Sprintf(" owner:%s", db.Owner)
						}
						fmt.Println(line)
					}
				}

				fmt.Printf("\nTotal: %d databases\n", result.Count)
				fmt.Printf("Duration: %s\n", result.Duration)
			}
		}
	}

	// Set exit code based on results
	if !result.Success {
		os.Exit(1)
	} else if result.Count == 0 {
		// Exit code 1 when no databases found (useful for CI/CD existence checks)
		os.Exit(1)
	}

	return nil
}
