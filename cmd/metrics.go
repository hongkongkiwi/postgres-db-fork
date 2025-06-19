package cmd

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hongkongkiwi/postgres-db-fork/internal/fork"

	"github.com/spf13/cobra"
)

// MetricsReport represents transfer performance metrics
type MetricsReport struct {
	GeneratedAt      time.Time        `json:"generated_at"`
	Period           string           `json:"period"`
	Summary          MetricsSummary   `json:"summary"`
	JobMetrics       []JobMetric      `json:"job_metrics"`
	PerformanceStats PerformanceStats `json:"performance_stats"`
	Trends           TrendAnalysis    `json:"trends,omitempty"`
}

// MetricsSummary provides high-level statistics
type MetricsSummary struct {
	TotalJobs            int           `json:"total_jobs"`
	CompletedJobs        int           `json:"completed_jobs"`
	FailedJobs           int           `json:"failed_jobs"`
	RunningJobs          int           `json:"running_jobs"`
	SuccessRate          float64       `json:"success_rate"`
	AverageDuration      time.Duration `json:"average_duration"`
	TotalDataMoved       int64         `json:"total_data_moved_bytes"`
	TotalTablesProcessed int           `json:"total_tables_processed"`
}

// JobMetric represents metrics for a single job
type JobMetric struct {
	JobID           string        `json:"job_id"`
	Status          string        `json:"status"`
	StartTime       time.Time     `json:"start_time"`
	EndTime         *time.Time    `json:"end_time,omitempty"`
	Duration        time.Duration `json:"duration"`
	TablesProcessed int           `json:"tables_processed"`
	TablesFailed    int           `json:"tables_failed"`
	DataTransferred int64         `json:"data_transferred_bytes"`
	TransferRate    float64       `json:"transfer_rate_mbps"`
	ErrorCount      int           `json:"error_count"`
	Source          string        `json:"source"`
	Target          string        `json:"target"`
}

// PerformanceStats provides performance analytics
type PerformanceStats struct {
	FastestJob      *JobMetric    `json:"fastest_job,omitempty"`
	SlowestJob      *JobMetric    `json:"slowest_job,omitempty"`
	LargestTransfer *JobMetric    `json:"largest_transfer,omitempty"`
	MostTables      *JobMetric    `json:"most_tables,omitempty"`
	AverageSpeed    float64       `json:"average_speed_mbps"`
	MedianDuration  time.Duration `json:"median_duration"`
	P95Duration     time.Duration `json:"p95_duration"`
	ErrorRate       float64       `json:"error_rate"`
}

// TrendAnalysis provides trend information over time
type TrendAnalysis struct {
	DailyStats   []DailyStat `json:"daily_stats,omitempty"`
	SpeedTrend   string      `json:"speed_trend"`   // "improving", "declining", "stable"
	SuccessTrend string      `json:"success_trend"` // "improving", "declining", "stable"
}

// DailyStat represents daily aggregated statistics
type DailyStat struct {
	Date        string  `json:"date"`
	JobCount    int     `json:"job_count"`
	SuccessRate float64 `json:"success_rate"`
	AvgSpeed    float64 `json:"avg_speed_mbps"`
	DataMoved   int64   `json:"data_moved_bytes"`
}

// metricsCmd represents the metrics command
var metricsCmd = &cobra.Command{
	Use:   "metrics",
	Short: "Display transfer performance metrics and statistics",
	Long: `Analyze and display comprehensive metrics about database transfer operations.

This command provides detailed performance analytics including:
- Success/failure rates and job statistics
- Transfer speeds and data volume metrics
- Performance trends over time
- Fastest/slowest job identification
- Error rate analysis
- Operational insights for optimization

Examples:
  # Show metrics for last 7 days
  postgres-db-fork metrics

  # Show metrics for last 30 days
  postgres-db-fork metrics --period 30d

  # JSON output for automation
  postgres-db-fork metrics --output json

  # Show detailed job breakdowns
  postgres-db-fork metrics --detailed

  # Show only performance summary
  postgres-db-fork metrics --summary-only`,
	RunE: runMetrics,
}

func init() {
	rootCmd.AddCommand(metricsCmd)

	metricsCmd.Flags().String("output-format", "text", "Output format: text or json")
	metricsCmd.Flags().String("period", "7d", "Time period for metrics (1d, 7d, 30d, 90d)")
	metricsCmd.Flags().Bool("detailed", false, "Show detailed job breakdowns")
	metricsCmd.Flags().Bool("summary-only", false, "Show only summary statistics")
	metricsCmd.Flags().String("state-dir", "", "Job state directory")
	metricsCmd.Flags().Bool("trends", false, "Include trend analysis")
}

