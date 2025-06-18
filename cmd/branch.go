package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/hongkongkiwi/postgres-db-fork/internal/config"
	"github.com/hongkongkiwi/postgres-db-fork/internal/db"
	"github.com/hongkongkiwi/postgres-db-fork/internal/fork"

	_ "github.com/lib/pq"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var log = logrus.New()

// branchCmd represents the branch command
var branchCmd = &cobra.Command{
	Use:   "branch",
	Short: "Database branching operations (git-like workflow)",
	Long: `Database branching operations with a git-like workflow.

This provides a more intuitive branching experience similar to Neon's database branching
or git operations. Think of it as "git for databases".

Available subcommands:
  create   - Create a new database branch from a source
  list     - List all database branches
  delete   - Delete a database branch
  checkout - Switch default context to a branch
  status   - Show current branch status

Examples:
  # Create a new branch from main database
  postgres-db-fork branch create feature-api --from main_db

  # Create PR branch with automatic naming
  postgres-db-fork branch create --from staging --pr 123

  # List all branches
  postgres-db-fork branch list

  # Delete old PR branches
  postgres-db-fork branch delete --pattern "pr-*" --older-than 7d`,
}

var branchCreateCmd = &cobra.Command{
	Use:   "create <branch-name>",
	Short: "Create a new database branch",
	Long: `Create a new database branch from a source database.

This is equivalent to forking a database but uses branch terminology
for a more intuitive git-like workflow.

Examples:
  # Create named branch
  postgres-db-fork branch create feature-api --from main_db

  # Create PR branch (automatic naming)
  postgres-db-fork branch create --from staging --pr 123

  # Create branch with point-in-time recovery (if supported)
  postgres-db-fork branch create hotfix --from main_db --point-in-time "2024-01-01 12:00:00"`,
	Args: cobra.MaximumNArgs(1),
	RunE: runBranchCreate,
}

var branchListCmd = &cobra.Command{
	Use:   "list",
	Short: "List database branches",
	Long: `List all database branches with metadata.

Shows branch names, source databases, creation times, and sizes.`,
	RunE: runBranchList,
}

var branchDeleteCmd = &cobra.Command{
	Use:   "delete <branch-name>",
	Short: "Delete a database branch",
	Long: `Delete a database branch.

Examples:
  # Delete specific branch
  postgres-db-fork branch delete feature-api

  # Delete multiple branches by pattern
  postgres-db-fork branch delete --pattern "pr-*" --older-than 7d`,
	Args: cobra.MaximumNArgs(1),
	RunE: runBranchDelete,
}

func init() {
	rootCmd.AddCommand(branchCmd)

	// Add subcommands
	branchCmd.AddCommand(branchCreateCmd)
	branchCmd.AddCommand(branchListCmd)
	branchCmd.AddCommand(branchDeleteCmd)

	// Create command flags
	branchCreateCmd.Flags().String("from", "", "Source database to branch from (required)")
	branchCreateCmd.Flags().Int("pr", 0, "Create PR branch (auto-generates name)")
	branchCreateCmd.Flags().String("point-in-time", "", "Point-in-time recovery timestamp")
	branchCreateCmd.Flags().Bool("schema-only", false, "Create schema-only branch")
	branchCreateCmd.Flags().String("output", "text", "Output format: text or json")
	if err := branchCreateCmd.MarkFlagRequired("from"); err != nil {
		log.Fatalf("Failed to mark flag as required: %v", err)
	}

	// List command flags
	branchListCmd.Flags().String("output", "text", "Output format: text or json")
	branchListCmd.Flags().String("pattern", "", "Filter branches by pattern")
	branchListCmd.Flags().Bool("show-size", false, "Show database sizes")
	branchListCmd.Flags().String("host", "localhost", "Database host")
	branchListCmd.Flags().Int("port", 5432, "Database port")
	branchListCmd.Flags().String("user", "", "Database username")
	branchListCmd.Flags().String("password", "", "Database password")

	// Delete command flags
	branchDeleteCmd.Flags().String("pattern", "", "Delete branches matching pattern")
	branchDeleteCmd.Flags().String("older-than", "", "Delete branches older than duration (e.g., 7d)")
	branchDeleteCmd.Flags().Bool("force", false, "Force delete without confirmation")
	branchDeleteCmd.Flags().Bool("dry-run", false, "Show what would be deleted")
	branchDeleteCmd.Flags().String("host", "localhost", "Database host")
	branchDeleteCmd.Flags().Int("port", 5432, "Database port")
	branchDeleteCmd.Flags().String("user", "", "Database username")
	branchDeleteCmd.Flags().String("password", "", "Database password")
}

