package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/hongkongkiwi/postgres-db-fork/internal/config"
	"github.com/hongkongkiwi/postgres-db-fork/internal/db"

	"github.com/spf13/cobra"
)

// ValidationResult represents the result of a validation check
type ValidationResult struct {
	Check   string `json:"check"`
	Status  string `json:"status"` // "pass", "warn", "fail"
	Message string `json:"message,omitempty"`
	Details string `json:"details,omitempty"`
}

// ValidateOutput represents the complete validation output
type ValidateOutput struct {
	Format   string             `json:"format"`
	Success  bool               `json:"success"`
	Message  string             `json:"message,omitempty"`
	Error    string             `json:"error,omitempty"`
	Results  []ValidationResult `json:"results"`
	Duration string             `json:"duration"`
}

// validateCmd represents the validate command
var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate configuration and database connectivity",
	Long: `Validate configuration settings and test database connectivity before running fork operations.

This command performs comprehensive pre-flight checks to prevent failures during actual
fork operations. It's particularly useful in CI/CD workflows to catch configuration
issues early.

Validation checks include:
- Configuration file syntax and completeness
- Database connectivity (source and destination)
- User permissions and database existence
- Template variable resolution
- Resource availability estimates

Examples:
  # Validate configuration from flags
  postgres-db-fork validate --source-host localhost --source-db myapp

  # Validate configuration file
  postgres-db-fork validate --config my-config.yaml

  # Quick connectivity test
  postgres-db-fork validate --source-host prod.example.com --quick

  # JSON output for CI/CD
  postgres-db-fork validate --output-format json`,
	RunE: runValidate,
}

func init() {
	rootCmd.AddCommand(validateCmd)

	// Use same flags as fork command for consistency
	validateCmd.Flags().String("source-host", "localhost", "Source database host")
	validateCmd.Flags().Int("source-port", 5432, "Source database port")
	validateCmd.Flags().String("source-user", "", "Source database username")
	validateCmd.Flags().String("source-password", "", "Source database password")
	validateCmd.Flags().String("source-db", "", "Source database name")
	validateCmd.Flags().String("source-sslmode", "prefer", "Source database SSL mode")

	validateCmd.Flags().String("dest-host", "", "Destination database host")
	validateCmd.Flags().Int("dest-port", 0, "Destination database port")
	validateCmd.Flags().String("dest-user", "", "Destination database username")
	validateCmd.Flags().String("dest-password", "", "Destination database password")
	validateCmd.Flags().String("dest-sslmode", "", "Destination database SSL mode")

	validateCmd.Flags().String("target-db", "", "Target database name (supports templates)")
	validateCmd.Flags().StringToString("template-var", map[string]string{}, "Template variables")

	// Validation options
	validateCmd.Flags().Bool("quick", false, "Only test basic connectivity (skip detailed checks)")
	validateCmd.Flags().Bool("skip-permissions", false, "Skip permission checks")
	validateCmd.Flags().Bool("check-resources", false, "Check available disk space and resources")

	// Output options
	validateCmd.Flags().String("output-format", "text", "Output format: text or json")
	validateCmd.Flags().Bool("quiet", false, "Only output errors and final result")
}

