package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hongkongkiwi/postgres-db-fork/internal/fork"

	"github.com/spf13/cobra"
)

// jobsCmd represents the jobs command
var jobsCmd = &cobra.Command{
	Use:   "jobs",
	Short: "Manage transfer jobs",
	Long: `Manage transfer jobs including listing, canceling, pausing, and resuming operations.

This command provides comprehensive job management capabilities for monitoring and controlling
database transfer operations. Essential for operational visibility and control.

Available subcommands:
  list    - List all jobs with their status
  cancel  - Cancel a running job
  pause   - Pause a running job
  resume  - Resume a paused job
  show    - Show detailed information about a specific job

Examples:
  # List all jobs
  postgres-db-fork jobs list

  # List only running jobs
  postgres-db-fork jobs list --status running

  # Cancel a specific job
  postgres-db-fork jobs cancel job-abc123

  # Pause a running job
  postgres-db-fork jobs pause job-abc123

  # Resume a paused job
  postgres-db-fork jobs resume job-abc123`,
}

var jobsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List transfer jobs",
	Long: `List all transfer jobs with their current status and progress information.

Provides a comprehensive view of all jobs including running, paused, completed, and failed jobs.
Useful for monitoring and operational visibility.

Examples:
  # List all jobs
  postgres-db-fork jobs list

  # JSON output for automation
  postgres-db-fork jobs list --output json

  # Filter by status
  postgres-db-fork jobs list --status running

  # Show only recent jobs
  postgres-db-fork jobs list --limit 10`,
	RunE: runJobsList,
}

var jobsCancelCmd = &cobra.Command{
	Use:   "cancel <job-id>",
	Short: "Cancel a running job",
	Long: `Cancel a currently running transfer job.

This will attempt to gracefully stop the job and update its status to cancelled.
Use with caution as cancelling a job will require starting over.

Examples:
  # Cancel a specific job
  postgres-db-fork jobs cancel job-abc123`,
	Args: cobra.ExactArgs(1),
	RunE: runJobsCancel,
}

var jobsPauseCmd = &cobra.Command{
	Use:   "pause <job-id>",
	Short: "Pause a running job",
	Long: `Pause a currently running transfer job.

The job can be resumed later from where it left off. This is useful for
maintenance windows or resource management.

Examples:
  # Pause a specific job
  postgres-db-fork jobs pause job-abc123`,
	Args: cobra.ExactArgs(1),
	RunE: runJobsPause,
}

var jobsResumeCmd = &cobra.Command{
	Use:   "resume <job-id>",
	Short: "Resume a paused job",
	Long: `Resume a previously paused transfer job.

The job will continue from where it was paused, maintaining all progress
and state information.

Examples:
  # Resume a specific job
  postgres-db-fork jobs resume job-abc123`,
	Args: cobra.ExactArgs(1),
	RunE: runJobsResume,
}

var jobsShowCmd = &cobra.Command{
	Use:   "show <job-id>",
	Short: "Show detailed job information",
	Long: `Display detailed information about a specific job including progress,
configuration, and error details.

Examples:
  # Show job details
  postgres-db-fork jobs show job-abc123

  # JSON output for automation
  postgres-db-fork jobs show job-abc123 --output json`,
	Args: cobra.ExactArgs(1),
	RunE: runJobsShow,
}

func init() {
	rootCmd.AddCommand(jobsCmd)

	// Add subcommands
	jobsCmd.AddCommand(jobsListCmd)
	jobsCmd.AddCommand(jobsCancelCmd)
	jobsCmd.AddCommand(jobsPauseCmd)
	jobsCmd.AddCommand(jobsResumeCmd)
	jobsCmd.AddCommand(jobsShowCmd)

	// List command flags
	jobsListCmd.Flags().String("output", "text", "Output format: text or json")
	jobsListCmd.Flags().String("status", "", "Filter by status: running, paused, completed, failed")
	jobsListCmd.Flags().Int("limit", 0, "Limit number of jobs shown (0 = no limit)")
	jobsListCmd.Flags().String("state-dir", "", "Job state directory")

	// Show command flags
	jobsShowCmd.Flags().String("output", "text", "Output format: text or json")
	jobsShowCmd.Flags().String("state-dir", "", "Job state directory")

	// Global state-dir flag for other commands
	jobsCancelCmd.Flags().String("state-dir", "", "Job state directory")
	jobsPauseCmd.Flags().String("state-dir", "", "Job state directory")
	jobsResumeCmd.Flags().String("state-dir", "", "Job state directory")
}

