package fork

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/hongkongkiwi/postgres-db-fork/internal/config"
	"github.com/hongkongkiwi/postgres-db-fork/internal/db"
	"github.com/hongkongkiwi/postgres-db-fork/internal/logging"

	"github.com/oklog/run"
	"github.com/schollz/progressbar/v3"
	"github.com/sirupsen/logrus"
)

// HookRunner executes custom user-defined hooks
type HookRunner struct {
	logger *logging.Logger
}

// NewHookRunner creates a new hook runner
func NewHookRunner(logger *logging.Logger) *HookRunner {
	return &HookRunner{logger: logger}
}

// Run executes a list of shell commands
func (hr *HookRunner) Run(hooks []string, stage string) error {
	if len(hooks) == 0 {
		return nil
	}

	hr.logger.Infof("Running %s hooks...", stage)
	for _, command := range hooks {
		if command == "" {
			continue
		}

		hr.logger.Debugf("Executing hook: %s", command)

		// Using "sh -c" to allow for complex commands with pipes and redirection
		cmd := exec.Command("sh", "-c", command)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			hr.logger.Errorf("Hook command failed: %s", command)
			return fmt.Errorf("hook command '%s' failed: %w", command, err)
		}
	}

	hr.logger.Infof("Finished %s hooks successfully", stage)
	return nil
}

// Forker handles database forking operations with enhanced robustness
type Forker struct {
	config       *config.ForkConfig
	logger       *logging.Logger
	progressBar  *progressbar.ProgressBar
	runGroup     *run.Group
	shutdownChan chan os.Signal
	metrics      *MetricsCollector
}

// MetricsCollector handles metrics collection and export
type MetricsCollector struct {
	startTime        time.Time
	transferredBytes int64
	transferredRows  int64
	errorCount       int64
	tablesProcessed  int64
	metricsFile      string
	mu               sync.RWMutex
}

// NewForker creates a new database forker with enhanced features
func NewForker(cfg *config.ForkConfig) *Forker {
	logger, err := logging.NewLogger(&logging.Config{
		Level:  "info",
		Format: "text",
	})
	if err != nil {
		// Create a basic logger as fallback
		logrus.Warnf("Failed to initialize logger, falling back to default: %v", err)
		logger = &logging.Logger{Logger: logrus.New()}
	}

	// Create metrics collector
	metrics := &MetricsCollector{
		startTime:   time.Now(),
		metricsFile: "/tmp/postgres-fork-metrics.txt",
	}

	// Create run group for graceful shutdown
	runGroup := &run.Group{}

	// Create shutdown channel
	shutdownChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, syscall.SIGINT, syscall.SIGTERM)

	forker := &Forker{
		config:       cfg,
		logger:       logger,
		runGroup:     runGroup,
		shutdownChan: shutdownChan,
		metrics:      metrics,
	}

	// Add signal handler to run group
	runGroup.Add(func() error {
		sig, ok := <-shutdownChan
		if !ok {
			return nil // Clean shutdown
		}
		logger.Infof("Received signal %v, initiating graceful shutdown...", sig)
		return fmt.Errorf("shutdown signal received: %v", sig)
	}, func(error) {
		close(shutdownChan)
	})

	return forker
}

// Fork executes the database fork operation with enhanced robustness
func (f *Forker) Fork(ctx context.Context) error {
	// Ensure logger is initialized
	if f.logger == nil || f.logger.Logger == nil {
		logrus.Warn("Logger not properly initialized, using default")
	} else {
		f.logger.LogAudit("fork_started", map[string]interface{}{
			"source_db": f.config.Source.Database,
			"target_db": f.config.TargetDatabase,
		})
	}

	// Start metrics collection
	f.metrics.startTime = time.Now()

	// Create context that can be cancelled by signals
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Add the main fork operation to run group
	f.runGroup.Add(func() error {
		return f.executeFork(ctx)
	}, func(error) {
		cancel()
	})

	// Execute with graceful shutdown support
	if err := f.runGroup.Run(); err != nil {
		if err.Error() == "shutdown signal received: interrupt" ||
			err.Error() == "shutdown signal received: terminated" {
			f.logger.Info("Fork operation was gracefully interrupted")
			f.saveMetrics("interrupted")
			return fmt.Errorf("operation interrupted by user")
		}
		f.metrics.errorCount++
		f.saveMetrics("failed")
		return err
	}

	f.saveMetrics("completed")
	return nil
}