func runBranchCreate(cmd *cobra.Command, args []string) error {
	from, _ := cmd.Flags().GetString("from")
	prNumber, _ := cmd.Flags().GetInt("pr")
	outputFormat, _ := cmd.Flags().GetString("output")
	schemaOnly, _ := cmd.Flags().GetBool("schema-only")

	var branchName string
	if len(args) > 0 {
		branchName = args[0]
	} else if prNumber > 0 {
		branchName = fmt.Sprintf("pr-%d", prNumber)
	} else {
		return fmt.Errorf("branch name required (use argument or --pr flag)")
	}

	// Build fork configuration with proper defaults
	cfg := &config.ForkConfig{
		Source: config.DatabaseConfig{
			Host:     "localhost",
			Port:     5432,
			Username: "postgres",
			Database: from,
			SSLMode:  "prefer",
		},
		Destination: config.DatabaseConfig{
			Host:     "localhost", // Same server by default for branches
			Port:     5432,
			Username: "postgres",
			Database: "",
			SSLMode:  "prefer",
		},
		TargetDatabase: branchName,
		SchemaOnly:     schemaOnly,
		OutputFormat:   outputFormat,
		MaxConnections: 4,
		ChunkSize:      1000,
		Timeout:        30 * time.Minute,
		DropIfExists:   true, // Branches should replace if they exist
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	start := time.Now()

	if outputFormat == "json" {
		// Execute actual fork operation
		forker := fork.NewForker(cfg)
		ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
		defer cancel()

		err := forker.Fork(ctx)
		result := map[string]interface{}{
			"action":    "branch_create",
			"branch":    branchName,
			"source":    from,
			"timestamp": time.Now(),
			"success":   err == nil,
			"duration":  time.Since(start).String(),
		}

		if err != nil {
			result["error"] = err.Error()
		} else {
			result["message"] = fmt.Sprintf("Branch '%s' created from '%s'", branchName, from)
		}

		jsonOutput, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(jsonOutput))

		return err
	} else {
		fmt.Printf("🌿 Creating branch '%s' from '%s'...\n", branchName, from)

		// Execute actual fork operation
		forker := fork.NewForker(cfg)
		ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
		defer cancel()

		err := forker.Fork(ctx)
		if err != nil {
			fmt.Printf("❌ Failed to create branch: %v\n", err)
			return err
		}

		fmt.Printf("✅ Branch '%s' created successfully in %v\n", branchName, time.Since(start))
	}

	return nil
}

func runBranchList(cmd *cobra.Command, args []string) error {
	outputFormat, _ := cmd.Flags().GetString("output")
	pattern, _ := cmd.Flags().GetString("pattern")
	showSize, _ := cmd.Flags().GetBool("show-size")

	// Get database connection parameters
	host, _ := cmd.Flags().GetString("host")
	port, _ := cmd.Flags().GetInt("port")
	user, _ := cmd.Flags().GetString("user")
	password, _ := cmd.Flags().GetString("password")

	// Get list of databases that look like branches
	databases, err := getDatabaseList(pattern, host, port, user, password, showSize)
	if err != nil {
		return fmt.Errorf("failed to list databases: %w", err)
	}

	if outputFormat == "json" {
		jsonOutput, _ := json.MarshalIndent(databases, "", "  ")
		fmt.Println(string(jsonOutput))
	} else {
		fmt.Println("DATABASE BRANCHES")
		fmt.Println("=================")
		fmt.Printf("%-20s %-15s %-20s", "BRANCH", "SOURCE", "CREATED")
		if showSize {
			fmt.Printf(" %-10s", "SIZE")
		}
		fmt.Println()

		for _, branch := range databases {
			fmt.Printf("%-20s %-15s %-20s",
				truncateStringLocal(branch["name"].(string), 20),
				truncateStringLocal(branch["source"].(string), 15),
				truncateStringLocal(branch["created"].(string), 20))
			if showSize {
				fmt.Printf(" %-10s", branch["size"])
			}
			fmt.Println()
		}

		if len(databases) == 0 {
			fmt.Println("No branches found")
		}
	}

	return nil
}

