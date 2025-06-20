package fork

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/schollz/progressbar/v3"
	"github.com/sirupsen/logrus"
)

// ProgressPhase represents the current phase of the operation
type ProgressPhase string

const (
	PhaseInitializing ProgressPhase = "initializing"
	PhaseSchema       ProgressPhase = "schema"
	PhaseData         ProgressPhase = "data"
	PhaseIndexes      ProgressPhase = "indexes"
	PhaseConstraints  ProgressPhase = "constraints"
	PhaseFinalization ProgressPhase = "finalization"
	PhaseCompleted    ProgressPhase = "completed"
	PhaseFailed       ProgressPhase = "failed"
)

// TableProgress represents the progress of transferring a single table
type TableProgress struct {
	Name             string    `json:"name"`
	RowsTotal        int64     `json:"rows_total"`
	RowsCompleted    int64     `json:"rows_completed"`
	PercentComplete  float64   `json:"percent_complete"`
	BytesTransferred int64     `json:"bytes_transferred,omitempty"`
	StartTime        time.Time `json:"start_time"`
	Duration         string    `json:"duration"`
	Speed            string    `json:"speed,omitempty"`
	Status           string    `json:"status"` // "pending", "in_progress", "completed", "failed"
}

// ProgressReport represents a comprehensive progress report
type ProgressReport struct {
	Phase                  ProgressPhase   `json:"phase"`
	Overall                OverallProgress `json:"overall"`
	CurrentTable           *TableProgress  `json:"current_table,omitempty"`
	CompletedTables        []TableProgress `json:"completed_tables,omitempty"`
	EstimatedTimeRemaining string          `json:"estimated_time_remaining,omitempty"`
	TransferSpeed          string          `json:"transfer_speed,omitempty"`
	Message                string          `json:"message,omitempty"`
	Timestamp              time.Time       `json:"timestamp"`
}

// OverallProgress represents the overall operation progress
type OverallProgress struct {
	TablesTotal     int       `json:"tables_total"`
	TablesCompleted int       `json:"tables_completed"`
	RowsTotal       int64     `json:"rows_total"`
	RowsCompleted   int64     `json:"rows_completed"`
	PercentComplete float64   `json:"percent_complete"`
	Duration        string    `json:"duration"`
	StartTime       time.Time `json:"start_time"`
}

// ProgressMonitor provides CI/CD-relevant progress monitoring
type ProgressMonitor struct {
	startTime       time.Time
	phase           ProgressPhase
	tables          map[string]*TableProgress
	completedTables []TableProgress
	currentTable    *TableProgress
	overallProgress OverallProgress
	outputFormat    string
	quiet           bool
	progressFile    string
	progressBar     *progressbar.ProgressBar
	mu              sync.RWMutex
	ctx             context.Context
	cancel          context.CancelFunc
}

// NewProgressMonitor creates a new progress monitor
func NewProgressMonitor(outputFormat string, quiet bool, progressFile string) *ProgressMonitor {
	ctx, cancel := context.WithCancel(context.Background())

	monitor := &ProgressMonitor{
		startTime:    time.Now(),
		phase:        PhaseInitializing,
		tables:       make(map[string]*TableProgress),
		outputFormat: outputFormat,
		quiet:        quiet,
		progressFile: progressFile,
		ctx:          ctx,
		cancel:       cancel,
		overallProgress: OverallProgress{
			StartTime: time.Now(),
		},
	}

	// Start periodic progress reporting if not in quiet mode
	if !quiet {
		go monitor.periodicReport()
	}

	return monitor
}

// SetPhase updates the current operation phase
func (pm *ProgressMonitor) SetPhase(phase ProgressPhase, message string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.phase = phase

	if !pm.quiet {
		switch phase {
		case PhaseSchema:
			logrus.Info("📋 Transferring database schema...")
		case PhaseData:
			logrus.Info("📊 Starting data transfer...")
		case PhaseIndexes:
			logrus.Info("🔍 Creating indexes...")
		case PhaseConstraints:
			logrus.Info("🔗 Adding constraints...")
		case PhaseFinalization:
			logrus.Info("✨ Finalizing database...")
		case PhaseCompleted:
			logrus.Info("✅ Database fork completed successfully")
		case PhaseFailed:
			logrus.Error("❌ Database fork failed")
		}

		if message != "" {
			logrus.Info(message)
		}
	}

	pm.writeProgressFile()
}

