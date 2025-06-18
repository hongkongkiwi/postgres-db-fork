package fork

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"
)

// JobState represents the state of a transfer job
type JobState struct {
	JobID            string                 `json:"job_id"`
	StartTime        time.Time              `json:"start_time"`
	LastUpdated      time.Time              `json:"last_updated"`
	Phase            ProgressPhase          `json:"phase"`
	CompletedTables  map[string]bool        `json:"completed_tables"`
	FailedTables     map[string]string      `json:"failed_tables"`
	TableRowCounts   map[string]int64       `json:"table_row_counts"`
	SourceConfig     DatabaseConfigSnapshot `json:"source_config"`
	DestConfig       DatabaseConfigSnapshot `json:"dest_config"`
	TargetDatabase   string                 `json:"target_database"`
	SchemaCompleted  bool                   `json:"schema_completed"`
	IndexesCompleted bool                   `json:"indexes_completed"`
	Status           string                 `json:"status"` // "running", "paused", "completed", "failed"
	Error            string                 `json:"error,omitempty"`
}

// DatabaseConfigSnapshot stores essential database connection info for resumption
type DatabaseConfigSnapshot struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Database string `json:"database"`
	SSLMode  string `json:"sslmode"`
}

// ResumptionManager handles job state persistence and resumption
type ResumptionManager struct {
	stateDir  string
	jobID     string
	state     *JobState
	statePath string
}

// NewResumptionManager creates a new resumption manager
func NewResumptionManager(stateDir, jobID string) *ResumptionManager {
	if stateDir == "" {
		stateDir = filepath.Join(os.TempDir(), "postgres-db-fork", "jobs")
	}

	// Ensure state directory exists
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		fmt.Printf("Warning: Failed to create state directory: %v\n", err)
	}

	statePath := filepath.Join(stateDir, fmt.Sprintf("%s.json", jobID))

	return &ResumptionManager{
		stateDir:  stateDir,
		jobID:     jobID,
		statePath: statePath,
	}
}

// InitializeJob creates a new job state or loads existing one
func (rm *ResumptionManager) InitializeJob(sourceConfig, destConfig DatabaseConfigSnapshot, targetDB string, tables map[string]int64) (*JobState, bool, error) {
	// Check if job state already exists
	if existingState, err := rm.loadJobState(); err == nil && existingState != nil {
		// Job exists - check if it's resumable
		if existingState.Status == "running" || existingState.Status == "paused" {
			logrus.Infof("Found existing job state for job %s", rm.jobID)

			// Validate configuration compatibility
			if rm.isConfigCompatible(existingState, sourceConfig, destConfig, targetDB) {
				rm.state = existingState
				rm.state.Status = "running"
				rm.state.LastUpdated = time.Now()

				logrus.Infof("Resuming job from phase: %s", existingState.Phase)
				logrus.Infof("Completed tables: %d", len(existingState.CompletedTables))

				if err := rm.saveJobState(); err != nil {
					return nil, false, fmt.Errorf("failed to update job state: %w", err)
				}

				return rm.state, true, nil // true indicates resumption
			} else {
				logrus.Warnf("Existing job configuration is incompatible, starting fresh")
				if err := rm.cleanupJobState(); err != nil {
					logrus.Warnf("Failed to cleanup job state: %v", err)
				}
			}
		} else {
			logrus.Infof("Previous job was %s, starting fresh", existingState.Status)
			if err := rm.cleanupJobState(); err != nil {
				logrus.Warnf("Failed to cleanup job state: %v", err)
			}
		}
	}

	// Create new job state
	rm.state = &JobState{
		JobID:           rm.jobID,
		StartTime:       time.Now(),
		LastUpdated:     time.Now(),
		Phase:           PhaseInitializing,
		CompletedTables: make(map[string]bool),
		FailedTables:    make(map[string]string),
		TableRowCounts:  tables,
		SourceConfig:    sourceConfig,
		DestConfig:      destConfig,
		TargetDatabase:  targetDB,
		Status:          "running",
	}

	if err := rm.saveJobState(); err != nil {
		return nil, false, fmt.Errorf("failed to save initial job state: %w", err)
	}

	return rm.state, false, nil // false indicates new job
}