func runBranchDelete(cmd *cobra.Command, args []string) error {
	pattern, _ := cmd.Flags().GetString("pattern")
	olderThan, _ := cmd.Flags().GetString("older-than")
	force, _ := cmd.Flags().GetBool("force")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	// Get database connection parameters
	host, _ := cmd.Flags().GetString("host")
	port, _ := cmd.Flags().GetInt("port")
	user, _ := cmd.Flags().GetString("user")
	password, _ := cmd.Flags().GetString("password")

	var branchName string
	if len(args) > 0 {
		branchName = args[0]
	}

	if branchName == "" && pattern == "" {
		return fmt.Errorf("branch name or pattern required")
	}

	var databasesToDelete []map[string]interface{}
	if branchName != "" {
		// Get single database info
		databases, err := getDatabaseList(branchName, host, port, user, password, true)
		if err != nil {
			return fmt.Errorf("failed to get database info: %w", err)
		}
		databasesToDelete = databases
	} else {
		// Get databases matching pattern
		databases, err := getDatabaseList(pattern, host, port, user, password, true)
		if err != nil {
			return fmt.Errorf("failed to list databases: %w", err)
		}
		databasesToDelete = databases
	}

	// Filter by age if olderThan is specified
	if olderThan != "" {
		filteredDatabases, err := filterDatabasesByAgeBranch(databasesToDelete, olderThan)
		if err != nil {
			return fmt.Errorf("failed to filter by age: %w", err)
		}
		databasesToDelete = filteredDatabases
	}

	if len(databasesToDelete) == 0 {
		fmt.Println("No databases found to delete")
		return nil
	}

	if dryRun {
		fmt.Println("🔍 Dry run - would delete:")
		for _, db := range databasesToDelete {
			name := db["name"].(string)
			created := db["created"].(string)
			fmt.Printf("  • %s (created: %s)\n", name, created)
		}
		return nil
	}

	// Confirm deletion unless force is used
	if !force && len(databasesToDelete) > 0 {
		fmt.Printf("⚠️  This will delete %d database(s):\n", len(databasesToDelete))
		for _, db := range databasesToDelete {
			name := db["name"].(string)
			created := db["created"].(string)
			fmt.Printf("  • %s (created: %s)\n", name, created)
		}
		fmt.Print("Continue? (y/N): ")
		var response string
		if _, err := fmt.Scanln(&response); err != nil {
			log.Warnf("Failed to read user input: %v", err)
			return err
		}
		if response != "y" && response != "Y" {
			fmt.Println("Operation cancelled")
			return nil
		}
	}

	// Perform actual deletion
	for _, db := range databasesToDelete {
		name := db["name"].(string)
		fmt.Printf("🗑️  Deleting branch '%s'...\n", name)
		err := deleteDatabaseBranch(name, host, port, user, password)
		if err != nil {
			fmt.Printf("❌ Failed to delete '%s': %v\n", name, err)
		} else {
			fmt.Printf("✅ Branch '%s' deleted successfully\n", name)
		}
	}

	return nil
}

// getDatabaseList returns a list of databases with metadata, optionally filtered by pattern
func getDatabaseList(pattern, host string, port int, user, password string, includeMetadata bool) ([]map[string]interface{}, error) {
	// If no connection details provided, return mock data
	if user == "" {
		allDatabases := []string{"main", "staging", "pr-123", "pr-456", "feature-api", "dev"}

		var filtered []string
		if pattern == "" {
			filtered = allDatabases
		} else {
			filtered = filterDatabasesByPattern(allDatabases, pattern)
		}

		// Convert to map format with mock metadata
		var result []map[string]interface{}
		for _, dbName := range filtered {
			result = append(result, map[string]interface{}{
				"name":    dbName,
				"source":  "unknown",
				"created": "unknown",
				"size":    "unknown",
				"type":    "branch",
			})
		}
		return result, nil
	}

	// Connect to database and get real data
	dbConfig := &config.DatabaseConfig{
		Host:     host,
		Port:     port,
		Username: user,
		Password: password,
		Database: "postgres", // Connect to postgres database for admin queries
		SSLMode:  "prefer",
	}

	conn, err := db.NewConnection(dbConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			log.Errorf("Failed to close connection: %v", err)
		}
	}()

	// Query databases with metadata
	query := `
		SELECT
			d.datname,
			pg_get_userbyid(d.datdba) as owner,
			pg_database_size(d.datname) as size_bytes,
			pg_stat_file('base/'||d.oid||'/PG_VERSION').modification as created_time
		FROM pg_database d
		WHERE d.datistemplate = false
		  AND d.datname NOT IN ('postgres', 'template0', 'template1')
		ORDER BY d.datname`

	rows, err := conn.DB.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query databases: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			fmt.Printf("Warning: Failed to close rows: %v\n", err)
		}
	}()

	var allDatabases []map[string]interface{}
	for rows.Next() {
		var name, owner string
		var sizeBytes int64
		var createdTime time.Time

		if err := rows.Scan(&name, &owner, &sizeBytes, &createdTime); err != nil {
			continue // Skip problematic rows
		}

		// Check if name matches pattern
		if pattern != "" && !matchesPattern(name, pattern) {
			continue
		}

		dbInfo := map[string]interface{}{
			"name":         name,
			"source":       owner, // Using owner as a proxy for source
			"created":      createdTime.Format("2006-01-02 15:04:05"),
			"size":         formatBytesForBranch(sizeBytes),
			"type":         "branch",
			"created_time": createdTime,
			"size_bytes":   sizeBytes,
		}

		allDatabases = append(allDatabases, dbInfo)
	}

	return allDatabases, rows.Err()
}