// executeFork performs the actual fork operation
func (f *Forker) executeFork(ctx context.Context) error {
	f.logger.Info("Starting database fork operation...")
	f.logger.Infof("Source: %s:%d/%s", f.config.Source.Host, f.config.Source.Port, f.config.Source.Database)
	f.logger.Infof("Target: %s:%d/%s", f.config.Destination.Host, f.config.Destination.Port, f.config.TargetDatabase)

	hookRunner := NewHookRunner(f.logger)

	// Run PreFork hooks
	if err := hookRunner.Run(f.config.Hooks.PreFork, "PreFork"); err != nil {
		return fmt.Errorf("pre-fork hooks failed: %w", err)
	}

	var forkErr error
	if f.config.IsSameServer() {
		f.logger.Info("Detected same-server fork, using efficient template-based cloning")
		forkErr = f.forkSameServer(ctx)
	} else {
		f.logger.Info("Detected cross-server fork, using dump and restore")
		forkErr = f.forkCrossServer(ctx)
	}

	// Run PostFork or OnError hooks
	if forkErr != nil {
		f.logger.Errorf("Fork operation failed: %v", forkErr)
		if err := hookRunner.Run(f.config.Hooks.OnError, "OnError"); err != nil {
			return fmt.Errorf("on-error hooks also failed: %w (original error: %v)", err, forkErr)
		}
		return forkErr
	}

	if err := hookRunner.Run(f.config.Hooks.PostFork, "PostFork"); err != nil {
		return fmt.Errorf("post-fork hooks failed: %w", err)
	}

	f.logger.Info("✅ Database fork completed successfully!")
	return nil
}

// forkSameServer handles same-server forking using PostgreSQL templates
func (f *Forker) forkSameServer(ctx context.Context) error {
	// If we need selective features (schema-only, table filtering), use cross-server method
	// even on same server, as template-based cloning copies everything
	if f.config.SchemaOnly || len(f.config.IncludeTables) > 0 || len(f.config.ExcludeTables) > 0 {
		f.logger.Info("Schema-only or table filtering requested, using selective transfer method")
		return f.forkCrossServer(ctx)
	}

	// Connect to the destination server (using postgres database for admin operations)
	adminConfig := f.config.Destination
	adminConfig.Database = "postgres"

	conn, err := db.NewConnection(&adminConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to destination server: %w", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			f.logger.Warnf("Warning: Connection cleanup failed: %v", err)
		}
	}()

	// Check if source database exists
	exists, err := conn.DatabaseExists(f.config.Source.Database)
	if err != nil {
		return fmt.Errorf("failed to check source database: %w", err)
	}
	if !exists {
		return fmt.Errorf("source database '%s' does not exist", f.config.Source.Database)
	}

	// Check if target database exists
	targetExists, err := conn.DatabaseExists(f.config.TargetDatabase)
	if err != nil {
		return fmt.Errorf("failed to check target database: %w", err)
	}

	if targetExists {
		if f.config.DropIfExists {
			if err := conn.DropDatabase(f.config.TargetDatabase); err != nil {
				return fmt.Errorf("failed to drop existing target database: %w", err)
			}
		} else {
			return fmt.Errorf("target database '%s' already exists (use --drop-if-exists to overwrite)", f.config.TargetDatabase)
		}
	}

	// Get source database size for progress reporting
	sourceSize, err := conn.GetDatabaseSize(f.config.Source.Database)
	if err != nil {
		f.logger.Warnf("Could not get source database size: %v", err)
	} else {
		f.logger.Infof("Source database size: %s", formatBytes(sourceSize))
	}

	// Create the target database using the source as template
	if err := conn.CreateDatabase(f.config.TargetDatabase, f.config.Source.Database, false); err != nil {
		return fmt.Errorf("failed to create target database: %w", err)
	}

	// Verify the fork was successful
	targetSize, err := conn.GetDatabaseSize(f.config.TargetDatabase)
	if err != nil {
		f.logger.Warnf("Could not verify target database size: %v", err)
	} else {
		f.logger.Infof("Target database size: %s", formatBytes(targetSize))
	}

	return nil
}

