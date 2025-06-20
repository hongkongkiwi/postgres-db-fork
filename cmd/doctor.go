package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/hongkongkiwi/postgres-db-fork/internal/config"
	"github.com/hongkongkiwi/postgres-db-fork/internal/db"
	"github.com/hongkongkiwi/postgres-db-fork/internal/logging"

	"github.com/spf13/cobra"
)

// DoctorResult represents a single health check result
type DoctorResult struct {
	Check   string `json:"check"`
	Status  string `json:"status"` // "pass", "warn", "fail"
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

// DoctorOutput represents the complete doctor check output
type DoctorOutput struct {
	Format    string         `json:"format"`
	Success   bool           `json:"success"`
	Timestamp time.Time      `json:"timestamp"`
	Results   []DoctorResult `json:"results"`
	Summary   string         `json:"summary"`
	Duration  string         `json:"duration"`
}

// doctorCmd represents the doctor command
var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run comprehensive health checks",
	Long: `Run comprehensive health checks on the postgres-db-fork system.

This command checks all system components to ensure everything is working correctly:
- Configuration file validity
- Database connectivity
- System resources and permissions
- Job state directory
- Logging system
- Dependencies and versions

Examples:
  # Run all health checks
  postgres-db-fork doctor

  # JSON output for automation
  postgres-db-fork doctor --output json

  # Quick checks only
  postgres-db-fork doctor --quick`,
	RunE: runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)

	// Flags
	doctorCmd.Flags().String("output-format", "text", "Output format: text or json")
	doctorCmd.Flags().Bool("quick", false, "Run only quick checks (skip database connectivity)")
}

func runDoctor(cmd *cobra.Command, args []string) error {
	start := time.Now()
	outputFormat, _ := cmd.Flags().GetString("output-format")
	quick, _ := cmd.Flags().GetBool("quick")

	var results []DoctorResult

	// 1. System checks
	results = append(results, checkSystemHealth()...)

	// 2. Configuration checks
	results = append(results, checkConfiguration()...)

	// 3. File system checks
	results = append(results, checkFileSystem()...)

	// 4. Database connectivity (unless quick mode)
	if !quick {
		results = append(results, checkDatabaseConnectivity()...)
	}

	// 5. Dependencies check
	results = append(results, checkDependencies()...)

	// Calculate overall health
	passCount := 0
	warnCount := 0
	failCount := 0

	for _, result := range results {
		switch result.Status {
		case "pass":
			passCount++
		case "warn":
			warnCount++
		case "fail":
			failCount++
		}
	}

	success := failCount == 0
	var summary string
	if success {
		if warnCount > 0 {
			summary = fmt.Sprintf("System is healthy with %d warnings (%d passed, %d warnings)", warnCount, passCount, warnCount)
		} else {
			summary = fmt.Sprintf("System is healthy (%d checks passed)", passCount)
		}
	} else {
		summary = fmt.Sprintf("System has issues (%d passed, %d warnings, %d failed)", passCount, warnCount, failCount)
	}

	output := &DoctorOutput{
		Format:    outputFormat,
		Success:   success,
		Timestamp: time.Now(),
		Results:   results,
		Summary:   summary,
		Duration:  time.Since(start).String(),
	}

	return outputDoctorResults(output)
}

func checkSystemHealth() []DoctorResult {
	var results []DoctorResult

	// Go version check
	results = append(results, DoctorResult{
		Check:   "go_version",
		Status:  "pass",
		Message: fmt.Sprintf("Go runtime version: %s", runtime.Version()),
		Details: fmt.Sprintf("OS: %s, Arch: %s", runtime.GOOS, runtime.GOARCH),
	})

	// Memory check
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	memoryMB := m.Alloc / 1024 / 1024

	status := "pass"
	if memoryMB > 100 {
		status = "warn"
	}

	results = append(results, DoctorResult{
		Check:   "memory_usage",
		Status:  status,
		Message: fmt.Sprintf("Memory usage: %d MB", memoryMB),
		Details: fmt.Sprintf("Total allocated: %d MB, System memory: %d MB", m.TotalAlloc/1024/1024, m.Sys/1024/1024),
	})

	// Goroutines check
	goroutines := runtime.NumGoroutine()
	goStatus := "pass"
	if goroutines > 50 {
		goStatus = "warn"
	}

	results = append(results, DoctorResult{
		Check:   "goroutines",
		Status:  goStatus,
		Message: fmt.Sprintf("Active goroutines: %d", goroutines),
	})

	return results
}