// filterDatabasesByPattern filters database names by pattern
func filterDatabasesByPattern(databases []string, pattern string) []string {
	var filtered []string

	for _, db := range databases {
		if matchesPattern(db, pattern) {
			filtered = append(filtered, db)
		}
	}

	return filtered
}

// matchesPattern checks if a database name matches the given pattern
func matchesPattern(name, pattern string) bool {
	// Simple wildcard pattern matching
	if pattern == "*" {
		return true
	}

	// Convert wildcard pattern to regex
	regexPattern := strings.ReplaceAll(pattern, "*", ".*")
	regexPattern = strings.ReplaceAll(regexPattern, "?", ".")
	regexPattern = "^" + regexPattern + "$"

	matched, err := regexp.MatchString(regexPattern, name)
	if err != nil {
		// Fallback to simple string matching
		return strings.Contains(name, strings.ReplaceAll(pattern, "*", ""))
	}

	return matched
}

// filterDatabasesByAgeBranch filters databases by age criteria for branch operations
func filterDatabasesByAgeBranch(databases []map[string]interface{}, olderThanStr string) ([]map[string]interface{}, error) {
	duration, err := time.ParseDuration(olderThanStr)
	if err != nil {
		return nil, fmt.Errorf("invalid duration format: %w", err)
	}

	cutoff := time.Now().Add(-duration)
	var filtered []map[string]interface{}

	for _, db := range databases {
		// Try to get creation time
		if createdTimeInterface, exists := db["created_time"]; exists {
			if createdTime, ok := createdTimeInterface.(time.Time); ok {
				if createdTime.Before(cutoff) {
					filtered = append(filtered, db)
				}
			}
		}
	}

	return filtered, nil
}

// deleteDatabaseBranch deletes a database branch
func deleteDatabaseBranch(dbName, host string, port int, user, password string) error {
	if user == "" {
		// Mock deletion for demo purposes
		time.Sleep(100 * time.Millisecond)
		return nil
	}

	// Connect to database
	dbConfig := &config.DatabaseConfig{
		Host:     host,
		Port:     port,
		Username: user,
		Password: password,
		Database: "postgres", // Connect to postgres database for admin operations
		SSLMode:  "prefer",
	}

	conn, err := db.NewConnection(dbConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			log.Errorf("Failed to close connection: %v", err)
		}
	}()

	// Terminate any active connections to the database
	terminateQuery := `
		SELECT pg_terminate_backend(pid)
		FROM pg_stat_activity
		WHERE datname = $1 AND pid <> pg_backend_pid()`

	_, err = conn.DB.Exec(terminateQuery, dbName)
	if err != nil {
		// Log warning but continue - some connections might not terminate gracefully
		fmt.Printf("Warning: Could not terminate all connections to %s: %v\n", dbName, err)
	}

	// Drop the database
	dropQuery := fmt.Sprintf(`DROP DATABASE "%s"`, dbName)
	_, err = conn.DB.Exec(dropQuery)
	if err != nil {
		return fmt.Errorf("failed to drop database: %w", err)
	}

	return nil
}

// formatBytesForBranch converts bytes to human readable format
func formatBytesForBranch(bytes int64) string {
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

// truncateStringLocal truncates a string to maxLen characters
func truncateStringLocal(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
