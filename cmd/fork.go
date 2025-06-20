package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/hongkongkiwi/postgres-db-fork/internal/config"
	"github.com/hongkongkiwi/postgres-db-fork/internal/fork"

	"github.com/AlecAivazis/survey/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// forkCmd represents the fork command
var forkCmd = &cobra.Command{
	Use:   "fork",
	Short: "Fork a PostgreSQL database",
	Long: `Fork (copy) a PostgreSQL database from source to destination.

The tool supports two execution modes and two forking strategies:

EXECUTION MODES:
1. Foreground mode (default): Blocks until completion, perfect for CI/CD pipelines
2. Background mode (--background): Returns immediately with job ID, runs in background

FORKING STRATEGIES:
1. Same-server forking: When source and destination are on the same PostgreSQL server,
   uses efficient template-based cloning.
2. Cross-server forking: When source and destination are on different servers,
   uses dump and restore operations with parallel data transfer.

Environment Variables (PGFORK_ prefix):
  PGFORK_SOURCE_HOST, PGFORK_SOURCE_PORT, PGFORK_SOURCE_USER, PGFORK_SOURCE_PASSWORD
  PGFORK_SOURCE_DATABASE, PGFORK_DEST_HOST, PGFORK_DEST_USER, etc.
  PGFORK_VAR_* variables can be used in templates (e.g., PGFORK_VAR_PR_NUMBER=123)

Template Variables:
  {{.PR_NUMBER}} - GitHub PR number or GitLab MR IID
  {{.BRANCH}} - Sanitized branch name
  {{.COMMIT_SHORT}} - First 8 characters of commit SHA
  Custom variables via --template-var or PGFORK_VAR_* environment variables

Examples:
  # Fork database within same server
  postgres-db-fork fork --source-host localhost --source-db myapp_prod --target-db myapp_dev

  # Fork with template naming for PR preview
  postgres-db-fork fork \
    --source-db myapp_staging \
    --target-db "myapp_pr_{{.PR_NUMBER}}" \
    --template-var PR_NUMBER=123

  # CI/CD integration with environment variables and JSON output
  export PGFORK_SOURCE_HOST=staging.example.com
  export PGFORK_SOURCE_DATABASE=myapp_staging
  export PGFORK_TARGET_DATABASE="myapp_pr_{{.PR_NUMBER}}"
  export PGFORK_VAR_PR_NUMBER=123
  postgres-db-fork fork --output-format json --quiet

  # Dry run to preview what would be done
  postgres-db-fork fork --source-db prod --target-db test --dry-run

  # Background mode - start fork and continue working
  postgres-db-fork fork --source-db large_db --target-db copy --background
  postgres-db-fork jobs list  # Monitor progress

  # Background mode with JSON output for automation
  postgres-db-fork fork --source-db prod --target-db staging --background --output-format json`,
	RunE: runFork,
}