// InitializeTables sets up the tables to be transferred
func (pm *ProgressMonitor) InitializeTables(tableInfo map[string]int64) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.overallProgress.TablesTotal = len(tableInfo)
	pm.overallProgress.RowsTotal = 0

	for tableName, rowCount := range tableInfo {
		pm.tables[tableName] = &TableProgress{
			Name:      tableName,
			RowsTotal: rowCount,
			Status:    "pending",
			StartTime: time.Now(),
		}
		pm.overallProgress.RowsTotal += rowCount
	}

	// Create overall progress bar if not in quiet mode
	if !pm.quiet && pm.overallProgress.TablesTotal > 0 {
		pm.progressBar = progressbar.NewOptions(pm.overallProgress.TablesTotal,
			progressbar.OptionSetDescription("Overall Progress"),
			progressbar.OptionSetWidth(60),
			progressbar.OptionShowCount(),
			progressbar.OptionShowIts(),
			progressbar.OptionSetTheme(progressbar.Theme{
				Saucer:        "█",
				SaucerHead:    "█",
				SaucerPadding: "░",
				BarStart:      "╢",
				BarEnd:        "╟",
			}),
			progressbar.OptionEnableColorCodes(true),
			progressbar.OptionSetPredictTime(true),
		)
	}

	if !pm.quiet {
		logrus.Infof("📋 Preparing to transfer %d tables with %d total rows",
			pm.overallProgress.TablesTotal, pm.overallProgress.RowsTotal)
	}

	pm.writeProgressFile()
}

// StartTable marks a table as starting transfer
func (pm *ProgressMonitor) StartTable(tableName string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if table, exists := pm.tables[tableName]; exists {
		table.Status = "in_progress"
		table.StartTime = time.Now()
		pm.currentTable = table

		if !pm.quiet {
			logrus.Infof("📄 Transferring table: %s (%d rows)", tableName, table.RowsTotal)
		}
	}

	pm.writeProgressFile()
}

// UpdateTableProgress updates the progress of the current table
func (pm *ProgressMonitor) UpdateTableProgress(tableName string, rowsCompleted int64, bytesTransferred int64) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if table, exists := pm.tables[tableName]; exists {
		table.RowsCompleted = rowsCompleted
		table.BytesTransferred = bytesTransferred

		if table.RowsTotal > 0 {
			table.PercentComplete = float64(rowsCompleted) / float64(table.RowsTotal) * 100
		}

		duration := time.Since(table.StartTime)
		table.Duration = duration.String()

		// Calculate transfer speed
		if duration.Seconds() > 1 {
			rowsPerSecond := float64(rowsCompleted) / duration.Seconds()
			table.Speed = fmt.Sprintf("%.1f rows/sec", rowsPerSecond)
		}

		// Update overall progress
		pm.updateOverallProgress()
	}
}

// CompleteTable marks a table as completed
func (pm *ProgressMonitor) CompleteTable(tableName string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if table, exists := pm.tables[tableName]; exists {
		table.Status = "completed"
		table.PercentComplete = 100
		table.Duration = time.Since(table.StartTime).String()

		pm.completedTables = append(pm.completedTables, *table)
		pm.overallProgress.TablesCompleted++
		pm.currentTable = nil

		// Update overall progress bar
		if pm.progressBar != nil {
			if err := pm.progressBar.Add(1); err != nil {
				logrus.Debugf("Failed to update overall progress bar: %v", err)
			}
		}

		if !pm.quiet {
			logrus.Infof("✅ Completed table: %s (%d rows in %s)",
				tableName, table.RowsCompleted, table.Duration)
		}

		pm.updateOverallProgress()
	}

	pm.writeProgressFile()
}

// FailTable marks a table as failed
func (pm *ProgressMonitor) FailTable(tableName string, err error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if table, exists := pm.tables[tableName]; exists {
		table.Status = "failed"
		table.Duration = time.Since(table.StartTime).String()

		pm.completedTables = append(pm.completedTables, *table)
		pm.currentTable = nil

		logrus.Errorf("❌ Failed to transfer table %s: %v", tableName, err)
	}

	pm.writeProgressFile()
}

// updateOverallProgress calculates overall progress statistics
func (pm *ProgressMonitor) updateOverallProgress() {
	pm.overallProgress.RowsCompleted = 0

	for _, table := range pm.tables {
		pm.overallProgress.RowsCompleted += table.RowsCompleted
	}

	if pm.overallProgress.RowsTotal > 0 {
		pm.overallProgress.PercentComplete = float64(pm.overallProgress.RowsCompleted) / float64(pm.overallProgress.RowsTotal) * 100
	}

	pm.overallProgress.Duration = time.Since(pm.overallProgress.StartTime).String()
}

