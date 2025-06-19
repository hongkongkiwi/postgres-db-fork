package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/hongkongkiwi/postgres-db-fork/internal/config"
	"github.com/hongkongkiwi/postgres-db-fork/internal/db"

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
			Check:   "state_directory",
			Status:  "fail",
			Message: "Cannot create job state directory",
			Details: err.Error(),
		})
	} else {
		// Test write permissions
		testFile := filepath.Join(stateDir, "test-write")
		if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
			results = append(results, DoctorResult{
				Check:   "state_directory",
				Status:  "fail",
				Message: "Job state directory not writable",
				Details: err.Error(),
			})
		} else {
			if err := os.Remove(testFile); err != nil {
				fmt.Printf("Warning: Failed to remove test file: %v\n", err)
			}
			results = append(results, DoctorResult{
				Check:   "state_directory",
				Status:  "pass",
				Message: "Job state directory accessible",
				Details: stateDir,
			})
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
	var results []DoctorResult

	// Try to create a basic configuration and test connectivity
	cfg := &config.ForkConfig{}
	cfg.LoadFromEnvironment()

	if cfg.Source.Host == "" {
		results = append(results, DoctorResult{
			Check:   "database_connectivity",
			Status:  "pass",
			Message: "No database configuration provided",
			Details: "Skipping database connectivity test - provide PGFORK_SOURCE_HOST to test",
		})
		return results
	}

	// Test source database connectivity
	sourceConn, err := db.NewConnection(&cfg.Source)
	if err != nil {
		results = append(results, DoctorResult{
			Check:   "source_database",
			Status:  "fail",
			Message: "Cannot connect to source database",
			Details: err.Error(),
		})
	} else {
		if err := sourceConn.Close(); err != nil {
			fmt.Printf("Warning: Failed to close source connection: %v\n", err)
		}
		results = append(results, DoctorResult{
			Check:   "source_database",
			Status:  "pass",
			Message: "Source database connection successful",
			Details: fmt.Sprintf("%s:%d/%s", cfg.Source.Host, cfg.Source.Port, cfg.Source.Database),
		})
	}

	// Test destination database connectivity (if different from source)
	if !cfg.IsSameServer() && cfg.Destination.Host != "" {
		destConn, err := db.NewConnection(&cfg.Destination)
		if err != nil {
			results = append(results, DoctorResult{
				Check:   "destination_database",
				Status:  "fail",
				Message: "Cannot connect to destination database",
				Details: err.Error(),
			})
		} else {
			if err := destConn.Close(); err != nil {
				fmt.Printf("Warning: Failed to close destination connection: %v\n", err)
			}
			results = append(results, DoctorResult{
				Check:   "destination_database",
				Status:  "pass",
				Message: "Destination database connection successful",
				Details: fmt.Sprintf("%s:%d", cfg.Destination.Host, cfg.Destination.Port),
			})
		}
	}

	return results
}

func checkDependencies() []DoctorResult {
	var results []DoctorResult

	// Check if postgresql client tools are available (if needed for cross-server operations)
	// This is more informational since we don't strictly require them
	results = append(results, DoctorResult{
		Check:   "postgresql_client",
		Status:  "pass",
		Message: "PostgreSQL client tools not required",
		Details: "postgres-db-fork uses native Go database drivers",
	})

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