// forkCrossServer handles cross-server forking using dump and restore
func (f *Forker) forkCrossServer(ctx context.Context) error {
	// This is a more complex operation that would involve:
	// 1. Creating a custom dump and restore mechanism
	// 2. Or using pg_dump and pg_restore utilities
	// 3. Or streaming data table by table

	// Connect to source database
	sourceConn, err := db.NewConnection(&f.config.Source)
	if err != nil {
		return fmt.Errorf("failed to connect to source database: %w", err)
	}
	defer func() {
		if err := sourceConn.Close(); err != nil {
			f.logger.Warnf("Warning: Source connection cleanup failed: %v", err)
		}
	}()

	// Connect to destination server for admin operations
	adminConfig := f.config.Destination
	adminConfig.Database = "postgres"

	destAdminConn, err := db.NewConnection(&adminConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to destination server: %w", err)
	}
	defer func() {
		if err := destAdminConn.Close(); err != nil {
			f.logger.Warnf("Warning: Destination admin connection cleanup failed: %v", err)
		}
	}()

	// Check if target database exists
	targetExists, err := destAdminConn.DatabaseExists(f.config.TargetDatabase)
	if err != nil {
		return fmt.Errorf("failed to check target database: %w", err)
	}

	if targetExists {
		if f.config.DropIfExists {
			if err := destAdminConn.DropDatabase(f.config.TargetDatabase); err != nil {
				return fmt.Errorf("failed to drop existing target database: %w", err)
			}
		} else {
			return fmt.Errorf("target database '%s' already exists (use --drop-if-exists to overwrite)", f.config.TargetDatabase)
		}
	}

	// Create empty target database
	if err := destAdminConn.CreateDatabase(f.config.TargetDatabase, "template1", false); err != nil {
		return fmt.Errorf("failed to create target database: %w", err)
	}

	// Connect to the target database
	targetConfig := f.config.Destination
	targetConfig.Database = f.config.TargetDatabase

	destConn, err := db.NewConnection(&targetConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to target database: %w", err)
	}
	defer func() {
		if err := destConn.Close(); err != nil {
			f.logger.Warnf("Warning: Destination connection cleanup failed: %v", err)
		}
	}()

	// Get source database size for progress reporting
	sourceSize, err := sourceConn.GetDatabaseSize(f.config.Source.Database)
	if err != nil {
		f.logger.Warnf("Could not get source database size: %v", err)
	} else {
		f.logger.Infof("Source database size: %s", formatBytes(sourceSize))
		f.progressBar = progressbar.NewOptions64(
			sourceSize,
			progressbar.OptionSetDescription("Transferring data..."),
			progressbar.OptionSetWriter(os.Stderr),
			progressbar.OptionShowBytes(true),
			progressbar.OptionSetWidth(40),
			progressbar.OptionThrottle(100*time.Millisecond),
			progressbar.OptionShowCount(),
			progressbar.OptionOnCompletion(func() {
				fmt.Fprintln(os.Stderr)
			}),
			progressbar.OptionSpinnerType(14),
		)
	}

	// Create a data transfer manager
	transferManager := NewDataTransferManager(sourceConn, destConn, &f.config.Source, &f.config.Destination, f.config, f.logger)

	// Set metrics updater
	transferManager.SetMetricsUpdater(f)

	// Execute the data transfer
	if err := transferManager.Transfer(ctx); err != nil {
		return fmt.Errorf("failed to transfer data: %w", err)
	}

	return nil
}

// formatBytes converts bytes to human readable format
func formatBytes(bytes int64) string {
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

// saveMetrics saves metrics to file
func (f *Forker) saveMetrics(status string) {
	f.metrics.mu.Lock()
	defer f.metrics.mu.Unlock()

	duration := time.Since(f.metrics.startTime)

	metrics := fmt.Sprintf(`# PostgreSQL Fork Metrics
# Generated at: %s
# Status: %s
# Duration: %v
postgres_fork_duration_seconds %f
postgres_fork_transferred_bytes %d
postgres_fork_transferred_rows %d
postgres_fork_error_count %d
postgres_fork_tables_processed %d
postgres_fork_transfer_rate_bytes_per_second %f
postgres_fork_transfer_rate_rows_per_second %f
postgres_fork_status{status="%s"} 1
`,
		time.Now().Format(time.RFC3339),
		status,
		duration,
		duration.Seconds(),
		f.metrics.transferredBytes,
		f.metrics.transferredRows,
		f.metrics.errorCount,
		f.metrics.tablesProcessed,
		float64(f.metrics.transferredBytes)/duration.Seconds(),
		float64(f.metrics.transferredRows)/duration.Seconds(),
		status,
	)

	if err := os.WriteFile(f.metrics.metricsFile, []byte(metrics), 0644); err != nil {
		f.logger.Warnf("Failed to write metrics file: %v", err)
	} else {
		f.logger.Debugf("Metrics saved to %s", f.metrics.metricsFile)
	}
}

// updateMetrics updates transfer metrics
func (f *Forker) updateMetrics(bytesTransferred, rowsTransferred int64) {
	f.metrics.mu.Lock()
	defer f.metrics.mu.Unlock()

	f.metrics.transferredBytes += bytesTransferred
	f.metrics.transferredRows += rowsTransferred
	if f.progressBar != nil {
		if err := f.progressBar.Add64(bytesTransferred); err != nil {
			f.logger.Warnf("Failed to update progress bar: %v", err)
		}
	}
}

// incrementTableCount increments the processed table count
func (f *Forker) incrementTableCount() {
	f.metrics.mu.Lock()
	defer f.metrics.mu.Unlock()

	f.metrics.tablesProcessed++
}