func runMetrics(cmd *cobra.Command, args []string) error {
	outputFormat, _ := cmd.Flags().GetString("output-format")
	period, _ := cmd.Flags().GetString("period")
	detailed, _ := cmd.Flags().GetBool("detailed")
	summaryOnly, _ := cmd.Flags().GetBool("summary-only")
	stateDir, _ := cmd.Flags().GetString("state-dir")
	includeTrends, _ := cmd.Flags().GetBool("trends")

	// Parse period
	duration, err := parsePeriod(period)
	if err != nil {
		return fmt.Errorf("invalid period: %w", err)
	}

	// Generate metrics report
	report, err := generateMetricsReport(stateDir, duration, includeTrends)
	if err != nil {
		return fmt.Errorf("failed to generate metrics: %w", err)
	}

	// Output results
	if outputFormat == "json" {
		return outputMetricsJSON(report)
	}

	return outputMetricsText(report, detailed, summaryOnly)
}

func parsePeriod(period string) (time.Duration, error) {
	switch period {
	case "1d":
		return 24 * time.Hour, nil
	case "7d":
		return 7 * 24 * time.Hour, nil
	case "30d":
		return 30 * 24 * time.Hour, nil
	case "90d":
		return 90 * 24 * time.Hour, nil
	default:
		return time.ParseDuration(period)
	}
}

func generateMetricsReport(stateDir string, period time.Duration, includeTrends bool) (*MetricsReport, error) {
	// Get all jobs
	jobs, err := fork.ListJobs(stateDir)
	if err != nil {
		return nil, fmt.Errorf("failed to list jobs: %w", err)
	}

	// Filter jobs by period
	cutoff := time.Now().Add(-period)
	var filteredJobs []fork.JobState
	for _, job := range jobs {
		if job.StartTime.After(cutoff) {
			filteredJobs = append(filteredJobs, job)
		}
	}

	// Convert to job metrics
	jobMetrics := make([]JobMetric, len(filteredJobs))
	for i, job := range filteredJobs {
		jobMetrics[i] = convertToJobMetric(&job)
	}

	// Generate report
	report := &MetricsReport{
		GeneratedAt:      time.Now(),
		Period:           period.String(),
		Summary:          calculateSummary(jobMetrics),
		JobMetrics:       jobMetrics,
		PerformanceStats: calculatePerformanceStats(jobMetrics),
	}

	if includeTrends {
		report.Trends = calculateTrends(jobMetrics)
	}

	return report, nil
}

func convertToJobMetric(job *fork.JobState) JobMetric {
	metric := JobMetric{
		JobID:           job.JobID,
		Status:          job.Status,
		StartTime:       job.StartTime,
		TablesProcessed: len(job.CompletedTables),
		TablesFailed:    len(job.FailedTables),
		Source:          fmt.Sprintf("%s@%s:%d/%s", job.SourceConfig.Username, job.SourceConfig.Host, job.SourceConfig.Port, job.SourceConfig.Database),
		Target:          job.TargetDatabase,
	}

	// Calculate duration
	if job.Status == "completed" || job.Status == "failed" || job.Status == "cancelled" {
		endTime := job.LastUpdated
		metric.EndTime = &endTime
		metric.Duration = endTime.Sub(job.StartTime)
	} else {
		metric.Duration = time.Since(job.StartTime)
	}

	// Estimate data transferred (simplified calculation)
	metric.DataTransferred = estimateDataTransferred(job)

	// Calculate transfer rate (MB/s)
	if metric.Duration > 0 {
		mbTransferred := float64(metric.DataTransferred) / (1024 * 1024)
		seconds := metric.Duration.Seconds()
		metric.TransferRate = mbTransferred / seconds
	}

	// Count errors
	metric.ErrorCount = len(job.FailedTables)
	if job.Error != "" {
		metric.ErrorCount++
	}

	return metric
}

func estimateDataTransferred(job *fork.JobState) int64 {
	// Simplified estimation based on table row counts
	// In a real implementation, this would track actual bytes transferred
	var totalRows int64
	for _, count := range job.TableRowCounts {
		totalRows += count
	}

	// Rough estimate: 100 bytes per row on average
	return totalRows * 100
}