func runJobsList(cmd *cobra.Command, args []string) error {
	outputFormat, _ := cmd.Flags().GetString("output")
	statusFilter, _ := cmd.Flags().GetString("status")
	limit, _ := cmd.Flags().GetInt("limit")
	stateDir, _ := cmd.Flags().GetString("state-dir")

	jobs, err := fork.ListJobs(stateDir)
	if err != nil {
		return fmt.Errorf("failed to list jobs: %w", err)
	}

	// Filter by status if specified
	if statusFilter != "" {
		var filtered []fork.JobState
		for _, job := range jobs {
			if job.Status == statusFilter {
				filtered = append(filtered, job)
			}
		}
		jobs = filtered
	}

	// Apply limit if specified
	if limit > 0 && len(jobs) > limit {
		jobs = jobs[:limit]
	}

	if outputFormat == "json" {
		return outputJobsJSON(jobs)
	}

	return outputJobsText(jobs)
}

func runJobsCancel(cmd *cobra.Command, args []string) error {
	jobID := args[0]
	stateDir, _ := cmd.Flags().GetString("state-dir")

	if err := cancelJob(stateDir, jobID); err != nil {
		return fmt.Errorf("failed to cancel job %s: %w", jobID, err)
	}

	fmt.Printf("Job %s has been cancelled\n", jobID)
	return nil
}

func runJobsPause(cmd *cobra.Command, args []string) error {
	jobID := args[0]
	stateDir, _ := cmd.Flags().GetString("state-dir")

	if err := pauseJob(stateDir, jobID); err != nil {
		return fmt.Errorf("failed to pause job %s: %w", jobID, err)
	}

	fmt.Printf("Job %s has been paused\n", jobID)
	return nil
}

func runJobsResume(cmd *cobra.Command, args []string) error {
	jobID := args[0]
	stateDir, _ := cmd.Flags().GetString("state-dir")

	if err := resumeJob(stateDir, jobID); err != nil {
		return fmt.Errorf("failed to resume job %s: %w", jobID, err)
	}

	fmt.Printf("Job %s has been resumed\n", jobID)
	return nil
}

func runJobsShow(cmd *cobra.Command, args []string) error {
	jobID := args[0]
	outputFormat, _ := cmd.Flags().GetString("output")
	stateDir, _ := cmd.Flags().GetString("state-dir")

	job, err := getJobByID(stateDir, jobID)
	if err != nil {
		return fmt.Errorf("failed to get job %s: %w", jobID, err)
	}

	if outputFormat == "json" {
		jsonOutput, err := json.MarshalIndent(job, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal job: %w", err)
		}
		fmt.Println(string(jsonOutput))
	} else {
		printJobDetails(job)
	}

	return nil
}

func outputJobsJSON(jobs []fork.JobState) error {
	jsonOutput, err := json.MarshalIndent(jobs, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal jobs: %w", err)
	}
	fmt.Println(string(jsonOutput))
	return nil
}

func outputJobsText(jobs []fork.JobState) error {
	if len(jobs) == 0 {
		fmt.Println("No jobs found")
		return nil
	}

	// Print header
	fmt.Printf("%-20s %-10s %-8s %-19s %-19s %-50s\n",
		"JOB ID", "STATUS", "PROGRESS", "STARTED", "LAST UPDATED", "SOURCE → TARGET")
	fmt.Printf("%s\n", strings.Repeat("-", 120))

	for _, job := range jobs {
		progress := calculateProgress(&job)
		source := fmt.Sprintf("%s@%s:%d/%s",
			job.SourceConfig.Username,
			job.SourceConfig.Host,
			job.SourceConfig.Port,
			job.SourceConfig.Database)
		target := fmt.Sprintf("%s → %s", source, job.TargetDatabase)

		fmt.Printf("%-20s %-10s %6.1f%% %-19s %-19s %-50s\n",
			truncateString(job.JobID, 20),
			getJobStatusIcon(job.Status)+job.Status,
			progress,
			job.StartTime.Format("2006-01-02 15:04:05"),
			job.LastUpdated.Format("2006-01-02 15:04:05"),
			truncateString(target, 50))
	}

	fmt.Printf("\nTotal: %d jobs\n", len(jobs))
	return nil
}