func runValidate(cmd *cobra.Command, args []string) error {
	start := time.Now()

	outputFormat, _ := cmd.Flags().GetString("output-format")
	quiet, _ := cmd.Flags().GetBool("quiet")
	quick, _ := cmd.Flags().GetBool("quick")
	skipPermissions, _ := cmd.Flags().GetBool("skip-permissions")
	checkResources, _ := cmd.Flags().GetBool("check-resources")

	var results []ValidationResult

	// Build configuration similar to fork command
	cfg := &config.ForkConfig{}

	// Load from environment variables
	cfg.LoadFromEnvironment()

	// Apply flag values (similar to fork command logic)
	applyValidationFlags(cmd, cfg)

	// 1. Configuration validation
	results = append(results, validateConfiguration(cfg)...)

	// 2. Template processing validation
	if cfg.TargetDatabase != "" {
		results = append(results, validateTemplateProcessing(cfg)...)
	}

	// 3. Database connectivity tests
	if !quick {
		results = append(results, validateDatabaseConnectivity(cfg)...)

		// 4. Permission checks
		if !skipPermissions {
			results = append(results, validatePermissions(cfg)...)
		}

		// 5. Resource checks
		if checkResources {
			results = append(results, validateResources(cfg)...)
		}
	} else {
		results = append(results, validateQuickConnectivity(cfg)...)
	}

	// Determine overall success
	success := true
	var failedChecks []string
	for _, result := range results {
		if result.Status == "fail" {
			success = false
			failedChecks = append(failedChecks, result.Check)
		}
	}

	output := &ValidateOutput{
		Format:   outputFormat,
		Success:  success,
		Results:  results,
		Duration: time.Since(start).String(),
	}

	if success {
		output.Message = "All validation checks passed"
	} else {
		output.Error = fmt.Sprintf("Validation failed: %v", failedChecks)
	}

	return outputValidationResult(output, quiet)
}

// applyValidationFlags applies command flags to configuration
func applyValidationFlags(cmd *cobra.Command, cfg *config.ForkConfig) {
	// Source configuration
	if cmd.Flag("source-host").Changed {
		cfg.Source.Host, _ = cmd.Flags().GetString("source-host")
	}
	if cmd.Flag("source-port").Changed {
		cfg.Source.Port, _ = cmd.Flags().GetInt("source-port")
	}
	if cmd.Flag("source-user").Changed {
		cfg.Source.Username, _ = cmd.Flags().GetString("source-user")
	}
	if cmd.Flag("source-password").Changed {
		cfg.Source.Password, _ = cmd.Flags().GetString("source-password")
	}
	if cmd.Flag("source-db").Changed {
		cfg.Source.Database, _ = cmd.Flags().GetString("source-db")
	}
	if cmd.Flag("source-sslmode").Changed {
		cfg.Source.SSLMode, _ = cmd.Flags().GetString("source-sslmode")
	}

	// Destination configuration with defaults
	if cmd.Flag("dest-host").Changed {
		cfg.Destination.Host, _ = cmd.Flags().GetString("dest-host")
	} else if cfg.Destination.Host == "" {
		cfg.Destination.Host = cfg.Source.Host
	}

	if cmd.Flag("dest-port").Changed {
		cfg.Destination.Port, _ = cmd.Flags().GetInt("dest-port")
	} else if cfg.Destination.Port == 0 {
		cfg.Destination.Port = cfg.Source.Port
	}

	if cmd.Flag("dest-user").Changed {
		cfg.Destination.Username, _ = cmd.Flags().GetString("dest-user")
	} else if cfg.Destination.Username == "" {
		cfg.Destination.Username = cfg.Source.Username
	}

	if cmd.Flag("dest-password").Changed {
		cfg.Destination.Password, _ = cmd.Flags().GetString("dest-password") // pragma: allowlist secret
	} else if cfg.Destination.Password == "" {
		cfg.Destination.Password = cfg.Source.Password // pragma: allowlist secret
	}

	if cmd.Flag("dest-sslmode").Changed {
		cfg.Destination.SSLMode, _ = cmd.Flags().GetString("dest-sslmode")
	} else if cfg.Destination.SSLMode == "" {
		cfg.Destination.SSLMode = cfg.Source.SSLMode
	}

	// Target database
	if cmd.Flag("target-db").Changed {
		cfg.TargetDatabase, _ = cmd.Flags().GetString("target-db")
	}

	// Template variables
	if cmd.Flag("template-var").Changed {
		templateVars, _ := cmd.Flags().GetStringToString("template-var")
		if cfg.TemplateVars == nil {
			cfg.TemplateVars = make(map[string]string)
		}
		for k, v := range templateVars {
			cfg.TemplateVars[k] = v
		}
	}
}