func checkConfiguration() []DoctorResult {
	var results []DoctorResult

	// Check if config file exists and is readable
	home, err := os.UserHomeDir()
	if err != nil {
		results = append(results, DoctorResult{
			Check:   "config_file",
			Status:  "warn",
			Message: "Cannot determine home directory",
			Details: err.Error(),
		})
	} else {
		configPath := filepath.Join(home, ".postgres-db-fork.yaml")
		if _, err := os.Stat(configPath); err == nil {
			results = append(results, DoctorResult{
				Check:   "config_file",
				Status:  "pass",
				Message: "Configuration file found",
				Details: configPath,
			})
		} else {
			results = append(results, DoctorResult{
				Check:   "config_file",
				Status:  "pass",
				Message: "No configuration file (using defaults)",
				Details: "This is normal - configuration can be provided via flags or environment variables",
			})
		}
	}

	// Check environment variables
	envVars := []string{
		"PGFORK_SOURCE_HOST", "PGFORK_SOURCE_DATABASE",
		"PGFORK_TARGET_DATABASE", "PGFORK_SOURCE_USER",
	}

	envCount := 0
	for _, envVar := range envVars {
		if os.Getenv(envVar) != "" {
			envCount++
		}
	}

	if envCount > 0 {
		results = append(results, DoctorResult{
			Check:   "environment_variables",
			Status:  "pass",
			Message: fmt.Sprintf("Found %d PGFORK environment variables", envCount),
		})
	} else {
		results = append(results, DoctorResult{
			Check:   "environment_variables",
			Status:  "pass",
			Message: "No PGFORK environment variables set",
			Details: "This is normal - configuration can be provided via flags or config file",
		})
	}

	return results
}

func checkFileSystem() []DoctorResult {
	var results []DoctorResult

	// Check job state directory
	stateDir := filepath.Join(os.TempDir(), "postgres-db-fork", "jobs")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		results = append(results, DoctorResult{
			Check:   "state_directory_creation",
			Status:  "fail",
			Message: "Cannot create job state directory",
			Details: err.Error(),
		})
	} else {
		results = append(results, DoctorResult{
			Check:   "state_directory_creation",
			Status:  "pass",
			Message: "Job state directory is accessible",
			Details: stateDir,
		})

		// Check write permissions
		testFile := filepath.Join(stateDir, "test.tmp")
		if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
			results = append(results, DoctorResult{
				Check:   "state_directory_write_perms",
				Status:  "fail",
				Message: "Cannot write to job state directory",
				Details: err.Error(),
			})
		} else {
			results = append(results, DoctorResult{
				Check:   "state_directory_write_perms",
				Status:  "pass",
				Message: "Write permissions are correct for job state directory",
			})
			_ = os.Remove(testFile)
		}
	}

	// Check profiles directory
	home, err := os.UserHomeDir()
	if err == nil {
		profilesDir := filepath.Join(home, ".postgres-db-fork", "profiles")
		if err := os.MkdirAll(profilesDir, 0755); err != nil {
			results = append(results, DoctorResult{
				Check:   "profiles_directory",
				Status:  "warn",
				Message: "Cannot create profiles directory",
				Details: err.Error(),
			})
		} else {
			results = append(results, DoctorResult{
				Check:   "profiles_directory",
				Status:  "pass",
				Message: "Profiles directory accessible",
				Details: profilesDir,
			})
		}
	}

	return results
}