func printJobDetails(job *fork.JobState) {
	fmt.Printf("Job ID: %s\n", job.JobID)
	fmt.Printf("Status: %s %s\n", getJobStatusIcon(job.Status), job.Status)
	fmt.Printf("Phase: %s\n", job.Phase)
	fmt.Printf("Progress: %.1f%%\n", calculateProgress(job))
	fmt.Printf("Started: %s\n", job.StartTime.Format(time.RFC3339))
	fmt.Printf("Last Updated: %s\n", job.LastUpdated.Format(time.RFC3339))
	fmt.Println()

	fmt.Printf("Source: %s@%s:%d/%s\n",
		job.SourceConfig.Username,
		job.SourceConfig.Host,
		job.SourceConfig.Port,
		job.SourceConfig.Database)
	fmt.Printf("Target: %s\n", job.TargetDatabase)
	fmt.Println()

	fmt.Printf("Schema Completed: %s\n", getJobBoolIcon(job.SchemaCompleted))
	fmt.Printf("Indexes Completed: %s\n", getJobBoolIcon(job.IndexesCompleted))
	fmt.Printf("Completed Tables: %d\n", len(job.CompletedTables))
	fmt.Printf("Failed Tables: %d\n", len(job.FailedTables))
	fmt.Printf("Total Tables: %d\n", len(job.TableRowCounts))
	fmt.Println()

	if len(job.FailedTables) > 0 {
		fmt.Println("Failed Tables:")
		for table, err := range job.FailedTables {
			fmt.Printf("  %s: %s\n", table, err)
		}
		fmt.Println()
	}

	if job.Error != "" {
		fmt.Printf("Error: %s\n", job.Error)
	}
}

func calculateProgress(job *fork.JobState) float64 {
	totalTables := len(job.TableRowCounts)
	if totalTables == 0 {
		return 0.0
	}

	completedTables := len(job.CompletedTables)
	return float64(completedTables) / float64(totalTables) * 100.0
}

func getJobByID(stateDir, jobID string) (*fork.JobState, error) {
	jobs, err := fork.ListJobs(stateDir)
	if err != nil {
		return nil, err
	}

	for _, job := range jobs {
		if job.JobID == jobID {
			return &job, nil
		}
	}

	return nil, fmt.Errorf("job not found: %s", jobID)
}

func cancelJob(stateDir, jobID string) error {
	return updateJobStatus(stateDir, jobID, "cancelled")
}

func pauseJob(stateDir, jobID string) error {
	job, err := getJobByID(stateDir, jobID)
	if err != nil {
		return err
	}

	if job.Status != "running" {
		return fmt.Errorf("job is not running (current status: %s)", job.Status)
	}

	return updateJobStatus(stateDir, jobID, "paused")
}

func resumeJob(stateDir, jobID string) error {
	job, err := getJobByID(stateDir, jobID)
	if err != nil {
		return err
	}

	if job.Status != "paused" {
		return fmt.Errorf("job is not paused (current status: %s)", job.Status)
	}

	return updateJobStatus(stateDir, jobID, "running")
}

func updateJobStatus(stateDir, jobID, newStatus string) error {
	// Use ResumptionManager to update the job status
	rm := fork.NewResumptionManager(stateDir, jobID)

	// Load existing state
	existingState := rm.GetJobState()
	if existingState == nil {
		return fmt.Errorf("job state not found: %s", jobID)
	}

	// Update status based on the new status
	switch newStatus {
	case "cancelled", "failed":
		return rm.SetError(fmt.Errorf("job %s", newStatus))
	case "paused":
		return rm.PauseJob()
	case "completed":
		return rm.CompleteJob(false)
	default:
		return fmt.Errorf("unsupported status update: %s", newStatus)
	}
}

func getJobStatusIcon(status string) string {
	switch status {
	case "running":
		return "🏃 "
	case "paused":
		return "⏸️ "
	case "completed":
		return "✅ "
	case "failed":
		return "❌ "
	case "cancelled":
		return "🛑 "
	default:
		return "❓ "
	}
}

func getJobBoolIcon(value bool) string {
	if value {
		return "✅ Yes"
	}
	return "❌ No"
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