// validateConfiguration validates basic configuration
func validateConfiguration(cfg *config.ForkConfig) []ValidationResult {
	var results []ValidationResult

	// Check required fields
	if cfg.Source.Host == "" {
		results = append(results, ValidationResult{
			Check:   "source_host",
			Status:  "fail",
			Message: "Source host is required",
		})
	} else {
		results = append(results, ValidationResult{
			Check:   "source_host",
			Status:  "pass",
			Message: fmt.Sprintf("Source host configured: %s", cfg.Source.Host),
		})
	}

	if cfg.Source.Database == "" {
		results = append(results, ValidationResult{
			Check:   "source_database",
			Status:  "fail",
			Message: "Source database is required",
		})
	} else {
		results = append(results, ValidationResult{
			Check:   "source_database",
			Status:  "pass",
			Message: fmt.Sprintf("Source database configured: %s", cfg.Source.Database),
		})
	}

	if cfg.TargetDatabase == "" {
		results = append(results, ValidationResult{
			Check:   "target_database",
			Status:  "warn",
			Message: "Target database not specified",
		})
	} else {
		results = append(results, ValidationResult{
			Check:   "target_database",
			Status:  "pass",
			Message: fmt.Sprintf("Target database configured: %s", cfg.TargetDatabase),
		})
	}

	// Check for same-server vs cross-server
	if cfg.IsSameServer() {
		results = append(results, ValidationResult{
			Check:   "fork_mode",
			Status:  "pass",
			Message: "Same-server fork detected (fast template-based cloning)",
		})
	} else {
		results = append(results, ValidationResult{
			Check:   "fork_mode",
			Status:  "pass",
			Message: "Cross-server fork detected (data transfer required)",
		})
	}

	return results
}

// validateTemplateProcessing validates template variable resolution
func validateTemplateProcessing(cfg *config.ForkConfig) []ValidationResult {
	var results []ValidationResult

	// Try to process templates
	originalTarget := cfg.TargetDatabase
	err := cfg.ProcessTemplates()

	if err != nil {
		results = append(results, ValidationResult{
			Check:   "template_processing",
			Status:  "fail",
			Message: "Template processing failed",
			Details: err.Error(),
		})
	} else if originalTarget != cfg.TargetDatabase {
		results = append(results, ValidationResult{
			Check:   "template_processing",
			Status:  "pass",
			Message: fmt.Sprintf("Template processed: %s → %s", originalTarget, cfg.TargetDatabase),
		})
	} else {
		results = append(results, ValidationResult{
			Check:   "template_processing",
			Status:  "pass",
			Message: "No templates to process",
		})
	}

	return results
}

// validateQuickConnectivity performs basic connectivity tests
func validateQuickConnectivity(cfg *config.ForkConfig) []ValidationResult {
	var results []ValidationResult

	// Test source connection
	sourceConn, err := db.NewConnection(&cfg.Source)
	if err != nil {
		results = append(results, ValidationResult{
			Check:   "source_connectivity",
			Status:  "fail",
			Message: "Cannot connect to source database",
			Details: err.Error(),
		})
	} else {
		if err := sourceConn.Close(); err != nil {
			fmt.Printf("Warning: Failed to close source connection: %v\n", err)
		}
		results = append(results, ValidationResult{
			Check:   "source_connectivity",
			Status:  "pass",
			Message: "Source database connection successful",
		})
	}

	// Test destination connection (admin database)
	adminConfig := cfg.Destination
	adminConfig.Database = "postgres"
	destConn, err := db.NewConnection(&adminConfig)
	if err != nil {
		results = append(results, ValidationResult{
			Check:   "destination_connectivity",
			Status:  "fail",
			Message: "Cannot connect to destination server",
			Details: err.Error(),
		})
	} else {
		defer func() {
			if err := destConn.Close(); err != nil {
				fmt.Printf("Warning: Failed to close destination connection: %v\n", err)
			}
		}()
		results = append(results, ValidationResult{
			Check:   "destination_connectivity",
			Status:  "pass",
			Message: "Destination server connection successful",
		})
	}

	return results
}