// UpdatePhase updates the current job phase
func (rm *ResumptionManager) UpdatePhase(phase ProgressPhase) error {
	if rm.state == nil {
		return fmt.Errorf("job state not initialized")
	}

	rm.state.Phase = phase
	rm.state.LastUpdated = time.Now()

	switch phase {
	case PhaseSchema:
		rm.state.SchemaCompleted = false
	case PhaseData:
		rm.state.SchemaCompleted = true
	case PhaseIndexes:
		rm.state.IndexesCompleted = false
	case PhaseCompleted:
		rm.state.Status = "completed"
		rm.state.IndexesCompleted = true
	case PhaseFailed:
		rm.state.Status = "failed"
	}

	return rm.saveJobState()
}

// MarkTableCompleted marks a table as successfully completed
func (rm *ResumptionManager) MarkTableCompleted(tableName string) error {
	if rm.state == nil {
		return fmt.Errorf("job state not initialized")
	}

	rm.state.CompletedTables[tableName] = true
	rm.state.LastUpdated = time.Now()

	// Remove from failed tables if it was there
	delete(rm.state.FailedTables, tableName)

	return rm.saveJobState()
}

// MarkTableFailed marks a table as failed with error details
func (rm *ResumptionManager) MarkTableFailed(tableName string, err error) error {
	if rm.state == nil {
		return fmt.Errorf("job state not initialized")
	}

	rm.state.FailedTables[tableName] = err.Error()
	rm.state.LastUpdated = time.Now()

	return rm.saveJobState()
}

// GetRemainingTables returns the list of tables that still need to be transferred
func (rm *ResumptionManager) GetRemainingTables() []string {
	if rm.state == nil {
		return nil
	}

	var remaining []string
	for tableName := range rm.state.TableRowCounts {
		if !rm.state.CompletedTables[tableName] {
			remaining = append(remaining, tableName)
		}
	}

	return remaining
}

// GetFailedTables returns the list of tables that failed to transfer
func (rm *ResumptionManager) GetFailedTables() map[string]string {
	if rm.state == nil {
		return nil
	}

	return rm.state.FailedTables
}

// IsTableCompleted checks if a table has been completed
func (rm *ResumptionManager) IsTableCompleted(tableName string) bool {
	if rm.state == nil {
		return false
	}

	return rm.state.CompletedTables[tableName]
}

// ShouldSkipSchema returns true if schema transfer should be skipped
func (rm *ResumptionManager) ShouldSkipSchema() bool {
	return rm.state != nil && rm.state.SchemaCompleted
}

// ShouldSkipIndexes returns true if index creation should be skipped
func (rm *ResumptionManager) ShouldSkipIndexes() bool {
	return rm.state != nil && rm.state.IndexesCompleted
}

// SetError sets an error state for the job
func (rm *ResumptionManager) SetError(err error) error {
	if rm.state == nil {
		return fmt.Errorf("job state not initialized")
	}

	rm.state.Status = "failed"
	rm.state.Error = err.Error()
	rm.state.LastUpdated = time.Now()

	return rm.saveJobState()
}

// PauseJob pauses the current job
func (rm *ResumptionManager) PauseJob() error {
	if rm.state == nil {
		return fmt.Errorf("job state not initialized")
	}

	rm.state.Status = "paused"
	rm.state.LastUpdated = time.Now()

	logrus.Infof("Job %s paused at phase %s", rm.jobID, rm.state.Phase)
	return rm.saveJobState()
}

// CompleteJob marks the job as completed and optionally cleans up state
func (rm *ResumptionManager) CompleteJob(cleanup bool) error {
	if rm.state == nil {
		return fmt.Errorf("job state not initialized")
	}

	rm.state.Status = "completed"
	rm.state.Phase = PhaseCompleted
	rm.state.LastUpdated = time.Now()

	if err := rm.saveJobState(); err != nil {
		return err
	}

	logrus.Infof("Job %s completed successfully", rm.jobID)

	if cleanup {
		return rm.cleanupJobState()
	}

	return nil
}