func init() {
	rootCmd.AddCommand(forkCmd)

	// Source database flags
	forkCmd.Flags().String("source-host", "localhost", "Source database host")
	forkCmd.Flags().Int("source-port", 5432, "Source database port")
	forkCmd.Flags().String("source-user", "", "Source database username")
	forkCmd.Flags().String("source-password", "", "Source database password")
	forkCmd.Flags().String("source-db", "", "Source database name (required)")
	forkCmd.Flags().String("source-sslmode", "prefer", "Source database SSL mode")

	// Destination database flags
	forkCmd.Flags().String("dest-host", "", "Destination database host (defaults to source-host)")
	forkCmd.Flags().Int("dest-port", 0, "Destination database port (defaults to source-port)")
	forkCmd.Flags().String("dest-user", "", "Destination database username (defaults to source-user)")
	forkCmd.Flags().String("dest-password", "", "Destination database password (defaults to source-password)")
	forkCmd.Flags().String("dest-sslmode", "", "Destination database SSL mode (defaults to source-sslmode)")

	// Target database
	forkCmd.Flags().String("target-db", "", "Target database name (required, supports templates)")

	// Fork options
	forkCmd.Flags().Bool("drop-if-exists", false, "Drop target database if it exists")
	forkCmd.Flags().Int("max-connections", 4, "Maximum number of parallel connections for data transfer")
	forkCmd.Flags().Int("chunk-size", 1000, "Number of rows to transfer in each batch")
	forkCmd.Flags().Duration("timeout", 30*time.Minute, "Operation timeout")
	forkCmd.Flags().StringSlice("exclude-tables", []string{}, "Tables to exclude from transfer")
	forkCmd.Flags().StringSlice("include-tables", []string{}, "Tables to include in transfer (if specified, only these tables will be transferred)")
	forkCmd.Flags().Bool("schema-only", false, "Transfer schema only (no data)")
	forkCmd.Flags().Bool("data-only", false, "Transfer data only (no schema)")

	// CI/CD Integration flags
	forkCmd.Flags().String("output-format", "text", "Output format: text or json")
	forkCmd.Flags().Bool("quiet", false, "Suppress all output except errors and final result")
	forkCmd.Flags().Bool("dry-run", false, "Preview what would be done without making changes")
	forkCmd.Flags().StringToString("template-var", map[string]string{}, "Template variables (e.g., --template-var PR_NUMBER=123)")
	forkCmd.Flags().Bool("env-vars", true, "Load configuration from PGFORK_* environment variables")
	forkCmd.Flags().Bool("background", false, "Run fork operation in background (daemon mode)")

	// Interactive mode
	forkCmd.Flags().Bool("interactive", false, "Run in interactive mode to be prompted for configuration")

	// Note: source-db and target-db are required, but we check this after loading env vars
	// to allow them to be provided via environment variables

	// Bind flags to viper
	bindFlag("source.host", forkCmd.Flags().Lookup("source-host"))
	bindFlag("source.port", forkCmd.Flags().Lookup("source-port"))
	bindFlag("source.username", forkCmd.Flags().Lookup("source-user"))
	bindFlag("source.password", forkCmd.Flags().Lookup("source-password"))
	bindFlag("source.database", forkCmd.Flags().Lookup("source-db"))
	bindFlag("source.sslmode", forkCmd.Flags().Lookup("source-sslmode"))

	bindFlag("destination.host", forkCmd.Flags().Lookup("dest-host"))
	bindFlag("destination.port", forkCmd.Flags().Lookup("dest-port"))
	bindFlag("destination.username", forkCmd.Flags().Lookup("dest-user"))
	bindFlag("destination.password", forkCmd.Flags().Lookup("dest-password"))
	bindFlag("destination.sslmode", forkCmd.Flags().Lookup("dest-sslmode"))

	bindFlag("target_database", forkCmd.Flags().Lookup("target-db"))
	bindFlag("drop_if_exists", forkCmd.Flags().Lookup("drop-if-exists"))
	bindFlag("max_connections", forkCmd.Flags().Lookup("max-connections"))
	bindFlag("chunk_size", forkCmd.Flags().Lookup("chunk-size"))
	bindFlag("timeout", forkCmd.Flags().Lookup("timeout"))
	bindFlag("exclude_tables", forkCmd.Flags().Lookup("exclude-tables"))
	bindFlag("include_tables", forkCmd.Flags().Lookup("include-tables"))
	bindFlag("schema_only", forkCmd.Flags().Lookup("schema-only"))
	bindFlag("data_only", forkCmd.Flags().Lookup("data-only"))

	// CI/CD flags
	bindFlag("output_format", forkCmd.Flags().Lookup("output-format"))
	bindFlag("quiet", forkCmd.Flags().Lookup("quiet"))
	bindFlag("dry_run", forkCmd.Flags().Lookup("dry-run"))
	bindFlag("template_vars", forkCmd.Flags().Lookup("template-var"))
	bindFlag("background", forkCmd.Flags().Lookup("background"))
}

// bindFlag is a helper to bind flags and handle errors gracefully
func bindFlag(key string, flag *pflag.Flag) {
	if err := viper.BindPFlag(key, flag); err != nil {
		fmt.Printf("Warning: Failed to bind flag %s: %v\n", key, err)
	}
}