// validateDatabaseConnectivity performs detailed connectivity and existence checks
func validateDatabaseConnectivity(cfg *config.ForkConfig) []ValidationResult {
	var results []ValidationResult

	// Source database checks
	sourceConn, err := db.NewConnection(&cfg.Source)
	if err != nil {
		results = append(results, ValidationResult{
			Check:   "source_connectivity",
			Status:  "fail",
			Message: "Cannot connect to source database",
			Details: err.Error(),
		})
		return results
	}
	if err := sourceConn.Close(); err != nil {
		fmt.Printf("Warning: Failed to close source connection: %v\n", err)
	}

	results = append(results, ValidationResult{
		Check:   "source_connectivity",
		Status:  "pass",
		Message: "Source database connection successful",
	})

	// Check if source database exists (for cross-server forks)
	if !cfg.IsSameServer() {
		exists, err := sourceConn.DatabaseExists(cfg.Source.Database)
		if err != nil {
			results = append(results, ValidationResult{
				Check:   "source_database_exists",
				Status:  "warn",
				Message: "Cannot verify source database existence",
				Details: err.Error(),
			})
		} else if !exists {
			results = append(results, ValidationResult{
				Check:   "source_database_exists",
				Status:  "fail",
				Message: "Source database does not exist",
			})
		} else {
			results = append(results, ValidationResult{
				Check:   "source_database_exists",
				Status:  "pass",
				Message: "Source database exists",
			})
		}
	}

	// Destination server checks
	adminConfig := cfg.Destination
	adminConfig.Database = "postgres"
	destConn, err := db.NewConnection(&adminConfig)
	if err != nil {
		results = append(results, ValidationResult{
			Check:   "destination_connectivity",
			Status:  "fail",
			Message: "Cannot connect to destination server",
			Details: err.Error(),
		})
		return results
	}
	defer func() {
		if err := destConn.Close(); err != nil {
			fmt.Printf("Warning: Failed to close destination connection: %v\n", err)
		}
	}()

	results = append(results, ValidationResult{
		Check:   "destination_connectivity",
		Status:  "pass",
		Message: "Destination server connection successful",
	})

	// Check if target database already exists
	if cfg.TargetDatabase != "" {
		exists, err := destConn.DatabaseExists(cfg.TargetDatabase)
		if err != nil {
			results = append(results, ValidationResult{
				Check:   "target_database_exists",
				Status:  "warn",
				Message: "Cannot check if target database exists",
				Details: err.Error(),
			})
		} else if exists {
			if cfg.DropIfExists {
				results = append(results, ValidationResult{
					Check:   "target_database_exists",
					Status:  "warn",
					Message: "Target database exists but will be dropped",
				})
			} else {
				results = append(results, ValidationResult{
					Check:   "target_database_exists",
					Status:  "fail",
					Message: "Target database already exists (use --drop-if-exists to overwrite)",
				})
			}
		} else {
			results = append(results, ValidationResult{
				Check:   "target_database_exists",
				Status:  "pass",
				Message: "Target database does not exist (ready for creation)",
			})
		}
	}

	return results
}