func checkDatabaseConnectivity() []DoctorResult {
	doctorLogger, _ := logging.NewLogger(&logging.Config{Level: "info", Format: "text"})
	// Load config from environment to check for source details
	cfg := &config.ForkConfig{}
	cfg.LoadFromEnvironment()

	// Only test if source is configured
	if cfg.Source.Host == "" || cfg.Source.Username == "" || cfg.Source.Database == "" {
		return []DoctorResult{
			{
				Check:   "source_database_connection",
				Status:  "warn",
				Message: "Source database not configured, skipping connection test.",
				Details: "Set PGFORK_SOURCE_* environment variables to enable this check.",
			},
		}
	}

	// Attempt connection to source
	conn, err := db.NewConnection(&cfg.Source)
	if err != nil {
		return []DoctorResult{
			{
				Check:   "source_database_connection",
				Status:  "fail",
				Message: fmt.Sprintf("Failed to connect to source database: %s", cfg.Source.Database),
				Details: err.Error(),
			},
		}
	}
	defer func() {
		if err := conn.Close(); err != nil {
			doctorLogger.Warnf("Failed to close database connection: %v", err)
		}
	}()

	// Check version
	version, err := conn.GetVersion()
	if err != nil {
		return []DoctorResult{
			{
				Check:   "source_database_connection",
				Status:  "fail",
				Message: "Connected, but failed to get PostgreSQL version.",
				Details: err.Error(),
			},
		}
	}

	return []DoctorResult{
		{
			Check:   "source_database_connection",
			Status:  "pass",
			Message: fmt.Sprintf("Successfully connected to source database: %s", cfg.Source.Database),
			Details: fmt.Sprintf("PostgreSQL Version: %s", version),
		},
	}
}

func checkDependencies() []DoctorResult {
	var results []DoctorResult
	deps := []string{"psql", "pg_dump", "pg_restore"}

	for _, dep := range deps {
		if _, err := exec.LookPath(dep); err != nil {
			results = append(results, DoctorResult{
				Check:   fmt.Sprintf("dependency_%s", dep),
				Status:  "fail",
				Message: fmt.Sprintf("Required dependency '%s' not found in PATH.", dep),
				Details: "This is required for optimized cross-server data transfers.",
			})
		} else {
			results = append(results, DoctorResult{
				Check:   fmt.Sprintf("dependency_%s", dep),
				Status:  "pass",
				Message: fmt.Sprintf("Dependency '%s' found in PATH.", dep),
			})
		}
	}

	return results
}

func outputDoctorResults(output *DoctorOutput) error {
	if output.Format == "json" {
		jsonOutput, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON output: %w", err)
		}
		fmt.Println(string(jsonOutput))
		return nil
	}

	// Text output
	fmt.Printf("🏥 postgres-db-fork Health Check\n")
	fmt.Printf("Timestamp: %s\n", output.Timestamp.Format(time.RFC3339))
	fmt.Printf("Duration: %s\n\n", output.Duration)

	passCount := 0
	warnCount := 0
	failCount := 0

	for _, result := range output.Results {
		var icon string
		switch result.Status {
		case "pass":
			icon = "✅"
			passCount++
		case "warn":
			icon = "⚠️"
			warnCount++
		case "fail":
			icon = "❌"
			failCount++
		default:
			icon = "❓"
		}

		fmt.Printf("%s %s: %s\n", icon, result.Check, result.Message)
		if result.Details != "" {
			fmt.Printf("   %s\n", result.Details)
		}
	}

	fmt.Printf("\n📊 Summary: %s\n", output.Summary)

	if !output.Success {
		fmt.Printf("\n💡 Some checks failed. Please address the failing checks before using postgres-db-fork.\n")
		os.Exit(1)
	} else if warnCount > 0 {
		fmt.Printf("\n⚠️  Some checks have warnings. The system should work but consider addressing these.\n")
	} else {
		fmt.Printf("\n🎉 All checks passed! postgres-db-fork is ready to use.\n")
	}

	return nil
}