func runFork(cmd *cobra.Command, args []string) error {
	start := time.Now()

	// Build configuration from flags and config file
	cfg := &config.ForkConfig{}

	// Check for interactive mode
	interactive, _ := cmd.Flags().GetBool("interactive")
	if interactive {
		if err := runInteractiveMode(cfg); err != nil {
			return err
		}
	} else {
		// Load from environment if not interactive
		cfg.LoadFromEnvironment()
	}

	// Set default values for required fields that might not be set
	if cfg.ChunkSize == 0 {
		cfg.ChunkSize = 1000 // Default chunk size
	}
	if cfg.MaxConnections == 0 {
		cfg.MaxConnections = 4 // Default max connections
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Minute // Default timeout
	}
	if cfg.OutputFormat == "" {
		cfg.OutputFormat = "text" // Default output format
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info" // Default log level
	}

	// Override with flag values
	// Source configuration
	if cmd.Flag("source-host").Changed || cfg.Source.Host == "" {
		cfg.Source.Host = viper.GetString("source.host")
	}
	if cmd.Flag("source-port").Changed || cfg.Source.Port == 0 {
		cfg.Source.Port = viper.GetInt("source.port")
	}
	if cmd.Flag("source-user").Changed || cfg.Source.Username == "" {
		cfg.Source.Username = viper.GetString("source.username")
	}
	if cmd.Flag("source-password").Changed || cfg.Source.Password == "" {
		cfg.Source.Password = viper.GetString("source.password")
	}
	if cmd.Flag("source-db").Changed || cfg.Source.Database == "" {
		cfg.Source.Database = viper.GetString("source.database")
	}
	if cmd.Flag("source-sslmode").Changed || cfg.Source.SSLMode == "" {
		cfg.Source.SSLMode = viper.GetString("source.sslmode")
	}

	// Destination configuration - default to source values if not specified
	if cmd.Flag("dest-host").Changed || cfg.Destination.Host == "" {
		cfg.Destination.Host = viper.GetString("destination.host")
		if cfg.Destination.Host == "" {
			cfg.Destination.Host = cfg.Source.Host
		}
	}

	if cmd.Flag("dest-port").Changed || cfg.Destination.Port == 0 {
		cfg.Destination.Port = viper.GetInt("destination.port")
		if cfg.Destination.Port == 0 {
			cfg.Destination.Port = cfg.Source.Port
		}
	}

	if cmd.Flag("dest-user").Changed || cfg.Destination.Username == "" {
		cfg.Destination.Username = viper.GetString("destination.username")
		if cfg.Destination.Username == "" {
			cfg.Destination.Username = cfg.Source.Username
		}
	}

	if cmd.Flag("dest-password").Changed || cfg.Destination.Password == "" {
		cfg.Destination.Password = viper.GetString("destination.password")
		if cfg.Destination.Password == "" {
			cfg.Destination.Password = cfg.Source.Password
		}
	}

	if cmd.Flag("dest-sslmode").Changed || cfg.Destination.SSLMode == "" {
		cfg.Destination.SSLMode = viper.GetString("destination.sslmode")
		if cfg.Destination.SSLMode == "" {
			cfg.Destination.SSLMode = cfg.Source.SSLMode
		}
	}

	// Other configuration
	if cmd.Flag("target-db").Changed || cfg.TargetDatabase == "" {
		cfg.TargetDatabase = viper.GetString("target_database")
	}

	// Ensure destination database is set to the target database
	// This is required for validation but will be overridden during the fork process
	if cfg.Destination.Database == "" {
		cfg.Destination.Database = cfg.TargetDatabase
	}
	if cmd.Flag("drop-if-exists").Changed {
		cfg.DropIfExists = viper.GetBool("drop_if_exists")
	}
	if cmd.Flag("max-connections").Changed || cfg.MaxConnections == 0 {
		if flagValue := viper.GetInt("max_connections"); flagValue > 0 {
			cfg.MaxConnections = flagValue
		}
	}
	if cmd.Flag("chunk-size").Changed || cfg.ChunkSize == 0 {
		if flagValue := viper.GetInt("chunk_size"); flagValue > 0 {
			cfg.ChunkSize = flagValue
		}
	}
	if cmd.Flag("timeout").Changed || cfg.Timeout == 0 {
		if flagValue := viper.GetDuration("timeout"); flagValue > 0 {
			cfg.Timeout = flagValue
		}
	}
	if cmd.Flag("exclude-tables").Changed {
		cfg.ExcludeTables = viper.GetStringSlice("exclude_tables")
	}
	if cmd.Flag("include-tables").Changed {
		cfg.IncludeTables = viper.GetStringSlice("include_tables")
	}
	if cmd.Flag("schema-only").Changed {
		cfg.SchemaOnly = viper.GetBool("schema_only")
	}
	if cmd.Flag("data-only").Changed {
		cfg.DataOnly = viper.GetBool("data_only")
	}

	// CI/CD configuration
	if cmd.Flag("output-format").Changed || cfg.OutputFormat == "" {
		if flagValue := viper.GetString("output_format"); flagValue != "" {
			cfg.OutputFormat = flagValue
		}
	}
	if cmd.Flag("quiet").Changed {
		cfg.Quiet = viper.GetBool("quiet")
	}
	if cmd.Flag("dry-run").Changed {
		cfg.DryRun = viper.GetBool("dry_run")
	}
	if cmd.Flag("template-var").Changed {
		templateVars := viper.GetStringMapString("template_vars")
		if cfg.TemplateVars == nil {
			cfg.TemplateVars = make(map[string]string)
		}
		for k, v := range templateVars {
			cfg.TemplateVars[k] = v
		}
	}

	// Check required fields that might be provided via environment variables
	if cfg.Source.Database == "" {
		return outputResult(cfg, false, "", "Source database is required (use --source-db flag or PGFORK_SOURCE_DATABASE environment variable)", time.Since(start))
	}
	if cfg.TargetDatabase == "" {
		return outputResult(cfg, false, "", "Target database is required (use --target-db flag or PGFORK_TARGET_DATABASE environment variable)", time.Since(start))
	}

	// Process templates in configuration
	if err := cfg.ProcessTemplates(); err != nil {
		return outputResult(cfg, false, "", fmt.Sprintf("Template processing failed: %v", err), time.Since(start))
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return outputResult(cfg, false, "", fmt.Sprintf("Configuration validation failed: %v", err), time.Since(start))
	}

	// Validate conflicting options
	if cfg.SchemaOnly && cfg.DataOnly {
		return outputResult(cfg, false, "", "Cannot specify both --schema-only and --data-only", time.Since(start))
	}

	// Handle dry run
	if cfg.DryRun {
		return handleDryRun(cfg, time.Since(start))
	}

	// Check background mode
	backgroundMode, _ := cmd.Flags().GetBool("background")

	if backgroundMode {
		return runForkBackground(cfg, time.Since(start))
	}

	// Create forker and execute (foreground mode)
	forker := fork.NewForker(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	err := forker.Fork(ctx)
	duration := time.Since(start)

	if err != nil {
		return outputResult(cfg, false, "", err.Error(), duration)
	}

	return outputResult(cfg, true, "Database fork completed successfully", "", duration)
}

// runInteractiveMode guides the user through setting up the fork configuration
func runInteractiveMode(cfg *config.ForkConfig) error {
	fmt.Println("🚀 Starting interactive fork setup...")

	// Source Database Questions
	sourceQuestions := []*survey.Question{
		{
			Name:     "Host",
			Prompt:   &survey.Input{Message: "Source Host:", Default: "localhost"},
			Validate: survey.Required,
		},
		{
			Name:     "Port",
			Prompt:   &survey.Input{Message: "Source Port:", Default: "5432"},
			Validate: survey.Required,
		},
		{
			Name:     "Username",
			Prompt:   &survey.Input{Message: "Source Username:", Default: "postgres"},
			Validate: survey.Required,
		},
		{
			Name:   "Password",
			Prompt: &survey.Password{Message: "Source Password:"},
		},
		{
			Name:     "Database",
			Prompt:   &survey.Input{Message: "Source Database Name:"},
			Validate: survey.Required,
		},
	}
	fmt.Println("\n--- Source Database ---")
	if err := survey.Ask(sourceQuestions, &cfg.Source); err != nil {
		return err
	}

	// Ask if destination is the same server
	isSameServer := false
	promptSame := &survey.Confirm{
		Message: "Is the destination on the same server as the source?",
		Default: true,
	}
	if err := survey.AskOne(promptSame, &isSameServer); err != nil {
		return err
	}

	if isSameServer {
		cfg.Destination = cfg.Source
	} else {
		// Destination Database Questions
		destQuestions := []*survey.Question{
			{
				Name:     "Host",
				Prompt:   &survey.Input{Message: "Destination Host:"},
				Validate: survey.Required,
			},
			{
				Name:     "Port",
				Prompt:   &survey.Input{Message: "Destination Port:", Default: "5432"},
				Validate: survey.Required,
			},
			{
				Name:     "Username",
				Prompt:   &survey.Input{Message: "Destination Username:"},
				Validate: survey.Required,
			},
			{
				Name:   "Password",
				Prompt: &survey.Password{Message: "Destination Password:"},
			},
		}
		fmt.Println("\n--- Destination Server ---")
		if err := survey.Ask(destQuestions, &cfg.Destination); err != nil {
			return err
		}
	}

	// Target Database Name
	targetPrompt := &survey.Input{Message: "New (target) database name:"}
	if err := survey.AskOne(targetPrompt, &cfg.TargetDatabase, survey.WithValidator(survey.Required)); err != nil {
		return err
	}

	// Fork Options
	optionsQuestions := []*survey.Question{
		{
			Name:   "DropIfExists",
			Prompt: &survey.Confirm{Message: "Drop target database if it already exists?", Default: false},
		},
		{
			Name:   "SchemaOnly",
			Prompt: &survey.Confirm{Message: "Transfer schema only (no data)?", Default: false},
		},
		{
			Name:   "DataOnly",
			Prompt: &survey.Confirm{Message: "Transfer data only (no schema)?", Default: false},
		},
	}
	fmt.Println("\n--- Fork Options ---")
	if err := survey.Ask(optionsQuestions, cfg); err != nil {
		return err
	}

	fmt.Println("\n✅ Configuration complete!")
	return nil
}

// handleDryRun handles dry run mode
func handleDryRun(cfg *config.ForkConfig, duration time.Duration) error {
	message := fmt.Sprintf("DRY RUN: Would fork database '%s' to '%s'", cfg.Source.Database, cfg.TargetDatabase)

	if cfg.IsSameServer() {
		message += "\nMethod: Same-server template-based cloning (fast)"
	} else {
		message += "\nMethod: Cross-server data transfer with COPY operations"
		message += fmt.Sprintf("\nSettings: %d max connections, %d chunk size", cfg.MaxConnections, cfg.ChunkSize)
	}

	if len(cfg.ExcludeTables) > 0 {
		message += fmt.Sprintf("\nExcluding tables: %v", cfg.ExcludeTables)
	}
	if len(cfg.IncludeTables) > 0 {
		message += fmt.Sprintf("\nIncluding only tables: %v", cfg.IncludeTables)
	}
	if cfg.SchemaOnly {
		message += "\nTransferring schema only (no data)"
	}
	if cfg.DataOnly {
		message += "\nTransferring data only (no schema)"
	}

	return outputResult(cfg, true, message, "", duration)
}

// runForkBackground executes the fork operation in background mode
func runForkBackground(cfg *config.ForkConfig, startDuration time.Duration) error {
	// Import necessary packages for background execution
	// We'll use the job system to track the background operation

	// Generate a unique job ID
	jobID := fmt.Sprintf("fork-%d", time.Now().Unix())

	// Create resumption manager for job tracking
	resumptionManager := fork.NewResumptionManager("", jobID)

	// Initialize the job state
	sourceSnapshot := fork.DatabaseConfigSnapshot{
		Host:     cfg.Source.Host,
		Port:     cfg.Source.Port,
		Username: cfg.Source.Username,
		Database: cfg.Source.Database,
		SSLMode:  cfg.Source.SSLMode,
	}

	destSnapshot := fork.DatabaseConfigSnapshot{
		Host:     cfg.Destination.Host,
		Port:     cfg.Destination.Port,
		Username: cfg.Destination.Username,
		Database: cfg.Destination.Database,
		SSLMode:  cfg.Destination.SSLMode,
	}

	// Start the fork operation in a goroutine
	go func() {
		forker := fork.NewForker(cfg)
		ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
		defer cancel()

		// Initialize job state
		_, _, err := resumptionManager.InitializeJob(sourceSnapshot, destSnapshot, cfg.TargetDatabase, map[string]int64{})
		if err != nil {
			if err := resumptionManager.SetError(err); err != nil {
				fmt.Printf("Warning: Failed to set error in resumption manager: %v\n", err)
			}
			return
		}

		// Execute the fork
		err = forker.Fork(ctx)
		if err != nil {
			if err := resumptionManager.SetError(err); err != nil {
				fmt.Printf("Warning: Failed to set error in resumption manager: %v\n", err)
			}
		} else {
			if err := resumptionManager.CompleteJob(false); err != nil {
				fmt.Printf("Warning: Failed to complete job in resumption manager: %v\n", err)
			}
		}
	}()

	// Output immediate response
	result := &config.OutputConfig{
		Format:   cfg.OutputFormat,
		Success:  true,
		Message:  fmt.Sprintf("Background fork started with job ID: %s", jobID),
		Database: cfg.TargetDatabase,
		Duration: startDuration.String(),
	}

	if cfg.OutputFormat == "json" {
		// Create a custom output map with job ID
		output := map[string]interface{}{
			"format":   result.Format,
			"success":  result.Success,
			"message":  result.Message,
			"database": result.Database,
			"duration": result.Duration,
			"job_id":   jobID,
		}
		jsonOutput, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON output: %w", err)
		}
		fmt.Println(string(jsonOutput))
	} else {
		if !cfg.Quiet {
			fmt.Printf("🚀 Background fork started\n")
			fmt.Printf("Job ID: %s\n", jobID)
			fmt.Printf("Target Database: %s\n", cfg.TargetDatabase)
			fmt.Printf("Monitor with: postgres-db-fork jobs show %s\n", jobID)
		} else {
			fmt.Printf("%s\n", jobID)
		}
	}

	return nil
}

// outputResult outputs the final result in the requested format
func outputResult(cfg *config.ForkConfig, success bool, message, errorMsg string, duration time.Duration) error {
	result := &config.OutputConfig{
		Format:   cfg.OutputFormat,
		Success:  success,
		Message:  message,
		Error:    errorMsg,
		Database: cfg.TargetDatabase,
		Duration: duration.String(),
	}

	if cfg.OutputFormat == "json" {
		jsonOutput, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON output: %w", err)
		}
		fmt.Println(string(jsonOutput))
	} else {
		// Text output
		if !cfg.Quiet {
			if success {
				fmt.Printf("✅ %s\n", message)
				if cfg.TargetDatabase != "" {
					fmt.Printf("Database: %s\n", cfg.TargetDatabase)
				}
				fmt.Printf("Duration: %s\n", duration)
			} else {
				fmt.Printf("❌ %s\n", errorMsg)
			}
		} else {
			// Quiet mode - only output essential information
			if success {
				fmt.Println(cfg.TargetDatabase)
			} else {
				fmt.Fprintf(os.Stderr, "Error: %s\n", errorMsg)
			}
		}
	}

	// Set appropriate exit code
	if !success {
		os.Exit(1)
	}

	return nil
}