// validatePermissions checks database permissions
func validatePermissions(cfg *config.ForkConfig) []ValidationResult {
	var results []ValidationResult

	// Check source permissions (should be read-only)
	sourceConn, err := db.NewConnection(&cfg.Source)
	if err != nil {
		return results // Connection already tested
	}
	defer func() {
		if err := sourceConn.Close(); err != nil {
			fmt.Printf("Warning: Failed to close source connection: %v\n", err)
		}
	}()

	// Test SELECT permission
	_, err = sourceConn.DB.Query("SELECT 1 LIMIT 1")
	if err != nil {
		results = append(results, ValidationResult{
			Check:   "source_read_permission",
			Status:  "fail",
			Message: "Cannot read from source database",
			Details: err.Error(),
		})
	} else {
		results = append(results, ValidationResult{
			Check:   "source_read_permission",
			Status:  "pass",
			Message: "Source database read access confirmed",
		})
	}

	// Check destination permissions (should be able to create databases)
	adminConfig := cfg.Destination
	adminConfig.Database = "postgres"
	destConn, err := db.NewConnection(&adminConfig)
	if err != nil {
		return results // Connection already tested
	}
	defer func() {
		if err := destConn.Close(); err != nil {
			fmt.Printf("Warning: Failed to close destination connection: %v\n", err)
		}
	}()

	// Test CREATEDB permission by checking user attributes
	query := "SELECT rolcreatedb FROM pg_roles WHERE rolname = CURRENT_USER"
	var canCreateDB bool
	err = destConn.DB.QueryRow(query).Scan(&canCreateDB)
	if err != nil {
		results = append(results, ValidationResult{
			Check:   "destination_createdb_permission",
			Status:  "warn",
			Message: "Cannot verify CREATEDB permission",
			Details: err.Error(),
		})
	} else if !canCreateDB {
		results = append(results, ValidationResult{
			Check:   "destination_createdb_permission",
			Status:  "fail",
			Message: "User does not have CREATEDB permission",
		})
	} else {
		results = append(results, ValidationResult{
			Check:   "destination_createdb_permission",
			Status:  "pass",
			Message: "User has CREATEDB permission",
		})
	}

	return results
}

// validateResources checks available resources
func validateResources(cfg *config.ForkConfig) []ValidationResult {
	var results []ValidationResult

	// Get source database size
	sourceConn, err := db.NewConnection(&cfg.Source)
	if err != nil {
		return results
	}
	defer func() {
		if err := sourceConn.Close(); err != nil {
			fmt.Printf("Warning: Failed to close source connection: %v\n", err)
		}
	}()

	sourceSize, err := sourceConn.GetDatabaseSize(cfg.Source.Database)
	if err != nil {
		results = append(results, ValidationResult{
			Check:   "source_database_size",
			Status:  "warn",
			Message: "Cannot determine source database size",
			Details: err.Error(),
		})
	} else {
		results = append(results, ValidationResult{
			Check:   "source_database_size",
			Status:  "pass",
			Message: fmt.Sprintf("Source database size: %s", formatBytes(sourceSize)),
		})

		// Warn if database is very large
		if sourceSize > 100*1024*1024*1024 { // 100GB
			results = append(results, ValidationResult{
				Check:   "large_database_warning",
				Status:  "warn",
				Message: "Large database detected - transfer may take several hours",
			})
		}
	}

	return results
}

// outputValidationResult outputs the validation result in the specified format
func outputValidationResult(output *ValidateOutput, quiet bool) error {
	if output.Format == "json" {
		jsonOutput, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON output: %w", err)
		}
		fmt.Println(string(jsonOutput))
	} else {
		// Text output
		if !quiet {
			if output.Success {
				fmt.Printf("✅ %s\n", output.Message)
			} else {
				fmt.Printf("❌ %s\n", output.Error)
			}

			fmt.Println("\nValidation Results:")
			for _, result := range output.Results {
				var icon string
				switch result.Status {
				case "pass":
					icon = "✅"
				case "warn":
					icon = "⚠️"
				case "fail":
					icon = "❌"
				default:
					icon = "ℹ️"
				}

				fmt.Printf("  %s %s: %s\n", icon, result.Check, result.Message)
				if result.Details != "" && result.Status != "pass" {
					fmt.Printf("      Details: %s\n", result.Details)
				}
			}

			fmt.Printf("\nValidation completed in %s\n", output.Duration)
		} else {
			// Quiet mode - only output final status
			if output.Success {
				fmt.Println("PASS")
			} else {
				fmt.Printf("FAIL: %s\n", output.Error)
			}
		}
	}

	// Set exit code
	if !output.Success {
		os.Exit(1)
	}

	return nil
}