// GetProgressReport returns the current progress report
func (pm *ProgressMonitor) GetProgressReport() ProgressReport {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	report := ProgressReport{
		Phase:           pm.phase,
		Overall:         pm.overallProgress,
		CurrentTable:    pm.currentTable,
		CompletedTables: pm.completedTables,
		Timestamp:       time.Now(),
	}

	// Calculate estimated time remaining
	if pm.overallProgress.PercentComplete > 0 && pm.overallProgress.PercentComplete < 100 {
		elapsed := time.Since(pm.overallProgress.StartTime)
		estimatedTotal := time.Duration(float64(elapsed) / (pm.overallProgress.PercentComplete / 100))
		remaining := estimatedTotal - elapsed

		if remaining > 0 {
			report.EstimatedTimeRemaining = remaining.Round(time.Second).String()
		}
	}

	// Calculate overall transfer speed
	elapsed := time.Since(pm.overallProgress.StartTime)
	if elapsed.Seconds() > 1 && pm.overallProgress.RowsCompleted > 0 {
		rowsPerSecond := float64(pm.overallProgress.RowsCompleted) / elapsed.Seconds()
		report.TransferSpeed = fmt.Sprintf("%.1f rows/sec", rowsPerSecond)
	}

	return report
}

// periodicReport provides regular progress updates
func (pm *ProgressMonitor) periodicReport() {
	ticker := time.NewTicker(30 * time.Second) // Report every 30 seconds
	defer ticker.Stop()

	for {
		select {
		case <-pm.ctx.Done():
			return
		case <-ticker.C:
			if pm.phase == PhaseData {
				pm.logProgressUpdate()
			}
		}
	}
}

// logProgressUpdate logs a progress update
func (pm *ProgressMonitor) logProgressUpdate() {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if pm.overallProgress.PercentComplete > 0 {
		message := fmt.Sprintf("🚀 Progress: %.1f%% (%d/%d tables, %d/%d rows)",
			pm.overallProgress.PercentComplete,
			pm.overallProgress.TablesCompleted,
			pm.overallProgress.TablesTotal,
			pm.overallProgress.RowsCompleted,
			pm.overallProgress.RowsTotal)

		elapsed := time.Since(pm.overallProgress.StartTime)
		if elapsed.Seconds() > 1 {
			estimatedTotal := time.Duration(float64(elapsed) / (pm.overallProgress.PercentComplete / 100))
			remaining := estimatedTotal - elapsed

			if remaining > 0 {
				message += fmt.Sprintf(" - ETA: %s", remaining.Round(time.Second))
			}
		}

		logrus.Info(message)
	}
}

// writeProgressFile writes progress to a file for CI/CD consumption
func (pm *ProgressMonitor) writeProgressFile() {
	if pm.progressFile == "" {
		return
	}

	report := pm.GetProgressReport()

	var output []byte
	var err error

	if pm.outputFormat == "json" {
		output, err = json.MarshalIndent(report, "", "  ")
	} else {
		// Simple text format for CI/CD scripts
		text := fmt.Sprintf("PHASE=%s\nPERCENT=%.1f\nTABLES_COMPLETED=%d\nTABLES_TOTAL=%d\nROWS_COMPLETED=%d\nROWS_TOTAL=%d\nDURATION=%s\n",
			report.Phase,
			report.Overall.PercentComplete,
			report.Overall.TablesCompleted,
			report.Overall.TablesTotal,
			report.Overall.RowsCompleted,
			report.Overall.RowsTotal,
			report.Overall.Duration)

		if report.EstimatedTimeRemaining != "" {
			text += fmt.Sprintf("ETA=%s\n", report.EstimatedTimeRemaining)
		}

		if report.CurrentTable != nil {
			text += fmt.Sprintf("CURRENT_TABLE=%s\nCURRENT_TABLE_PERCENT=%.1f\n",
				report.CurrentTable.Name,
				report.CurrentTable.PercentComplete)
		}

		output = []byte(text)
	}

	if err == nil {
		// Write atomically by writing to temp file and renaming
		tempFile := pm.progressFile + ".tmp"
		if err := os.WriteFile(tempFile, output, 0644); err == nil {
			if err := os.Rename(tempFile, pm.progressFile); err != nil {
				// If rename fails, remove temp file to avoid clutter
				if err := os.Remove(tempFile); err != nil {
					fmt.Printf("Warning: Failed to remove temp file: %v\n", err)
				}
			}
		}
	}
}

// Close stops the progress monitor
func (pm *ProgressMonitor) Close() {
	pm.cancel()

	// Finish progress bar
	if pm.progressBar != nil {
		if err := pm.progressBar.Finish(); err != nil {
			logrus.Debugf("Failed to finish overall progress bar: %v", err)
		}
	}

	// Write final progress file
	if pm.progressFile != "" {
		pm.writeProgressFile()
	}
}
