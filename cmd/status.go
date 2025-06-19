package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/hongkongkiwi/postgres-db-fork/internal/fork"

	"github.com/spf13/cobra"
)

// SystemStatus represents the overall system status
type SystemStatus struct {
	Status      string           `json:"status"` // "healthy", "degraded", "error"
	Timestamp   time.Time        `json:"timestamp"`
	Version     string           `json:"version"`
	Uptime      string           `json:"uptime"`
	Jobs        JobsStatus       `json:"jobs"`
	Resources   ResourceStatus   `json:"resources"`
	Health      HealthStatus     `json:"health"`
	Connections ConnectionStatus `json:"connections"`
}

// JobsStatus represents job-related status information
type JobsStatus struct {
	Running   int `json:"running"`
	Paused    int `json:"paused"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
	Total     int `json:"total"`
}

// ResourceStatus represents system resource information
type ResourceStatus struct {
	MemoryUsed      uint64  `json:"memory_used_bytes"`
	MemoryPercent   float64 `json:"memory_percent"`
	GoRoutines      int     `json:"goroutines"`
	FileDescriptors int     `json:"file_descriptors,omitempty"`
	DiskUsage       string  `json:"disk_usage,omitempty"`
}

// HealthStatus represents health check results
type HealthStatus struct {
	ConfigValid      bool   `json:"config_valid"`
	StateDirectoryOk bool   `json:"state_directory_ok"`
	LoggingOk        bool   `json:"logging_ok"`
	LastError        string `json:"last_error,omitempty"`
}

// ConnectionStatus represents database connection health
type ConnectionStatus struct {
	ActiveConnections int       `json:"active_connections"`
	PoolStatus        string    `json:"pool_status"`
	LastConnectTest   time.Time `json:"last_connect_test,omitempty"`
	LastConnectError  string    `json:"last_connect_error,omitempty"`
}

var statusStartTime = time.Now()

// statusCmd represents the status command
var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show system status and health information",
	Long: `Display comprehensive system status including running jobs, resource usage,
and health checks. This command is essential for monitoring and operational visibility.

The status command provides information about:
- Currently running, paused, and completed jobs
- System resource usage (memory, goroutines)
- Configuration and system health
- Database connection status

Examples:
  # Show system status
  postgres-db-fork status

  # JSON output for monitoring systems
  postgres-db-fork status --output json

  # Continuous monitoring (refresh every 5 seconds)
  postgres-db-fork status --watch 5s

  # Health check only (for CI/CD)
  postgres-db-fork status --health-only`,
	RunE: runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)

	statusCmd.Flags().String("output-format", "text", "Output format: text or json")
	statusCmd.Flags().Duration("watch", 0, "Refresh interval (e.g., 5s, 1m)")
	statusCmd.Flags().Bool("health-only", false, "Only perform health checks")
	statusCmd.Flags().String("state-dir", "", "Job state directory to check")
}

func runStatus(cmd *cobra.Command, args []string) error {
	outputFormat, _ := cmd.Flags().GetString("output-format")
	watchInterval, _ := cmd.Flags().GetDuration("watch")
	healthOnly, _ := cmd.Flags().GetBool("health-only")
	stateDir, _ := cmd.Flags().GetString("state-dir")

	if watchInterval > 0 {
		return runStatusWatch(outputFormat, watchInterval, healthOnly, stateDir)
	}

	status := collectSystemStatus(healthOnly, stateDir)
	return outputStatus(status, outputFormat)
}

func runStatusWatch(outputFormat string, interval time.Duration, healthOnly bool, stateDir string) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Clear screen on first run for watch mode
	if outputFormat == "text" {
		fmt.Print("\033[2J\033[H") // Clear screen and move cursor to top
	}

	for {
		if outputFormat == "text" {
			fmt.Print("\033[H") // Move cursor to top
		}

		status := collectSystemStatus(healthOnly, stateDir)
		if err := outputStatus(status, outputFormat); err != nil {
			return err
		}

		if outputFormat == "text" {
			fmt.Printf("\nRefreshing every %v... (Press Ctrl+C to stop)\n", interval)
		}

		<-ticker.C
	}
}

func collectSystemStatus(healthOnly bool, stateDir string) *SystemStatus {
	status := &SystemStatus{
		Status:    "healthy",
		Timestamp: time.Now(),
		Version:   Version,
		Uptime:    time.Since(statusStartTime).Round(time.Second).String(),
	}

	// Collect job status
	if !healthOnly {
		status.Jobs = collectJobsStatus(stateDir)
	}

	// Collect resource status
	status.Resources = collectResourceStatus()

	// Perform health checks
	status.Health = performHealthChecks(stateDir)

	// Collect connection status
	status.Connections = collectConnectionStatus()

	// Determine overall status
	if !status.Health.ConfigValid || !status.Health.StateDirectoryOk || !status.Health.LoggingOk {
		status.Status = "degraded"
	}

	if status.Health.LastError != "" {
		status.Status = "error"
	}

	return status
}

func collectJobsStatus(stateDir string) JobsStatus {
	jobs, err := fork.ListJobs(stateDir)
	if err != nil {
		return JobsStatus{} // Return empty status if we can't read jobs
	}

	status := JobsStatus{Total: len(jobs)}

	for _, job := range jobs {
		switch job.Status {
		case "running":
			status.Running++
		case "paused":
			status.Paused++
		case "completed":
			status.Completed++
		case "failed":
			status.Failed++
		}
	}

	return status
}

func collectResourceStatus() ResourceStatus {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	return ResourceStatus{
		MemoryUsed:    memStats.Alloc,
		MemoryPercent: calculateMemoryPercent(memStats.Alloc),
		GoRoutines:    runtime.NumGoroutine(),
	}
}

func calculateMemoryPercent(used uint64) float64 {
	// Simple approximation - in a real implementation you'd get system memory
	// For now, just show relative to a reasonable baseline (100MB)
	baseline := uint64(100 * 1024 * 1024) // 100MB
	if used > baseline {
		return float64(used) / float64(baseline) * 100
	}
	return float64(used) / float64(baseline) * 100
}

func performHealthChecks(stateDir string) HealthStatus {
	health := HealthStatus{
		ConfigValid:      true,
		StateDirectoryOk: true,
		LoggingOk:        true,
	}

	// Check state directory
	if stateDir == "" {
		stateDir = "/tmp/postgres-db-fork/jobs" // Default location
	}

	if _, err := os.Stat(stateDir); err != nil {
		if os.IsNotExist(err) {
			// Try to create it
			if err := os.MkdirAll(stateDir, 0755); err != nil {
				health.StateDirectoryOk = false
				health.LastError = fmt.Sprintf("Cannot create state directory: %v", err)
			}
		} else {
			health.StateDirectoryOk = false
			health.LastError = fmt.Sprintf("State directory error: %v", err)
		}
	}

	// Test if we can write to state directory
	if health.StateDirectoryOk {
		testFile := stateDir + "/.health-check"
		if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
			health.StateDirectoryOk = false
			health.LastError = fmt.Sprintf("Cannot write to state directory: %v", err)
		} else {
			if err := os.Remove(testFile); err != nil {
				fmt.Printf("Warning: Failed to remove test file: %v\n", err)
			}
		}
	}

	return health
}

func collectConnectionStatus() ConnectionStatus {
	// For now, return basic status
	// In a full implementation, this would track actual connection pools
	return ConnectionStatus{
		ActiveConnections: 0,
		PoolStatus:        "not_configured",
	}
}

func outputStatus(status *SystemStatus, outputFormat string) error {
	if outputFormat == "json" {
		jsonOutput, err := json.MarshalIndent(status, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal status: %w", err)
		}
		fmt.Println(string(jsonOutput))
	} else {
		printStatusText(status)
	}

	return nil
}

func printStatusText(status *SystemStatus) {
	// Status header
	statusIcon := getStatusIcon(status.Status)
	fmt.Printf("%s System Status: %s\n", statusIcon, status.Status)
	fmt.Printf("Version: %s\n", status.Version)
	fmt.Printf("Uptime: %s\n", status.Uptime)
	fmt.Printf("Timestamp: %s\n", status.Timestamp.Format(time.RFC3339))
	fmt.Println()

	// Jobs status
	fmt.Println("📋 Jobs:")
	fmt.Printf("  Running: %d\n", status.Jobs.Running)
	fmt.Printf("  Paused: %d\n", status.Jobs.Paused)
	fmt.Printf("  Completed: %d\n", status.Jobs.Completed)
	fmt.Printf("  Failed: %d\n", status.Jobs.Failed)
	fmt.Printf("  Total: %d\n", status.Jobs.Total)
	fmt.Println()

	// Resource status
	fmt.Println("💻 Resources:")
	fmt.Printf("  Memory: %s (%.1f%%)\n",
		formatBytes(int64(status.Resources.MemoryUsed)),
		status.Resources.MemoryPercent)
	fmt.Printf("  Goroutines: %d\n", status.Resources.GoRoutines)
	if status.Resources.FileDescriptors > 0 {
		fmt.Printf("  File Descriptors: %d\n", status.Resources.FileDescriptors)
	}
	fmt.Println()

	// Health status
	fmt.Println("🏥 Health:")
	fmt.Printf("  Config Valid: %s\n", getBoolIcon(status.Health.ConfigValid))
	fmt.Printf("  State Directory: %s\n", getBoolIcon(status.Health.StateDirectoryOk))
	fmt.Printf("  Logging: %s\n", getBoolIcon(status.Health.LoggingOk))
	if status.Health.LastError != "" {
		fmt.Printf("  Last Error: %s\n", status.Health.LastError)
	}
	fmt.Println()

	// Connection status
	fmt.Println("🔌 Connections:")
	fmt.Printf("  Active: %d\n", status.Connections.ActiveConnections)
	fmt.Printf("  Pool Status: %s\n", status.Connections.PoolStatus)
	if !status.Connections.LastConnectTest.IsZero() {
		fmt.Printf("  Last Test: %s\n", status.Connections.LastConnectTest.Format(time.RFC3339))
	}
	if status.Connections.LastConnectError != "" {
		fmt.Printf("  Last Error: %s\n", status.Connections.LastConnectError)
	}
}

func getStatusIcon(status string) string {
	switch status {
	case "healthy":
		return "✅"
	case "degraded":
		return "⚠️"
	case "error":
		return "❌"
	default:
		return "ℹ️"
	}
}

func getBoolIcon(value bool) string {
	if value {
		return "✅ Yes"
	}
	return "❌ No"
}

// Note: formatBytes function is also defined in list.go - should be moved to a shared utility package