// GetJobState returns the current job state
func (rm *ResumptionManager) GetJobState() *JobState {
	return rm.state
}

// saveJobState persists the job state to disk
func (rm *ResumptionManager) saveJobState() error {
	if rm.state == nil {
		return fmt.Errorf("no job state to save")
	}

	data, err := json.MarshalIndent(rm.state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal job state: %w", err)
	}

	// Write atomically by writing to temp file and renaming
	tempPath := rm.statePath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write job state: %w", err)
	}

	if err := os.Rename(tempPath, rm.statePath); err != nil {
		return fmt.Errorf("failed to save job state: %w", err)
	}

	return nil
}

// loadJobState loads job state from disk
func (rm *ResumptionManager) loadJobState() (*JobState, error) {
	data, err := os.ReadFile(rm.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No existing state
		}
		return nil, fmt.Errorf("failed to read job state: %w", err)
	}

	var state JobState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal job state: %w", err)
	}

	return &state, nil
}

// cleanupJobState removes the job state file
func (rm *ResumptionManager) cleanupJobState() error {
	if err := os.Remove(rm.statePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to cleanup job state: %w", err)
	}

	logrus.Debugf("Cleaned up job state for job %s", rm.jobID)
	return nil
}

// isConfigCompatible checks if the stored configuration is compatible with current request
func (rm *ResumptionManager) isConfigCompatible(state *JobState, sourceConfig, destConfig DatabaseConfigSnapshot, targetDB string) bool {
	return state.SourceConfig.Host == sourceConfig.Host &&
		state.SourceConfig.Port == sourceConfig.Port &&
		state.SourceConfig.Database == sourceConfig.Database &&
		state.DestConfig.Host == destConfig.Host &&
		state.DestConfig.Port == destConfig.Port &&
		state.TargetDatabase == targetDB
}

// ListJobs lists all jobs in the state directory
func ListJobs(stateDir string) ([]JobState, error) {
	if stateDir == "" {
		stateDir = filepath.Join(os.TempDir(), "postgres-db-fork", "jobs")
	}

	files, err := os.ReadDir(stateDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []JobState{}, nil
		}
		return nil, fmt.Errorf("failed to read jobs directory: %w", err)
	}

	var jobs []JobState
	for _, file := range files {
		if filepath.Ext(file.Name()) != ".json" {
			continue
		}

		statePath := filepath.Join(stateDir, file.Name())
		data, err := os.ReadFile(statePath)
		if err != nil {
			logrus.Warnf("Failed to read job state %s: %v", file.Name(), err)
			continue
		}

		var state JobState
		if err := json.Unmarshal(data, &state); err != nil {
			logrus.Warnf("Failed to unmarshal job state %s: %v", file.Name(), err)
			continue
		}

		jobs = append(jobs, state)
	}

	return jobs, nil
}

// CleanupOldJobs removes job states older than the specified duration
func CleanupOldJobs(stateDir string, maxAge time.Duration) error {
	jobs, err := ListJobs(stateDir)
	if err != nil {
		return err
	}

	cutoff := time.Now().Add(-maxAge)
	var cleaned int

	for _, job := range jobs {
		if job.LastUpdated.Before(cutoff) && (job.Status == "completed" || job.Status == "failed") {
			statePath := filepath.Join(stateDir, fmt.Sprintf("%s.json", job.JobID))
			if err := os.Remove(statePath); err != nil {
				logrus.Warnf("Failed to remove old job state %s: %v", job.JobID, err)
			} else {
				cleaned++
				logrus.Debugf("Cleaned up old job state: %s", job.JobID)
			}
		}
	}

	if cleaned > 0 {
		logrus.Infof("Cleaned up %d old job states", cleaned)
	}

	return nil
}