func calculateSummary(metrics []JobMetric) MetricsSummary {
	summary := MetricsSummary{
		TotalJobs: len(metrics),
	}

	if len(metrics) == 0 {
		return summary
	}

	var totalDuration time.Duration
	var totalData int64
	var totalTables int

	for _, metric := range metrics {
		switch metric.Status {
		case "completed":
			summary.CompletedJobs++
		case "failed":
			summary.FailedJobs++
		case "running", "paused":
			summary.RunningJobs++
		}

		totalDuration += metric.Duration
		totalData += metric.DataTransferred
		totalTables += metric.TablesProcessed
	}

	// Calculate rates and averages
	if summary.TotalJobs > 0 {
		summary.SuccessRate = float64(summary.CompletedJobs) / float64(summary.TotalJobs) * 100
		summary.AverageDuration = totalDuration / time.Duration(summary.TotalJobs)
	}

	summary.TotalDataMoved = totalData
	summary.TotalTablesProcessed = totalTables

	return summary
}

func calculatePerformanceStats(metrics []JobMetric) PerformanceStats {
	stats := PerformanceStats{}

	if len(metrics) == 0 {
		return stats
	}

	// Find fastest, slowest, largest, and most tables
	for i := range metrics {
		metric := &metrics[i]

		if metric.Status != "completed" {
			continue // Only consider completed jobs for performance stats
		}

		if stats.FastestJob == nil || metric.Duration < stats.FastestJob.Duration {
			stats.FastestJob = metric
		}

		if stats.SlowestJob == nil || metric.Duration > stats.SlowestJob.Duration {
			stats.SlowestJob = metric
		}

		if stats.LargestTransfer == nil || metric.DataTransferred > stats.LargestTransfer.DataTransferred {
			stats.LargestTransfer = metric
		}

		if stats.MostTables == nil || metric.TablesProcessed > stats.MostTables.TablesProcessed {
			stats.MostTables = metric
		}
	}

	// Calculate average speed
	var totalSpeed float64
	var completedJobs int
	var durations []time.Duration
	var errorCount int

	for _, metric := range metrics {
		if metric.Status == "completed" {
			totalSpeed += metric.TransferRate
			completedJobs++
			durations = append(durations, metric.Duration)
		}
		if metric.ErrorCount > 0 {
			errorCount++
		}
	}

	if completedJobs > 0 {
		stats.AverageSpeed = totalSpeed / float64(completedJobs)
	}

	// Calculate percentiles
	if len(durations) > 0 {
		sort.Slice(durations, func(i, j int) bool {
			return durations[i] < durations[j]
		})

		medianIndex := len(durations) / 2
		stats.MedianDuration = durations[medianIndex]

		p95Index := int(float64(len(durations)) * 0.95)
		if p95Index >= len(durations) {
			p95Index = len(durations) - 1
		}
		stats.P95Duration = durations[p95Index]
	}

	// Calculate error rate
	if len(metrics) > 0 {
		stats.ErrorRate = float64(errorCount) / float64(len(metrics)) * 100
	}

	return stats
}

func calculateTrends(metrics []JobMetric) TrendAnalysis {
	trends := TrendAnalysis{}

	// Group metrics by day
	dailyStats := make(map[string]*DailyStat)

	for _, metric := range metrics {
		date := metric.StartTime.Format("2006-01-02")

		if dailyStats[date] == nil {
			dailyStats[date] = &DailyStat{
				Date: date,
			}
		}

		stat := dailyStats[date]
		stat.JobCount++
		stat.DataMoved += metric.DataTransferred

		if metric.Status == "completed" {
			stat.SuccessRate = (stat.SuccessRate*float64(stat.JobCount-1) + 100) / float64(stat.JobCount)
			stat.AvgSpeed = (stat.AvgSpeed*float64(stat.JobCount-1) + metric.TransferRate) / float64(stat.JobCount)
		} else {
			stat.SuccessRate = (stat.SuccessRate * float64(stat.JobCount-1)) / float64(stat.JobCount)
		}
	}

	// Convert to slice and sort
	for _, stat := range dailyStats {
		trends.DailyStats = append(trends.DailyStats, *stat)
	}

	sort.Slice(trends.DailyStats, func(i, j int) bool {
		return trends.DailyStats[i].Date < trends.DailyStats[j].Date
	})

	// Analyze trends (simplified)
	if len(trends.DailyStats) >= 2 {
		first := trends.DailyStats[0]
		last := trends.DailyStats[len(trends.DailyStats)-1]

		// Speed trend
		if last.AvgSpeed > first.AvgSpeed*1.1 {
			trends.SpeedTrend = "improving"
		} else if last.AvgSpeed < first.AvgSpeed*0.9 {
			trends.SpeedTrend = "declining"
		} else {
			trends.SpeedTrend = "stable"
		}

		// Success trend
		if last.SuccessRate > first.SuccessRate+5 {
			trends.SuccessTrend = "improving"
		} else if last.SuccessRate < first.SuccessRate-5 {
			trends.SuccessTrend = "declining"
		} else {
			trends.SuccessTrend = "stable"
		}
	}

	return trends
}

func outputMetricsJSON(report *MetricsReport) error {
	jsonOutput, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metrics: %w", err)
	}
	fmt.Println(string(jsonOutput))
	return nil
}

func outputMetricsText(report *MetricsReport, detailed, summaryOnly bool) error {
	fmt.Printf("📊 Transfer Metrics Report - %s\n", report.Period)
	fmt.Printf("Generated: %s\n", report.GeneratedAt.Format(time.RFC3339))
	fmt.Println()

	// Summary
	summary := report.Summary
	fmt.Println("📈 Summary:")
	fmt.Printf("  Total Jobs: %d\n", summary.TotalJobs)
	fmt.Printf("  Completed: %d (%.1f%%)\n", summary.CompletedJobs, summary.SuccessRate)
	fmt.Printf("  Failed: %d\n", summary.FailedJobs)
	fmt.Printf("  Running: %d\n", summary.RunningJobs)
	fmt.Printf("  Average Duration: %v\n", summary.AverageDuration.Round(time.Second))
	fmt.Printf("  Total Data Moved: %s\n", formatBytesMetrics(summary.TotalDataMoved))
	fmt.Printf("  Total Tables: %d\n", summary.TotalTablesProcessed)
	fmt.Println()

	if summaryOnly {
		return nil
	}

	// Performance Stats
	stats := report.PerformanceStats
	fmt.Println("⚡ Performance:")
	fmt.Printf("  Average Speed: %.2f MB/s\n", stats.AverageSpeed)
	fmt.Printf("  Median Duration: %v\n", stats.MedianDuration.Round(time.Second))
	fmt.Printf("  95th Percentile Duration: %v\n", stats.P95Duration.Round(time.Second))
	fmt.Printf("  Error Rate: %.1f%%\n", stats.ErrorRate)
	fmt.Println()

	// Record holders
	if stats.FastestJob != nil {
		fmt.Println("🏆 Records:")
		fmt.Printf("  Fastest Job: %s (%v)\n", stats.FastestJob.JobID, stats.FastestJob.Duration.Round(time.Second))
		if stats.SlowestJob != nil {
			fmt.Printf("  Slowest Job: %s (%v)\n", stats.SlowestJob.JobID, stats.SlowestJob.Duration.Round(time.Second))
		}
		if stats.LargestTransfer != nil {
			fmt.Printf("  Largest Transfer: %s (%s)\n", stats.LargestTransfer.JobID, formatBytesMetrics(stats.LargestTransfer.DataTransferred))
		}
		if stats.MostTables != nil {
			fmt.Printf("  Most Tables: %s (%d tables)\n", stats.MostTables.JobID, stats.MostTables.TablesProcessed)
		}
		fmt.Println()
	}

	// Trends
	if len(report.Trends.DailyStats) > 0 {
		fmt.Println("📊 Trends:")
		fmt.Printf("  Speed Trend: %s\n", getTrendIcon(report.Trends.SpeedTrend)+report.Trends.SpeedTrend)
		fmt.Printf("  Success Trend: %s\n", getTrendIcon(report.Trends.SuccessTrend)+report.Trends.SuccessTrend)
		fmt.Println()
	}

	// Detailed job metrics
	if detailed && len(report.JobMetrics) > 0 {
		fmt.Println("📋 Job Details:")
		fmt.Printf("%-20s %-10s %-8s %-10s %-8s %-8s %-10s\n",
			"JOB ID", "STATUS", "DURATION", "SPEED", "TABLES", "ERRORS", "DATA")
		fmt.Printf("%s\n", strings.Repeat("-", 80))

		for _, metric := range report.JobMetrics {
			fmt.Printf("%-20s %-10s %-8s %-10s %-8d %-8d %-10s\n",
				truncateString(metric.JobID, 20),
				metric.Status,
				metric.Duration.Round(time.Second).String(),
				fmt.Sprintf("%.1fMB/s", metric.TransferRate),
				metric.TablesProcessed,
				metric.ErrorCount,
				formatBytesMetrics(metric.DataTransferred))
		}
	}

	return nil
}

func getTrendIcon(trend string) string {
	switch trend {
	case "improving":
		return "📈 "
	case "declining":
		return "📉 "
	case "stable":
		return "➡️ "
	default:
		return "❓ "
	}
}

// formatBytesMetrics converts bytes to human readable format
func formatBytesMetrics(bytes int64) string {
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

// Note: truncateString function is also defined in other cmd files - should be moved to shared utility
